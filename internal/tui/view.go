package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	lgtable "charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

// View renders the full-screen UI and enables the alternate screen buffer.
// It composes a left-side application table, an optional right-side detail
// sidebar, and a bottom status bar.
// Kill-confirm and help modals replace the entire screen when active.
func (m Model) View() tea.View {
	if m.width == 0 || m.snap == nil {
		content := "  Collecting…"
		if m.err != nil {
			content = "  Collection failed: " + displayText(m.err.Error()) + "\n  q quit • retrying each interval"
		}
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}

	if m.commandOpen {
		v := tea.NewView(m.viewCommand())
		v.AltScreen = true
		return v
	}

	apps := m.sortedApps()
	rows := m.visibleRows(apps)

	// Layout: sidebar appears at ≥90 cols
	hasSide := m.width >= 90
	mainH := max(m.height-1, 0) // 1 for status bar

	var sideW, tableW int
	if hasSide {
		sideW = min(m.width*3/10, 36)
		sideW = max(sideW, 28)
		tableW = m.width - sideW
	} else {
		tableW = m.width
	}

	table := m.viewTable(rows, tableW, mainH)
	tablePane := lipgloss.NewStyle().Width(tableW).Height(mainH).MaxHeight(mainH).Render(table)

	var body string
	if hasSide {
		side := m.viewSide(rows, sideW-2)
		sidePane := sidebarStyle.Width(sideW).Height(mainH).MaxHeight(mainH).Render(side)
		body = lipgloss.JoinHorizontal(lipgloss.Top, tablePane, sidePane)
	} else {
		body = tablePane
	}

	status := m.viewStatus(m.width)
	out := body + "\n" + status

	// Modal overlays replace the whole screen
	if m.killConfirm {
		modal := m.viewKillModal()
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}
	if m.help && !m.killConfirm {
		modal := m.viewHelpModal()
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

// tableFixedW is the non-name, non-bar column budget:
// prefix(2) + rss(8) + swap(8) + procs/PID(10) + mem%(7) + cpu%(8) + gap(1).
const tableFixedW = 2 + 8 + 8 + 10 + 7 + 8 + 1

func (m Model) viewTable(rows []VisibleRow, w, h int) string {
	nameW, barW, maxRSS := tableLayout(rows, w)

	var b strings.Builder
	b.WriteString(titleStyle.Render(m.tableTitle()))
	b.WriteByte('\n')

	hdr := fmt.Sprintf("  %-*s %7s %7s %9s %6s %7s", nameW, "NAME / ROLE", "RSS", strings.ToUpper(m.secondaryLabel()), "PROCS/PID", "MEM%", "CPU%")
	b.WriteString(headerStyle.Render(hdr))
	b.WriteByte('\n')
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')

	maxVisible := max(h-3, 0)
	start := viewportStart(m.cursor, m.offset, len(rows), maxVisible)
	for i := start; i < min(start+maxVisible, len(rows)); i++ {
		r := rows[i]
		selected := i == m.cursor
		switch r.Kind {
		case RowGroup:
			b.WriteString(m.renderGroupRow(r.App, nameW, barW, maxRSS, selected))
		case RowChild:
			b.WriteString(m.renderChildRow(r.App.Name, r.Child, nameW, selected))
		}
		b.WriteByte('\n')
	}

	if len(rows) == 0 {
		b.WriteString(dimStyle.Render("  (no processes)"))
		b.WriteByte('\n')
	}

	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	lines = lines[:min(len(lines), max(h, 0))]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(w, 0), "")
	}
	return strings.Join(lines, "\n")
}

// tableTitle builds the header line, appending the active filter and pause
// indicator when set.
func (m Model) tableTitle() string {
	sortLabel := m.sort.String()
	if m.sort == SortSwap {
		sortLabel = m.secondaryLabel()
	}
	title := fmt.Sprintf(" Applications  sort:%s", sortLabel)
	if m.filter != "" {
		title += "  /" + displayText(m.filter)
	}
	if m.paused {
		title += "  ⏸"
	}
	return title
}

