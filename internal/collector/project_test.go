package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectProcess(pid, parent int, exe, cwd string, ancestors ...int) ProcDetail {
	p := ProcDetail{PID: pid, ParentPID: parent, StartTime: 1, Executable: exe, Cwd: cwd, Args: []string{exe}, Context: &ProcessContext{Application: filepath.Base(exe), ApplicationPID: pid, ApplicationStart: 1, ProjectDir: cwd, ProjectSource: currentDirectorySource}}
	for _, id := range ancestors {
		p.Context.Ancestors = append(p.Context.Ancestors, ProcessAncestor{PID: id, StartTime: 1})
	}
	return p
}

func attributeProjectFixture(children []ProcDetail) {
	processes := make(map[int]*collectedProcess, len(children))
	for _, p := range children {
		processes[p.PID] = &collectedProcess{detail: p}
	}
	attributeProjects(processes)
	for i := range children {
		children[i] = processes[children[i].PID].detail
	}
}

func fixtureRepository(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProjectAttributionFollowsSessionWorkspaceAndDependencies(t *testing.T) {
	base := t.TempDir()
	a := fixtureRepository(t, filepath.Join(base, "a"), "same-name")
	b := fixtureRepository(t, filepath.Join(base, "b"), "same-name")
	cache := filepath.Join(base, ".cache", "dependency")
	processes := []ProcDetail{
		projectProcess(10, 1, "zed-editor", a),
		projectProcess(20, 10, "claude", b, 10),
		projectProcess(21, 20, "node", cache, 20, 10),
		projectProcess(30, 10, "codex", "~", 10),
		projectProcess(31, 30, "node", cache, 30, 10),
		projectProcess(40, 10, "gopls", filepath.Join(b, "src", "nested"), 10),
		projectProcess(41, 40, "worker", "~", 40, 10),
		projectProcess(50, 10, "zed-editor", b, 10),
		projectProcess(60, 1, "node", a),
	}
	processes[1].Session = &SessionInfo{ID: "session", ProjectDir: filepath.Join(a, "src")}
	processes[3].Args = []string{"codex", "--cd", b}
	apps := []AppStat{{Children: processes, RootProcess: &processes[0]}}
	attributeProjectFixture(apps[0].Children)
	for _, tt := range []struct {
		index     int
		directory string
		owner     int
	}{
		{1, a, 20}, {2, a, 20}, {3, b, 30}, {4, b, 30}, {5, b, 40}, {6, b, 40},
	} {
		p := apps[0].Children[tt.index]
		if p.Project == nil || p.Project.Directory != tt.directory || p.Project.Owner.PID != tt.owner {
			t.Fatalf("PID %d incorrectly attributed: %+v", p.PID, p.Project)
		}
	}
	for _, index := range []int{0, 7, 8} {
		if apps[0].Children[index].Project != nil {
			t.Fatalf("shared editor or unrelated process assigned to project: %d", index)
		}
	}
	if !strings.Contains(processes[2].Project.Source, "ancestor") || processes[1].Session.ProjectDir != filepath.Join(a, "src") || processes[1].Cwd != b {
		t.Fatal("attribution lost evidence or changed recorded/current directories")
	}
}

func TestProjectAttributionRejectsReusedAncestorAndCacheDirectory(t *testing.T) {
	dir := fixtureRepository(t, t.TempDir(), "repo")
	parent := projectProcess(20, 1, "claude", dir)
	child := projectProcess(21, 20, "worker", "~", 20)
	child.Context.Ancestors[0].StartTime = 2
	cache := projectProcess(30, 1, "codex", filepath.Join(t.TempDir(), ".npm", "_npx", "package"))
	apps := []AppStat{{Children: []ProcDetail{parent, child, cache}}}
	attributeProjectFixture(apps[0].Children)
	if apps[0].Children[0].Project == nil || apps[0].Children[1].Project != nil || apps[0].Children[2].Project != nil {
		t.Fatal("reused parent or package cache invented workspace ownership")
	}
}

func TestCodexProjectFlagsAndRelativeDirectoryHonesty(t *testing.T) {
	dir := fixtureRepository(t, t.TempDir(), "repo")
	for _, args := range [][]string{{"codex", "-C", dir}, {"codex", "--cd=" + dir}, {"codex", "--cwd", dir}} {
		p := projectProcess(20, 1, "codex", "~")
		p.Args = args
		apps := []AppStat{{Children: []ProcDetail{p}}}
		attributeProjectFixture(apps[0].Children)
		if project := apps[0].Children[0].Project; project == nil || project.Directory != dir || project.Source != "workspace argument" {
			t.Fatalf("workspace flag not used: %v", args)
		}
	}
	p := projectProcess(20, 1, "codex", dir)
	p.Args = []string{"codex", "-C", "possibly-already-entered"}
	apps := []AppStat{{Children: []ProcDetail{p}}}
	attributeProjectFixture(apps[0].Children)
	if apps[0].Children[0].Project.Directory != dir || apps[0].Children[0].Project.Source != "cwd" {
		t.Fatal("relative argument invented an original launch directory")
	}
}

func TestProjectSymlinksAndSameBasenames(t *testing.T) {
	base := t.TempDir()
	a := fixtureRepository(t, filepath.Join(base, "one"), "repo")
	b := fixtureRepository(t, filepath.Join(base, "two"), "repo")
	r := projectResolver{directories: make(map[string]string)}
	if r.projectDirectory(filepath.Join(a, "src")) != a || r.projectDirectory(b) != b {
		t.Fatal("repository roots or namesake projects merged")
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(a, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if r.projectDirectory(filepath.Join(alias, "src")) != a {
		t.Fatal("same repository accessed through symlink split into another project")
	}
}

func TestProjectAttributionRetainsZeroMemoryWorkspaceAncestors(t *testing.T) {
	dir := fixtureRepository(t, t.TempDir(), "workspace")
	zed := projectProcess(10, 1, "zed-editor", "~")
	zed.RSSKB = 1024
	adapter := projectProcess(20, 10, "node", dir, 10)
	adapter.Args = []string{"node", "/opt/claude-agent-acp/dist/index.js"}
	child := projectProcess(30, 20, "node", "~", 20, 10)
	child.RSSKB = 2048
	processes := map[int]*collectedProcess{
		10: {name: "zed-editor", detail: zed},
		20: {name: "node adapter", detail: adapter},
		30: {name: "node worker", detail: child},
	}
	apps := groupProcesses(processes, 8192, GroupTree)
	if len(apps) != 1 || len(apps[0].Children) != 2 {
		t.Fatal("identity-only ancestor became a memory row or split the tree")
	}
	for _, p := range apps[0].Children {
		if p.PID == 30 && (p.Project == nil || p.Project.Directory != dir || p.Project.Owner.PID != 20) {
			t.Fatal("zero-memory ancestor lost verified dependency workspace")
		}
	}
}

func TestCodexDisplayOnlyArgumentsCannotInventWorkspace(t *testing.T) {
	dir := fixtureRepository(t, t.TempDir(), "repo with spaces")
	p := projectProcess(20, 1, "codex", "~")
	p.Args = nil
	p.Cmdline = "codex --cd " + dir
	processes := []ProcDetail{p}
	attributeProjectFixture(processes)
	if processes[0].Project != nil {
		t.Fatal("display command was interpreted as structured workspace arguments")
	}
}
