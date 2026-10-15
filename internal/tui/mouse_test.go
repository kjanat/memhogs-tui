package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMouseInputCannotResizeBehindModalsOrFilter(t *testing.T) {
	for _, mode := range []string{"loading", "help", "kill", "command", "filter"} {
		m := draggableModel()
		switch mode {
		case "loading":
			m.snap = nil
		case "help":
			m.help = true
		case "kill":
			m.killConfirm = true
		case "command":
			m.commandOpen = true
		case "filter":
			m.filtering = true
		}
		m, _ = updateModel(m, tea.MouseClickMsg{X: 18, Y: 1, Button: tea.MouseLeft})
		require(t, !m.columnDrag.active && m.columns == (columnWidths{}) && !m.mouseEnabled(), "%s accepted a hidden table drag", mode)
	}
	m := draggableModel()
	require(t, m.View().MouseMode == tea.MouseModeCellMotion, "view did not enable terminal drag reporting")
}

func TestMouseSelectionAndWheelRespectTableAndSidebar(t *testing.T) {
	m := draggableModel()
	m, _ = updateModel(m, tea.MouseClickMsg{X: 2, Y: 6, Button: tea.MouseLeft})
	require(t, m.cursor == 3 && !m.sideFocused, "click did not select process row")
	m, _ = updateModel(m, tea.MouseWheelMsg{X: 2, Y: 6, Button: tea.MouseWheelUp})
	require(t, m.cursor == 0, "wheel did not navigate table")
	m, _ = updateModel(m, tea.MouseClickMsg{X: m.width - 2, Y: 5, Button: tea.MouseLeft})
	require(t, m.sideFocused, "sidebar click did not focus details")
	m, _ = updateModel(m, tea.MouseWheelMsg{X: m.width - 2, Y: 5, Button: tea.MouseWheelDown})
	require(t, m.sideOffset > 0 && m.cursor == 0, "sidebar wheel moved table selection")
	m.width = 80 // full-width focused details cover the hidden table header
	m, _ = updateModel(m, tea.MouseClickMsg{X: 18, Y: 1, Button: tea.MouseLeft})
	require(t, !m.columnDrag.active, "hidden table intercepted sidebar click")
}

func TestDragCancelsOnKeyboardResizeAndLostButton(t *testing.T) {
	for _, msg := range []tea.Msg{press('p'), tea.WindowSizeMsg{Width: 180, Height: 30}, tea.MouseMotionMsg{X: 20, Y: 1, Button: tea.MouseNone}} {
		m := draggableModel()
		x := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth()).widths.boundary(columnName)
		m, _ = updateModel(m, tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
		require(t, m.columnDrag.active, "header did not start drag")
		m, _ = updateModel(m, msg)
		require(t, !m.columnDrag.active, "drag remained active after interruption")
	}
}
