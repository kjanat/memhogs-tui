//go:build windows

package tui

import (
	"fmt"
	"os"
)

// killProcess terminates a single PID.
// Windows has no SIGTERM equivalent via os.Process; both modes call Kill
// (TerminateProcess). The force parameter is accepted for interface
// compatibility but has no behavioral difference on Windows.
func killProcess(pid int, force bool) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// killSignalName returns the label for the kill confirmation modal.
// Both modes map to TerminateProcess on Windows; the force flag is
// accepted for cross-platform interface compatibility.
func killSignalName(force bool) string {
	return "TerminateProcess"
}

// killCmdStr returns the command shown in the kill modal.
// Both modes use /F since Go's os.Process.Kill maps to TerminateProcess.
func killCmdStr(name string, force bool) string {
	return fmt.Sprintf("taskkill /IM %q /F", name+".exe")
}

// rescueCmdStr returns the emergency kill command shown in the sidebar.
func rescueCmdStr(name string) string {
	return fmt.Sprintf("taskkill /IM %q /F", name+".exe")
}
