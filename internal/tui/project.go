package tui

import (
	"fmt"
	"slices"
	"strings"

	"memhogs.kjanat.dev/internal/collector"
)

const sharedProject = "."

func processProject(p collector.ProcDetail) string {
	if p.Project != nil && p.Project.Directory != "" {
		return p.Project.Directory
	}
	return sharedProject
}

func projectGroups(app collector.AppStat) []collector.AppStat {
	if app.GroupID.Mode == collector.GroupExecutable || app.GroupID.Project != "" {
		return nil
	}
	byProject := make(map[string][]collector.ProcDetail)
	known := false
	for _, p := range app.Children {
		key := processProject(p)
		known = known || key != sharedProject
		byProject[key] = append(byProject[key], p)
	}
	if !known {
		return nil
	}
	groups := make([]collector.AppStat, 0, len(byProject))
	for path, children := range byProject {
		groups = append(groups, projectSubset(app, path, children))
	}
	return groups
}

func projectSubset(owner collector.AppStat, path string, children []collector.ProcDetail) collector.AppStat {
	app := collector.AppStat{
		GroupID: owner.GroupID, Name: owner.Name, Root: owner.Root, Directory: path,
		Children: children, ProcCount: len(children), SwapKnown: len(children) > 0, CommitKnown: len(children) > 0, CPUKnown: len(children) > 0,
	}
	app.GroupID.Project = path
	for _, p := range children {
		app.RSSKB += p.RSSKB
		app.SwapKB += p.SwapKB
		app.CommitKB += p.CommitKB
		app.CPUPct += p.CPUPct
		app.SwapKnown = app.SwapKnown && p.SwapKnown
		app.CommitKnown = app.CommitKnown && p.CommitKnown
		app.CPUKnown = app.CPUKnown && p.CPUKnown
	}
	if owner.RSSKB > 0 {
		app.MemPct = owner.MemPct * float64(app.RSSKB) / float64(owner.RSSKB)
	}
	return app
}

func projectLabelForGroup(app collector.AppStat) string {
	if app.GroupID.Project == sharedProject {
		return "Shared / unassigned"
	}
	return projectLabel(app.GroupID.Project)
}

func (m Model) groupedProcessRows(app collector.AppStat, appIndex int) []VisibleRow {
	projects := projectGroups(app)
	if len(projects) == 0 {
		return m.processRows(app, appIndex)
	}
	projects = m.sortAppStats(projects)
	var rows []VisibleRow
	for i, project := range projects {
		branch, guides := "├─ ", "│  "
		if i == len(projects)-1 {
			branch, guides = "└─ ", "   "
		}
		rows = append(rows, VisibleRow{Kind: RowGroup, App: project, AppIdx: appIndex, Depth: 1, TreePrefix: branch})
		if !m.expanded[project.GroupID] {
			continue
		}
		children := m.processRows(project, appIndex)
		for j := range children {
			children[j].Depth++
			children[j].TreePrefix = guides + children[j].TreePrefix
		}
		rows = append(rows, children...)
	}
	return rows
}

func (m Model) projectContext(app collector.AppStat, width int) string {
	directory := app.GroupID.Project
	if directory == sharedProject {
		directory = "Shared editor infrastructure or unavailable project evidence"
	}
	sources := make(map[string]bool)
	for _, p := range app.Children {
		if p.Project != nil {
			sources[p.Project.Source] = true
		}
	}
	ordered := make([]string, 0, len(sources))
	for source := range sources {
		ordered = append(ordered, source)
	}
	slices.Sort(ordered)
	parts := make([]string, 0, 3+len(sources))
	parts = append(parts, sectionDivStyle.Width(width).Render("Project context"), sidebarField("Directory", directory, width, 4), sidebarField("Owner", fmt.Sprintf("%s [PID %d]", app.Name, app.Root.PID), width, 2))
	for _, source := range ordered {
		parts = append(parts, sidebarField("Evidence", source, width, 2))
	}
	return strings.Join(parts, "\n")
}

func selectionGroupFallback(rows []VisibleRow, id collector.GroupID) (int, bool) {
	for range 2 {
		for i, row := range rows {
			if row.Kind == RowGroup && row.App.GroupID == id {
				return i, true
			}
		}
		id.Project = ""
	}
	return 0, false
}

func projectMatchesFilter(p collector.ProcDetail, query string) bool {
	return p.Project != nil && strings.Contains(strings.ToLower(p.Project.Directory), query)
}

func (m Model) collapseProject(row VisibleRow, rows []VisibleRow) Model {
	if m.expanded[row.App.GroupID] {
		m.expanded[row.App.GroupID] = false
		return m
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if rows[i].Depth == 0 {
			m.cursor = i
			break
		}
	}
	return m
}

func projectMemberMatches(p collector.ProcDetail, id collector.GroupID) bool {
	return id.Project == "" || processProject(p) == id.Project
}
