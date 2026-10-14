package tui

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"memhogs.kjanat.dev/internal/collector"
)

// processSummary replaces Chromium's repeated launch flags with the role they
// explicitly report. It cannot identify tabs or sites absent from the command.
func processSummary(app string, process collector.ProcDetail) string {
	args := strings.Fields(displayText(process.Cmdline))
	if app == "claude" {
		return claudeSummary(process)
	}
	if role := chromiumRole(args); role != "" {
		return role
	}
	if browserGroup(app) && commandFlag(args, "--type") == "" {
		return "browser (no --type)"
	}
	if shellGroup(app) && len(args) <= 1 {
		return shellSummary(process)
	}
	if len(args) == 0 {
		return displayText(app)
	}
	return strings.Join(args, " ")
}

func claudeSummary(process collector.ProcDetail) string {
	if session := process.Session; session != nil {
		name := session.Name
		id := session.ID
		if len(id) > 8 {
			id = id[:8]
		}
		if name == "" {
			name = "session"
		}
		directory := "spawn dir unavailable"
		if session.ProjectDir != "" {
			directory = projectLabel(session.ProjectDir)
		}
		return directory + " · " + displayText(name) + " [" + displayText(id) + "]"
	}
	return "cwd: " + projectLabel(process.Cwd) + " · session unavailable"
}

func projectLabel(directory string) string {
	if directory == "" {
		return "directory unavailable"
	}
	// Keep the parent for nested repositories such as dprint/dprint.
	directory = filepath.Clean(directory)
	parent, name := filepath.Base(filepath.Dir(directory)), filepath.Base(directory)
	if parent == "projects" || parent == "~" || parent == "." || parent == string(filepath.Separator) {
		return displayText(name)
	}
	return displayText(filepath.Join(parent, name))
}

func chromiumRole(args []string) string {
	role := commandFlag(args, "--type")
	switch role {
	case "renderer":
		if slices.Contains(args, "--extension-process") {
			return "extension renderer"
		}
		return "renderer"
	case "gpu-process":
		return "GPU process"
	case "utility":
		return utilitySummary(commandFlag(args, "--utility-sub-type"))
	case "zygote", "broker", "crashpad-handler":
		return role
	}
	return ""
}

func utilitySummary(subtype string) string {
	switch subtype {
	case "network.mojom.NetworkService":
		return "network service"
	case "audio.mojom.AudioService":
		return "audio service"
	case "storage.mojom.StorageService":
		return "storage service"
	case "":
		return "utility"
	default:
		parts := strings.Split(subtype, ".")
		return "utility: " + parts[len(parts)-1]
	}
}

func shellGroup(name string) bool {
	return slices.Contains([]string{"zsh", "bash", "fish", "sh", "dash", "ksh"}, strings.ToLower(name))
}

func shellSummary(process collector.ProcDetail) string {
	cwd := displayText(process.Cwd)
	if cwd == "" {
		cwd = "directory unavailable"
	}
	if process.Terminal != "" {
		return displayText(process.Terminal) + " · " + cwd
	}
	return cwd
}

func commandFlag(args []string, name string) string {
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func browserGroup(name string) bool {
	fields := strings.Fields(strings.ToLower(name))
	if len(fields) == 0 {
		return false
	}
	return slices.Contains([]string{"chrome", "chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "brave", "brave-browser", "msedge", "vivaldi", "opera"}, fields[0])
}

func (m Model) childSecondary(c collector.ProcDetail) (int64, bool) {
	if m.snap != nil && m.snap.ProcessCommit {
		return c.CommitKB, c.CommitKnown
	}
	return c.SwapKB, c.SwapKnown
}

func (m Model) sortedChildren(app collector.AppStat) []collector.ProcDetail {
	children := slices.Clone(app.Children)
	slices.SortFunc(children, func(a, b collector.ProcDetail) int {
		order := cmp.Compare(b.RSSKB, a.RSSKB)
		switch m.sort {
		case SortSwap:
			av, ak := m.childSecondary(a)
			bv, bk := m.childSecondary(b)
			order = compareMemory(av, ak, bv, bk)
		case SortName:
			order = cmp.Compare(processSummary(app.Name, a), processSummary(app.Name, b))
		default: // RSS, process count, and the inactive sentinel use child RSS.
		}
		return cmp.Or(order, cmp.Compare(a.PID, b.PID))
	})
	return children
}

func compareMemory(a int64, aKnown bool, b int64, bKnown bool) int {
	if aKnown != bKnown {
		if aKnown {
			return -1
		}
		return 1
	}
	return cmp.Compare(b, a)
}
