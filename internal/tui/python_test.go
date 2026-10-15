package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func TestPythonEntryPointsReplaceInterpreterCachePaths(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"uv console tool", []string{"/home/user/.cache/uv/archive-v0/hash/bin/python", "/home/user/.cache/uv/archive-v0/hash/bin/serena", "start-mcp-server", "--context", "ide-assistant"}, "serena start-mcp-server"},
		{"another console tool", []string{"python", "/cache/bin/blender-mcp"}, "blender-mcp"},
		{"module", []string{"python3", "-u", "-m", "mcp.server", "serve", "--port", "8080"}, "python3 -m mcp.server serve"},
		{"isolated module", []string{"python3.13", "-I", "-m", "http.server", "8080"}, "python3.13 -m http.server 8080"},
		{"module option", []string{"python3.13", "-mhttp.server", "8080"}, "python3.13 -m http.server 8080"},
		{"script options", []string{"python", "-W", "ignore", "-X", "dev", "--check-hash-based-pycs", "always", "-OO", "/projects/work/job.py", "run"}, "job.py run"},
		{"attached options", []string{"python", "-Wignore", "-Xdev", "/projects/work/job.py"}, "job.py"},
		{"package main", []string{"pypy3", "/env/site-packages/serena/__main__.py"}, "serena/__main__.py"},
		{"main script", []string{"python", "main.py"}, "main.py"},
		{"windows paths and spaces", []string{`C:\Program Files\Python\python.exe`, `C:\Users\user\cache dir\bin\blender-mcp`}, "blender-mcp"},
		{"inline code", []string{"python", "-c", "private_inline_code()", "ignored"}, "python -c (inline code)"},
		{"joined inline code", []string{"python", "-cprivate_inline_code()"}, "python -c (inline code)"},
		{"stdin", []string{"python", "-"}, "python (stdin)"},
		{"end of options", []string{"python", "--", "-script.py"}, "-script.py"},
		{"bare interpreter", []string{"python", "-i"}, "python"},
		{"missing module", []string{"python", "-m"}, "python -m"},
		{"missing option value", []string{"python", "-X"}, "python"},
		{"unrelated program", []string{"python-helper", "/cache/bin/tool"}, ""},
		{"no command", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.args)
			require(t, pythonCommand(tt.args) == tt.want, "got %q, want %q", pythonCommand(tt.args), tt.want)
			require(t, reflect.DeepEqual(tt.args, before), "summary changed observed argv")
		})
	}
}

func TestPythonTableKeepsToolAndProjectVisibleAndInspectorComplete(t *testing.T) {
	app := testApp("python", 4096, 123, 124)
	for i, name := range []string{"serena", "blender-mcp"} {
		p := &app.Children[i]
		p.Args = []string{"/home/user/.cache/uv/archive-v0/opaque-hash/bin/python", "/home/user/.cache/uv/archive-v0/opaque-hash/bin/" + name}
		if i == 0 {
			p.Args = append(p.Args, "start-mcp-server", "--context", "ide-assistant")
		}
		p.Cmdline = strings.Join(p.Args, " ")
		p.Cwd = "/home/user/projects/shellcheck-rs"
	}
	m := testModel(app)
	m.expanded[app.GroupID] = true
	table := ansi.Strip(m.viewTable(m.visibleRows(m.sortedApps()), 145, 8))
	for _, want := range []string{"shellcheck-rs [serena start-mcp-server]", "shellcheck-rs [blender-mcp]"} {
		require(t, strings.Contains(table, want), "tool hidden behind cache path: %s", table)
	}
	require(t, !strings.Contains(table, ".cache/uv") && !strings.Contains(table, "opaque-hash"), "cache noise remained in table")
	m.cursor = 1
	m = m.openCommand()
	require(t, strings.Contains(m.commandText, app.Children[0].Cmdline), "inspector lost original command")
	m.filter = "serena"
	require(t, len(m.sortedApps()) == 1, "entry point not searchable")
}

func TestPythonSummarySanitizesScriptNames(t *testing.T) {
	p := collector.ProcDetail{Args: []string{"python", "/cache/bin/worker\x1b[2J\nunsafe"}}
	require(t, !strings.ContainsAny(processSummary("python", p), "\x1b\n"), "script name injected terminal controls")
}

func TestPythonDependencyRetainsItsToolInsideClaudeTree(t *testing.T) {
	p := collector.ProcDetail{Args: []string{"python", "/cache/bin/serena", "start-mcp-server"}, Cwd: "/projects/shellcheck-rs"}
	require(t, processSummary("claude", p) == "shellcheck-rs [serena start-mcp-server]", "dependency mislabeled as unavailable Claude session")
}

func TestPythonToolSurvivesNarrowTableTruncation(t *testing.T) {
	for _, tool := range []string{"serena", "blender-mcp"} {
		p := collector.ProcDetail{Args: []string{"python", "/cache/bin/" + tool}, Cwd: "/projects/long-project-directory-name", Context: &collector.ProcessContext{Application: "python", ProjectDir: "/projects/long-project-directory-name", Origin: "uv"}}
		row := VisibleRow{Kind: RowChild, App: collector.AppStat{Name: "python3.13"}, Child: p}
		for _, width := range []int{18, 24, 40, 80} {
			label := processTreeLabel(row, width)
			require(t, strings.Contains(label, tool), "tool disappeared at width %d: %q", width, label)
			require(t, ansi.StringWidth(label) == width, "label broke table column width")
		}
	}
}

func TestPythonLabelKeepsResolvedProjectAheadOfRuntimeNoise(t *testing.T) {
	p := collector.ProcDetail{
		Args: []string{"/env/bin/python", "/env/bin/blender-mcp"}, Cwd: "/home/user/.cache/uv/archive-v0/hash",
		Context: &collector.ProcessContext{Application: "python", ProjectDir: "/home/user/.cache/uv/archive-v0/hash", Origin: "claude"},
		Project: &collector.ProjectInfo{Directory: "/projects/dprint/dprint", Source: "ancestor workspace"},
	}
	require(t, processSummary("python3.13", p) == "dprint/dprint [blender-mcp(via claude)]", "resolved project lost to runtime/cache labels")
	p.Context.Application = "CustomAnalyzer"
	require(t, strings.HasPrefix(processSummary("zed-editor", p), "CustomAnalyzer dprint/dprint ["), "concrete application identity was suppressed")
}