// tableLayout computes the name-column width, RSS-bar width, and the largest
// group RSS used to scale the bars.
func tableLayout(rows []VisibleRow, w int) (nameW, barW int, maxRSS int64) {
	nameW = 16
	for _, r := range rows {
		label := r.App.Name
		if r.Kind == RowChild {
			label = processSummary(r.App.Name, r.Child)
		}
		nameW = max(nameW, lipgloss.Width(displayText(label)))
		if r.Kind == RowGroup && r.App.RSSKB > maxRSS {
			maxRSS = r.App.RSSKB
		}
	}
	nameW = min(nameW, max((w-tableFixedW)/2, 16))
	barW = min(max(w-nameW-tableFixedW, 0), w/3)
	return nameW, barW, maxRSS
}

// renderGroupRow formats one aggregated application row, including the fold
// indicator and the RSS bar scaled against maxRSS.
func (m Model) renderGroupRow(a collector.AppStat, nameW, barW int, maxRSS int64, selected bool) string {
	name := fitText(a.Name, nameW)

	pfx := "  "
	if a.ProcCount > 1 {
		pfx = "▸ "
		if m.expanded[a.Name] {
			pfx = "▾ "
		}
	}

	row := fmt.Sprintf("%s%s %4dMiB %7s %9d %5.1f%% %7s",
		pfx, name,
		a.RSSKB/1024, m.appSecondaryText(a),
		a.ProcCount, a.MemPct, cpuText(a.CPUPct, a.CPUKnown))

	if barW > 0 && maxRSS > 0 {
		bLen := int(float64(a.RSSKB) / float64(maxRSS) * float64(barW))
		if a.RSSKB > 0 && bLen < 1 {
			bLen = 1
		}
		bar := lipgloss.NewStyle().Foreground(colorForPct(a.MemPct)).Render(strings.Repeat("█", bLen))
		row += " " + bar
	}

	if selected {
		return selectedStyle.Render(row)
	}
	return row
}

// renderChildRow aligns the process role, memory counters, and PID with
// group columns. Full launch arguments remain accessible with c.
func (m Model) renderChildRow(app string, child collector.ProcDetail, nameW int, selected bool) string {
	var memPct float64
	if total := m.snap.System.MemTotalKB; total > 0 {
		memPct = float64(child.RSSKB) / float64(total) * 100
	}
	row := fmt.Sprintf("↳ %s %4dMiB %7s %9d %5.1f%% %7s",
		fitText(processSummary(app, child), nameW), child.RSSKB/1024,
		m.childSecondaryText(child), child.PID, memPct, cpuText(child.CPUPct, child.CPUKnown))
	if selected {
		return selectedStyle.Render(row)
	}
	return dimStyle.Render(row)
}

