//go:build unix

package tui

import (
	"fmt"
	"os"
	"syscall"
)

// killProcess sends SIGTERM or SIGKILL to a single PID using the standard
// library's os.FindProcess + Signal.
func killProcess(pid int, force bool) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if force {
		return p.Kill()
	}
	return p.Signal(syscall.SIGTERM)
}

// killSignalName returns the signal label for the kill confirmation modal.
func killSignalName(force bool) string {
	if force {
		return "SIGKILL"
	}
	return "SIGTERM"
}

// killCmdStr returns the shell command shown in the kill modal.
func killCmdStr(name string, force bool) string {
	sig := "TERM"
	if force {
		sig = "KILL"
	}
	return fmt.Sprintf("pkill -%s -x -- %q", sig, name)
}

// killCmdStrPID returns the shell command shown when killing a single process.
func killCmdStrPID(pid int, force bool) string {
	sig := "TERM"
	if force {
		sig = "KILL"
	}
	return fmt.Sprintf("kill -%s %d", sig, pid)
}

// rescueCmdStr returns the emergency kill command shown in the sidebar.
func rescueCmdStr(name string) string {
	return fmt.Sprintf("pkill -x -- %q", name)
}
