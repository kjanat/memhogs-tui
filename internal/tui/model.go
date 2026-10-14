// Package tui implements the terminal user interface for memhogs using
// [Bubble Tea].
// It follows The Elm Architecture: [Model] holds state, [Model.Update] handles
// messages, and [Model.View] renders the screen.
//
// [Bubble Tea]: https://charm.sh/libs/bubbletea
package tui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"memhogs.kjanat.dev/internal/collector"
)

// SortMode determines which column the application table is sorted by.
// Cycle through modes with the "s" key.
type SortMode int

// Sort modes for the application table, in the order the "s" key cycles them.
const (
	SortRSS   SortMode = iota // resident set size
	SortSwap                  // swap usage
	SortProcs                 // process count
	SortName                  // alphabetical
	SortCPU                   // interval CPU usage
	sortModeCount
)

// String returns the short label shown in the table header.
func (s SortMode) String() string {
	return [...]string{"RSS", "Swap", "Procs", "Name", "CPU"}[s]
}

// Config holds startup parameters for [New].
type Config struct {
	// Interval is the time between snapshot refreshes.
	// Zero defaults to 3 seconds.
	Interval time.Duration
	Grouping collector.Grouping
}

// Model holds the full application state: current and previous snapshots,
// cursor position, sort/filter/expand state, and terminal dimensions.
type Model struct {
	snap     *collector.Snapshot
	prevSnap *collector.Snapshot

	cursor    int
	offset    int
	sort      SortMode
	filter    string
	filtering bool
	paused    bool
	expanded  map[string]bool // which groups are unfolded

	killConfirm bool
	killTarget  VisibleRow
	signal      func(collector.ProcDetail, bool) error
	killForce   bool // false = graceful, true = force kill

	width       int
	height      int
	sideFocused bool
	sideOffset  int

	interval   time.Duration
	grouping   collector.Grouping
	collecting bool
	err        error
	status     string

	input        textinput.Model
	help         bool
	commandOpen  bool
	commandTitle string
	commandText  string
	command      viewport.Model
}

type (
	snapMsg     struct{ s *collector.Snapshot }
	snapErr     struct{ e error }
	tick        struct{}
	clearStatus struct{}
)

type killDone struct {
	name string
	err  error
}

// New returns a Model initialized with the given refresh interval and
// an empty expanded-groups set.
// The first snapshot is collected when [Model.Init] runs.
func New(cfg Config) Model {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.CharLimit = 64

	d := cfg.Interval
	if d <= 0 {
		d = 3 * time.Second
	}
	grouping := cfg.Grouping
	if grouping == "" {
		grouping = collector.DefaultGrouping()
	}
	return Model{interval: d, grouping: grouping, collecting: true, signal: killProcess, input: ti, expanded: make(map[string]bool)}
}

// Init starts the first collection and tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(doCollect(m.grouping), doTick(m.interval))
}

func doCollect(grouping collector.Grouping) tea.Cmd {
	return func() tea.Msg {
		s, err := collector.CollectGrouped(grouping)
		if err != nil {
			return snapErr{e: err}
		}
		return snapMsg{s: s}
	}
}

func doTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tick{} })
}

func doClearStatus() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearStatus{} })
}

// doKill signals only the captured processes, never a refreshed group.
func doKill(row VisibleRow, force bool, signal func(collector.ProcDetail, bool) error) tea.Cmd {
	return func() tea.Msg {
		children := row.App.Children
		name := appLabel(row.App)
		if row.Kind == RowChild {
			children = []collector.ProcDetail{row.Child}
			name = fmt.Sprintf("%s [%d]", row.App.Name, row.Child.PID)
		}
		var failures []error
		for _, child := range children {
			if err := signal(child, force); err != nil {
				failures = append(failures, fmt.Errorf("PID %d: %w", child.PID, err))
			}
		}
		return killDone{name: name, err: errors.Join(failures...)}
	}
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.command.SetWidth(max(m.width-4, 1))
		m.command.SetHeight(max(m.height-4, 1))

	case snapMsg:
		m.collecting = false
		m = m.applySnapshot(msg.s)

	case snapErr:
		m.collecting = false
		m.err = msg.e

	case tick:
		cmds := []tea.Cmd{doTick(m.interval)}
		if !m.paused && !m.collecting {
			m.collecting = true
			cmds = append(cmds, doCollect(m.grouping))
		}
		return m, tea.Batch(cmds...)

	case clearStatus:
		m.status = ""

	case killDone:
		m.status = killStatus(msg)
		return m, doClearStatus()

	case tea.KeyPressMsg:
		before, selected := m.currentRow(m.visibleRows(m.sortedApps()))
		next, cmd := m.handleKey(msg)
		m = next.(Model)
		return m.resetSideSelection(before, selected).adjustViewport(), cmd
	}

	return m.adjustViewport(), nil
}

