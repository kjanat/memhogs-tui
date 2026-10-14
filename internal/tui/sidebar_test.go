package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func contextModel() Model {
	a := testApp("node", 4096, 101, 100)
	a.Root = collector.ProcessIdentity{PID: 100, StartTime: 1}
	a.Children[0].Cwd = "/workspace/helper"
	a.Children[1].Cwd = "/workspace/root"
	a.Children[1].UID, a.Children[1].User = "1000", "alice"
	a.Children[1].ParentPID = 99
	a.Children[1].Executable = "/usr/bin/node"
	a.Children[1].Args = []string{"/usr/bin/node", "server.js", "--workspace", "project with spaces"}
	a.Children[1].Context = &collector.ProcessContext{
		Origin: "zed-editor", OriginPID: 99,
		Ancestors: []collector.ProcessAncestor{{PID: 99, Name: "zed-editor"}},
	}
	m := testModel(a)
	m.width, m.height = 160, 24
	m.snap.System = collector.SystemInfo{
		MemUsedKB: 4 * 1048576, MemTotalKB: 8 * 1048576,
		MemAvailableKB: 4 * 1048576, SwapKnown: true, SwapUsedKB: 1048576, SwapTotalKB: 2 * 1048576,
	}
	m.snap.PSI = &collector.PSIInfo{SomeAvg10: 2.5}
	return m
}

func TestSelectedProcessUsesRootNotLargestChild(t *testing.T) {
	m := contextModel()
	row := m.visibleRows(m.sortedApps())[0]
	p, ok := selectedProcess(row)
	require(t, ok && p.PID == 100, "group context must use verified root")
	row.Kind, row.Child = RowChild, row.App.Children[0]
	p, ok = selectedProcess(row)
	require(t, ok && p.PID == 101, "child context must follow hovered child")
	row.Kind = RowGroup
	row.App.Root.StartTime++
	_, ok = selectedProcess(row)
	require(t, !ok, "reused root must not borrow another process's context")
	require(t, strings.Contains(m.viewProcessContext(row, 48), "select a child"), "broad group needs explicit child guidance")
}

func TestSidebarCanInspectIdentityOnlyRoot(t *testing.T) {
	m := contextModel()
	root := m.snap.Apps[0].Children[1]
	root.RSSKB = 0
	m.snap.Apps[0].Children = m.snap.Apps[0].Children[:1]
	m.snap.Apps[0].RootProcess = &root
	row := m.visibleRows(m.sortedApps())[0]
	p, ok := selectedProcess(row)
	require(t, ok && p.PID == 100 && p.RSSKB == 0, "identity-only root context unavailable")
}

func TestProcessContextShowsOwnerDirectoryAndArguments(t *testing.T) {
	m := contextModel()
	row := m.visibleRows(m.sortedApps())[0]
	text := ansi.Strip(m.viewProcessContext(row, 50))
	for _, want := range []string{"Root PID 100", "alice (UID 1000)", "Launched by\nzed-editor [PID 99]", "Current dir\n/workspace/root", "Launch dir not recorded", "Arguments\n/usr/bin/node server.js", "Parent", "Executable\n/usr/bin/node"} {
		require(t, strings.Contains(text, want), "missing %q:\n%s", want, text)
	}
	require(t, strings.Contains(processCommand(row.App.Children[1]), `"project with spaces"`), "argv boundaries lost")
	m = m.openCommand()
	require(t, strings.Contains(m.commandText, "Account owner: alice (UID 1000)"), "full inspector omits account owner")
}

func TestSessionDirectoryHasExplicitSource(t *testing.T) {
	p := collector.ProcDetail{Cwd: "/changed", Session: &collector.SessionInfo{ProjectDir: "/original"}}
	text := ansi.Strip(strings.Join(processDirectories(p, 50), "\n"))
	for _, want := range []string{"Launch dir (session)\n/original", "Current dir\n/changed"} {
		require(t, strings.Contains(text, want), "missing %q: %s", want, text)
	}
	require(t, !strings.Contains(text, "not recorded"), "verified session launch directory mislabeled")
}

func TestSidebarViewportFitsAndPinsMemory(t *testing.T) {
	for _, width := range []int{80, 90, 120, 160} {
		for _, height := range []int{6, 24, 40} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := contextModel()
				m.width, m.height = width, height
				m.sideFocused = true
				assertContextDimensions(t, m)
				m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEnd})
				assertContextDimensions(t, m)
				view := ansi.Strip(m.View().Content)
				require(t, strings.Contains(view, "RAM") && strings.Contains(view, "Swap"), "memory disappeared while scrolling:\n%s", view)
			})
		}
	}
}

func assertContextDimensions(t *testing.T, m Model) {
	t.Helper()
	view := m.View().Content
	for line := range strings.SplitSeq(view, "\n") {
		require(t, ansi.StringWidth(line) <= m.width, "line exceeds width %d: %q", m.width, line)
	}
	require(t, lipgloss.Height(view) == m.height, "height=%d, want %d", lipgloss.Height(view), m.height)
}

func TestContextAvailableWithoutOpeningInspector(t *testing.T) {
	m := contextModel()
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Process context", "zed-editor", "/workspace/root", "Arguments", "server.js", "alice", "PSI10"} {
		require(t, strings.Contains(view, want), "initial sidebar omits %q:\n%s", want, view)
	}
	m.width = 80
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyTab})
	require(t, strings.Contains(m.View().Content, "Process context"), "Tab must expose context on narrow terminals")
}

func TestHelpFitsStandardTerminal(t *testing.T) {
	help := contextModel().viewHelpModal()
	require(t, lipgloss.Height(help) <= 24 && lipgloss.Width(help) <= 80, "help exceeds 80x24 terminal")
	require(t, strings.Contains(help, "PgUp/PgDn") && strings.Contains(help, "Tab"), "new keys missing from help")
}

func TestSidebarFieldsSanitizeAndBoundUnicode(t *testing.T) {
	value := strings.Repeat("\x1b[31m界🙂\x1b[0m\n", 100)
	text := sidebarField("Arguments", value, 12, 3)
	lines := strings.Split(ansi.Strip(text), "\n")
	require(t, len(lines) <= 4 && utf8.ValidString(text), "preview corrupt or too tall")
	for _, line := range lines {
		require(t, ansi.StringWidth(line) <= 12 && !strings.Contains(line, "[31m"), "unsafe or oversized line: %q", line)
	}
	for _, tt := range []struct{ uid, user, want string }{
		{"1000", "alice", "alice (UID 1000)"}, {"0", "", "UID 0"}, {"", "alice", "alice"}, {"", "", ""},
	} {
		require(t, processAccount(collector.ProcDetail{UID: tt.uid, User: tt.user}) == tt.want, "incorrect account fallback")
	}
}
