package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"memhogs.kjanat.dev/internal/collector"
)

func instanceApp(id, name string, root int, rss int64, pids ...int) collector.AppStat {
	app := testApp(name, rss, pids...)
	app.GroupID = id
	app.Root = collector.ProcessIdentity{PID: root, StartTime: 1}
	return app
}

func TestDuplicateGroupNamesHaveIndependentState(t *testing.T) {
	a := instanceApp("application:10:1", "editor", 10, 8192, 10, 11)
	b := instanceApp("application:20:1", "editor", 20, 4096, 20, 21)
	m := testModel(a, b)
	m.cursor = 1
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	require(t, m.expanded[b.GroupID] && !m.expanded[a.GroupID], "fold state leaked between namesakes")
	b.RSSKB, b.Name, b.Directory = 16384, "renamed editor", "/projects/workspace"
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{a, b}})
	rows := m.visibleRows(m.sortedApps())
	require(t, rows[m.cursor].App.GroupID == b.GroupID && rows[m.cursor].Kind == RowGroup, "selection followed a display name or row index")
	dRSS, _ := m.delta(b.GroupID)
	require(t, dRSS == 12288, "memory delta compared different instances: %d", dRSS)
	m.filter = "workspace"
	require(t, len(m.sortedApps()) == 1, "group workspace not searchable")
	m.filter = ""
	m.cursor++ // select a child in b
	b.Children = nil
	b.ProcCount = 0
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{a, b}})
	rows = m.visibleRows(m.sortedApps())
	require(t, rows[m.cursor].App.GroupID == b.GroupID && rows[m.cursor].Kind == RowGroup, "child fallback selected another instance")
}

func TestNewRootLifetimeDoesNotInheritGroupHistory(t *testing.T) {
	old := instanceApp("application:10:1", "editor", 10, 4096, 10, 11)
	m := testModel(old)
	m.expanded[old.GroupID] = true
	next := instanceApp("application:10:2", "editor", 10, 8192, 10, 12)
	next.Root.StartTime = 2
	for i := range next.Children {
		next.Children[i].StartTime = 2
	}
	require(t, !sameRow(VisibleRow{App: old}, VisibleRow{App: next}), "reused root PID matched old group")
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{next}})
	require(t, len(m.expanded) == 0 && len(m.visibleRows(m.sortedApps())) == 1, "new instance inherited expansion or old state leaked")
	dRSS, dSwap := m.delta(next.GroupID)
	require(t, dRSS == 0 && dSwap == 0, "new lifetime inherited memory history")
}

func TestFrozenKillTargetsOnlyOneApplicationInstance(t *testing.T) {
	a := instanceApp("application:10:1", "ChatGPT", 10, 8192, 10, 11)
	b := instanceApp("application:20:1", "ChatGPT", 20, 4096, 20, 21)
	b.Directory = "/projects/second"
	m := testModel(a, b)
	m.cursor = 1
	var signals []collector.ProcessIdentity
	m.signal = func(p collector.ProcDetail, _ bool) error {
		signals = append(signals, collector.ProcessIdentity{PID: p.PID, StartTime: p.StartTime})
		return nil
	}
	m = m.beginKill(false)
	before := m.viewKillModal()
	require(t, strings.Contains(before, "application instance, root PID 20") && strings.Contains(before, "second"), "confirmation lost instance scope: %s", before)
	replacement := instanceApp("application:20:2", "ChatGPT", 20, 32768, 20, 29)
	replacement.Root.StartTime = 2
	for i := range replacement.Children {
		replacement.Children[i].StartTime = 2
	}
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{replacement, a}})
	require(t, m.viewKillModal() == before, "refresh changed confirmation scope")
	_, command := updateModel(m, press('y'))
	done := command().(killDone)
	require(t, done.err == nil, "fake termination failed")
	want := []collector.ProcessIdentity{{PID: 20, StartTime: 1}, {PID: 21, StartTime: 1}}
	require(t, reflect.DeepEqual(signals, want), "captured another instance or a reused PID identity: %v", signals)
}

func TestGroupingScopeAndInstanceLabels(t *testing.T) {
	app := instanceApp("application:10:1", "editor\x1b[2J", 10, 4096, 10)
	app.Directory = "/projects/work\nspace"
	label := appLabel(app)
	require(t, strings.Contains(label, "[10]") && !strings.ContainsAny(label, "\x1b\n"), "unsafe or ambiguous instance label: %q", label)
	app.GroupID = "executable:/usr/bin/editor"
	require(t, strings.Contains(killScope(VisibleRow{App: app}), "all listed instances"), "broad grouping scope hidden")
	app.GroupID = "tree:10:1"
	require(t, strings.Contains(killScope(VisibleRow{App: app}), "process tree"), "tree scope hidden")
	require(t, killScope(VisibleRow{Kind: RowChild, App: app}) == "single process", "child acquired group scope")
}

func TestNamesakeGroupSortsUseStableIdentityTies(t *testing.T) {
	a := instanceApp("application:10:1", "editor", 10, 4096, 10)
	b := instanceApp("application:20:1", "editor", 20, 4096, 20)
	m := testModel(b, a)
	for mode := range sortModeCount {
		m.sort = mode
		require(t, m.sortedApps()[0].GroupID == a.GroupID, "unstable %s sort for namesakes", mode)
	}
}
