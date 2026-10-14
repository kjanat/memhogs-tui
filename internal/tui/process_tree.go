package tui

import (
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

type processNode struct {
	process  collector.ProcDetail
	index    int
	parent   *processNode
	children []*processNode
}

func processID(p collector.ProcDetail) collector.ProcessIdentity {
	return collector.ProcessIdentity{PID: p.PID, StartTime: p.StartTime}
}

func groupExpandable(app collector.AppStat) bool {
	return len(app.Children) > 1 || (len(app.Children) == 1 && app.Root.PID != 0 && processID(app.Children[0]) != app.Root)
}

func (m Model) processRows(app collector.AppStat, appIndex int) []VisibleRow {
	if app.GroupID.Mode == collector.GroupExecutable {
		rows := make([]VisibleRow, 0, len(app.Children))
		for i, p := range m.sortedChildren(app) {
			rows = append(rows, VisibleRow{Kind: RowChild, App: app, AppIdx: appIndex, Child: p, ChildIdx: i, Depth: 1})
		}
		return rows
	}
	roots := m.processForest(app)
	rows := make([]VisibleRow, 0, len(app.Children))
	var walk func([]*processNode, string, int)
	walk = func(nodes []*processNode, guides string, depth int) {
		for i, node := range nodes {
			branch, next := "├─ ", "│  "
			if i == len(nodes)-1 {
				branch, next = "└─ ", "   "
			}
			rows = append(rows, VisibleRow{
				Kind: RowChild, App: app, AppIdx: appIndex, Child: node.process,
				ChildIdx: node.index, Depth: depth, TreePrefix: guides + branch, HasChildren: len(node.children) > 0,
				BranchClosed: m.collapsed[processID(node.process)],
			})
			if !m.collapsed[processID(node.process)] {
				walk(node.children, guides+next, depth+1)
			}
		}
	}
	walk(roots, "", 1)
	return rows
}

// Sorting determines sibling order. Traversal keeps each parent's descendants
// adjacent even when a child uses more memory or CPU than its parent.
func (m Model) processForest(app collector.AppStat) []*processNode {
	children := m.sortedChildren(app)
	nodes := make([]*processNode, 0, len(children))
	byPID := make(map[int]*processNode, len(children))
	for i, p := range children {
		node := &processNode{process: p, index: i}
		nodes = append(nodes, node)
		byPID[p.PID] = node
	}
	for _, node := range nodes {
		if processID(node.process) != app.Root {
			node.parent = displayedParent(node.process, byPID, app.Root)
		}
	}
	detachProcessCycles(nodes)
	var roots []*processNode
	for _, node := range nodes {
		if node.parent == nil {
			roots = append(roots, node)
		} else {
			node.parent.children = append(node.parent.children, node)
		}
	}
	return rootProcessFirst(roots, app.Root)
}

func displayedParent(p collector.ProcDetail, nodes map[int]*processNode, root collector.ProcessIdentity) *processNode {
	if p.StartTime == 0 {
		return nil
	}
	if p.Context != nil && len(p.Context.Ancestors) > 0 {
		return displayedAncestor(p, nodes, root)
	}
	parent := nodes[p.ParentPID]
	if parent == nil || parent.process.StartTime == 0 || parent.process.StartTime > p.StartTime || processID(parent.process) == root || parent.process.PID == p.PID {
		return nil
	}
	return parent
}

func displayedAncestor(p collector.ProcDetail, nodes map[int]*processNode, root collector.ProcessIdentity) *processNode {
	if p.Context.Ancestors[0].PID != p.ParentPID {
		return nil
	}
	start := p.StartTime
	for _, ancestor := range p.Context.Ancestors {
		id := collector.ProcessIdentity{PID: ancestor.PID, StartTime: ancestor.StartTime}
		if id.StartTime == 0 || id.StartTime > start || id == root {
			return nil
		}
		start = id.StartTime
		if node := nodes[id.PID]; node != nil {
			if processID(node.process) == id && id != processID(p) {
				return node
			}
			return nil
		}
	}
	return nil
}

func detachProcessCycles(nodes []*processNode) {
	state := make(map[*processNode]uint8, len(nodes))
	var visit func(*processNode)
	visit = func(node *processNode) {
		if state[node] != 0 {
			return
		}
		state[node] = 1
		if parent := node.parent; parent != nil {
			if state[parent] == 1 {
				node.parent = nil
			} else {
				visit(parent)
			}
		}
		state[node] = 2
	}
	for _, node := range nodes {
		visit(node)
	}
}

func rootProcessFirst(roots []*processNode, root collector.ProcessIdentity) []*processNode {
	for i, node := range roots {
		if processID(node.process) == root {
			copy(roots[1:i+1], roots[:i])
			roots[0] = node
			break
		}
	}
	return roots
}

func processTreePrefix(row VisibleRow) string {
	if row.TreePrefix == "" {
		return "↳ "
	}
	if row.HasChildren {
		if row.BranchClosed {
			return row.TreePrefix + "▸ "
		}
		return row.TreePrefix + "▾ "
	}
	return row.TreePrefix
}

func processTreeLabel(row VisibleRow, width int) string {
	prefix := processTreePrefix(row)
	maxPrefix := max(width-8, 3)
	if n := ansi.StringWidth(prefix); n > maxPrefix {
		prefix = "…" + ansi.Cut(prefix, n-maxPrefix+1, n)
	}
	label := processSummary(row.App.Name, row.Child)
	if row.TreePrefix != "" && processID(row.Child) == row.App.Root {
		label += " (self)"
	}
	return fitText(prefix+label, width)
}

func (m Model) collapseProcessRow(row VisibleRow, rows []VisibleRow) Model {
	if row.HasChildren && !m.collapsed[processID(row.Child)] {
		m.collapsed[processID(row.Child)] = true
		return m
	}
	flat := row.TreePrefix == ""
	for i := m.cursor - 1; i >= 0; i-- {
		if rows[i].Depth < row.Depth || rows[i].Kind == RowGroup {
			m.cursor = i
			if flat {
				m.expanded[row.App.GroupID] = false
			}
			break
		}
	}
	return m
}
