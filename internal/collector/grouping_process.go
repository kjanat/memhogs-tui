package collector

import (
	"path/filepath"
	"slices"
	"strings"
)

type collectedProcess struct {
	name   string
	detail ProcDetail
}

func groupProcesses(processes map[int]*collectedProcess, totalKB int64, mode Grouping) []AppStat {
	groups := make(map[GroupID]*appGroup)
	for _, process := range processes {
		if !memoryConsumer(process.detail) {
			continue // identity-only ancestors do not contribute rows or kill targets
		}
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

func processGroup(process *collectedProcess, processes map[int]*collectedProcess, mode Grouping) (GroupID, *appGroup) {
	if mode == GroupExecutable {
		return executableGroup(process)
	}
	root := applicationRoot(process, processes)
	if mode == GroupTree {
		root = processTreeRoot(process, processes)
	}
	identity := ProcessIdentity{PID: root.detail.PID, StartTime: root.detail.StartTime}
	id := GroupID{Mode: mode, Root: identity}
	rootDetail := root.detail
	group := &appGroup{name: root.name, root: identity, rootProcess: &rootDetail, directory: root.detail.Cwd}
	if ctx := root.detail.Context; ctx != nil {
		group.name, group.directory = ctx.Application, ctx.ProjectDir
	}
	if session := root.detail.Session; session != nil && session.ProjectDir != "" {
		group.directory = session.ProjectDir
	}
	return id, group
}

func executableGroup(process *collectedProcess) (GroupID, *appGroup) {
	identity, name := process.detail.ExecutableID, process.name
	if executable := strings.TrimSuffix(process.detail.Executable, " (deleted)"); executable != "" {
		identity.Path, name = executable, filepath.Base(executable)
	}
	id := GroupID{Mode: GroupExecutable, Executable: identity}
	if identity.Path == "" {
		// An unreadable executable is not evidence that two namesakes share it.
		id.Root = ProcessIdentity{PID: process.detail.PID, StartTime: process.detail.StartTime}
	}
	return id, &appGroup{name: name}
}

func memoryConsumer(p ProcDetail) bool {
	return p.RSSKB > 0 || p.SwapKB > 0 || p.CommitKB > 0
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
		if candidate.detail.PID <= 1 || treeBoundary(candidate.name) {
			break
		}
		root = candidate
	}
	return root
}

func treeBoundary(name string) bool {
	return contextBoundary(name) || slices.Contains([]string{"sh", "bash", "zsh", "fish", "dash", "ksh", "cmd", "powershell", "pwsh", "conhost", "openconsole", "windowsterminal"}, contextProgram(name))
}
