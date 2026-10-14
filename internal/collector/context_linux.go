//go:build linux

package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func enrichProcessContexts(processes map[int]*collectedProcess, root string) {
	sockets := unixSocketPaths(filepath.Join(root, "net", "unix"))
	for _, process := range processes {
		orphan := contextRoot(process.detail, processes)
		initializeProcessContext(process, root, sockets, orphan)
	}
	// Build every chain before inheriting labels: map iteration order must not
	// decide whether a child gets its concrete application's name.
	for _, process := range processes {
		process.detail.Context.Ancestors = processAncestors(process.detail, processes)
	}
	for _, process := range processes {
		if chromiumProcessType(process.detail.Arguments()) {
			markChromiumFamily(process, processes)
		}
	}
	for _, process := range processes {
		inheritApplication(process, processes)
	}
	for _, process := range processes {
		setProcessOrigin(process, processes)
	}
}

func contextRoot(process ProcDetail, processes map[int]*collectedProcess) bool {
	parent := processes[process.ParentPID]
	return process.ParentPID <= 1 || parent == nil || parent.name == "systemd"
}

func chromiumProcessType(args []string) bool {
	return slices.Contains([]string{"renderer", "gpu-process", "utility", "zygote", "broker", "crashpad-handler"}, argumentValue(args, "--type"))
}

func applicationHint(process *collectedProcess) (string, string) {
	args := process.detail.Arguments()
	if path := applicationPackage(args); path != "" {
		if class := argumentValue(args, "--class"); class != "" {
			return class, "package argument and --class"
		}
		return packageName(path), "package argument: " + path
	}
	return process.name, "executable/process name"
}

func applicationPackage(args []string) string {
	path := argumentValue(args, "--app-path")
	if path == "" {
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") && strings.HasSuffix(arg, ".asar") {
				path = arg
				break
			}
		}
	}
	if path == "" || !strings.HasSuffix(path, ".asar") || strings.HasSuffix(path, "/default_app.asar") {
		return ""
	}
	return filepath.Clean(path)
}

func packageName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".asar")
	if name != "app" {
		return name
	}
	directory := filepath.Dir(path)
	if filepath.Base(directory) == "resources" {
		directory = filepath.Dir(directory)
	}
	return strings.TrimSuffix(filepath.Base(directory), "-bin")
}

func sameExecutable(a, b *collectedProcess) bool {
	aID, bID := a.detail.ExecutableID, b.detail.ExecutableID
	if aID.Inode != 0 && bID.Inode != 0 && (aID.Inode != bID.Inode || aID.Device != bID.Device) {
		return false
	}
	if a.detail.Executable != "" && b.detail.Executable != "" {
		return strings.TrimSuffix(a.detail.Executable, " (deleted)") == strings.TrimSuffix(b.detail.Executable, " (deleted)")
	}
	return false // a shared display name does not prove executable identity
}

func inheritApplication(process *collectedProcess, processes map[int]*collectedProcess) {
	ctx := process.detail.Context
	worker := chromiumProcessType(process.detail.Arguments())
	if startsApplicationInstance(ctx, worker) {
		return // a packaged/Chromium main process starts its own instance
	}
	for _, ancestor := range ctx.Ancestors {
		parent := processes[ancestor.PID]
		if inheritanceBoundary(process, parent) {
			break
		}
		if !sameExecutable(process, parent) {
			continue // launchers and sandbox helpers can sit between matching executables
		}
		if !inheritParentContext(ctx, parent, worker) {
			break
		}
		if worker && !chromiumProcessType(parent.detail.Arguments()) {
			break // nearest verified main process, not an older independent launch
		}
	}
}

func startsApplicationInstance(ctx *ProcessContext, worker bool) bool {
	return !worker && (ctx.Chromium || ctx.PackagePath != "")
}

func inheritanceBoundary(process, parent *collectedProcess) bool {
	return contextBoundary(parent.name) || distinctSession(process.detail.Session, parent.detail.Session)
}

func inheritParentContext(ctx *ProcessContext, parent *collectedProcess, worker bool) bool {
	// Recompute from original arguments instead of an already inherited label.
	application, source := applicationHint(parent)
	if !inheritApplicationHint(ctx, application, source, applicationPackage(parent.detail.Arguments()), parent.detail, worker) {
		return false
	}
	if unhelpfulDirectory(ctx.ProjectDir) && parent.detail.Cwd != "" && !strings.HasPrefix(parent.detail.Cwd, "/proc/") {
		ctx.ProjectDir, ctx.ProjectSource = parent.detail.Cwd, "application ancestor cwd"
	}
	return true
}

