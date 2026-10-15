package tui

import tea "charm.land/bubbletea/v2"

func (m Model) mouseEnabled() bool {
	if m.snap == nil || m.width <= 0 || m.height <= 0 {
		return false
	}
	return !m.help && !m.killConfirm && !m.commandOpen && !m.filtering
}

func (m Model) tableMouseVisible() bool {
	return m.sidebarWidth() > 0 || !m.sideFocused
}

func (m Model) tableWidth() int {
	return m.width - m.sidebarWidth()
}

func (m Model) headerHit(mouse tea.Mouse) bool {
	return m.tableMouseVisible() && (mouse.Y == 1 || mouse.Y == 2) && mouse.X >= 0 && mouse.X < m.tableWidth()
}

func (m Model) mouseInside(mouse tea.Mouse) bool {
	return mouse.X >= 0 && mouse.X < m.width && mouse.Y >= 0 && mouse.Y < m.height-1
}

func (m Model) handleMouse(msg tea.MouseMsg) Model {
	if !m.mouseEnabled() {
		m.columnDrag = columnDrag{}
		return m
	}
	before, selected := m.currentRow(m.visibleRows(m.sortedApps()))
	switch event := msg.(type) {
	case tea.MouseClickMsg:
		m = m.mouseClick(event.Mouse())
	case tea.MouseMotionMsg:
		if event.Button == tea.MouseLeft {
			m = m.dragColumn(event.X)
		} else {
			m.columnDrag = columnDrag{}
		}
	case tea.MouseReleaseMsg:
		m = m.dragColumn(event.X)
		m.columnDrag = columnDrag{}
	case tea.MouseWheelMsg:
		m = m.mouseWheel(event.Mouse())
	}
	return m.resetSideSelection(before, selected)
}

func (m Model) mouseClick(mouse tea.Mouse) Model {
	m.columnDrag = columnDrag{}
	if m.headerHit(mouse) {
		switch mouse.Button {
		case tea.MouseRight:
			return m.resetColumns()
		case tea.MouseLeft:
			return m.startColumnDrag(mouse.X)
		default:
			return m
		}
	}
	if mouse.Button != tea.MouseLeft || !m.mouseInside(mouse) {
		return m
	}
	if !m.tableMouseVisible() || mouse.X >= m.tableWidth() {
		m.sideFocused = true
		return m
	}
	return m.selectMouseRow(mouse.Y)
}

func (m Model) selectMouseRow(y int) Model {
	if y < 3 {
		return m
	}
	rows := m.visibleRows(m.sortedApps())
	start := viewportStart(m.cursor, m.offset, len(rows), max(m.height-4, 0))
	row := start + y - 3
	if row < len(rows) {
		m.cursor, m.sideFocused = row, false
	}
	return m
}

func (m Model) startColumnDrag(x int) Model {
	layout := m.tableLayout(m.visibleRows(m.sortedApps()), m.tableWidth())
	for column := range columnCount {
		boundary := layout.widths.boundary(column)
		if max(x-boundary, boundary-x) <= 1 {
			m.columns = layout.widths
			m.columnDrag = columnDrag{active: true, column: column, x: x, widths: layout.widths}
			break
		}
	}
	return m
}

func (m Model) dragColumn(x int) Model {
	if !m.columnDrag.active {
		return m
	}
	drag := m.columnDrag
	other := drag.widths.total() - drag.widths[drag.column]
	limit := max(m.tableWidth()-1-other, minimumColumns[drag.column])
	m.columns = drag.widths
	m.columns[drag.column] = min(max(drag.widths[drag.column]+x-drag.x, minimumColumns[drag.column]), limit)
	return m
}

func (m Model) mouseWheel(mouse tea.Mouse) Model {
	if !m.mouseInside(mouse) {
		return m
	}
	var delta int
	switch mouse.Button {
	case tea.MouseWheelUp:
		delta = -3
	case tea.MouseWheelDown:
		delta = 3
	default:
		return m
	}
	rows := m.visibleRows(m.sortedApps())
	if !m.tableMouseVisible() || mouse.X >= m.tableWidth() {
		width := m.width
		if side := m.sidebarWidth(); side > 0 {
			width = side - sidebarStyle.GetHorizontalFrameSize()
		}
		body, _, capacity := m.sidebarParts(rows, width, m.height-1)
		m.sideOffset = min(max(m.sideOffset+delta, 0), max(len(body)-capacity, 0))
	} else {
		m.cursor = min(max(m.cursor+delta, 0), max(len(rows)-1, 0))
	}
	return m
}
