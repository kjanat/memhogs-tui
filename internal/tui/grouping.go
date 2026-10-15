package tui

import (
	"cmp"
	"fmt"

	"memhogs.kjanat.dev/internal/collector"
)

func appLabel(app collector.AppStat) string {
	label := app.Name
	if app.Root.PID != 0 {
		label += fmt.Sprintf(" [%d]", app.Root.PID)
		if app.Directory != "" && app.Directory != "~" && app.Directory != "/" {
			label += " " + projectLabel(app.Directory)
		}
	}
	if app.GroupID.Project != "" {
		label = fmt.Sprintf("%s [%d] %s", app.Name, app.Root.PID, projectLabelForGroup(app))
	}
	return displayText(label)
}

func killScope(row VisibleRow) string {
	if row.Kind == RowChild {
		return "single process"
	}
	if row.App.GroupID.Project != "" {
		path := row.App.GroupID.Project
		if path == sharedProject {
			path = projectLabelForGroup(row.App)
		}
		return fmt.Sprintf("project %s within %s [PID %d]", displayText(path), displayText(row.App.Name), row.App.Root.PID)
	}
	switch row.App.GroupID.Mode {
	case collector.GroupApplication:
		return fmt.Sprintf("application instance, root PID %d", row.App.Root.PID)
	case collector.GroupTree:
		return fmt.Sprintf("process tree, root PID %d", row.App.Root.PID)
	default:
		return "executable group, all listed instances"
	}
}

func compareAppName(a, b collector.AppStat) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), a.GroupID.Compare(b.GroupID))
}

func findGroup(apps []collector.AppStat, id collector.GroupID) (collector.AppStat, bool) {
	owner := id
	owner.Project = ""
	for _, app := range apps {
		if app.GroupID == id {
			return app, true
		}
		if app.GroupID != owner {
			continue
		}
		for _, project := range projectGroups(app) {
			if project.GroupID == id {
				return project, true
			}
		}
	}
	return collector.AppStat{}, false
}

func (m Model) pruneExpanded() {
	active := make(map[collector.GroupID]bool)
	processes := make(map[collector.ProcessIdentity]bool)
	if m.snap != nil {
		for _, app := range m.snap.Apps {
			active[app.GroupID] = true
			for _, project := range projectGroups(app) {
				active[project.GroupID] = true
			}
			for _, child := range app.Children {
				processes[processID(child)] = true
			}
		}
	}
	for id := range m.expanded {
		if !active[id] {
			delete(m.expanded, id)
		}
	}
	for id := range m.collapsed {
		if !processes[id] {
			delete(m.collapsed, id)
		}
	}
}
