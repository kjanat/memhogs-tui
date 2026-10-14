package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"memhogs.kjanat.dev/internal/collector"
)

func TestTablePageNavigation(t *testing.T) {
	pids := make([]int, 40)
	for i := range pids {
		pids[i] = i + 1
	}
	m := testModel(testApp("workers", 40960, pids...))
	m.expanded[testGroupID("workers")] = true
	for _, tt := range []struct {
		code   rune
		cursor int
	}{
		{tea.KeyPgDown, 6},
		{tea.KeyPgDown, 12},
		{tea.KeyPgUp, 6},
		{tea.KeyHome, 0},
		{tea.KeyPgUp, 0},
		{tea.KeyEnd, 40},
		{tea.KeyPgDown, 40},
		{tea.KeyPgUp, 34},
		{tea.KeyHome, 0},
	} {
		m, _ = updateModel(m, tea.KeyPressMsg{Code: tt.code})
		if m.cursor != tt.cursor {
			t.Fatalf("%v: cursor=%d, want %d", tt.code, m.cursor, tt.cursor)
		}
		require(t, m.offset <= m.cursor && m.cursor < m.offset+6, "selected row outside viewport")
	}
}

func TestPageNavigationBounds(t *testing.T) {
	for _, height := range []int{1, 4, 24} {
		m := testModel(testApp("a", 4096, 1), testApp("b", 2048, 2))
		m.height = height
		m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
		require(t, m.cursor == 1, "page navigation must move at least one row and clamp")
		m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
		require(t, m.cursor == 0, "page up must clamp at first row")
		m.snap.Apps = nil
		m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnd})
		require(t, m.cursor == 0 && m.offset == 0, "empty table has invalid cursor")
	}
}

func TestSidebarNavigationKeepsSelection(t *testing.T) {
	m := contextModel()
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyTab})
	require(t, m.sideFocused, "Tab must focus details")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	require(t, m.sideOffset > 0 && m.cursor == 0 && m.offset == 0, "sidebar paging changed process selection")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	end := m.sideOffset
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	require(t, m.sideOffset == end, "sidebar scrolled beyond last page")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyHome})
	require(t, m.sideOffset == 0, "Home must return to process context")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	require(t, m.commandOpen, "Enter in details must open full inspector")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	require(t, !m.commandOpen && m.sideFocused, "closing inspector lost details focus")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	require(t, !m.sideFocused, "Escape must return to table")
}

func TestSidebarOffsetFollowsProcessIdentity(t *testing.T) {
	m := contextModel()
	m.snap.Apps = append(m.snap.Apps, testApp("other", 1024, 200))
	m.sideOffset = 8
	first := m.snap.Apps[0]
	other := m.snap.Apps[1]
	other.RSSKB = first.RSSKB + 1
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{other, first}}})
	require(t, m.cursor == 1 && m.sideOffset == 8, "refresh reorder reset the same process's context")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyUp})
	require(t, m.cursor == 0 && m.sideOffset == 0, "changing selection must start new context at top")
}
