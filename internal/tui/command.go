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
	lines := []string{"Selection: " + appLabel(row.App), "Scope: " + killScope(row), ""}
	for _, child := range children {
		lines = append(lines, fmt.Sprintf("PID %d · %s", child.PID, processSummary(row.App.Name, child)))
		if session := child.Session; session != nil {
			lines = append(lines, "Session: "+displayText(session.ID), "Name: "+displayText(session.Name),
				"Spawn dir (session): "+displayText(session.ProjectDir), "Entrypoint: "+displayText(session.Entrypoint))
		}
		lines = append(lines, processContextLines(child)...)
		directoryLabel := "Directory: "
		if row.App.Name == "claude" {
			directoryLabel = "Current dir: "
		}
		lines = append(lines, directoryLabel+displayText(child.Cwd), "Terminal: "+displayText(child.Terminal), displayText(child.Cmdline), "")
	}
	m.commandText = strings.Join(lines, "\n")
	m.commandTitle = appLabel(row.App)
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

func processContextLines(p collector.ProcDetail) []string {
	lines := []string{
		fmt.Sprintf("Parent PID: %d", p.ParentPID), "Executable: " + displayText(p.Executable),
		"Account owner: " + displayText(processAccount(p)),
	}
	if p.Project != nil {
		lines = append(lines, "Project: "+displayText(p.Project.Directory), "Project source: "+displayText(p.Project.Source), fmt.Sprintf("Project owner PID: %d", p.Project.Owner.PID))
	}
	if ctx := p.Context; ctx != nil {
		lines = append(lines, fmt.Sprintf("Application: %s [PID %d]", displayText(ctx.Application), ctx.ApplicationPID),
			"Application source: "+displayText(ctx.ApplicationSource),
			"Context directory: "+displayText(ctx.ProjectDir), "Directory source: "+displayText(ctx.ProjectSource))
		if ctx.ApplicationKey != "" {
			lines = append(lines, "Application package: "+displayText(ctx.ApplicationKey))
		}
		if ctx.PackagePath != "" {
			lines = append(lines, "Process package: "+displayText(ctx.PackagePath))
		}
		if ctx.Origin != "" {
			lines = append(lines, fmt.Sprintf("Launched via: %s [PID %d]", displayText(ctx.Origin), ctx.OriginPID))
		}
		for _, a := range ctx.Ancestors {
			lines = append(lines, fmt.Sprintf("Ancestor: %s [PID %d] · %s", displayText(a.Name), a.PID, displayText(a.Cwd)))
		}
		for _, resource := range ctx.Resources {
			lines = append(lines, "Open resource: "+displayText(resource))
		}
	}
	return lines
}
