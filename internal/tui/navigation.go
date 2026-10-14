package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func navigationOffset(msg tea.KeyPressMsg, current, page, limit int) (int, bool) {
	position := current
	switch {
	case key.Matches(msg, keys.Up):
		position--
	case key.Matches(msg, keys.Down):
		position++
	case key.Matches(msg, keys.PageUp):
		position -= page
	case key.Matches(msg, keys.PageDown):
		position += page
	case key.Matches(msg, keys.Home):
		position = 0
	case key.Matches(msg, keys.End):
		position = limit
	default:
		return current, false
	}
	return min(max(position, 0), limit), true
}

func (m Model) resetSideSelection(before VisibleRow, hadSelection bool) Model {
	after, selected := m.currentRow(m.visibleRows(m.sortedApps()))
	if hadSelection != selected || (selected && !sameRow(before, after)) {
		m.sideOffset = 0
	}
	return m
}

func (m Model) handleSideKey(msg tea.KeyPressMsg) (Model, bool) {
	if msg.String() == "tab" {
		m.sideFocused = !m.sideFocused
		return m, true
	}
	if !m.sideFocused {
		return m, false
	}
	if msg.Code == tea.KeyEscape {
		m.sideFocused = false
		return m, true
	}
	if msg.String() == "enter" {
		return m.openCommand(), true
	}
	return m.handleSideNavigation(msg)
}
