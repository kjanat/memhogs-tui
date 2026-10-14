package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"memhogs.kjanat.dev/internal/collector"
)

func press(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: string(code)} }

func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func testApp(name string, rss int64, pids ...int) collector.AppStat {
	a := collector.AppStat{Name: name, RSSKB: rss, ProcCount: len(pids), SwapKnown: true}
	for _, pid := range pids {
		a.Children = append(a.Children, collector.ProcDetail{PID: pid, StartTime: 1, Cmdline: fmt.Sprintf("%s --worker %d", name, pid), RSSKB: rss / int64(len(pids)), SwapKnown: true})
	}
	return a
}

func testModel(apps ...collector.AppStat) Model {
	m := New(Config{})
	m.width, m.height = 80, 10
	m.snap = &collector.Snapshot{Apps: apps}
	m.collecting = false
	return m
}

func TestSelectionFollowsGroupAndChild(t *testing.T) {
	m := testModel(testApp("a", 4096, 1), testApp("b", 2048, 2, 3))
	m.cursor = 1
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{testApp("a", 2048, 1), testApp("b", 4096, 2, 3)}}})
	if m.cursor != 0 {
		t.Fatal("selected group did not follow RSS reorder")
	}
	m.expanded["b"] = true
	m.cursor = 2 // PID 3
	b := testApp("b", 1024, 3, 2)
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{testApp("a", 4096, 1), b}}})
	rows := m.visibleRows(m.sortedApps())
	if rows[m.cursor].Child.PID != 3 {
		t.Fatal("child selection changed on reorder")
	}
	m, _ = updateModel(m, press('s'))
	rows = m.visibleRows(m.sortedApps())
	if rows[m.cursor].Child.PID != 3 {
		t.Fatal("child selection changed on sort")
	}
	b.Children = slices.Clone(b.Children)
	b.Children[0].StartTime++ // same PID, different process
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{b}}})
	rows = m.visibleRows(m.sortedApps())
	if rows[m.cursor].Kind != RowGroup {
		t.Fatal("PID reuse must fall back to parent")
	}
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{}})
	if m.cursor != 0 || m.offset != 0 {
		t.Fatal("empty snapshot must reset navigation")
	}
}

func TestScrollingAndAllChildren(t *testing.T) {
	pids := make([]int, 40)
	for i := range pids {
		pids[i] = i + 1
	}
	m := testModel(testApp("workers", 40960, pids...))
	m.expanded["workers"] = true
	if len(m.visibleRows(m.sortedApps())) != 41 {
		t.Fatal("children truncated")
	}
	for range 40 {
		m, _ = updateModel(m, press('j'))
	}
	require(t, m.cursor == 40 && m.offset == 35, "cursor=%d offset=%d", m.cursor, m.offset)
	table := m.viewTable(m.visibleRows(m.sortedApps()), 80, 9)
	require(t, strings.Contains(table, "--worker 40") && !strings.Contains(table, "--worker 1 "), "selected process not visible:\n%s", table)
	m, _ = updateModel(m, tea.WindowSizeMsg{Width: 80, Height: 6})
	if m.offset != 39 {
		t.Fatalf("resize failed: offset=%d", m.offset)
	}
	m, _ = updateModel(m, press('h'))
	require(t, m.cursor == 0 && m.offset == 0, "collapse failed to show group")
	for range 5 {
		m, _ = updateModel(m, press('k'))
	}
	if m.cursor != 0 {
		t.Fatal("cursor became negative")
	}
}

