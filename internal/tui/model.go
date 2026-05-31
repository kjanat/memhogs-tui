// Package tui implements the terminal user interface for memhogs using
// [Bubble Tea].
// It follows The Elm Architecture: [Model] holds state, [Model.Update] handles
// messages, and [Model.View] renders the screen.
//
// [Bubble Tea]: https://charm.sh/libs/bubbletea
package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"memhogs.kjanat.dev/internal/collector"
)

// SortMode determines which column the application table is sorted by.
// Cycle through modes with the "s" key.
type SortMode int

// Sort modes for the application table, in the order the "s" key cycles them.
const (
	SortRSS    SortMode = iota // resident set size
	SortSwap                   // swap usage
	SortProcs                  // process count
	SortMemPct                 // percentage of total RAM
	SortName                   // alphabetical
	sortModeCount
)

// String returns the short label shown in the table header.
func (s SortMode) String() string {
	return [...]string{"RSS", "Swap", "Procs", "Mem%", "Name"}[s]
}

// Config holds startup parameters for [New].
type Config struct {
	// Interval is the time between snapshot refreshes.
	// Zero defaults to 3 seconds.
	Interval time.Duration
}

// Model holds the full application state: current and previous snapshots,
// cursor position, sort/filter/expand state, and terminal dimensions.
type Model struct {
	snap     *collector.Snapshot
	prevSnap *collector.Snapshot

	cursor    int
	sort      SortMode
	filter    string
	filtering bool
	paused    bool
	expanded  map[string]bool // which groups are unfolded

	killConfirm bool
	killForce   bool // false = graceful, true = force kill

	width  int
	height int

	interval time.Duration
	err      error
	status   string

	input textinput.Model
	help  bool
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
	if d == 0 {
		d = 3 * time.Second
	}
	return Model{interval: d, input: ti, expanded: make(map[string]bool)}
}

// Init starts the first collection and tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(doCollect(), doTick(m.interval))
}

func doCollect() tea.Cmd {
	return func() tea.Msg {
		s, err := collector.Collect()
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

// doKill kills the process(es) the cursor row points at: a single PID for a
// RowChild, or every PID in the group for a RowGroup header.
func doKill(row VisibleRow, force bool) tea.Cmd {
	return func() tea.Msg {
		switch row.Kind {
		case RowChild:
			err := killProcess(row.Child.PID, force)
			return killDone{name: fmt.Sprintf("%s [%d]", row.App.Name, row.Child.PID), err: err}
		default:
			var firstErr error
			for _, c := range row.App.Children {
				if err := killProcess(c.PID, force); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			return killDone{name: row.App.Name, err: firstErr}
		}
	}
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case snapMsg:
		m = m.applySnapshot(msg.s)

	case snapErr:
		m.err = msg.e

	case tick:
		cmds := []tea.Cmd{doTick(m.interval)}
		if !m.paused {
			cmds = append(cmds, doCollect())
		}
		return m, tea.Batch(cmds...)

	case clearStatus:
		m.status = ""

	case killDone:
		m.status = killStatus(msg)
		return m, doClearStatus()

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// applySnapshot stores a fresh snapshot, retaining the previous one for deltas,
// and clamps the cursor if the new row count shrank.
func (m Model) applySnapshot(s *collector.Snapshot) Model {
	m.prevSnap = m.snap
	m.snap = s
	m.err = nil
	if rows := m.visibleRows(m.sortedApps()); m.cursor >= len(rows) {
		m.cursor = max(0, len(rows)-1)
	}
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
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.killConfirm {
		return m.handleKillKey(msg)
	}
	return m.handleNormalKey(msg)
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
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
		rows := m.visibleRows(m.sortedApps())
		if m.cursor < len(rows) {
			return m, doKill(rows[m.cursor], m.killForce)
		}
	case key.Matches(msg, keys.Cancel):
		m.killConfirm = false
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
		m.sort = (m.sort + 1) % sortModeCount
	case key.Matches(msg, keys.Pause):
		m.paused = !m.paused
		m.status = ""
		if m.paused {
			m.status = "⏸ paused"
		}
	case key.Matches(msg, keys.Kill):
		m.killConfirm, m.killForce = true, false
	case key.Matches(msg, keys.ForceKill):
		m.killConfirm, m.killForce = true, true
	case key.Matches(msg, keys.Help):
		m.help = !m.help
	default:
		return m.handleNavKey(msg), nil
	}
	return m, nil
}

// handleNavKey handles cursor movement and fold/unfold against the current
// flattened row list.
func (m Model) handleNavKey(msg tea.KeyPressMsg) Model {
	rows := m.visibleRows(m.sortedApps())
	switch {
	case key.Matches(msg, keys.Up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg, keys.Down):
		m.cursor = min(m.cursor+1, max(0, len(rows)-1))
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
		m.expanded[r.App.Name] = !m.expanded[r.App.Name]
	}
	return m
}

// foldGroup sets the expanded state of the highlighted multi-process group.
func (m Model) foldGroup(rows []VisibleRow, open bool) Model {
	if r, ok := m.currentRow(rows); ok && r.Kind == RowGroup && r.App.ProcCount > 1 {
		m.expanded[r.App.Name] = open
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
		m.expanded[r.App.Name] = false
	case RowChild:
		m.expanded[r.App.Name] = false
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
		apps = slices.DeleteFunc(apps, func(a collector.AppStat) bool {
			return !strings.Contains(strings.ToLower(a.Name), q)
		})
	}

	switch m.sort {
	case SortRSS:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Compare(b.RSSKB, a.RSSKB)
		})
	case SortSwap:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Compare(b.SwapKB, a.SwapKB)
		})
	case SortProcs:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Compare(b.ProcCount, a.ProcCount)
		})
	case SortMemPct:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Compare(b.MemPct, a.MemPct)
		})
	case SortName:
		slices.SortFunc(apps, func(a, b collector.AppStat) int {
			return cmp.Compare(a.Name, b.Name)
		})
	case sortModeCount: // sentinel, never an active mode; leave order as-is
	}

	return apps
}

// delta returns RSS/swap change for a named app vs previous snapshot.
func (m Model) delta(name string) (rssKB, swapKB int64) {
	if m.prevSnap == nil || m.snap == nil {
		return 0, 0
	}
	var cr, cs, pr, ps int64
	for _, a := range m.snap.Apps {
		if a.Name == name {
			cr, cs = a.RSSKB, a.SwapKB
			break
		}
	}
	for _, a := range m.prevSnap.Apps {
		if a.Name == name {
			pr, ps = a.RSSKB, a.SwapKB
			break
		}
	}
	return cr - pr, cs - ps
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
		if m.expanded[app.Name] {
			limit := min(len(app.Children), 15)
			for j := range limit {
				rows = append(rows, VisibleRow{
					Kind: RowChild, App: app, AppIdx: i,
					Child: app.Children[j], ChildIdx: j,
				})
			}
		}
	}
	return rows
}
