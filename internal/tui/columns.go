package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type tableColumn int

const (
	columnName tableColumn = iota
	columnRSS
	columnSwap
	columnPID
	columnMemory
	columnCPU
	columnCount
)

type (
	columnWidths [columnCount]int
	columnCells  [columnCount]string
)

var minimumColumns = columnWidths{18, 7, 7, 9, 6, 7}

type tableGeometry struct {
	widths columnWidths
	bar    int
	maxRSS int64
}

type columnDrag struct {
	active bool
	column tableColumn
	x      int
	widths columnWidths
}

func (widths columnWidths) total() int {
	total := int(columnCount) - 1 // one separator between columns
	for _, width := range widths {
		total += width
	}
	return total
}

func (widths columnWidths) boundary(column tableColumn) int {
	x := 0
	for i := tableColumn(0); i <= column; i++ {
		x += widths[i] + 1
	}
	return x - 1
}

func (widths columnWidths) fit(width int) columnWidths {
	excess := max(widths.total()-width, 0)
	for i := range widths {
		shrink := min(excess, widths[i]-minimumColumns[i])
		widths[i] -= shrink
		excess -= shrink
	}
	return widths
}

func (m Model) tableLayout(rows []VisibleRow, width int) tableGeometry {
	layout := tableGeometry{widths: minimumColumns}
	for _, row := range rows {
		cells := m.rowCells(row)
		cells[columnName] = "  " + appLabel(row.App)
		if row.Kind == RowChild {
			cells[columnName] = processTreePrefix(row) + processSummary(row.App.Name, row.Child)
		} else if row.App.GroupID.Project != "" {
			cells[columnName] = row.TreePrefix + "▸ " + projectLabelForGroup(row.App)
		}
		for i, cell := range cells {
			layout.widths[i] = max(layout.widths[i], ansi.StringWidth(displayText(cell)))
		}
		if row.Kind == RowGroup {
			layout.maxRSS = max(layout.maxRSS, row.App.RSSKB)
		}
	}
	// Leave room for RSS bars until the user chooses explicit widths.
	metrics := layout.widths.total() - layout.widths[columnName]
	layout.widths[columnName] = min(layout.widths[columnName], max((width-metrics)/2, minimumColumns[columnName]))
	for i, requested := range m.columns {
		if requested > 0 {
			layout.widths[i] = max(requested, minimumColumns[i])
		}
	}
	layout.widths = layout.widths.fit(max(width-1, 0)) // keep the final header grip reachable
	layout.bar = min(max(width-layout.widths.total()-1, 0), width/3)
	return layout
}

func (m Model) rowCells(row VisibleRow) columnCells {
	a := row.App
	if row.Kind == RowChild {
		p := row.Child
		var memory float64
		if m.snap != nil && m.snap.System.MemTotalKB > 0 {
			memory = float64(p.RSSKB) / float64(m.snap.System.MemTotalKB) * 100
		}
		return columnCells{"", fmt.Sprintf("%dMiB", p.RSSKB/1024), m.childSecondaryText(p), fmt.Sprint(p.PID), fmt.Sprintf("%.1f%%", memory), cpuText(p.CPUPct, p.CPUKnown)}
	}
	return columnCells{"", fmt.Sprintf("%dMiB", a.RSSKB/1024), m.appSecondaryText(a), fmt.Sprint(a.ProcCount), fmt.Sprintf("%.1f%%", a.MemPct), cpuText(a.CPUPct, a.CPUKnown)}
}

func (layout tableGeometry) render(cells columnCells, separator string) string {
	var line strings.Builder
	for i, text := range cells {
		if i > 0 {
			line.WriteString(separator)
		}
		cell := ansi.Truncate(displayText(text), layout.widths[i], "…")
		padding := strings.Repeat(" ", max(layout.widths[i]-ansi.StringWidth(cell), 0))
		if i == int(columnName) {
			line.WriteString(cell + padding)
		} else {
			line.WriteString(padding + cell)
		}
	}
	return line.String()
}

func (m Model) resetColumns() Model {
	m.columns = columnWidths{}
	m.columnDrag = columnDrag{}
	return m
}
