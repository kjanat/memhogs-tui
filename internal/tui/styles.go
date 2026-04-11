package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorSubtle = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#444444"}
	colorAccent = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}
	colorDim    = lipgloss.AdaptiveColor{Light: "#BBBBBB", Dark: "#444444"}

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

	selectedStyle = lipgloss.NewStyle().
			Bold(true)

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

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	deltaUpStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	deltaDownStyle = lipgloss.NewStyle().
			Foreground(colorSafe)
)

func colorForPct(pct float64) lipgloss.TerminalColor {
	switch {
	case pct > 10:
		return colorDanger
	case pct > 5:
		return colorWarning
	default:
		return colorSafe
	}
}
