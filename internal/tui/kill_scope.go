package tui

import "memhogs.kjanat.dev/internal/collector"

// Check accounting scope before offering bulk termination. Explicit executable
// grouping stays broad; application/tree groups require per-member evidence.
func bulkKillProblem(row VisibleRow) string {
	if row.Kind == RowChild || (len(row.App.Children) <= 1 && row.App.GroupID.Project == "") {
		return ""
	}
	id := row.App.GroupID
	if id.Mode == collector.GroupExecutable {
		return ""
	}
	if !validInstanceGroup(row.App) {
		return "group root identity unavailable; select an individual process"
	}
	for _, process := range row.App.Children {
		if !belongsToGroup(process, id) || !projectMemberMatches(process, id) {
			return "group ownership is ambiguous; select an individual process"
		}
	}
	return ""
}

func validInstanceGroup(app collector.AppStat) bool {
	id := app.GroupID
	return id.Root == app.Root && id.Root.PID > 0 && id.Root.StartTime > 0 &&
		(id.Mode == collector.GroupApplication || id.Mode == collector.GroupTree)
}

func belongsToGroup(p collector.ProcDetail, id collector.GroupID) bool {
	if p.PID == id.Root.PID && p.StartTime == id.Root.StartTime {
		return true
	}
	if p.Context == nil {
		return false
	}
	switch id.Mode {
	case collector.GroupApplication:
		return p.Context.ApplicationPID == id.Root.PID && p.Context.ApplicationStart == id.Root.StartTime
	case collector.GroupTree:
		for _, ancestor := range p.Context.Ancestors {
			if ancestor.PID == id.Root.PID && ancestor.StartTime == id.Root.StartTime {
				return true
			}
		}
	default:
		return false
	}
	return false
}