func TestFrozenKillTarget(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprint(child), func(t *testing.T) {
			m := testModel(testApp("node workers", 8192, 10, 11), testApp("other", 4096, 20))
			if child {
				m.expanded["node workers"] = true
				m.cursor = 2
			}
			var signaled []int
			m.signal = func(p collector.ProcDetail, force bool) error {
				signaled = append(signaled, p.PID)
				if !force {
					t.Error("force flag lost")
				}
				return nil
			}
			m, _ = updateModel(m, press('X'))
			before := m.viewKillModal()
			// Reorder, replace membership, then remove the original group entirely.
			m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{testApp("other", 16384, 20), testApp("node workers", 2048, 99)}}})
			m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{testApp("other", 16384, 20)}}})
			if m.viewKillModal() != before {
				t.Fatal("confirmation changed after refresh")
			}
			require(t, !strings.Contains(before, "pkill") && !strings.Contains(before, "99"), "dialog has wrong scope")
			m, cmd := updateModel(m, press('y'))
			done := cmd().(killDone)
			require(t, !m.killConfirm && done.err == nil, "confirmation failed: %+v", done)
			want := []int{10, 11}
			if child {
				want = []int{11}
			}
			if !reflect.DeepEqual(signaled, want) {
				t.Fatalf("signaled %v, want %v", signaled, want)
			}
		})
	}
}

func TestKillScopeCopiedAndCancellation(t *testing.T) {
	m := testModel(testApp("a", 4096, 1, 2))
	m = m.beginKill(false)
	m.snap.Apps[0].Children[0].PID = 99
	if m.killTarget.App.Children[0].PID != 1 {
		t.Fatal("kill scope aliases snapshot")
	}
	m, cmd := updateModel(m, press('n'))
	if cmd != nil || m.killConfirm || len(m.killTarget.App.Children) != 0 {
		t.Fatal("cancel retained target")
	}
	m = testModel()
	m, cmd = updateModel(m, press('x'))
	if m.killConfirm || cmd != nil {
		t.Fatal("empty table opened kill dialog")
	}
}

func TestKillReportsAllFailuresAndContinues(t *testing.T) {
	sentinel := errors.New("denied")
	var called []int
	cmd := doKill(VisibleRow{App: testApp("app", 4096, 1, 2, 3)}, false, func(p collector.ProcDetail, _ bool) error { called = append(called, p.PID); return sentinel })
	msg := cmd().(killDone)
	if len(called) != 3 || !errors.Is(msg.err, sentinel) {
		t.Fatal("kill stopped early or lost errors")
	}
	for _, pid := range called {
		if !strings.Contains(msg.err.Error(), fmt.Sprintf("PID %d", pid)) {
			t.Fatal("missing failed PID")
		}
	}
}

func TestCollectionSerializedAndErrorsVisible(t *testing.T) {
	m := New(Config{Interval: time.Nanosecond})
	for range 3 {
		var cmd tea.Cmd
		m, cmd = updateModel(m, tick{})
		require(t, m.collecting && cmd != nil, "in-flight scan lost")
		// With a scan outstanding only the next timer is returned, not a batch.
		if _, ok := cmd().(tea.BatchMsg); ok {
			t.Fatal("overlapping collection scheduled")
		}
	}
	m, _ = updateModel(m, snapErr{errors.New("unsupported platform")})
	require(t, !m.collecting && strings.Contains(m.View().Content, "unsupported platform") && !strings.Contains(m.View().Content, "Collecting"), "initial error hidden")
	m, cmd := updateModel(m, tick{})
	batch, ok := cmd().(tea.BatchMsg)
	require(t, ok && len(batch) == 2 && m.collecting, "error did not allow retry")
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{}})
	require(t, !m.collecting && m.err == nil, "success did not clear error")
	m.paused = true
	m, _ = updateModel(m, tick{})
	if m.collecting {
		t.Fatal("paused model scheduled collection")
	}
}

