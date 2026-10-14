//go:build linux

package collector

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func groupProcesses(processes map[int]*collectedProcess, totalKB int64, mode Grouping) []AppStat {
	groups := make(map[string]*appGroup)
	for _, process := range processes {
		id, descriptor := processGroup(process, processes, mode)
		group := groups[id]
		if group == nil {
			group = descriptor
			groups[id] = group
		}
		group.rssKB += process.detail.RSSKB
		group.swapKB += process.detail.SwapKB
		group.count++
		group.children = append(group.children, process.detail)
	}
	return buildAppStats(groups, totalKB)
}

func processGroup(process *collectedProcess, processes map[int]*collectedProcess, mode Grouping) (string, *appGroup) {
	if mode == GroupExecutable {
		return executableGroup(process)
	}
	root := applicationRoot(process, processes)
	if mode == GroupTree {
		root = processTreeRoot(process, processes)
	}
	identity := ProcessIdentity{PID: root.detail.PID, StartTime: root.detail.StartTime}
	id := fmt.Sprintf("%s:%d:%d", mode, identity.PID, identity.StartTime)
	group := &appGroup{name: root.name, root: identity, directory: root.detail.Cwd}
	if ctx := root.detail.Context; ctx != nil {
		group.name, group.directory = ctx.Application, ctx.ProjectDir
	}
	if session := root.detail.Session; session != nil && session.ProjectDir != "" {
		group.directory = session.ProjectDir
	}
	return id, group
}

func executableGroup(process *collectedProcess) (string, *appGroup) {
	key, name := process.name, process.name
	if executable := strings.TrimSuffix(process.detail.Executable, " (deleted)"); executable != "" {
		key, name = executable, filepath.Base(executable)
	}
	return "executable:" + key, &appGroup{name: name}
}

func applicationRoot(process *collectedProcess, processes map[int]*collectedProcess) *collectedProcess {
	if ctx := process.detail.Context; ctx != nil {
		candidate := processes[ctx.ApplicationPID]
		if candidate != nil && ctx.ApplicationStart != 0 && candidate.detail.StartTime == ctx.ApplicationStart {
			return candidate
		}
	}
	return process
}

func processTreeRoot(process *collectedProcess, processes map[int]*collectedProcess) *collectedProcess {
	root := process
	if process.detail.Context == nil {
		return root
	}
	for _, ancestor := range process.detail.Context.Ancestors {
		candidate := processes[ancestor.PID]
		if candidate == nil || candidate.detail.StartTime != ancestor.StartTime {
			break
		}
		if treeBoundary(candidate.name) {
			break
		}
		root = candidate
	}
	return root
}

func treeBoundary(name string) bool {
	program, _, _ := strings.Cut(name, " ")
	return contextBoundary(name) || slices.Contains([]string{"sh", "bash", "zsh", "fish", "dash", "ksh"}, strings.TrimPrefix(program, "-"))
}
