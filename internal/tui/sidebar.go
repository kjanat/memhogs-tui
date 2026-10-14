package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func (m Model) sidebarWidth() int {
	if m.width < 90 {
		return 0
	}
	return min(max(m.width/3, 28), 52, m.width-61)
}

func (m Model) viewBody(rows []VisibleRow, height int) string {
	sideWidth := m.sidebarWidth()
	if sideWidth == 0 && m.sideFocused {
		return m.viewSideViewport(rows, m.width, height)
	}
	tableWidth := m.width - sideWidth
	table := lipgloss.NewStyle().Width(tableWidth).Height(height).MaxHeight(height).
		Render(m.viewTable(rows, tableWidth, height))
	if sideWidth == 0 {
		return table
	}
	style := sidebarStyle
	if m.sideFocused {
		style = style.BorderLeftForeground(colorAccent)
	}
	side := m.viewSideViewport(rows, sideWidth-style.GetHorizontalFrameSize(), height)
	return lipgloss.JoinHorizontal(lipgloss.Top, table, style.Render(side))
}

func selectedProcess(row VisibleRow) (collector.ProcDetail, bool) {
	if row.Kind == RowChild {
		return row.Child, true
	}
	for _, process := range row.App.Children {
		if process.PID == row.App.Root.PID && process.StartTime == row.App.Root.StartTime {
			return process, true
		}
	}
	if process := row.App.RootProcess; process != nil && process.PID == row.App.Root.PID && process.StartTime == row.App.Root.StartTime {
		return *process, true
	}
	if len(row.App.Children) == 1 {
		return row.App.Children[0], true
	}
	return collector.ProcDetail{}, false
}

func (m Model) viewProcessContext(row VisibleRow, width int) string {
	process, ok := selectedProcess(row)
	header := sectionDivStyle.Width(width).Render("Process context")
	if !ok {
		return header + "\n" + dimStyle.Render("Multiple processes; select a child\nfor its owner and launch details.")
	}
	role := "PID"
	if row.Kind == RowGroup {
		role = "Root PID"
	}
	identity := fmt.Sprintf("%s %d", role, process.PID)
	if account := processAccount(process); account != "" {
		identity += " · " + account
	}
	parts := []string{header, valueStyle.Render(ansi.Hardwrap(displayText(identity), max(width, 1), true))}
	parts = append(parts, sidebarField("Launched by", processLauncher(process), width, 2))
	parts = append(parts, processDirectories(process, width)...)
	parts = append(parts, sidebarField("Arguments", processCommand(process), width, 6))
	parts = append(parts, sidebarField("Parent", processParent(process), width, 2))
	parts = append(parts, sidebarField("Executable", process.Executable, width, 3))
	if process.Terminal != "" {
		parts = append(parts, sidebarField("Terminal", process.Terminal, width, 1))
	}
	if session := process.Session; session != nil {
		parts = append(parts, sidebarField("Session", session.Name+" · "+session.ID, width, 3))
	}
	return strings.Join(parts, "\n")
}

func sidebarField(label, value string, width, maxLines int) string {
	if value == "" {
		value = "unavailable"
	}
	// Bound previews before wrapping. The full inspector retains complete text.
	text := ansi.Truncate(displayText(value), max(width, 1)*maxLines, "…")
	lines := strings.Split(ansi.Hardwrap(text, max(width, 1), true), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[len(lines)-1] = ansi.Truncate(lines[len(lines)-1], max(width-1, 0), "") + "…"
	}
	return labelStyle.Render(label) + "\n" + valueStyle.Render(strings.Join(lines, "\n"))
}

func processLauncher(p collector.ProcDetail) string {
	if ctx := p.Context; ctx != nil && ctx.Origin != "" {
		return fmt.Sprintf("%s [PID %d]", ctx.Origin, ctx.OriginPID)
	}
	return processParent(p)
}

func processParent(p collector.ProcDetail) string {
	if p.ParentPID == 0 {
		return ""
	}
	if ctx := p.Context; ctx != nil && len(ctx.Ancestors) > 0 && ctx.Ancestors[0].PID == p.ParentPID {
		return fmt.Sprintf("%s [PID %d]", ctx.Ancestors[0].Name, p.ParentPID)
	}
	return fmt.Sprintf("PID %d", p.ParentPID)
}

func processAccount(p collector.ProcDetail) string {
	if p.User != "" && p.UID != "" {
		return p.User + " (UID " + p.UID + ")"
	}
	if p.UID != "" {
		return "UID " + p.UID
	}
	return p.User
}

