package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kjanat/memhogs-tui/internal/collector"
)

// View renders the full UI.
func (m Model) View() string {
	if m.width == 0 || m.snap == nil {
		return "  Collecting…"
	}

	apps := m.sortedApps()

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

	table := m.viewTable(apps, tableW, mainH)
	tablePane := lipgloss.NewStyle().Width(tableW).Height(mainH).Render(table)

	var body string
	if hasSide {
		side := m.viewSide(apps, sideW-2, mainH) // -2 for border+pad
		sidePane := sidebarStyle.Width(sideW).Height(mainH).Render(side)
		body = lipgloss.JoinHorizontal(lipgloss.Top, tablePane, sidePane)
	} else {
		body = tablePane
	}

	status := m.viewStatus(m.width)
	out := body + "\n" + status

	// Modal overlays replace the whole screen
	if m.killConfirm {
		modal := m.viewKillModal(apps)
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}
	if m.help {
		modal := m.viewHelpModal()
		out = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	return out
}

// --- app table ---

func (m Model) viewTable(apps []collector.AppStat, w, h int) string {
	nameW := 16
	for _, a := range apps {
		if n := len(a.Name); n > nameW && n <= 24 {
			nameW = n
		}
	}

	// prefix(2) + name + rss(8) + swap(8) + procs(6) + pct(7) + gap(1)
	fixed := 2 + 8 + 8 + 6 + 7 + 1
	barW := w - nameW - fixed
	if barW < 0 {
		barW = 0
	}

	var maxRSS int64
	for _, a := range apps {
		if a.RSSKB > maxRSS {
			maxRSS = a.RSSKB
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

	// Header
	hdr := fmt.Sprintf("  %-*s %7s %7s %5s %6s", nameW, "NAME", "RSS", "SWAP", "PROCS", "MEM%")
	b.WriteString(headerStyle.Render(hdr))
	b.WriteByte('\n')

	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')

	maxRows := h - 3
	if maxRows < 0 {
		maxRows = 0
	}

	for i, a := range apps {
		if i >= maxRows {
			break
		}

		name := a.Name
		if len(name) > nameW {
			name = name[:nameW-1] + "…"
		}

		pfx := "  "
		if i == m.cursor {
			pfx = "▸ "
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
		b.WriteByte('\n')
	}

	if len(apps) == 0 {
		b.WriteString(dimStyle.Render("  (no processes)"))
		b.WriteByte('\n')
	}

	return b.String()
}

// --- sidebar ---

func (m Model) viewSide(apps []collector.AppStat, w, h int) string {
	var b strings.Builder

	// Selected app detail
	if m.cursor < len(apps) {
		a := apps[m.cursor]
		b.WriteString(titleStyle.Render("▸ " + a.Name))
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("  RSS   %5d MB\n", a.RSSKB/1024))
		b.WriteString(fmt.Sprintf("  Swap  %5d MB\n", a.SwapKB/1024))
		b.WriteString(fmt.Sprintf("  Procs %5d\n", a.ProcCount))
		b.WriteString(fmt.Sprintf("  Mem%%  %5.1f%%\n", a.MemPct))

		if m.prevSnap != nil {
			dRSS, dSwap := m.delta(a.Name)
			if dRSS != 0 {
				b.WriteString(fmtDelta("  Δ RSS ", dRSS))
				b.WriteByte('\n')
			}
			if dSwap != 0 {
				b.WriteString(fmtDelta("  Δ Swap", dSwap))
				b.WriteByte('\n')
			}
		}
	}

	b.WriteByte('\n')

	// System info
	sys := m.snap.System
	b.WriteString(sectionStyle.Render("── System ──"))
	b.WriteByte('\n')

	usedGB := float64(sys.MemUsedKB) / 1048576
	totalGB := float64(sys.MemTotalKB) / 1048576
	availGB := float64(sys.MemAvailableKB) / 1048576
	b.WriteString(fmt.Sprintf("  RAM  %4.1f/%4.1f GB\n", usedGB, totalGB))
	b.WriteString(dimStyle.Render(fmt.Sprintf("       %4.1f GB avail", availGB)))
	b.WriteByte('\n')

	swUsedGB := float64(sys.SwapUsedKB) / 1048576
	swTotalGB := float64(sys.SwapTotalKB) / 1048576
	swFreeGB := float64(sys.SwapFreeKB) / 1048576
	b.WriteString(fmt.Sprintf("  Swap %4.1f/%4.1f GB\n", swUsedGB, swTotalGB))
	b.WriteString(dimStyle.Render(fmt.Sprintf("       %4.1f GB free", swFreeGB)))
	b.WriteByte('\n')

	if sys.ZswapPoolKB > 0 {
		poolMB := sys.ZswapPoolKB / 1024
		dataMB := sys.ZswapDataKB / 1024
		ratio := 0.0
		if sys.ZswapPoolKB > 0 {
			ratio = float64(sys.ZswapDataKB) / float64(sys.ZswapPoolKB)
		}
		b.WriteString(fmt.Sprintf("  Zswap %d→%dMB %.1fx\n", poolMB, dataMB, ratio))
	}

	if psi := m.snap.PSI; psi != nil {
		b.WriteString(fmt.Sprintf("  PSI some %5.2f %5.2f\n", psi.SomeAvg10, psi.SomeAvg60))
		b.WriteString(fmt.Sprintf("  PSI full %5.2f %5.2f\n", psi.FullAvg10, psi.FullAvg60))
	}

	b.WriteByte('\n')

	// OOM targets
	if len(m.snap.OOM) > 0 {
		b.WriteString(sectionStyle.Render("── OOM Top ──"))
		b.WriteByte('\n')

		for i, o := range m.snap.OOM {
			if i >= 5 {
				break
			}
			name := o.Name
			if len(name) > 14 {
				name = name[:13] + "…"
			}
			b.WriteString(fmt.Sprintf("  %4d %-14s %4dMB\n", o.Score, name, o.RSSKB/1024))
		}
		b.WriteByte('\n')
	}

	// Key hints
	b.WriteString(dimStyle.Render("↑↓ nav  s sort  / filter"))
	b.WriteByte('\n')
	b.WriteString(dimStyle.Render("d kill  p pause  ? help"))

	return b.String()
}

// --- status bar ---

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
	gap := w - leftW - rightW
	if gap < 0 {
		gap = 0
	}

	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

// --- modals ---

func (m Model) viewKillModal(apps []collector.AppStat) string {
	if m.cursor >= len(apps) {
		return ""
	}
	a := apps[m.cursor]

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
		"  " + helpKeyStyle.Render("d") + "    Send SIGTERM to selected\n" +
		"  " + helpKeyStyle.Render("D") + "    Send SIGKILL to selected\n\n" +
		sectionStyle.Render("Display") + "\n" +
		"  " + helpKeyStyle.Render("s") + "    Cycle sort mode\n" +
		"  " + helpKeyStyle.Render("/") + "    Filter by name\n" +
		"  " + helpKeyStyle.Render("p") + "    Pause/resume refresh\n\n" +
		"  " + helpKeyStyle.Render("?") + "    Toggle help\n" +
		"  " + helpKeyStyle.Render("q") + "    Quit"

	return helpModalStyle.Render(help)
}

// --- helpers ---

func fmtDelta(prefix string, kb int64) string {
	mb := kb / 1024
	if mb > 0 {
		return deltaUpStyle.Render(fmt.Sprintf("%s +%dMB", prefix, mb))
	}
	return deltaDownStyle.Render(fmt.Sprintf("%s %dMB", prefix, mb))
}
