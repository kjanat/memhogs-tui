package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"memhogs.kjanat.dev/internal/collector"
)

func (m Model) openCommand() Model {
	row, ok := m.currentRow(m.visibleRows(m.sortedApps()))
	if !ok {
		return m
	}
	children := row.App.Children
	if row.Kind == RowChild {
		children = []collector.ProcDetail{row.Child}
	}
	var lines []string
	for _, child := range children {
		lines = append(lines, fmt.Sprintf("PID %d · %s", child.PID, processSummary(row.App.Name, child)))
		if session := child.Session; session != nil {
			lines = append(lines, "Session: "+displayText(session.ID), "Name: "+displayText(session.Name),
				"Spawn dir (session): "+displayText(session.ProjectDir), "Entrypoint: "+displayText(session.Entrypoint))
		}
		directoryLabel := "Directory: "
		if row.App.Name == "claude" {
			directoryLabel = "Current dir: "
		}
		lines = append(lines, directoryLabel+displayText(child.Cwd), "Terminal: "+displayText(child.Terminal), displayText(child.Cmdline), "")
	}
	m.commandText = strings.Join(lines, "\n")
	m.commandTitle = displayText(row.App.Name)
	m.command = viewport.New(viewport.WithWidth(max(m.width-4, 1)), viewport.WithHeight(max(m.height-4, 1)))
	m.command.SoftWrap = true
	m.command.SetContent(m.commandText)
	m.commandOpen = true
	return m
}

func (m Model) viewCommand() string {
	header := titleStyle.Render(" " + m.commandTitle + " · full command")
	footer := dimStyle.Render(" ↑↓ / PgUp PgDn scroll · c / Esc close")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.command.View(), footer)
}
