package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	lgtable "charm.land/lipgloss/v2/table"

	"github.com/kjanat/memhogs-tui/internal/collector"
)

// View renders the full-screen UI and enables the alternate screen buffer.
// It composes a left-side application table, an optional right-side detail
// sidebar, and a bottom status bar.
// Kill-confirm and help modals replace the entire screen when active.
func (m Model) View() tea.View {
	if m.width == 0 || m.snap == nil {
		v := tea.NewView("  Collecting…")
		v.AltScreen = true
		return v
	}

	apps := m.sortedApps()
	rows := m.visibleRows(apps)

	// Layout: sidebar appears at ≥90 cols
	hasSide := m.width >= 90
	mainH := m.height - 1 // 1 for status bar

	var sideW, tableW int
	if hasSide {
		sideW = min(m.width*3/10, 36)
		sideW = max(sideW, 28)
		tableW = m.width - sideW
	} else {
		tableW = m.width
	}

	table := m.viewTable(rows, tableW, mainH)
	tablePane := lipgloss.NewStyle().Width(tableW).Height(mainH).Render(table)

	var body string
	if hasSide {
		side := m.viewSide(rows, sideW-2)
		sidePane := sidebarStyle.Width(sideW).Height(mainH).Render(side)
		body = lipgloss.JoinHorizontal(lipgloss.Top, tablePane, sidePane)
	} else {
		body = tablePane
	}

	status := m.viewStatus(m.width)
	out := body + "\n" + status

	// Modal overlays replace the whole screen
	if m.killConfirm {
		modal := m.viewKillModal(rows)
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}
	if m.help {
		modal := m.viewHelpModal()
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func (m Model) viewTable(rows []VisibleRow, w, h int) string {
	// prefix(2) + name + rss(8) + swap(8) + procs(6) + pct(7) + gap(1)
	const fixed = 2 + 8 + 8 + 6 + 7 + 1

	nameW := 16
	for _, r := range rows {
		if r.Kind == RowGroup {
			if n := len(r.App.Name); n > nameW {
				nameW = n
			}
		}
	}
	nameW = min(nameW, max((w-fixed)/2, 16))
	barW := max(w-nameW-fixed, 0)
	barW = min(barW, w/3)

	var maxRSS int64
	for _, r := range rows {
		if r.Kind == RowGroup && r.App.RSSKB > maxRSS {
			maxRSS = r.App.RSSKB
		}
	}

	var b strings.Builder

	// Title
	title := fmt.Sprintf(" Applications  sort:%s", m.sort)
	if m.filter != "" {
		title += "  /" + m.filter
	}
	if m.paused {
		title += "  ⏸"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteByte('\n')

	hdr := fmt.Sprintf("  %-*s %7s %7s %5s %6s", nameW, "NAME", "RSS", "SWAP", "PROCS", "MEM%")
	b.WriteString(headerStyle.Render(hdr))
	b.WriteByte('\n')
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')

	maxVisible := max(h-3, 0)

	for i, r := range rows {
		if i >= maxVisible {
			break
		}

		switch r.Kind {
		case RowGroup:
			a := r.App
			name := a.Name
			if len(name) > nameW {
				name = name[:nameW-1] + "…"
			}

			// Fold indicator
			pfx := "▸ "
			if m.expanded[a.Name] {
				pfx = "▾ "
			}

			row := fmt.Sprintf("%s%-*s %5dMB %5dMB %5d %5.1f%%",
				pfx, nameW, name,
				a.RSSKB/1024, a.SwapKB/1024,
				a.ProcCount, a.MemPct)

			if barW > 0 && maxRSS > 0 {
				bLen := int(float64(a.RSSKB) / float64(maxRSS) * float64(barW))
				if a.RSSKB > 0 && bLen < 1 {
					bLen = 1
				}
				c := colorForPct(a.MemPct)
				bar := lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("█", bLen))
				row += " " + bar
			}

			if i == m.cursor {
				row = selectedStyle.Render(row)
			}
			b.WriteString(row)

		case RowChild:
			c := r.Child
			cmdW := max(w-12, 20) // full width minus RSS column
			cmd := c.Cmdline
			if len(cmd) > cmdW {
				cmd = cmd[:cmdW-1] + "…"
			}
			row := dimStyle.Render(fmt.Sprintf("    %-*s %5dMB", cmdW, cmd, c.RSSKB/1024))
			if i == m.cursor {
				row = selectedStyle.Render(fmt.Sprintf("  ▸ %-*s %5dMB", cmdW, cmd, c.RSSKB/1024))
			}
			b.WriteString(row)
		}

		b.WriteByte('\n')
	}

	if len(rows) == 0 {
		b.WriteString(dimStyle.Render("  (no processes)"))
		b.WriteByte('\n')
	}

	return b.String()
}

func (m Model) viewSide(rows []VisibleRow, width int) string {
	sections := make([]string, 0, 6)

	if m.cursor < len(rows) {
		sections = append(sections, m.viewDetail(rows[m.cursor].App, width))
	}

	sections = append(sections, m.viewSystem(width))

	if oom := m.viewOOM(width); oom != "" {
		sections = append(sections, oom)
	}

	if rescue := m.viewRescue(width); rescue != "" {
		sections = append(sections, rescue)
	}

	sections = append(sections, viewKeyHints(width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) viewDetail(a collector.AppStat, width int) string {
	rows := []string{
		titleStyle.Width(width).Render("▸ " + a.Name),
		kvRow("RSS", fmt.Sprintf("%d MB", a.RSSKB/1024), width),
		kvRow("Swap", fmt.Sprintf("%d MB", a.SwapKB/1024), width),
		kvRow("Procs", fmt.Sprintf("%d", a.ProcCount), width),
		kvRow("Mem%", fmt.Sprintf("%.1f%%", a.MemPct), width),
	}

	// Always reserve delta rows to prevent layout jitter
	dRSS, dSwap := m.delta(a.Name)
	if dRSS != 0 {
		rows = append(rows, fmtDelta("Δ RSS", dRSS, width))
	} else {
		rows = append(rows, strings.Repeat(" ", width))
	}
	if dSwap != 0 {
		rows = append(rows, fmtDelta("Δ Swap", dSwap, width))
	} else {
		rows = append(rows, strings.Repeat(" ", width))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m Model) viewSystem(width int) string {
	sys := m.snap.System

	rows := []string{
		sectionDivStyle.Width(width).Render("System"),
	}

	usedGB := float64(sys.MemUsedKB) / 1048576
	totalGB := float64(sys.MemTotalKB) / 1048576
	availGB := float64(sys.MemAvailableKB) / 1048576
	rows = append(rows,
		kvRow("RAM", fmt.Sprintf("%.1f / %.1f GB", usedGB, totalGB), width),
		kvDetail(fmt.Sprintf("%.1f GB avail", availGB), width),
	)

	swUsedGB := float64(sys.SwapUsedKB) / 1048576
	swTotalGB := float64(sys.SwapTotalKB) / 1048576
	swFreeGB := float64(sys.SwapFreeKB) / 1048576
	rows = append(rows,
		kvRow("Swap", fmt.Sprintf("%.1f / %.1f GB", swUsedGB, swTotalGB), width),
		kvDetail(fmt.Sprintf("%.1f GB free", swFreeGB), width),
	)

	if sys.ZswapPoolKB > 0 {
		poolGB := float64(sys.ZswapPoolKB) / 1048576
		dataGB := float64(sys.ZswapDataKB) / 1048576
		ratio := float64(sys.ZswapDataKB) / float64(sys.ZswapPoolKB)
		rows = append(rows,
			kvRow("Zswp", fmt.Sprintf("%.1f / %.1f GB", poolGB, dataGB), width),
			kvDetail(fmt.Sprintf("%.1fx ratio", ratio), width),
		)
	}

	if psi := m.snap.PSI; psi != nil {
		rows = append(rows,
			kvRow("PSI", fmt.Sprintf("some %.2f %.2f %.2f", psi.SomeAvg10, psi.SomeAvg60, psi.SomeAvg300), width),
			kvDetail(fmt.Sprintf("full %.2f %.2f %.2f", psi.FullAvg10, psi.FullAvg60, psi.FullAvg300), width),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m Model) viewOOM(width int) string {
	if len(m.snap.OOM) == 0 {
		return ""
	}

	t := lgtable.New().
		Headers("SCORE", "NAME", "RSS").
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderHeader(true).
		BorderColumn(false).
		BorderRow(false).
		BorderStyle(lipgloss.NewStyle().Foreground(colorSubtle)).
		Width(width).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(1)
			if row == lgtable.HeaderRow {
				return s.Foreground(colorMuted)
			}
			if col == 0 && row < len(m.snap.OOM) {
				c := colorForPct(float64(m.snap.OOM[row].Score) / 10)
				return s.Foreground(c)
			}
			return s
		})

	limit := min(len(m.snap.OOM), 5)
	for i := range limit {
		o := m.snap.OOM[i]
		t = t.Row(
			fmt.Sprintf("%d", o.Score),
			o.Name,
			fmt.Sprintf("%d MB", o.RSSKB/1024),
		)
	}

	header := sectionDivStyle.Width(width).Render("OOM Top")
	return lipgloss.JoinVertical(lipgloss.Left, header, t.Render())
}

func (m Model) viewRescue(width int) string {
	if m.snap == nil || len(m.snap.Apps) == 0 {
		return ""
	}
	header := sectionDivStyle.Width(width).Render("Rescue")
	top := m.snap.Apps[0]
	cmd := warnStyle.PaddingLeft(2).Width(width).Render(
		fmt.Sprintf("pkill -x -- %s", top.Name),
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, cmd)
}

func viewKeyHints(width int) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		dimStyle.Width(width).Render("↑↓ nav  ↵ fold  s sort"),
		dimStyle.Width(width).Render("/ filter  x kill  ? help"),
	)
}

func (m Model) viewStatus(w int) string {
	var left, right string

	if m.filtering {
		left = " / " + m.input.View()
	} else if m.status != "" {
		left = " " + m.status
	}

	if m.err != nil {
		right = lipgloss.NewStyle().Foreground(colorDanger).Render(m.err.Error())
	} else if m.snap != nil {
		right = m.snap.At.Format("15:04:05")
	}
	right += " "

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := max(w-leftW-rightW, 0)

	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) viewKillModal(rows []VisibleRow) string {
	if m.cursor >= len(rows) {
		return ""
	}
	a := rows[m.cursor].App

	content := fmt.Sprintf(
		"%s  Kill %s?\n\n"+
			"  Signal:  SIG%s\n"+
			"  Procs:   %d\n"+
			"  RSS:     %d MB\n"+
			"  Command: pkill -%s -x -- %s\n\n"+
			"  %s confirm    %s cancel",
		lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Render("⚠"),
		titleStyle.Render(a.Name),
		m.killSignal, a.ProcCount, a.RSSKB/1024,
		m.killSignal, a.Name,
		helpKeyStyle.Render("y"),
		helpKeyStyle.Render("n/esc"),
	)

	return modalStyle.Render(content)
}

func (m Model) viewHelpModal() string {
	help := titleStyle.Render("memhogs — memory hog monitor") + "\n\n" +
		sectionStyle.Render("Navigation") + "\n" +
		"  " + helpKeyStyle.Render("↑/k") + "  Move up\n" +
		"  " + helpKeyStyle.Render("↓/j") + "  Move down\n\n" +
		sectionStyle.Render("Actions") + "\n" +
		"  " + helpKeyStyle.Render("↵") + "    Fold/unfold group\n" +
		"  " + helpKeyStyle.Render("x") + "    Send SIGTERM to selected\n" +
		"  " + helpKeyStyle.Render("X") + "    Send SIGKILL to selected\n\n" +
		sectionStyle.Render("Display") + "\n" +
		"  " + helpKeyStyle.Render("s") + "    Cycle sort mode\n" +
		"  " + helpKeyStyle.Render("/") + "    Filter by name\n" +
		"  " + helpKeyStyle.Render("p") + "    Pause/resume refresh\n\n" +
		"  " + helpKeyStyle.Render("?") + "    Toggle help\n" +
		"  " + helpKeyStyle.Render("q") + "    Quit"

	return helpModalStyle.Render(help)
}

const kvLabelW = 6

func kvRow(label, value string, width int) string {
	l := labelStyle.Width(kvLabelW).Render(label)
	v := valueStyle.Width(max(width-kvLabelW-2, 1)).Render(value)
	return "  " + l + v
}

func kvDetail(detail string, width int) string {
	return dimStyle.Width(width).PaddingLeft(2 + kvLabelW).Render(detail)
}

func fmtDelta(label string, kb int64, width int) string {
	mb := kb / 1024
	style := deltaDownStyle
	text := fmt.Sprintf("%d MB", mb)
	if mb > 0 {
		style = deltaUpStyle
		text = fmt.Sprintf("+%d MB", mb)
	}
	l := style.Width(kvLabelW).Render(label)
	v := style.Width(max(width-kvLabelW-2, 1)).Render(text)
	return "  " + l + v
}
