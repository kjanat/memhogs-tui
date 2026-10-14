//go:build linux

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func contextProcess(pid, parent int, name, exe, cwd string, args ...string) *collectedProcess {
	return &collectedProcess{name: name, detail: ProcDetail{PID: pid, ParentPID: parent, StartTime: 100, Executable: exe, Cwd: cwd, Args: args}}
}

func TestProcessContextDistinguishesSharedRuntimeApplications(t *testing.T) {
	const executable = "/usr/lib/electron42/electron"
	processes := map[int]*collectedProcess{
		10: contextProcess(10, 1, "electron", executable, "~", executable, "--class=ChatGPT", "/usr/lib/chatgpt-desktop-bin/resources/app.asar"),
		11: contextProcess(11, 10, "electron", executable, "/proc/10/fdinfo", executable, "--type=zygote"),
		12: contextProcess(12, 11, "electron", executable, "/proc/10/fdinfo", executable, "--type=renderer"),
		15: contextProcess(15, 10, "chrome-sandbox", "/usr/lib/chrome-sandbox", ""),
		16: contextProcess(16, 15, "electron", "", "", executable, "--type=zygote"),
		20: contextProcess(20, 1, "electron", executable, "~", executable, "/usr/lib/bitwarden/app.asar"),
		21: contextProcess(21, 20, "electron", executable, "~", executable, "--type=renderer"),
	}
	enrichProcessContexts(processes, t.TempDir())
	chat := processes[12].detail.Context
	if !reflect.DeepEqual([]any{chat.Application, chat.ApplicationPID, chat.ProjectDir, chat.Chromium, len(chat.Ancestors)}, []any{"ChatGPT", 10, "~", true, 2}) {
		t.Fatalf("ChatGPT context: %+v", chat)
	}
	sandboxed := processes[16].detail.Context
	if sandboxed.Application != "ChatGPT" || sandboxed.ProjectDir != "~" {
		t.Fatalf("sandbox helper hid application: %+v", sandboxed)
	}
	bitwarden := processes[21].detail.Context
	if bitwarden.Application != "bitwarden" || bitwarden.ApplicationPID != 20 || !bitwarden.Chromium {
		t.Fatalf("Bitwarden context: %+v", bitwarden)
	}
	if !processes[10].detail.Context.Chromium || !processes[20].detail.Context.Chromium {
		t.Fatal("main runtime processes did not inherit observed family type")
	}
}

func TestGenericContextPreservesWorkspaceAndLauncher(t *testing.T) {
	processes := map[int]*collectedProcess{
		1:  contextProcess(1, 0, "systemd", "/usr/lib/systemd/systemd", "/"),
		10: contextProcess(10, 1, "zed-editor", "/usr/bin/zed", "~"),
		20: contextProcess(20, 10, "node-MainThread", "/usr/bin/node", "~/projects/dprint/dprint", "node", "adapter.js"),
		21: contextProcess(21, 20, "codex", "/usr/bin/codex", "~/projects/dprint/dprint", "codex", "app-server"),
		30: contextProcess(30, 10, "claude", "/opt/claude", "~/projects/actionlint"),
		31: contextProcess(31, 30, "gopls", "/usr/bin/gopls", "~/projects/actionlint", "gopls"),
		32: contextProcess(32, 31, "gopls", "/usr/bin/gopls", "~/projects/actionlint", "gopls", "**", "telemetry", "**"),
	}
	processes[30].detail.Session = &SessionInfo{Name: "actionlint nix"}
	enrichProcessContexts(processes, t.TempDir())
	codex := processes[21].detail.Context
	if codex.Origin != "zed-editor" || codex.OriginPID != 10 || codex.ProjectDir != "~/projects/dprint/dprint" {
		t.Fatalf("Codex context: %+v", codex)
	}
	telemetry := processes[32].detail.Context
	if telemetry.Origin != "claude (actionlint nix)" || telemetry.OriginPID != 30 || telemetry.ProjectDir != "~/projects/actionlint" {
		t.Fatalf("helper context: %+v", telemetry)
	}
}

