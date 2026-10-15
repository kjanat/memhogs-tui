package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func draggableModel() Model {
	m := nestedModel()
	m.width, m.height = 220, 24
	return m
}

func dragTestColumn(m Model, column tableColumn, delta int) Model {
	layout := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	x := layout.widths.boundary(column)
	m, _ = updateModel(m, tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	m, _ = updateModel(m, tea.MouseMotionMsg{X: x + delta, Y: 5, Button: tea.MouseLeft})
	m, _ = updateModel(m, tea.MouseReleaseMsg{X: x + delta, Y: 5, Button: tea.MouseLeft})
	return m
}

func TestMouseDragChangesRenderedColumnsAndRetainsSelection(t *testing.T) {
	m := draggableModel()
	m.cursor = 3
	before := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	m = dragTestColumn(m, columnName, 12)
	after := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	require(t, after.widths[columnName] == before.widths[columnName]+12, "name drag did not move its boundary")
	require(t, !m.columnDrag.active && m.cursor == 3, "drag changed selection or remained active")
	assertColumnAlignment(t, m)
	chosen := m.columns
	m, _ = updateModel(m, press('s'))
	m = m.applySnapshot(&collector.Snapshot{Apps: m.snap.Apps})
	require(t, m.columns == chosen, "sorting or refresh reset chosen widths")
	m, _ = updateModel(m, tea.MouseMotionMsg{X: 5, Y: 1, Button: tea.MouseLeft})
	require(t, m.columns == chosen, "motion after release resized columns")
}

func assertColumnAlignment(t *testing.T, m Model) {
	t.Helper()
	rows := m.visibleRows(m.sortedApps())
	layout := m.tableLayout(rows, m.tableWidth())
	lines := strings.Split(ansi.Strip(m.viewTable(rows, m.tableWidth(), m.height-1)), "\n")
	for i, row := range rows {
		line := lines[i+3]
		cells := m.rowCells(row)
		for column := columnRSS; column < columnCount; column++ {
			end := layout.widths.boundary(column)
			start := end - layout.widths[column]
			got := strings.TrimSpace(ansi.Cut(line, start, end))
			require(t, got == cells[column], "row %d column %d misaligned: got %q, want %q", i, column, got, cells[column])
		}
		require(t, ansi.StringWidth(line) <= m.tableWidth(), "row escaped table bounds")
	}
}

func TestAllColumnBoundariesResizeAndClamp(t *testing.T) {
	for column := range columnCount {
		m := draggableModel()
		before := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
		m = dragTestColumn(m, column, 4)
		require(t, m.columns[column] == before.widths[column]+4, "column %d cannot be dragged", column)
		assertColumnAlignment(t, m)
		m = dragTestColumn(m, column, -10000)
		require(t, m.columns[column] == minimumColumns[column], "column %d shrank below readable minimum", column)
		m = dragTestColumn(m, column, 10000)
		effective := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
		require(t, effective.widths.total() < m.tableWidth(), "drag hid the final header grip")
	}
}

func TestColumnWidthsSurviveResizeAndResetToAutomatic(t *testing.T) {
	m := dragTestColumn(draggableModel(), columnName, 20)
	chosen := m.columns
	m, _ = updateModel(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	compressed := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	require(t, compressed.widths.total() <= m.tableWidth() && m.columns == chosen, "small terminal lost widths or overflowed")
	m, _ = updateModel(m, tea.WindowSizeMsg{Width: 220, Height: 24})
	restored := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	require(t, restored.widths == chosen, "enlarging terminal did not restore chosen widths")
	m, _ = updateModel(m, press('r'))
	require(t, m.columns == (columnWidths{}), "keyboard reset did not restore automatic sizing")
	m = dragTestColumn(m, columnName, 8)
	m, _ = updateModel(m, tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseRight})
	require(t, m.columns == (columnWidths{}), "right-click header did not reset widths")
}

func TestLargeCountersAndUnicodeRemainAligned(t *testing.T) {
	m := draggableModel()
	m.snap.Apps[0].RSSKB = 123456789 * 1024
	m.snap.Apps[0].CPUPct, m.snap.Apps[0].CPUKnown = 123456.7, true
	m.snap.Apps[0].Name = "日本語 👩‍💻 é"
	assertColumnAlignment(t, m)
	m = dragTestColumn(m, columnRSS, 2)
	assertColumnAlignment(t, m)
}
