package tui

import (
	"cmp"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/kjanat/memhogs-tui/internal/collector"
)

// SortMode controls the table sort order.
type SortMode int

const (
	SortRSS SortMode = iota
	SortSwap
	SortProcs
	SortMemPct
	SortName
	sortModeCount
)

func (s SortMode) String() string {
	return [...]string{"RSS", "Swap", "Procs", "Mem%", "Name"}[s]
}

// Config holds startup parameters.
type Config struct {
	Interval time.Duration
}

// Model is the Bubble Tea model.
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
	killSignal  string // "TERM" or "KILL"

	width  int
	height int

	interval time.Duration
	err      error
	status   string

	input textinput.Model
	help  bool
}

// --- messages ---

type snapMsg struct{ s *collector.Snapshot }
type snapErr struct{ e error }
type tick struct{}
type clearStatus struct{}

type killDone struct {
	name string
	err  error
}

// New creates a fresh model.
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

func doKill(name, sig string) tea.Cmd {
	return func() tea.Msg {
		err := exec.Command("pkill", "-"+sig, "-x", "--", name).Run()
		return killDone{name: name, err: err}
	}
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case snapMsg:
		m.prevSnap = m.snap
		m.snap = msg.s
		m.err = nil
		if rows := m.visibleRows(m.sortedApps()); m.cursor >= len(rows) {
			m.cursor = max(0, len(rows)-1)
		}

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
		if msg.err != nil {
			m.status = fmt.Sprintf("kill %s: %v", msg.name, msg.err)
		} else {
			m.status = fmt.Sprintf("sent signal → %s", msg.name)
		}
		return m, doClearStatus()

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// --- key handlers ---

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
			return m, doKill(rows[m.cursor].App.Name, m.killSignal)
		}
	case key.Matches(msg, keys.Cancel):
		m.killConfirm = false
	}
	return m, nil
}

func (m Model) handleNormalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg, keys.Down):
		rows := m.visibleRows(m.sortedApps())
		m.cursor = min(m.cursor+1, max(0, len(rows)-1))
	case key.Matches(msg, keys.Toggle):
		rows := m.visibleRows(m.sortedApps())
		if m.cursor < len(rows) {
			r := rows[m.cursor]
			if r.Kind == RowGroup {
				m.expanded[r.App.Name] = !m.expanded[r.App.Name]
			}
		}
	case key.Matches(msg, keys.Sort):
		m.sort = (m.sort + 1) % sortModeCount
	case key.Matches(msg, keys.Filter):
		m.filtering = true
		m.input.SetValue(m.filter)
		cmd := m.input.Focus()
		return m, cmd
	case key.Matches(msg, keys.Pause):
		m.paused = !m.paused
		if m.paused {
			m.status = "⏸ paused"
		} else {
			m.status = ""
		}
	case key.Matches(msg, keys.Kill):
		m.killConfirm = true
		m.killSignal = "TERM"
	case key.Matches(msg, keys.ForceKill):
		m.killConfirm = true
		m.killSignal = "KILL"
	case key.Matches(msg, keys.Help):
		m.help = !m.help
	}
	return m, nil
}

// --- derived state ---

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

// --- visible row abstraction for fold/unfold ---

// RowKind distinguishes group headers from expanded child processes.
type RowKind int

const (
	RowGroup RowKind = iota
	RowChild
)

// VisibleRow is one row in the flattened table (group or child).
type VisibleRow struct {
	Kind     RowKind
	App      collector.AppStat
	AppIdx   int
	Child    collector.ProcDetail
	ChildIdx int
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