// applySnapshot stores a fresh snapshot, retaining the previous one for deltas,
// and clamps the cursor if the new row count shrank.
func (m Model) applySnapshot(s *collector.Snapshot) Model {
	row, selected := m.currentRow(m.visibleRows(m.sortedApps()))
	m.prevSnap = m.snap
	m.snap = collector.WithCPU(s, m.prevSnap)
	m.pruneExpanded()
	m.err = nil
	m = m.restoreSelection(row, selected)
	m = m.resetSideSelection(row, selected)

	return m
}

// killStatus renders the status-line message for a completed kill.
func killStatus(msg killDone) string {
	if msg.err != nil {
		return fmt.Sprintf("kill %s: %v", msg.name, msg.err)
	}
	return fmt.Sprintf("sent signal → %s", msg.name)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.commandOpen {
		if msg.String() == "esc" || msg.String() == "c" || msg.String() == "q" {
			m.commandOpen = false
			return m, nil
		}
		var cmd tea.Cmd
		m.command, cmd = m.command.Update(msg)
		return m, cmd
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.killConfirm {
		return m.handleKillKey(msg)
	}
	if next, handled := m.handleSideKey(msg); handled {
		return next, nil
	}
	return m.handleNormalKey(msg)
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "esc":
		m.filtering = false
		m.filter = ""
		m.input.Reset()
		return m, nil
	case msg.String() == "enter":
		m.filtering = false
		m.filter = m.input.Value()
		m.cursor = 0
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filter = m.input.Value()
		m.cursor = 0
		return m, cmd
	}
}

func (m Model) handleKillKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Confirm):
		m.killConfirm = false
		target := m.killTarget
		m.killTarget = VisibleRow{}
		return m, doKill(target, m.killForce, m.signal)
	case key.Matches(msg, keys.Cancel):
		m.killConfirm = false
		m.killTarget = VisibleRow{}
	}
	return m, nil
}

// handleNormalKey dispatches keys that toggle global state. Cursor movement
// and fold actions, which all need the current row list, are delegated to
// handleNavKey.
func (m Model) handleNormalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Filter):
		m.filtering = true
		m.input.SetValue(m.filter)
		return m, m.input.Focus()
	case key.Matches(msg, keys.Sort):
		row, ok := m.currentRow(m.visibleRows(m.sortedApps()))
		m.sort = (m.sort + 1) % sortModeCount
		m = m.restoreSelection(row, ok)
	case key.Matches(msg, keys.Pause):
		m.paused = !m.paused
		m.status = ""
		if m.paused {
			m.status = "⏸ paused"
		}
	case key.Matches(msg, keys.Kill):
		m = m.beginKill(false)
	case key.Matches(msg, keys.ForceKill):
		m = m.beginKill(true)
	case key.Matches(msg, keys.Help):
		m.help = !m.help
	case key.Matches(msg, keys.Command):
		m = m.openCommand()
	default:
		return m.handleNavKey(msg), nil
	}
	return m, nil
}

// handleNavKey handles cursor movement and fold/unfold against the current
// flattened row list.
func (m Model) handleNavKey(msg tea.KeyPressMsg) Model {
	rows := m.visibleRows(m.sortedApps())
	if cursor, handled := navigationOffset(msg, m.cursor, max(m.height-4, 1), max(len(rows)-1, 0)); handled {
		m.cursor = cursor
		return m
	}
	switch {
	case key.Matches(msg, keys.Toggle):
		m = m.toggleFold(rows)
	case key.Matches(msg, keys.Expand):
		m = m.foldGroup(rows, true)
	case key.Matches(msg, keys.Collapse):
		m = m.collapseRow(rows)
	}
	return m
}

