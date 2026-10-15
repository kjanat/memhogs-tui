package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func projectModel() Model {
	m := nestedModel()
	a := &m.snap.Apps[0]
	for i := range a.Children {
		p := &a.Children[i]
		p.Context = &collector.ProcessContext{ApplicationPID: 10, ApplicationStart: 10}
		for parent := p.ParentPID; parent >= 10; parent = map[int]int{50: 10, 40: 30, 30: 20, 20: 10, 10: 1}[parent] {
			p.Context.Ancestors = append(p.Context.Ancestors, collector.ProcessAncestor{PID: parent, StartTime: uint64(parent)})
		}
		if p.PID == 20 || p.PID == 30 || p.PID == 40 {
			p.Project = &collector.ProjectInfo{Directory: "/projects/actionlint", Source: "verified session directory", Owner: collector.ProcessIdentity{PID: 20, StartTime: 20}, Tool: "claude"}
		}
		if p.PID == 50 {
			p.Project = &collector.ProjectInfo{Directory: "/projects/dprint/dprint", Source: "cwd", Owner: processID(*p), Tool: "zed"}
		}
	}
	return m
}

func TestProjectsPartitionTreeCountersWithoutDuplicatingProcesses(t *testing.T) {
	m := projectModel()
	app := m.snap.Apps[0]
	projects := projectGroups(app)
	pids := make([]int, 0, len(app.Children))
	var total int64
	for _, p := range projects {
		pids = append(pids, rowPIDs(m.processRows(p, 0))...)
		total += p.RSSKB
	}
	slices.Sort(pids)
	require(t, len(projects) == 3 && reflect.DeepEqual(pids, []int{10, 20, 30, 40, 50}), "project buckets lost or duplicated dependencies: %v", pids)
	var expected int64
	for _, p := range app.Children {
		expected += p.RSSKB
	}
	require(t, total == expected, "project totals changed accounting")
	rows := m.visibleRows(m.sortedApps())
	require(t, len(rows) == 4 && rows[1].Depth == 1 && rows[1].App.GroupID.Project == "/projects/actionlint", "project summaries missing or not sorted by usage")
	m.cursor = 1
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	rows = m.visibleRows(m.sortedApps())
	require(t, reflect.DeepEqual(rowPIDs(rows), []int{20, 30, 40}) && rows[4].Depth == 4, "project expansion flattened dependencies")
}

func TestProjectSelectionFoldingAndDeltasFollowOwnerAndFullPath(t *testing.T) {
	m := projectModel()
	rows := m.visibleRows(m.sortedApps())
	id := rows[1].App.GroupID
	m.cursor = 1
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	before := m.snap.Apps[0]
	next := before
	next.Children = slices.Clone(before.Children)
	next.Children[3].RSSKB += 1024
	next.Name = "renamed editor"
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{next}})
	rows = m.visibleRows(m.sortedApps())
	require(t, rows[m.cursor].App.GroupID == id && m.expanded[id], "project state followed label or row position")
	delta, _ := m.delta(id)
	require(t, delta == 1024, "project delta lost current subset: %d", delta)
	next.GroupID.Root.StartTime++
	next.Root.StartTime++
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{next}})
	require(t, !m.expanded[id], "reused owner inherited project expansion")
}

func TestProjectKillCapturesOnlyMembersAndSurvivesRefresh(t *testing.T) {
	m := projectModel()
	m.cursor = 1
	var signaled []int
	m.signal = func(p collector.ProcDetail, _ bool) error { signaled = append(signaled, p.PID); return nil }
	m = m.beginKill(false)
	modal := m.viewKillModal()
	require(t, m.killConfirm && strings.Contains(modal, "project /projects/actionlint") && strings.Contains(modal, "Procs:   3"), "project kill scope not disclosed: %s", modal)
	m = m.applySnapshot(&collector.Snapshot{})
	require(t, m.viewKillModal() == modal, "project confirmation changed after refresh")
	_, cmd := updateModel(m, press('y'))
	require(t, cmd().(killDone).err == nil && reflect.DeepEqual(signaled, []int{20, 30, 40}), "captured another project's processes: %v", signaled)
}

func TestProjectMembershipMismatchBlocksBulkKill(t *testing.T) {
	m := projectModel()
	group := m.visibleRows(m.sortedApps())[1].App
	group.GroupID.Project = "/unrelated"
	require(t, bulkKillProblem(VisibleRow{App: group}) != "", "project scope accepted mismatched process evidence")
	group.Children = group.Children[:1]
	require(t, bulkKillProblem(VisibleRow{App: group}) != "", "single-member project accepted mismatched evidence")
}

func TestProjectContextShowsFullDirectoryAndSharedInfrastructure(t *testing.T) {
	m := projectModel()
	m.cursor = 1
	text := ansi.Strip(m.View().Content)
	require(t, strings.Contains(text, "Project context") && strings.Contains(text, "/projects/actionlint") && strings.Contains(text, "verified session directory"), "project evidence missing from sidebar")
	for _, project := range projectGroups(m.snap.Apps[0]) {
		if project.GroupID.Project == sharedProject {
			require(t, strings.Contains(m.projectContext(project, 50), "Shared editor"), "shared root mislabeled as a project")
		}
	}
	m.filter = "/projects/actionlint"
	require(t, len(m.sortedApps()) == 1, "canonical project path not searchable")
}

func TestProjectsKeepIndependentOwnersAndExecutableScopeSeparate(t *testing.T) {
	m := projectModel()
	a := m.visibleRows(m.sortedApps())[1].App
	b := m.snap.Apps[0]
	b.GroupID.Root.StartTime++
	b.Root = b.GroupID.Root
	for _, project := range projectGroups(b) {
		require(t, project.GroupID != a.GroupID, "independent owner with the same project reused its identity")
	}
	b.GroupID.Mode = collector.GroupExecutable
	require(t, len(projectGroups(b)) == 0, "executable grouping silently changed scope")
	a.CPUKnown = false
	a.Children[1].CPUKnown = false
	project := projectSubset(a, a.GroupID.Project, a.Children)
	require(t, !project.CPUKnown && !project.CommitKnown && project.SwapKnown, "project counters lost unavailable metric semantics")
}

func TestProjectNavigationAndMissingProjectSelection(t *testing.T) {
	m := projectModel()
	m.cursor = 1
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyRight})
	rows := m.visibleRows(m.sortedApps())
	require(t, len(rows) == 7, "right arrow did not expand project")
	m.cursor = 4 // deepest process -> parent process -> project
	for range 5 {
		m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	require(t, m.cursor == 1, "left arrow skipped the project parent: %d", m.cursor)
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	require(t, len(m.visibleRows(m.sortedApps())) == 4, "project did not collapse independently")
	next := m.snap.Apps[0]
	next.Children = slices.Clone(next.Children)
	for i := range next.Children {
		next.Children[i].Project = nil
	}
	m = m.applySnapshot(&collector.Snapshot{Apps: []collector.AppStat{next}})
	require(t, m.cursor == 0, "disappeared project selected an unrelated process")
}

func TestSingleProcessProjectRemainsExpandable(t *testing.T) {
	m := projectModel()
	a := m.snap.Apps[0]
	a.Children = a.Children[1:2]
	a.Root = processID(a.Children[0])
	a.GroupID.Root = a.Root
	require(t, groupExpandable(a), "single-process session hid project section")
}