func processDirectories(p collector.ProcDetail, width int) []string {
	var parts []string
	if p.Session != nil && p.Session.ProjectDir != "" {
		parts = append(parts, sidebarField("Launch dir (session)", p.Session.ProjectDir, width, 3))
		if p.Cwd != p.Session.ProjectDir {
			parts = append(parts, sidebarField("Current dir", p.Cwd, width, 3))
		}
	} else {
		parts = append(parts, sidebarField("Current dir", p.Cwd, width, 3), dimStyle.Render("Launch dir not recorded"))
	}
	if ctx := p.Context; ctx != nil && ctx.ProjectDir != "" && ctx.ProjectDir != p.Cwd {
		parts = append(parts, sidebarField("Workspace ("+ctx.ProjectSource+")", ctx.ProjectDir, width, 3))
	}
	return parts
}

func processCommand(p collector.ProcDetail) string {
	if len(p.Args) == 0 {
		return p.Cmdline
	}
	args := make([]string, len(p.Args))
	for i, arg := range p.Args {
		arg = displayText(arg)
		if arg == "" || strings.ContainsAny(arg, " \t\"'\\") {
			arg = fmt.Sprintf("%q", arg)
		}
		args[i] = arg
	}
	// Rewritten argv[0] already contains a display command; don't quote it whole.
	if len(p.Args) == 1 && strings.Contains(p.Args[0], " --") {
		return p.Args[0]
	}
	return strings.Join(args, " ")
}

func (m Model) sidebarParts(rows []VisibleRow, width, height int) (body, pinned []string, capacity int) {
	body = strings.Split(m.viewSide(rows, width), "\n")
	pinned = strings.Split(m.viewSystemCompact(width), "\n")
	pinned = pinned[:min(len(pinned), max(height-1, 0))]
	capacity = max(height-len(pinned)-1, 0)
	return body, pinned, capacity
}

func (m Model) viewSideViewport(rows []VisibleRow, width, height int) string {
	body, pinned, capacity := m.sidebarParts(rows, width, height)
	start := min(max(m.sideOffset, 0), max(len(body)-capacity, 0))
	lines := append([]string(nil), body[start:min(start+capacity, len(body))]...)
	for len(lines) < capacity {
		lines = append(lines, "")
	}
	lines = append(lines, pinned...)
	footer := "Tab details · c full text"
	if m.sideFocused {
		footer = fmt.Sprintf("PgUp/PgDn · Tab: table · %d/%d", start+1, len(body))
	}
	if height > 0 {
		lines = append(lines, sectionStyle.Render(footer))
	}
	for i, line := range lines {
		line = ansi.Truncate(line, max(width, 0), "")
		lines[i] = line + strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
	}
	return strings.Join(lines, "\n")
}

func (m Model) handleSideNavigation(msg tea.KeyPressMsg) (Model, bool) {
	if m.snap == nil {
		return m, false
	}
	width := m.width
	if sideWidth := m.sidebarWidth(); sideWidth > 0 {
		width = sideWidth - sidebarStyle.GetHorizontalFrameSize()
	}
	body, _, capacity := m.sidebarParts(m.visibleRows(m.sortedApps()), width, max(m.height-1, 0))
	limit := max(len(body)-capacity, 0)
	offset := min(m.sideOffset, limit)
	next, handled := navigationOffset(msg, offset, max(capacity, 1), limit)
	m.sideOffset = next
	return m, handled
}

func (m Model) viewSystemCompact(width int) string {
	sys := m.snap.System
	parts := []string{
		kvRow("RAM", fmt.Sprintf("%.1f / %.1f GiB", float64(sys.MemUsedKB)/1048576, float64(sys.MemTotalKB)/1048576), width),
		kvDetail(fmt.Sprintf("%.1f GiB available", float64(sys.MemAvailableKB)/1048576), width),
		kvRow("Swap", systemSwapSummary(sys), width),
	}
	if psi := m.snap.PSI; psi != nil {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("PSI10 s:%.2f f:%.2f", psi.SomeAvg10, psi.FullAvg10)))
	}
	return strings.Join(parts, "\n")
}

func systemSwapSummary(sys collector.SystemInfo) string {
	if !sys.SwapKnown {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f / %.1f GiB", float64(sys.SwapUsedKB)/1048576, float64(sys.SwapTotalKB)/1048576)
}
