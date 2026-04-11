package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var (
	colorSubtle = lipgloss.Color("#444444")
	colorAccent = lipgloss.Color("#7D56F4")
	colorMuted  = lipgloss.Color("#666666")
	colorDim    = lipgloss.Color("#444444")

	colorSafe    = lipgloss.Color("#44CC44")
	colorWarning = lipgloss.Color("#CCAA00")
	colorDanger  = lipgloss.Color("#CC4444")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMuted)

	sectionDivStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorSubtle)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("#1a1a2e"))

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	sidebarStyle = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeftForeground(colorSubtle).
			PaddingLeft(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorDanger).
			Padding(1, 2)

	helpModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	helpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc"))

	warnStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	deltaUpStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	deltaDownStyle = lipgloss.NewStyle().
			Foreground(colorSafe)
)

func colorForPct(pct float64) color.Color {
	switch {
	case pct > 10:
		return colorDanger
	case pct > 5:
		return colorWarning
	default:
		return colorSafe
	}
}