func TestAncestorIdentityGuards(t *testing.T) {
	for _, tt := range []struct {
		name        string
		parentStart uint64
		cycle       bool
		want        int
	}{
		{"valid", 99, false, 1}, {"reused parent PID", 101, false, 0}, {"unknown parent identity", 0, false, 0}, {"cycle", 100, true, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := contextProcess(2, 3, "worker", "/bin/worker", "~")
			parent := contextProcess(3, 0, "parent", "/bin/parent", "~")
			parent.detail.StartTime = tt.parentStart
			if tt.cycle {
				parent.detail.ParentPID = 2
			}
			ancestors := processAncestors(p.detail, map[int]*collectedProcess{2: p, 3: parent})
			if len(ancestors) != tt.want {
				t.Fatalf("got %d ancestors, want %d", len(ancestors), tt.want)
			}
		})
	}
}

func TestRepositoryContextFromOpenSocket(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "50", "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	socketTable := "Num RefCount Protocol Flags Type St Inode Path\n000: 2 0 0 1 01 123 /projects/biome/.git/fsmonitor--daemon.ipc\n"
	writeContextFile(t, filepath.Join(root, "net", "unix"), socketTable)
	if err := os.Symlink("socket:[123]", filepath.Join(root, "50", "fd", "7")); err != nil {
		t.Fatal(err)
	}
	fields := strings.Repeat("0 ", 19) + "100"
	writeContextFile(t, filepath.Join(root, "50", "stat"), "50 (git) "+fields)
	// Detached daemons can be adopted by the user's systemd, rather than PID 1.
	process := contextProcess(50, 9, "git", "/usr/bin/git", "~", "git", "fsmonitor--daemon", "run")
	processes := map[int]*collectedProcess{50: process, 9: contextProcess(9, 1, "systemd", "/usr/lib/systemd/systemd", "~")}
	enrichProcessContexts(processes, root)
	ctx := process.detail.Context
	if ctx.ProjectDir != "/projects/biome" || ctx.ProjectSource != "open repository resource" || len(ctx.Resources) != 1 {
		t.Fatalf("repository context: %+v", ctx)
	}
	process.detail.StartTime++
	enrichProcessContexts(processes, root)
	if process.detail.Context.ProjectDir != "~" || len(process.detail.Context.Resources) != 0 {
		t.Fatal("accepted resources from a reused PID")
	}
}

func TestResourceContextDoesNotGuessBetweenRepositories(t *testing.T) {
	ctx := &ProcessContext{ProjectDir: "~", ProjectSource: "cwd", Resources: []string{"/a/.git/socket", "/b/.git/socket"}}
	setResourceProject(ctx)
	if ctx.ProjectDir != "~" {
		t.Fatal("ambiguous open repositories were treated as one workspace")
	}
}

func TestArgumentsPreserveBoundariesAndRewrittenTitles(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		command string
		want    string
	}{
		{[]string{"node", "/projects/path with spaces/script.js"}, "", "node|/projects/path with spaces/script.js"},
		{[]string{"/usr/lib/electron --class=ChatGPT /usr/lib/chatgpt/resources/app.asar"}, "", "/usr/lib/electron|--class=ChatGPT|/usr/lib/chatgpt/resources/app.asar"},
		{nil, "gopls ** telemetry **", "gopls|**|telemetry|**"},
	} {
		t.Run(fmt.Sprint(tt.args), func(t *testing.T) {
			p := ProcDetail{Args: tt.args, Cmdline: tt.command}
			if got := strings.Join(p.Arguments(), "|"); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func writeContextFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUnrelatedTypeFlagDoesNotImplyChromium(t *testing.T) {
	process := contextProcess(10, 1, "custom-daemon", "/bin/custom-daemon", "/projects/service", "custom-daemon", "--type=server")
	enrichProcessContexts(map[int]*collectedProcess{10: process}, t.TempDir())
	if process.detail.Context.Chromium {
		t.Fatal("an unrelated --type flag was treated as Chromium evidence")
	}
}
