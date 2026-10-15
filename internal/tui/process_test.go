package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"memhogs.kjanat.dev/internal/collector"
)

func TestProcessSummaries(t *testing.T) {
	for _, tt := range []struct{ name, app, command, want string }{
		{"browser with renamed argv0", "chrome", "GitHub9061 --change-stack-guard-on-fork=enable --ozone-platform=wayland", "GitHub9061"},
		{"renderer", "chrome", "GitHub9061 --ozone-platform=wayland --type=renderer --lang=en-US", "renderer"},
		{"extension", "chrome", "GitHub9061 --type=renderer --extension-process", "extension renderer"},
		{"gpu", "chrome", "GitHub9061 --type=gpu-process --gpu-preferences=AAA", "GPU process"},
		{"network", "chrome", "GitHub9061 --type=utility --utility-sub-type=network.mojom.NetworkService", "network service"},
		{"audio split flags", "chrome", "GitHub9061 --type utility --utility-sub-type audio.mojom.AudioService", "audio service"},
		{"storage", "chrome", "GitHub9061 --type=utility --utility-sub-type=storage.mojom.StorageService", "storage service"},
		{"other utility", "chrome", "GitHub9061 --type=utility --utility-sub-type=video_capture.mojom.VideoCaptureService", "utility: VideoCaptureService"},
		{"plain utility", "chrome", "GitHub9061 --type=utility", "utility"},
		{"zygote", "chrome", "GitHub9061 --type=zygote", "zygote"},
		{"generic app", "node", "node /projects/worker.js --port 9000", "/projects/worker.js"},
		{"empty command", "worker", "", "worker"},
		{"not a browser", "chrome-helper-script", "python run.py", "run.py"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := processSummary(tt.app, collector.ProcDetail{Cmdline: tt.command})
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestExpandedBrowserTableShowsRolePIDAndMetrics(t *testing.T) {
	app := testApp("chrome", 8192, 2995104, 3738480)
	app.Children[0].Cmdline = "GitHub9061 --type=renderer --change-stack-guard-on-fork=enable --ozone-platform=wayland"
	app.Children[0].SwapKB = 512 * 1024
	app.Children[1].Cmdline = "GitHub9061 --type=utility --utility-sub-type=network.mojom.NetworkService --gpu-preferences=AAAAAAAA"
	m := testModel(app)
	m.expanded[testGroupID("chrome")] = true
	table := m.viewTable(m.visibleRows(m.sortedApps()), 100, 10)
	for _, value := range []string{"PROCS/PID", "renderer", "network service", "2995104", "3738480", "512MiB"} {
		if !strings.Contains(table, value) {
			t.Fatalf("missing %q:\n%s", value, table)
		}
	}
	for _, noise := range []string{"GitHub9061", "change-stack-guard", "gpu-preferences", "ozone-platform"} {
		if strings.Contains(table, noise) {
			t.Fatalf("launch noise leaked into table: %s", noise)
		}
	}
}

func TestChildSortingMatchesActiveMetricWithoutMutation(t *testing.T) {
	app := testApp("chrome", 8192, 1, 2, 3)
	app.Children[0].RSSKB = 4096
	app.Children[0].SwapKB = 0
	app.Children[0].Cmdline = "chrome --type=renderer"
	app.Children[1].RSSKB = 1024
	app.Children[1].SwapKB = 4096
	app.Children[1].Cmdline = "chrome --type=gpu-process"
	app.Children[2].RSSKB = 2048
	app.Children[2].SwapKnown = false
	app.Children[2].SwapKB = 8192
	app.Children[2].Cmdline = "chrome --type=zygote"
	before := append([]collector.ProcDetail(nil), app.Children...)
	m := testModel(app)
	m.expanded[testGroupID("chrome")] = true
	for _, tt := range []struct {
		mode SortMode
		want int
	}{{SortRSS, 1}, {SortSwap, 2}, {SortName, 2}, {SortProcs, 1}} {
		m.sort = tt.mode
		rows := m.visibleRows(m.sortedApps())
		if rows[1].Child.PID != tt.want {
			t.Fatalf("%s starts with PID %d, want %d", tt.mode, rows[1].Child.PID, tt.want)
		}
	}
	require(t, reflect.DeepEqual(before, app.Children), "child sorting mutated snapshot")
}

func TestFullCommandViewerRetainsArgumentsAndCapturedSelection(t *testing.T) {
	app := testApp("chrome", 4096, 123)
	app.Children[0].Cmdline = "GitHub9061 --type=renderer " + strings.Repeat("--long-browser-option=abcdefghijk ", 30) + "--last-option=end\x1b[2J"
	m := testModel(app)
	m.expanded[testGroupID("chrome")] = true
	m.cursor = 1
	m, _ = updateModel(m, press('c'))
	require(t, m.commandOpen && strings.Contains(m.commandText, "--last-option=end"), "full command lost arguments")
	require(t, !strings.Contains(m.commandText, "\x1b"), "command viewer retained terminal controls")
	require(t, lipgloss.Width(m.command.View()) <= m.width-4, "command viewport exceeds terminal width")
	m, _ = updateModel(m, snapMsg{&collector.Snapshot{Apps: []collector.AppStat{testApp("other", 8192, 99)}}})
	require(t, strings.Contains(m.commandText, "PID 123") && !strings.Contains(m.commandText, "PID 99"), "viewer changed target on refresh")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	require(t, m.command.YOffset() > 0, "long command did not scroll")
	m, _ = updateModel(m, tea.WindowSizeMsg{Width: 40, Height: 8})
	require(t, m.command.Width() == 36 && m.command.Height() == 4, "viewer did not resize")
	m, _ = updateModel(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	require(t, !m.commandOpen, "escape did not close command viewer")
}

func TestRescueSectionStaysCompactForLargeGroup(t *testing.T) {
	pids := make([]int, 72)
	for i := range pids {
		pids[i] = 100000 + i
	}
	m := testModel(testApp("chrome", 8192, pids...))
	rescue := m.viewRescue(30)
	require(t, lipgloss.Height(rescue) <= 5 && strings.Contains(rescue, "72 processes"), "rescue section filled sidebar: %s", rescue)
	require(t, !strings.Contains(rescue, "100000"), "rescue dumped group PID list")
	m.expanded[testGroupID("chrome")] = true
	m.cursor = 1
	rescue = m.viewRescue(30)
	require(t, strings.Contains(rescue, "100000") && !strings.Contains(rescue, "100001"), "child rescue has wrong scope: %s", rescue)
}

func TestShellRowsExposeDirectoryAndTerminal(t *testing.T) {
	app := testApp("zsh", 8192, 1105020, 718890)
	app.Children[0].Cmdline = "-zsh"
	app.Children[0].Cwd = "~/projects/memhogs-tui"
	app.Children[0].Terminal = "pts/17"
	app.Children[1].Cmdline = "zsh"
	app.Children[1].Cwd = "~/projects/other-project"
	m := testModel(app)
	m.expanded[testGroupID("zsh")] = true
	table := m.viewTable(m.visibleRows(m.sortedApps()), 140, 10)
	for _, value := range []string{"pts/17", "memhogs-tui", "other-project", "1105020", "718890"} {
		require(t, strings.Contains(table, value), "missing shell context %q:\n%s", value, table)
	}
}

func TestClaudeSessionSummaryAndInspector(t *testing.T) {
	const id = "df80c404-a298-48b9-a7df-63e880bfe5a8"
	app := testApp("claude", 4096, 123)
	app.Children[0].Cmdline = "claude -c"
	app.Children[0].Cwd = "/home/user/projects/other-worktree"
	app.Children[0].Session = &collector.SessionInfo{ID: id, Name: "actionlint nix", ProjectDir: "/home/user/projects/actionlint", Entrypoint: "cli"}
	summary := processSummary("claude", app.Children[0])
	require(t, summary == "actionlint [actionlint nix [df80c404]]", "wrong Claude summary: %s", summary)
	m := testModel(app)
	m.expanded[testGroupID("claude")] = true
	m.cursor = 1
	m, _ = updateModel(m, press('c'))
	for _, value := range []string{id, "actionlint nix", "Spawn dir (session): /home/user/projects/actionlint", "Current dir: /home/user/projects/other-worktree", "Entrypoint: cli"} {
		require(t, strings.Contains(m.commandText, value), "inspector missing %q", value)
	}
	for _, query := range []string{"actionlint nix", "DF80C404", "projects/actionlint"} {
		m.filter = strings.ToLower(query)
		require(t, len(m.sortedApps()) == 1, "filter missed Claude session %q", query)
	}
}

func TestClaudeSessionFallbackAndSafeNames(t *testing.T) {
	p := collector.ProcDetail{Cmdline: "claude secret-startup-prompt", Cwd: "~/projects/dprint/dprint"}
	summary := processSummary("claude", p)
	require(t, summary == "dprint/dprint [session unavailable]", "unknown session should show current directory: %s", summary)
	p.Session = &collector.SessionInfo{ID: "c66ae174-d32f-4214-bb47-e3b9fa4b641e", ProjectDir: "~/projects/dprint/dprint"}
	summary = processSummary("claude", p)
	require(t, strings.Contains(summary, "c66ae174") && strings.Contains(summary, "dprint/dprint"), "unnamed session lost identity: %s", summary)
	p.Session.Name = "name\x1b[2J\nunsafe"
	summary = processSummary("claude", p)
	require(t, !strings.ContainsAny(summary, "\x1b\n"), "session name injected terminal controls")
}

func TestCommonContextAppliesWithoutApplicationSpecificRules(t *testing.T) {
	for _, tt := range []struct{ app, command, directory, origin, application, expectedRole string }{
		{"codex", "codex -c features.code_mode_host=true app-server --listen stdio://", "~", "ChatGPT", "codex", "app-server"},
		{"codex", "codex app-server", "~/projects/dprint/dprint", "zed-editor", "codex", "app-server"},
		{"gopls", "gopls", "~/projects/actionlint", "claude (actionlint nix)", "gopls", "gopls"},
		{"gopls", "gopls ** telemetry **", "~/projects/shellcheck-rs", "claude", "gopls", "telemetry"},
		{"git", "git fsmonitor--daemon run --detach --ipc-threads=8", "/projects/biome", "", "git", "fsmonitor--daemon run"},
		{"electron", "electron --type=renderer", "~", "", "ChatGPT", "renderer"},
		{"electron", "electron --type=renderer", "~", "", "bitwarden", "renderer"},
		{"custom-daemon", "custom-daemon serve --verbose", "/projects/unlisted-project", "custom-editor", "custom-daemon", "serve"},
	} {
		t.Run(tt.app+tt.directory+tt.application+tt.expectedRole, func(t *testing.T) {
			p := collector.ProcDetail{PID: 123, Cmdline: tt.command, Cwd: "~", Context: &collector.ProcessContext{Application: tt.application, ProjectDir: tt.directory, ProjectSource: "cwd", Origin: tt.origin, Chromium: tt.app == "electron"}}
			got := processSummary(tt.app, p)
			for _, value := range []string{projectLabel(tt.directory), tt.expectedRole} {
				require(t, strings.Contains(got, value), "missing common context %q: %s", value, got)
			}
			if tt.origin != "" {
				require(t, strings.Contains(got, "via "+tt.origin), "missing process owner: %s", got)
			}
			if tt.application != tt.app {
				require(t, strings.Contains(got, tt.application), "shared runtime lost concrete app: %s", got)
			}
		})
	}
}

func TestObservedChromiumFamilyHandlesUnlistedBrowsers(t *testing.T) {
	p := collector.ProcDetail{PID: 123, Args: []string{"/tmp/.mount_helium/opt/helium/helium", "http://localhost"}, Cwd: "~", Context: &collector.ProcessContext{Application: "helium", ApplicationPID: 123, ProjectDir: "~", Chromium: true}}
	got := processSummary("helium", p)
	require(t, got == "~ [main process]", "unlisted browser main process lost role: %s", got)
}

func TestContextSearchAndInspectorExposeEvidence(t *testing.T) {
	app := testApp("electron", 4096, 123)
	p := &app.Children[0]
	p.Cwd = "/proc/100/fdinfo"
	p.ParentPID = 110
	p.Executable = "/usr/lib/electron42/electron"
	p.Context = &collector.ProcessContext{Application: "ChatGPT", ApplicationPID: 100, ApplicationSource: "package argument", ProjectDir: "~", ProjectSource: "application ancestor cwd", Origin: "launcher", Ancestors: []collector.ProcessAncestor{{PID: 110, Name: "electron", Cwd: "/proc/100/fdinfo"}, {PID: 100, Name: "electron", Cwd: "~"}}, Resources: []string{"/usr/lib/chatgpt/resources/app.asar"}}
	m := testModel(app)
	m.filter = "chatgpt"
	require(t, len(m.sortedApps()) == 1, "filter missed inferred app name")
	m.expanded[testGroupID("electron")] = true
	m.cursor = 1
	m, _ = updateModel(m, press('c'))
	for _, value := range []string{"Parent PID: 110", "Executable: /usr/lib/electron42/electron", "Application: ChatGPT [PID 100]", "Directory source: application ancestor cwd", "Ancestor: electron [PID 100]", "Open resource: /usr/lib/chatgpt/resources/app.asar"} {
		require(t, strings.Contains(m.commandText, value), "inspector missing evidence %q", value)
	}
}
