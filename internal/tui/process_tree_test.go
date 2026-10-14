package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func nestedModel() Model {
	app := instanceApp("zed-editor", 10, 1, 20480, 10, 20, 30, 40, 50)
	app.GroupID.Mode = collector.GroupTree
	for i, parent := range []int{1, 10, 20, 30, 10} {
		p := &app.Children[i]
		p.ParentPID = parent
		p.StartTime = []uint64{10, 20, 30, 40, 50}[i]
		p.Cmdline = map[int]string{10: "zed-editor", 20: "node adapter.js", 30: "claude", 40: "mcp server", 50: "gopls"}[p.PID]
		p.RSSKB = map[int]int64{10: 1024, 20: 2048, 30: 4096, 40: 8192, 50: 512}[p.PID]
		p.CPUPct, p.CPUKnown = float64(p.RSSKB)/100, true
	}
	app.Root.StartTime = 10
	app.GroupID.Root = app.Root
	m := testModel(app)
	m.width, m.height = 160, 24
	m.expanded[app.GroupID] = true
	return m
}

func rowPIDs(rows []VisibleRow) []int {
	pids := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Kind == RowChild {
			pids = append(pids, row.Child.PID)
		}
	}
	return pids
}

func TestProcessTreeKeepsChildrenUnderParentsAcrossSorts(t *testing.T) {
	m := nestedModel()
	for _, mode := range []SortMode{SortRSS, SortSwap, SortProcs, SortCPU} {
		m.sort = mode
		rows := m.visibleRows(m.sortedApps())
		want := []int{10, 20, 30, 40, 50}
		require(t, reflect.DeepEqual(rowPIDs(rows), want), "%s flattened the process hierarchy: %v", mode, rowPIDs(rows))
		depths := make([]int, 0, len(rows))
		for _, row := range rows {
			depths = append(depths, row.Depth)
		}
		require(t, reflect.DeepEqual(depths, []int{0, 1, 1, 2, 3, 1}), "incorrect nesting depths: %v", depths)
		require(t, rows[2].HasChildren && rows[3].HasChildren && !rows[4].HasChildren, "branch metadata incorrect")
	}
}

func TestProcessTreeRendersIndentationAndAlignedCounters(t *testing.T) {
	m := nestedModel()
	rows := m.visibleRows(m.sortedApps())
	for _, width := range []int{61, 80, 160} {
		text := ansi.Strip(m.viewTable(rows, width, 20))
		lines := strings.Split(text, "\n")
		require(t, strings.Contains(lines[5], "├─ ▾ ") && strings.Contains(lines[6], "│  └─ ▾ "), "nested branches not drawn:\n%s", text)
		column := strings.Index(lines[1], "CPU%") + 4
		for i, row := range rows[1:] {
			line := lines[i+4]
			cpu := cpuText(row.Child.CPUPct, row.Child.CPUKnown)
			end := strings.LastIndex(line, cpu) + len(cpu)
			require(t, ansi.StringWidth(line[:end]) == column, "nested CPU counter misaligned: %q", line)
			require(t, ansi.StringWidth(line) <= width, "tree row exceeds terminal width")
		}
	}
}

func TestBranchesFoldIndependentlyAndLeftMovesToParent(t *testing.T) {
	m := nestedModel()
	m.cursor = 2 // adapter
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	rows := m.visibleRows(m.sortedApps())
	require(t, reflect.DeepEqual(rowPIDs(rows), []int{10, 20, 50}) && rows[2].BranchClosed, "folding adapter hid siblings or retained descendants")
	require(t, strings.Contains(processTreePrefix(rows[2]), "▸"), "folded branch indicator missing")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyRight})
	m.cursor = 4 // MCP
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	rows = m.visibleRows(m.sortedApps())
	require(t, rows[m.cursor].Child.PID == 30 && m.expanded[rows[m.cursor].App.GroupID], "left from leaf did not select its parent")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	rows = m.visibleRows(m.sortedApps())
	require(t, reflect.DeepEqual(rowPIDs(rows), []int{10, 20, 30, 50}), "left did not fold only the selected branch")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	require(t, m.cursor == 2, "left from folded branch did not select its parent")
}

