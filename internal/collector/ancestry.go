package collector

import (
	"path"
	"slices"
	"strings"
)

const (
	maxContextAncestors    = 16
	currentDirectorySource = "cwd"
)

func processAncestors(p ProcDetail, processes map[int]*collectedProcess) []ProcessAncestor {
	if p.StartTime == 0 {
		return nil
	}
	var ancestors []ProcessAncestor
	seen := map[int]bool{p.PID: true}
	parent := p.ParentPID
	descendantStart := p.StartTime
	for len(ancestors) < maxContextAncestors {
		candidate := processes[parent]
		if candidate == nil || seen[parent] || candidate.detail.StartTime == 0 || candidate.detail.StartTime > descendantStart {
			break
		}
		seen[parent] = true
		a := candidate.detail
		ancestors = append(ancestors, ProcessAncestor{PID: a.PID, StartTime: a.StartTime, Name: candidate.name, Executable: a.Executable, Cwd: a.Cwd})
		parent = a.ParentPID
		descendantStart = a.StartTime
	}
	return ancestors
}

func contextBoundary(name string) bool {
	program := contextProgram(name)
	return strings.HasPrefix(program, "tmux") || slices.Contains([]string{
		"systemd", "gnome-shell", "init", "login", "sshd", "launchd", "kernel_task", "loginwindow", "windowserver", "launchservicesd", "runningboardd", "finder", "dock",
		"explorer", "services", "wininit", "winlogon", "smss", "csrss", "svchost", "taskhostw", "taskeng", "sihost",
	}, program)
}

func contextProgram(name string) string {
	program, _, _ := strings.Cut(name, " ")
	program = path.Base(strings.ReplaceAll(program, "\\", "/"))
	return strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(program), ".exe"), "-")
}
