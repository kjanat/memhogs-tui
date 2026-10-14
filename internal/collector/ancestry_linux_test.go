//go:build linux

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func writeProcessFixture(t *testing.T, root string, pid, parent int, name, executable string, rss int, args ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[19] = "S", strconv.Itoa(parent), strconv.Itoa(pid+100)
	files := map[string]string{
		"stat": fmt.Sprintf("%d (%s) %s", pid, name, strings.Join(fields, " ")),
		"comm": name + "\n", "cmdline": strings.Join(args, "\x00") + "\x00",
		"status": fmt.Sprintf("Uid: 1000 1000 1000 1000\nVmRSS: %d kB\nVmSwap: 0 kB\n", rss),
	}
	for name, text := range files {
		writeContextFile(t, filepath.Join(dir, name), text)
	}
	if err := os.Symlink(executable, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
}

func TestScanKeepsZeroMemoryAncestryOutOfAccountingAndKillScope(t *testing.T) {
	root := t.TempDir()
	writeProcessFixture(t, root, 10, 1, "electron", runtimeExecutable, 0, runtimeExecutable, "/apps/chat/app.asar")
	writeProcessFixture(t, root, 11, 10, "sandbox", "/bin/sandbox", 0, "/bin/sandbox")
	writeProcessFixture(t, root, 12, 11, "electron", runtimeExecutable, 4096, runtimeExecutable, "--type=renderer", "--app-path=/alias/chat/app.asar")
	writeProcessFixture(t, root, 20, 1, "electron", runtimeExecutable, 2048, runtimeExecutable, "/apps/bitwarden/app.asar")
	processes := scanProcesses(root)
	if len(processes) != 4 || processes[10].detail.RSSKB != 0 || processes[11].detail.RSSKB != 0 {
		t.Fatalf("identity enumeration lost zero-memory intermediaries: %v", processes)
	}
	enrichProcessContexts(processes, root)
	for _, mode := range []Grouping{GroupApplication, GroupTree} {
		t.Run(string(mode), func(t *testing.T) { assertIdentityOnlyRoot(t, groupProcesses(processes, 0, mode)) })
	}
}

func assertIdentityOnlyRoot(t *testing.T, apps []AppStat) {
	t.Helper()
	if len(apps) != 2 || apps[0].Root.PID != 10 || !reflect.DeepEqual(groupPIDs(apps[0]), []int{12}) {
		t.Fatalf("lost ancestry or counted identity-only processes: %+v", apps)
	}
	if apps[0].RSSKB != 4096 || apps[0].ProcCount != 1 || apps[0].RootProcess == nil || apps[0].RootProcess.PID != 10 {
		t.Fatalf("incorrect accounting/root context: %+v", apps[0])
	}
}

func TestWorkerPackageAliasesDoNotSplitVerifiedInstance(t *testing.T) {
	processes := applicationFixture(t)
	processes[11].detail.Args = []string{runtimeExecutable, "--type=renderer", "--app-path=/alias/chat/app.asar"}
	processes[12].detail.Args = []string{runtimeExecutable, "--type=utility", "--app-path=/another-alias/resources/app.asar"}
	// A second main process stays independent even with identical package evidence
	// and an ancestor that belongs to the first instance.
	processes[30].detail.ParentPID = 10
	enrichProcessContexts(processes, t.TempDir())
	apps := groupProcesses(processes, 0, GroupApplication)
	if len(apps) != 3 || !reflect.DeepEqual(groupPIDs(apps[0]), []int{10, 11, 12}) {
		t.Fatalf("package aliases split an instance or joined separate mains: %+v", apps)
	}
	for _, pid := range []int{11, 12} {
		if processes[pid].detail.Context.ApplicationPID != 10 {
			t.Fatal("worker inherited a package path instead of a process root")
		}
	}
	if processes[11].detail.Context.PackagePath != "/alias/chat/app.asar" {
		t.Fatal("instance resolution overwrote directly observed package evidence")
	}
}

func TestUnknownExecutableAndExitedRootStayConservative(t *testing.T) {
	processes := applicationFixture(t)
	processes[11].detail.Executable = ""
	enrichProcessContexts(processes, t.TempDir())
	id, _ := processGroup(processes[11], processes, GroupApplication)
	if id.Root.PID != 11 {
		t.Fatal("unreadable executable joined by display name")
	}
	first, _ := processGroup(processes[11], processes, GroupExecutable)
	processes[12].detail.Executable = ""
	second, _ := processGroup(processes[12], processes, GroupExecutable)
	if first == second {
		t.Fatal("unknown executable namesakes silently merged")
	}
	delete(processes, 10)
	processes[11].detail.Executable = runtimeExecutable
	enrichProcessContexts(processes, t.TempDir())
	for _, app := range groupProcesses(processes, 0, GroupApplication) {
		pids := groupPIDs(app)
		if slices.Contains(pids, 11) && (slices.Contains(pids, 20) || slices.Contains(pids, 30)) {
			t.Fatal("exited root caused a merge with an unrelated instance")
		}
	}
}

func TestExecutableIdentityDetectsReplacement(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "tool")
	writeContextFile(t, image, "first")
	if err := os.Symlink(image, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
	first := readExecutableIdentity(dir, image)
	replacement := filepath.Join(dir, "replacement")
	writeContextFile(t, replacement, "second")
	if err := os.Rename(replacement, image); err != nil {
		t.Fatal(err)
	}
	second := readExecutableIdentity(dir, image)
	if first.Inode == 0 || first == second || first.Path != second.Path {
		t.Fatal("executable identity did not detect replacement at same path")
	}
}