func (m Model) viewSide(rows []VisibleRow, width int) string {
	sections := make([]string, 0, 6)

	if m.cursor < len(rows) {
		r := rows[m.cursor]
		if r.Kind == RowChild {
			sections = append(sections, m.viewDetailChild(r.App.Name, r.Child, width))
		} else {
			sections = append(sections, m.viewDetail(r.App, width))
		}
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
		titleStyle.Width(width).Render("▸ " + displayText(a.Name)),
		kvRow("RSS", fmt.Sprintf("%d MiB", a.RSSKB/1024), width),
		kvRow(m.secondaryLabel(), m.appSecondaryText(a), width),
		kvRow("Procs", fmt.Sprintf("%d", a.ProcCount), width),
		kvRow("Mem%", fmt.Sprintf("%.1f%%", a.MemPct), width),
		kvRow("CPU%", cpuText(a.CPUPct, a.CPUKnown), width),
	}

	// Always reserve delta rows to prevent layout jitter
	dRSS, dSwap := m.delta(a.Name)
	if dRSS != 0 {
		rows = append(rows, fmtDelta("Δ RSS", dRSS, width))
	} else {
		rows = append(rows, strings.Repeat(" ", width))
	}
	if dSwap != 0 && a.SwapKnown && !m.snap.ProcessCommit {
		rows = append(rows, fmtDelta("Δ Swap", dSwap, width))
	} else {
		rows = append(rows, strings.Repeat(" ", width))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// viewDetailChild renders the detail panel for a single highlighted process
// within an expanded group. Unlike viewDetail it shows the PID and command
// line; Mem% is computed against total RAM since ProcDetail carries no
// precomputed percentage, and per-PID deltas are not tracked.
func (m Model) viewDetailChild(name string, c collector.ProcDetail, width int) string {
	var memPct float64
	if total := m.snap.System.MemTotalKB; total > 0 {
		memPct = float64(c.RSSKB) / float64(total) * 100
	}

	// Reserve the last two rows (matching viewDetail's height) for the command
	// line. Truncate to a single line — wrapping a long cmdline would jitter
	// the layout as the cursor moves between rows.

	rows := make([]string, 0, 7)
	rows = append(rows,
		titleStyle.Width(width).Render(fmt.Sprintf("▸ %s [%d]", displayText(name), c.PID)),
		kvRow("RSS", fmt.Sprintf("%d MiB", c.RSSKB/1024), width),
		kvRow(m.secondaryLabel(), m.childSecondaryText(c), width),
		kvRow("Mem%", fmt.Sprintf("%.1f%%", memPct), width),
		kvRow("CPU%", cpuText(c.CPUPct, c.CPUKnown), width),
		sectionStyle.Width(width).Render("c: full command / directory"),
		dimStyle.Width(width).Render(fitText(processSummary(name, c), width)),
	)

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
		kvRow("RAM", fmt.Sprintf("%.1f / %.1f GiB", usedGB, totalGB), width),
		kvDetail(fmt.Sprintf("%.1f GiB avail", availGB), width),
	)

	if sys.SwapKnown {
		rows = append(rows,
			kvRow("Swap", fmt.Sprintf("%.1f / %.1f GiB", float64(sys.SwapUsedKB)/1048576, float64(sys.SwapTotalKB)/1048576), width),
			kvDetail(fmt.Sprintf("%.1f GiB free", float64(sys.SwapFreeKB)/1048576), width),
		)
	} else {
		rows = append(rows, kvRow("Swap", "unavailable", width))
	}
	if sys.CommitKnown {
		rows = append(rows,
			kvRow("Commit", fmt.Sprintf("%.1f GiB limit", float64(sys.CommitLimitKB)/1048576), width),
			kvDetail(fmt.Sprintf("%.1f GiB available", float64(sys.CommitAvailableKB)/1048576), width),
		)
	}

	if sys.ZswapPoolKB > 0 {
		poolGB := float64(sys.ZswapPoolKB) / 1048576
		dataGB := float64(sys.ZswapDataKB) / 1048576
		ratio := float64(sys.ZswapDataKB) / float64(sys.ZswapPoolKB)
		rows = append(rows,
			kvRow("Zswp", fmt.Sprintf("%.1f / %.1f GiB", poolGB, dataGB), width),
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
			displayText(o.Name),
			fmt.Sprintf("%d MiB", o.RSSKB/1024),
		)
	}

	header := sectionDivStyle.Width(width).Render("OOM Top")
	return lipgloss.JoinVertical(lipgloss.Left, header, t.Render())
}

func (m Model) viewRescue(width int) string {
	row, ok := m.currentRow(m.visibleRows(m.sortedApps()))
	if !ok {
		return ""
	}
	children := row.App.Children
	if row.Kind == RowChild {
		children = []collector.ProcDetail{row.Child}
	}
	header := sectionDivStyle.Width(width).Render("Rescue selected")
	var content string
	if len(children) == 1 {
		content = warnStyle.Width(width).Render(killCmdStr(children, false))
	} else {
		content = warnStyle.Width(width).Render(fmt.Sprintf("x: %s %d processes", killSignalName(false), len(children)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, content,
		dimStyle.Width(width).Render("x / X opens confirmation"))
}

func viewKeyHints(width int) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		dimStyle.Width(width).Render("↑↓ nav  ↵ fold  s sort"),
		dimStyle.Width(width).Render("/ filter  c command  x kill"),
	)
}

func (m Model) viewStatus(w int) string {
	var left, right string

	if m.filtering {
		left = " / " + m.input.View()
	} else if m.status != "" {
		left = " " + displayText(m.status)
	}

	if m.err != nil {
		right = lipgloss.NewStyle().Foreground(colorDanger).Render(displayText(m.err.Error()))
	} else if m.snap != nil {
		right = m.snap.At.Format("15:04:05")
	}
	right += " "

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := max(w-leftW-rightW, 0)

	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) viewKillModal() string {
	r := m.killTarget

	var title string
	var procs int
	var rssKB int64
	var cmd string
	if r.Kind == RowChild {
		title = fmt.Sprintf("%s [%d]", displayText(r.App.Name), r.Child.PID)
		procs = 1
		rssKB = r.Child.RSSKB
		cmd = killCmdStr([]collector.ProcDetail{r.Child}, m.killForce)
	} else {
		title = displayText(r.App.Name)
		procs = len(r.App.Children)
		rssKB = r.App.RSSKB
		cmd = killCmdStr(r.App.Children, m.killForce)
	}

	content := fmt.Sprintf(
		"%s  Kill %s?\n\n"+
			"  Signal:  %s\n"+
			"  Procs:   %d\n"+
			"  RSS:     %d MiB\n"+
			"  Command: %s\n\n"+
			"  %s confirm    %s cancel",
		lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Render("⚠"),
		titleStyle.Render(title),
		killSignalName(m.killForce), procs, rssKB/1024,
		cmd,
		helpKeyStyle.Render("y"),
		helpKeyStyle.Render("n/esc"),
	)

	return modalStyle.Width(max(m.width-2, 1)).Render(content)
}

func (m Model) viewHelpModal() string {
	help := titleStyle.Render("memhogs — memory hog monitor") + "\n\n" +
		sectionStyle.Render("Navigation") + "\n" +
		"  " + helpKeyStyle.Render("↑/k") + "  Move up\n" +
		"  " + helpKeyStyle.Render("↓/j") + "  Move down\n\n" +
		sectionStyle.Render("Actions") + "\n" +
		"  " + helpKeyStyle.Render("↵") + "    Fold/unfold group\n" +
		"  " + helpKeyStyle.Render("c") + "    Inspect full command(s)\n" +
		"  " + helpKeyStyle.Render("x") + "    " + killSignalName(false) + " selected\n" +
		"  " + helpKeyStyle.Render("X") + "    " + killSignalName(true) + " selected\n\n" +
		sectionStyle.Render("Display") + "\n" +
		"  " + helpKeyStyle.Render("s") + "    Cycle sort mode\n" +
		"  " + helpKeyStyle.Render("/") + "    Filter name/command\n" +
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
	text := fmt.Sprintf("%d MiB", mb)
	if mb > 0 {
		style = deltaUpStyle
		text = fmt.Sprintf("+%d MiB", mb)
	}
	l := style.Width(kvLabelW).Render(label)
	v := style.Width(max(width-kvLabelW-2, 1)).Render(text)
	return "  " + l + v
}

func memoryText(kb int64, known bool) string {
	if !known {
		return "n/a"
	}
	return fmt.Sprintf("%dMiB", kb/1024)
}

func (m Model) appSecondaryText(a collector.AppStat) string {
	if m.snap.ProcessCommit {
		return memoryText(a.CommitKB, a.CommitKnown)
	}
	return memoryText(a.SwapKB, a.SwapKnown)
}

func (m Model) childSecondaryText(c collector.ProcDetail) string {
	if m.snap.ProcessCommit {
		return memoryText(c.CommitKB, c.CommitKnown)
	}
	return memoryText(c.SwapKB, c.SwapKnown)
}