// currentRow returns the row under the cursor, or false if the cursor is out
// of range.
func (m Model) currentRow(rows []VisibleRow) (VisibleRow, bool) {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return VisibleRow{}, false
	}
	return rows[m.cursor], true
}

// toggleFold flips the expanded state of the highlighted multi-process group.
func (m Model) toggleFold(rows []VisibleRow) Model {
	if r, ok := m.currentRow(rows); ok && r.Kind == RowGroup && r.App.ProcCount > 1 {
		m.expanded[r.App.GroupID] = !m.expanded[r.App.GroupID]
	}
	return m
}

// foldGroup sets the expanded state of the highlighted multi-process group.
func (m Model) foldGroup(rows []VisibleRow, open bool) Model {
	if r, ok := m.currentRow(rows); ok && r.Kind == RowGroup && r.App.ProcCount > 1 {
		m.expanded[r.App.GroupID] = open
	}
	return m
}

// collapseRow folds the highlighted group, or for a child row folds its parent
// and moves the cursor up to that parent group.
func (m Model) collapseRow(rows []VisibleRow) Model {
	r, ok := m.currentRow(rows)
	if !ok {
		return m
	}
	switch r.Kind {
	case RowGroup:
		m.expanded[r.App.GroupID] = false
	case RowChild:
		m.expanded[r.App.GroupID] = false
		for j := m.cursor - 1; j >= 0; j-- {
			if rows[j].Kind == RowGroup {
				m.cursor = j
				break
			}
		}
	}
	return m
}

// sortedApps returns a filtered+sorted copy of the current snapshot's apps.
func (m Model) sortedApps() []collector.AppStat {
	if m.snap == nil {
		return nil
	}

	apps := make([]collector.AppStat, len(m.snap.Apps))
	copy(apps, m.snap.Apps)

	if m.filter != "" {
		q := strings.ToLower(m.filter)
		apps = slices.DeleteFunc(apps, func(a collector.AppStat) bool { return !matchesFilter(a, q) })
	}

	switch m.sort {
	case SortRSS:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Or(cmp.Compare(b.RSSKB, a.RSSKB), compareAppName(a, b))
		})
	case SortSwap:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Or(compareMemory(m.appSecondary(a), m.appSecondaryKnown(a), m.appSecondary(b), m.appSecondaryKnown(b)), compareAppName(a, b))
		})
	case SortProcs:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Or(cmp.Compare(b.ProcCount, a.ProcCount), compareAppName(a, b))
		})
	case SortName:
		slices.SortFunc(apps, compareAppName)
	case SortCPU:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Or(compareCPU(a.CPUPct, a.CPUKnown, b.CPUPct, b.CPUKnown), compareAppName(a, b))
		})
	case sortModeCount: // sentinel, never an active mode; leave order as-is
	}

	return apps
}

// delta compares one group identity; a new instance has no growth baseline.
func (m Model) delta(id string) (rssKB, swapKB int64) {
	if m.prevSnap == nil || m.snap == nil {
		return 0, 0
	}
	current, currentOK := findGroup(m.snap.Apps, id)
	previous, previousOK := findGroup(m.prevSnap.Apps, id)
	if !currentOK || !previousOK {
		return 0, 0
	}
	return current.RSSKB - previous.RSSKB, current.SwapKB - previous.SwapKB
}

// RowKind tags a [VisibleRow] as either a group header or an expanded child.
type RowKind int

// Row kinds in the flattened display table.
const (
	RowGroup RowKind = iota // aggregated application row
	RowChild                // individual process within an expanded group
)

// VisibleRow represents one row in the flattened display table.
// For a [RowGroup], App holds the aggregated stats.
// For a [RowChild], Child holds the individual process detail.
type VisibleRow struct {
	Kind     RowKind              // group header or expanded child
	App      collector.AppStat    // parent group (populated for both kinds)
	AppIdx   int                  // index into the sorted apps slice
	Child    collector.ProcDetail // individual process (only meaningful for RowChild)
	ChildIdx int                  // index into App.Children (only meaningful for RowChild)
}