func inheritApplicationHint(ctx *ProcessContext, application, source, key string, parent ProcDetail, worker bool) bool {
	if !worker && ctx.ApplicationKey != "" && key != "" && ctx.ApplicationKey != key {
		return false
	}
	if worker || ctx.ApplicationKey == "" || key != "" {
		ctx.Application, ctx.ApplicationSource = application, source
		ctx.ApplicationPID, ctx.ApplicationStart, ctx.ApplicationKey = parent.PID, parent.StartTime, key
	}
	return true
}

func distinctSession(a, b *SessionInfo) bool {
	return a != nil && b != nil && a.ID != "" && b.ID != "" && a.ID != b.ID
}

func setProcessOrigin(process *collectedProcess, processes map[int]*collectedProcess) {
	ctx := process.detail.Context
	for _, ancestor := range ctx.Ancestors {
		parent := processes[ancestor.PID]
		if sameExecutable(process, parent) {
			continue
		}
		if contextBoundary(parent.name) {
			break
		}
		if ctx.Origin == "" || !contextWrapper(parent.name) {
			ctx.Origin, ctx.OriginPID = parent.detail.Context.Application, parent.detail.PID
			if session := parent.detail.Session; session != nil && session.Name != "" {
				ctx.Origin += " (" + session.Name + ")"
			}
		}
		if !contextWrapper(parent.name) {
			break
		}
	}
}

func contextWrapper(name string) bool {
	return slices.Contains([]string{"sh", "bash", "zsh", "fish", "dash", "node", "node-MainThread", "MainThread", "npx", "npm", "npm exec", "AppRun"}, name) || strings.HasPrefix(name, "node ") || strings.HasPrefix(name, "npm ")
}

func markChromiumFamily(process *collectedProcess, processes map[int]*collectedProcess) {
	process.detail.Context.Chromium = true
	for _, ancestor := range process.detail.Context.Ancestors {
		parent := processes[ancestor.PID]
		if !sameExecutable(process, parent) {
			continue
		}
		parent.detail.Context.Chromium = true
	}
}

func unhelpfulDirectory(path string) bool {
	return path == "" || path == "~" || path == "/" || strings.HasPrefix(path, "/proc/")
}

func unixSocketPaths(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	paths := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 8 && strings.HasPrefix(fields[7], "/") {
			paths[fields[6]] = line[strings.IndexByte(line, '/'):]
		}
	}
	return paths
}

func openContextResources(directory string, sockets map[string]string) []string {
	file, err := os.Open(filepath.Join(directory, "fd"))
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	const maxContextFDs = 128
	names, _ := file.Readdirnames(maxContextFDs)
	var resources []string
	for _, name := range names {
		target, err := os.Readlink(filepath.Join(directory, "fd", name))
		if err != nil {
			continue
		}
		if inode, ok := strings.CutPrefix(target, "socket:["); ok {
			target = sockets[strings.TrimSuffix(inode, "]")]
		}
		if strings.Contains(target, "/.git/") || strings.HasSuffix(target, ".asar") {
			resources = append(resources, target)
		}
	}
	slices.Sort(resources)
	return slices.Compact(resources)
}

func setResourceProject(ctx *ProcessContext) {
	var repositories []string
	for _, resource := range ctx.Resources {
		if directory, _, ok := strings.Cut(resource, "/.git/"); ok {
			repositories = append(repositories, directory)
		}
	}
	repositories = slices.Compact(repositories)
	if len(repositories) == 1 {
		ctx.ProjectDir, ctx.ProjectSource = repositories[0], "open repository resource"
	}
}

func argumentValue(args []string, flag string) string {
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			return value
		}
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func initializeProcessContext(process *collectedProcess, root string, sockets map[string]string, orphan bool) {
	p := &process.detail
	application, source := applicationHint(process)
	p.Context = &ProcessContext{
		Application: application, ApplicationPID: p.PID, ApplicationStart: p.StartTime,
		PackagePath: applicationPackage(p.Arguments()), ApplicationKey: applicationPackage(p.Arguments()),
		ApplicationSource: source, ProjectDir: p.Cwd, ProjectSource: currentDirectorySource,
	}
	if !orphan || !unhelpfulDirectory(p.Cwd) {
		return
	}
	p.Context.Resources = openContextResources(filepath.Join(root, strconv.Itoa(p.PID)), sockets)
	setResourceProject(p.Context)
	if start, err := readStartTime(filepath.Join(root, strconv.Itoa(p.PID))); err != nil || start != p.StartTime {
		p.Context.Resources = nil
		p.Context.ProjectDir, p.Context.ProjectSource = p.Cwd, currentDirectorySource
	}
}
