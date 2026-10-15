package collector

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	toolClaude = "claude"
	toolCodex  = "codex"
	toolZed    = "zed"
)

type projectResolver struct {
	home        string
	processes   map[ProcessIdentity]*ProcDetail
	direct      map[ProcessIdentity]*ProjectInfo
	directories map[string]string
}

func attributeProjects(processes map[int]*collectedProcess) {
	home, _ := os.UserHomeDir()
	r := projectResolver{home: home, processes: make(map[ProcessIdentity]*ProcDetail), direct: make(map[ProcessIdentity]*ProjectInfo), directories: make(map[string]string)}
	for _, process := range processes {
		p := &process.detail
		r.processes[processIdentity(*p)] = p
	}
	for id, p := range r.processes {
		if r.developmentProcess(*p) {
			r.direct[id] = r.ownProject(*p)
		}
	}
	for _, p := range r.processes {
		p.Project = r.resolve(*p)
	}
}

func (r *projectResolver) developmentProcess(p ProcDetail) bool {
	if developmentTool(p) != "" {
		return true
	}
	if p.Context == nil {
		return false
	}
	for _, a := range p.Context.Ancestors {
		ancestor := r.processes[ProcessIdentity{PID: a.PID, StartTime: a.StartTime}]
		if ancestor != nil && developmentTool(*ancestor) != "" {
			return true
		}
	}
	return false
}

func processIdentity(p ProcDetail) ProcessIdentity {
	return ProcessIdentity{PID: p.PID, StartTime: p.StartTime}
}

func developmentTool(p ProcDetail) string {
	names := []string{filepath.Base(p.Executable)}
	if p.Context != nil && p.Context.ApplicationPID == p.PID {
		names = append(names, p.Context.Application)
	}
	if args := p.Arguments(); len(args) > 0 {
		names = append(names, filepath.Base(args[0]))
	}
	for _, name := range names {
		switch contextProgram(name) {
		case toolClaude, "claude-code":
			return toolClaude
		case toolCodex, "codex-cli":
			return toolCodex
		case toolZed, "zed-editor":
			return toolZed
		}
	}
	args := p.Arguments()
	if len(args) > 1 && strings.Contains(filepath.ToSlash(args[1]), "/claude-agent-acp/") {
		return toolClaude
	}
	return ""
}

func (r *projectResolver) ownProject(p ProcDetail) *ProjectInfo {
	if developmentTool(p) == toolZed {
		return nil
	}
	path, source := projectEvidence(p)
	directory := r.projectDirectory(path)
	if directory == "" {
		return nil
	}
	return &ProjectInfo{Directory: directory, Source: source, Owner: processIdentity(p), Tool: developmentTool(p)}
}

func projectEvidence(p ProcDetail) (string, string) {
	if p.Session != nil && p.Session.ProjectDir != "" {
		return p.Session.ProjectDir, "verified session directory"
	}
	if developmentTool(p) == toolCodex {
		if dir := workspaceArgument(p.Args); filepath.IsAbs(dir) || strings.HasPrefix(dir, "~/") {
			return dir, "workspace argument"
		}
	}
	if p.Context != nil && p.Context.ProjectDir != "" {
		return p.Context.ProjectDir, p.Context.ProjectSource
	}
	return p.Cwd, currentDirectorySource
}

func workspaceArgument(args []string) string {
	for i, arg := range args {
		key, value, equals := strings.Cut(arg, "=")
		switch key {
		case "--cd", "-C", "--cwd", "--working-directory", "--workspace-root":
			if equals {
				return value
			}
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

func (r *projectResolver) resolve(p ProcDetail) *ProjectInfo {
	tool := developmentTool(p)
	if tool == toolZed {
		return nil
	}
	own := r.direct[processIdentity(p)]
	if tool != "" && own != nil {
		return own
	}
	if p.Context == nil {
		return nil
	}
	tool, inherited := r.ancestorToolProject(p, tool)
	if inherited != nil {
		return inherited
	}
	if tool == "" {
		return nil
	}
	if own != nil {
		return scopedProject(own, tool)
	}
	return r.ancestorProject(p, tool)
}

func (r *projectResolver) ancestorToolProject(p ProcDetail, tool string) (string, *ProjectInfo) {
	for _, a := range p.Context.Ancestors {
		id := ProcessIdentity{PID: a.PID, StartTime: a.StartTime}
		ancestor := r.processes[id]
		if ancestor == nil {
			continue
		}
		ancestorTool := developmentTool(*ancestor)
		if ancestorTool == "" {
			continue
		}
		if tool == "" {
			tool = ancestorTool
		}
		if project := r.direct[id]; project != nil {
			return tool, inheritedProject(project, tool)
		}
	}
	return tool, nil
}

func (r *projectResolver) ancestorProject(p ProcDetail, tool string) *ProjectInfo {
	for _, a := range p.Context.Ancestors {
		if project := r.direct[ProcessIdentity{PID: a.PID, StartTime: a.StartTime}]; project != nil {
			return inheritedProject(project, tool)
		}
	}
	return nil
}

func scopedProject(project *ProjectInfo, tool string) *ProjectInfo {
	result := *project
	result.Tool = tool
	return &result
}

func inheritedProject(project *ProjectInfo, tool string) *ProjectInfo {
	result := scopedProject(project, tool)
	result.Source = "ancestor workspace: " + project.Source
	return result
}

func (r *projectResolver) projectDirectory(directory string) string {
	if result, ok := r.directories[directory]; ok {
		return result
	}
	result := r.findProjectDirectory(directory)
	r.directories[directory] = result
	return result
}

func (r *projectResolver) findProjectDirectory(directory string) string {
	if directory == "~" {
		return ""
	}
	if strings.HasPrefix(directory, "~/") {
		directory = filepath.Join(r.home, directory[2:])
	}
	if !filepath.IsAbs(directory) {
		return ""
	}
	directory = filepath.Clean(directory)
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		directory = resolved
	}
	return r.rootedProjectDirectory(directory)
}

func (r *projectResolver) rootedProjectDirectory(directory string) string {
	if directory == r.home || directory == filepath.Dir(directory) || strings.HasPrefix(directory, "/proc/") || packageCache(directory) {
		return ""
	}
	if root := r.repositoryRoot(directory); root != "" {
		return root
	}
	// A checked-out repository is a workspace even under Windows' Temp
	// directory. Unidentified temporary directories remain unassigned.
	if strings.Contains(strings.ToLower(filepath.ToSlash(directory))+"/", "/appdata/local/temp/") {
		return ""
	}
	return directory
}

func (r *projectResolver) repositoryRoot(directory string) string {
	for current := directory; current != filepath.Dir(current) && current != r.home; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current
		}
	}
	return ""
}

func packageCache(directory string) bool {
	path := filepath.ToSlash(directory) + "/"
	for _, fragment := range []string{"/node_modules/", "/.npm/", "/.cache/", "/.local/share/zed/", "/.local/share/Zed/", "/Library/Caches/"} {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}
