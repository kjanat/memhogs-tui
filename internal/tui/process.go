package tui

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"memhogs.kjanat.dev/internal/collector"
)

// processSummary always combines common process context with a concise role.
// Application metadata enriches the role without replacing workspace or ownership.
func processSummary(app string, process collector.ProcDetail) string {
	parts := processIdentity(app, process)
	parts = append(parts, processRole(app, process))
	if ctx := process.Context; ctx != nil && ctx.Origin != "" && !ctx.Chromium {
		parts = append(parts, "via "+ctx.Origin)
	}
	if process.Terminal != "" {
		parts = append(parts, process.Terminal)
	}
	return displayText(strings.Join(parts, " · "))
}

func processIdentity(app string, p collector.ProcDetail) []string {
	var parts []string
	directory := p.Cwd
	if ctx := p.Context; ctx != nil {
		if ctx.Application != "" && !strings.EqualFold(ctx.Application, app) {
			parts = append(parts, ctx.Application)
		}
		if ctx.ProjectDir != "" {
			directory = ctx.ProjectDir
		}
	}
	if p.Session != nil && p.Session.ProjectDir != "" {
		directory = p.Session.ProjectDir
	}
	if directory != "" {
		parts = append(parts, projectLabel(directory))
	}
	return parts
}

func processRole(app string, p collector.ProcDetail) string {
	if session := p.Session; session != nil {
		return sessionLabel(session)
	}
	if app == "claude" {
		return "session unavailable"
	}
	args := p.Arguments()
	if role := chromiumRole(args); role != "" {
		return role
	}
	if ctx := p.Context; ctx != nil && ctx.Chromium && ctx.ApplicationPID == p.PID {
		return "main process"
	}
	return compactCommand(app, args)
}

func sessionLabel(session *collector.SessionInfo) string {
	name := session.Name
	if name == "" {
		name = "session"
	}
	id := session.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return name + " [" + id + "]"
}

func compactCommand(app string, args []string) string {
	if len(args) == 0 {
		return app
	}
	program := filepath.Base(args[0])
	positional := commandPositionals(args[1:])
	if len(positional) == 0 {
		return program
	}
	if program == app {
		return strings.Join(positional, " ")
	}
	return program + " " + strings.Join(positional, " ")
}

func commandPositionals(args []string) []string {
	var positional []string
	for i := 0; i < len(args) && len(positional) < 3; i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if len(positional) > 0 {
				break
			}
			if valueFlag(arg) {
				i++
				continue
			}
			if strings.Contains(arg, "=") {
				continue
			}
			break
		}
		if strings.Contains(arg, "=") || arg == "**" {
			continue
		}
		positional = append(positional, arg)
	}
	return positional
}

func valueFlag(flag string) bool {
	return slices.Contains([]string{"-c", "--config", "--settings", "--name", "-n", "--model", "--effort", "--user-data-dir", "--class", "--app-path"}, flag)
}

func projectLabel(directory string) string {
	if directory == "" {
		return "directory unavailable"
	}
	if directory == "~" || directory == string(filepath.Separator) {
		return directory
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
		case SortCPU:
			order = compareCPU(a.CPUPct, a.CPUKnown, b.CPUPct, b.CPUKnown)
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

func compareCPU(a float64, aKnown bool, b float64, bKnown bool) int {
	if aKnown != bKnown {
		if aKnown {
			return -1
		}
		return 1
	}
	if !aKnown {
		return 0
	}
	return cmp.Compare(b, a)
}