func TestBranchStateFollowsLifetimeAndSelectionAcrossRefresh(t *testing.T) {
	m := nestedModel()
	m.cursor = 2
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	app := m.snap.Apps[0]
	app.Name = "renamed editor"
	app.Children = slices.Clone(app.Children)
	slices.Reverse(app.Children)
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{app}})
	rows := m.visibleRows(m.sortedApps())
	require(t, rows[m.cursor].Child.PID == 20 && rows[m.cursor].BranchClosed, "refresh lost selected or folded process identity")
	for i := range app.Children {
		if app.Children[i].PID == 20 {
			app.Children[i].StartTime = 200
		}
	}
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{app}})
	require(t, len(m.collapsed) == 0, "reused PID inherited old branch state")
}

func TestTreeUsesVerifiedAncestorsAcrossHiddenIntermediary(t *testing.T) {
	m := nestedModel()
	p := &m.snap.Apps[0].Children[3] // PID 40
	p.ParentPID = 35                 // no memory row for this intermediary
	p.Context = &collector.ProcessContext{Ancestors: []collector.ProcessAncestor{{PID: 35, StartTime: 35}, {PID: 30, StartTime: 30}, {PID: 20, StartTime: 20}, {PID: 10, StartTime: 10}}}
	rows := m.visibleRows(m.sortedApps())
	require(t, reflect.DeepEqual(rowPIDs(rows), []int{10, 20, 30, 40, 50}) && rows[4].Depth == 3, "identity-only intermediary flattened hierarchy")
	p.Context.Ancestors[1].StartTime = 31 // reused PID; must not attach to current PID 30
	rows = m.visibleRows(m.sortedApps())
	for _, row := range rows {
		if row.Child.PID == 40 {
			require(t, row.Depth == 1, "stale ancestor attached process to wrong parent")
		}
	}
}

func TestTreeCyclesDoNotDuplicateOrLoseProcesses(t *testing.T) {
	m := nestedModel()
	m.snap.Apps[0].Children[1].ParentPID = 30
	m.snap.Apps[0].Children[1].StartTime = 30
	rows := m.visibleRows(m.sortedApps())
	pids := rowPIDs(rows)
	slices.Sort(pids)
	require(t, reflect.DeepEqual(pids, []int{10, 20, 30, 40, 50}), "cycle lost or duplicated a process: %v", pids)
}

func TestFoldedBranchStillTargetsOnlySelectedPID(t *testing.T) {
	m := nestedModel()
	m.cursor = 2
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m.beginKill(false)
	require(t, m.killConfirm && m.killTarget.Kind == RowChild && m.killTarget.Child.PID == 20, "branch fold broadened termination scope")
	modal := m.viewKillModal()
	require(t, strings.Contains(modal, "single process") && strings.Contains(modal, "Procs:   1"), "branch confirmation implies subtree termination")
}

func TestDeepProcessTreeKeepsCountersVisible(t *testing.T) {
	pids := make([]int, 20)
	for i := range pids {
		pids[i] = i + 10
	}
	app := instanceApp("editor", 10, 1, 40960, pids...)
	app.GroupID.Mode = collector.GroupTree
	for i := range app.Children {
		app.Children[i].ParentPID = i + 9
	}
	m := testModel(app)
	m.expanded[app.GroupID] = true
	rows := m.visibleRows(m.sortedApps())
	require(t, len(rows) == 21 && rows[20].Depth == 19, "deep descendants were flattened or truncated")
	for _, width := range []int{61, 80, 160} {
		table := m.viewTable(rows, width, 30)
		for line := range strings.SplitSeq(table, "\n") {
			require(t, utf8.ValidString(line) && ansi.StringWidth(line) <= width, "deep indentation corrupts terminal row")
		}
	}
	require(t, strings.HasPrefix(processTreeLabel(rows[20], 18), "…"), "deep guides must leave space for process label")
}
