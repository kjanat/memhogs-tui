//go:build linux

package collector

import (
	"reflect"
	"slices"
	"testing"
)

const runtimeExecutable = "/usr/lib/electron42/electron"

func applicationFixture(t *testing.T) map[int]*collectedProcess {
	t.Helper()
	const chatPackage = "/usr/lib/chatgpt-desktop-bin/resources/app.asar"
	processes := map[int]*collectedProcess{
		10: contextProcess(10, 1, "electron", runtimeExecutable, "~/projects/chat", runtimeExecutable, "--class=ChatGPT", chatPackage),
		11: contextProcess(11, 10, "electron", runtimeExecutable, "~", runtimeExecutable, "--type=renderer", "--app-path="+chatPackage),
		12: contextProcess(12, 11, "electron", runtimeExecutable, "~", runtimeExecutable, "--type=utility", "--app-path", chatPackage),
		20: contextProcess(20, 1, "electron", runtimeExecutable, "~", runtimeExecutable, "/usr/lib/bitwarden/app.asar"),
		21: contextProcess(21, 20, "electron", runtimeExecutable, "~", runtimeExecutable, "--type=renderer"),
		30: contextProcess(30, 1, "electron", runtimeExecutable, "~/projects/other", runtimeExecutable, "--class=ChatGPT", chatPackage),
		31: contextProcess(31, 30, "electron", runtimeExecutable, "~", runtimeExecutable, "--type=renderer"),
	}
	for _, process := range processes {
		process.detail.RSSKB, process.detail.SwapKB, process.detail.SwapKnown = 2048, 128, true
	}
	enrichProcessContexts(processes, t.TempDir())
	return processes
}

func groupPIDs(app AppStat) []int {
	pids := make([]int, 0, len(app.Children))
	for _, child := range app.Children {
		pids = append(pids, child.PID)
	}
	slices.Sort(pids)
	return pids
}

func TestApplicationGroupsSeparatePackagesAndInstances(t *testing.T) {
	processes := applicationFixture(t)
	apps := groupProcesses(processes, 102400, GroupApplication)
	if len(apps) != 3 {
		t.Fatalf("got %d groups, want three independent roots: %+v", len(apps), apps)
	}
	want := map[int][]int{10: {10, 11, 12}, 20: {20, 21}, 30: {30, 31}}
	ids := make(map[string]bool)
	for _, app := range apps {
		requireCPU(t, !ids[app.GroupID], "group identity collision")
		ids[app.GroupID] = true
		if !reflect.DeepEqual(groupPIDs(app), want[app.Root.PID]) {
			t.Fatalf("mixed application membership: %+v", app)
		}
		requireCPU(t, app.RSSKB == int64(app.ProcCount)*2048, "incorrect instance RSS")
		requireCPU(t, app.SwapKnown && app.SwapKB == int64(app.ProcCount)*128, "incorrect instance swap")
		if app.Root.PID != 20 && app.Name != "ChatGPT" {
			t.Fatalf("class/package alias split one application: %+v", app)
		}
	}
}

func TestGroupIdentityIgnoresLabelsAndMembership(t *testing.T) {
	processes := applicationFixture(t)
	id, _ := processGroup(processes[11], processes, GroupApplication)
	processes[10].detail.Args = []string{runtimeExecutable, "--class=Renamed", "/usr/lib/chatgpt-desktop-bin/resources/app.asar"}
	delete(processes, 12)
	enrichProcessContexts(processes, t.TempDir())
	renamed, group := processGroup(processes[11], processes, GroupApplication)
	requireCPU(t, id == renamed && group.name == "Renamed", "display rename or child exit changed identity")
	processes[10].detail.StartTime++
	// Stale root metadata must not attach the child to the new process at that PID.
	reused, _ := processGroup(processes[11], processes, GroupApplication)
	requireCPU(t, id != reused, "reused root PID inherited old group identity")
	delete(processes, 10)
	orphan, group := processGroup(processes[11], processes, GroupApplication)
	requireCPU(t, orphan != id && group.root.PID == 11, "missing root broadened scope")
}

func TestExecutableGroupingIsExplicitlyBroad(t *testing.T) {
	processes := applicationFixture(t)
	apps := groupProcesses(processes, 0, GroupExecutable)
	if len(apps) != 1 || apps[0].ProcCount != 7 || apps[0].Name != "electron" {
		t.Fatalf("wrong executable aggregation: %+v", apps)
	}
	processes[30].detail.Executable = "/usr/lib/electron43/electron"
	if len(groupProcesses(processes, 0, GroupExecutable)) != 2 {
		t.Fatal("different executable paths were merged")
	}
}

func TestTreeGroupingIncludesDifferentExecutablesAndStopsAtShell(t *testing.T) {
	processes := map[int]*collectedProcess{
		1:  contextProcess(1, 0, "systemd", "/usr/lib/systemd", "/"),
		2:  contextProcess(2, 1, "zsh launch-script", "/usr/bin/zsh", "~"),
		10: contextProcess(10, 2, "editor", "/usr/bin/editor", "/projects/a"),
		11: contextProcess(11, 10, "node", "/usr/bin/node", "/projects/a"),
		12: contextProcess(12, 11, "codex", "/usr/bin/codex", "/projects/a"),
		20: contextProcess(20, 2, "editor", "/usr/bin/editor", "/projects/b"),
	}
	for _, process := range processes {
		process.detail.RSSKB = 2048
	}
	enrichProcessContexts(processes, t.TempDir())
	_, group := processGroup(processes[12], processes, GroupTree)
	requireCPU(t, group.root.PID == 10, "tree traversal stopped at a runtime or crossed a shell")
	apps := groupProcesses(processes, 0, GroupTree)
	for _, app := range apps {
		if app.Root.PID == 10 {
			requireCPU(t, reflect.DeepEqual(groupPIDs(app), []int{10, 11, 12}), "tree mixed independent applications")
			return
		}
	}
	t.Fatal("missing editor tree")
}

func TestApplicationGroupingTraversesSandboxAndRespectsPackageBoundaries(t *testing.T) {
	processes := applicationFixture(t)
	processes[15] = contextProcess(15, 10, "sandbox", "/usr/lib/sandbox", "~")
	processes[11].detail.ParentPID = 15
	// Even the same friendly name must not join a different package's root.
	processes[20].detail.ParentPID = 10
	processes[20].detail.Args = []string{runtimeExecutable, "--class=ChatGPT", "/usr/lib/bitwarden/app.asar"}
	enrichProcessContexts(processes, t.TempDir())
	id, group := processGroup(processes[11], processes, GroupApplication)
	other, _ := processGroup(processes[21], processes, GroupApplication)
	requireCPU(t, group.root.PID == 10 && id != other, "sandbox or friendly name changed ownership boundary")
}

func TestDistinctSessionMetadataKeepsNestedInstancesSeparate(t *testing.T) {
	a := contextProcess(10, 1, "tool", "/bin/tool", "/a")
	b := contextProcess(11, 10, "tool", "/bin/tool", "/b")
	a.detail.Session, b.detail.Session = &SessionInfo{ID: "first"}, &SessionInfo{ID: "second"}
	processes := map[int]*collectedProcess{10: a, 11: b}
	enrichProcessContexts(processes, t.TempDir())
	id, _ := processGroup(a, processes, GroupApplication)
	other, _ := processGroup(b, processes, GroupApplication)
	requireCPU(t, id != other, "distinct recorded sessions were grouped together")
}