func TestSearchSortAndSnapshotImmutability(t *testing.T) {
	apps := make([]collector.AppStat, 40)
	for i := range apps {
		apps[i] = testApp(fmt.Sprintf("app-%02d", i), int64(4096-i*50), i+1)
	}
	apps[39].SwapKB = 4096
	apps[39].ProcCount = 100
	apps[39].Children[0].Cmdline = "node /projects/needle/server.js"
	m := testModel(apps...)
	for _, mode := range []SortMode{SortSwap, SortProcs} {
		m.sort = mode
		if m.sortedApps()[0].Name != "app-39" {
			t.Fatal("sort missed low RSS group")
		}
	}
	m.filter = "NEEDLE"
	if got := m.sortedApps(); len(got) != 1 || got[0].Name != "app-39" {
		t.Fatal("command-line search missed process")
	}
	if len(m.snap.Apps) != 40 || m.snap.Apps[0].Name != "app-00" {
		t.Fatal("filter/sort mutated snapshot")
	}
	m.filter = "app-39"
	if len(m.sortedApps()) != 1 {
		t.Fatal("group-name filter failed")
	}
}

func TestMetricLabelsAndUnavailableValues(t *testing.T) {
	a := testApp("app", 2048, 1)
	a.SwapKnown = false
	m := testModel(a)
	table := m.viewTable(m.visibleRows(m.sortedApps()), 80, 9)
	if !strings.Contains(table, "n/a") || !strings.Contains(table, "MiB") || strings.Contains(table, "MB") {
		t.Fatal("bad units/unavailable swap")
	}
	m.snap.ProcessCommit = true
	m.snap.Apps[0].CommitKnown = true
	m.snap.Apps[0].CommitKB = 4096
	m.sort = SortSwap
	table = m.viewTable(m.visibleRows(m.sortedApps()), 80, 9)
	if !strings.Contains(table, "COMMIT") || !strings.Contains(table, "4MiB") || !strings.Contains(table, "sort:Commit") {
		t.Fatalf("commit mislabeled:\n%s", table)
	}
	m.snap.System.CommitKnown = true
	m.snap.System.CommitLimitKB = 2 * 1048576
	m.snap.System.CommitAvailableKB = 1048576
	system := m.viewSystem(36)
	if !strings.Contains(system, "unavailable") || !strings.Contains(system, "GiB limit") || !strings.Contains(system, "GiB available") {
		t.Fatalf("system commit/swap misleading:\n%s", system)
	}
}

func require(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func TestFilterAcceptsCancelLetterAndClears(t *testing.T) {
	m := testModel(testApp("node", 4096, 1))
	m, _ = updateModel(m, press('/'))
	m, _ = updateModel(m, press('n'))
	require(t, m.filtering && m.filter == "n", "typing n cancelled filter")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	require(t, !m.filtering && m.filter == "n", "enter did not accept filter")
	m, _ = updateModel(m, press('/'))
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	require(t, !m.filtering && m.filter == "", "escape did not clear filter")
}

func TestUnknownMetricsSortAfterKnownZero(t *testing.T) {
	unknown := testApp("unknown", 4096, 1)
	unknown.SwapKnown = false
	unknown.SwapKB = 8192
	known := testApp("known", 2048, 2)
	m := testModel(unknown, known)
	m.sort = SortSwap
	require(t, m.sortedApps()[0].Name == "known", "unavailable swap sorted ahead of known zero")
	m.snap.ProcessCommit = true
	m.snap.Apps[1].CommitKnown = true
	require(t, m.sortedApps()[0].Name == "known", "unavailable commit sorted ahead of known zero")
}

func TestTableRemainsWithinTerminalDimensions(t *testing.T) {
	m := testModel(testApp("日本語👩‍💻long-name", 4096, 1, 2))
	m.expanded[m.snap.Apps[0].Name] = true
	rows := m.visibleRows(m.sortedApps())
	for _, size := range []struct{ w, h int }{{80, 9}, {40, 5}, {10, 3}, {1, 1}, {0, 0}} {
		table := m.viewTable(rows, size.w, size.h)
		require(t, lipgloss.Width(table) <= size.w, "table exceeds width %d", size.w)
		if size.h > 0 {
			require(t, lipgloss.Height(table) <= size.h, "table exceeds height %d", size.h)
		}
	}
}