// visibleRows returns the flattened list of rows including expanded children.
func (m Model) visibleRows(apps []collector.AppStat) []VisibleRow {
	rows := make([]VisibleRow, 0, len(apps)*2)
	for i, app := range apps {
		rows = append(rows, VisibleRow{Kind: RowGroup, App: app, AppIdx: i})
		if m.expanded[app.GroupID] {
			for j, child := range m.sortedChildren(app) {
				rows = append(rows, VisibleRow{
					Kind: RowChild, App: app, AppIdx: i,
					Child: child, ChildIdx: j,
				})
			}
		}
	}
	return rows
}

// beginKill copies the complete intended scope before any refresh can change it.
func (m Model) beginKill(force bool) Model {
	row, ok := m.currentRow(m.visibleRows(m.sortedApps()))
	if !ok {
		return m
	}
	row.App.Children = slices.Clone(row.App.Children)
	m.killTarget = row
	m.killConfirm, m.killForce = true, force
	return m
}

func sameRow(a, b VisibleRow) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == RowGroup {
		return a.App.GroupID == b.App.GroupID
	}
	return a.Child.PID == b.Child.PID && a.Child.StartTime == b.Child.StartTime
}

func (m Model) restoreSelection(selected VisibleRow, ok bool) Model {
	rows := m.visibleRows(m.sortedApps())
	if ok {
		for i, row := range rows {
			if sameRow(row, selected) {
				m.cursor = i
				return m
			}
		}
		if selected.Kind == RowChild {
			for i, row := range rows {
				if row.Kind == RowGroup && row.App.GroupID == selected.App.GroupID {
					m.cursor = i
					return m
				}
			}
		}
	}
	m.cursor = min(max(m.cursor, 0), max(len(rows)-1, 0))
	return m
}

func viewportStart(cursor, offset, count, capacity int) int {
	if capacity <= 0 {
		return 0
	}
	offset = min(max(offset, 0), max(count-capacity, 0))
	if cursor < offset {
		return max(cursor, 0)
	}
	if cursor >= offset+capacity {
		return max(cursor-capacity+1, 0)
	}
	return offset
}

func (m Model) adjustViewport() Model {
	rows := m.visibleRows(m.sortedApps())
	m.cursor = min(max(m.cursor, 0), max(len(rows)-1, 0))
	m.offset = viewportStart(m.cursor, m.offset, len(rows), m.height-4)
	return m
}

func (m Model) secondaryLabel() string {
	if m.snap != nil && m.snap.ProcessCommit {
		return "Commit"
	}
	return "Swap"
}

func (m Model) appSecondary(a collector.AppStat) int64 {
	if m.snap != nil && m.snap.ProcessCommit {
		return a.CommitKB
	}
	return a.SwapKB
}

func (m Model) appSecondaryKnown(a collector.AppStat) bool {
	if m.snap != nil && m.snap.ProcessCommit {
		return a.CommitKnown
	}
	return a.SwapKnown
}

func matchesFilter(a collector.AppStat, query string) bool {
	if strings.Contains(strings.ToLower(a.Name), query) || strings.Contains(strings.ToLower(a.Directory), query) {
		return true
	}
	for _, child := range a.Children {
		if strings.Contains(strings.ToLower(child.Cmdline), query) || strings.Contains(strings.ToLower(child.Cwd), query) || sessionMatches(child.Session, query) || contextMatches(child.Context, query) || strings.Contains(strings.ToLower(child.Executable), query) {
			return true
		}
	}
	return false
}

func sessionMatches(session *collector.SessionInfo, query string) bool {
	if session == nil {
		return false
	}
	return strings.Contains(strings.ToLower(session.Name), query) ||
		strings.Contains(strings.ToLower(session.ID), query) ||
		strings.Contains(strings.ToLower(session.ProjectDir), query)
}

func contextMatches(ctx *collector.ProcessContext, query string) bool {
	if ctx == nil {
		return false
	}
	return strings.Contains(strings.ToLower(ctx.Application), query) || strings.Contains(strings.ToLower(ctx.ProjectDir), query) || strings.Contains(strings.ToLower(ctx.Origin), query)
}
