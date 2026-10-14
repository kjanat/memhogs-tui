//go:build windows

package tui

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"

	"memhogs.kjanat.dev/internal/collector"
)

// killProcess terminates a single PID.
// Windows has no SIGTERM equivalent via os.Process; both modes call Kill
// (TerminateProcess). The force parameter is accepted for interface
// compatibility but has no behavioral difference on Windows.
func killProcess(process collector.ProcDetail, force bool) error {
	if process.PID <= 0 || process.StartTime == 0 {
		return fmt.Errorf("process identity unavailable")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(process.PID))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return err
	}
	start := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	if start != process.StartTime {
		return fmt.Errorf("process identity changed; refusing termination")
	}
	return windows.TerminateProcess(h, 1)
}

// killSignalName returns the label for the kill confirmation modal.
// Both modes map to TerminateProcess on Windows; the force flag is
// accepted for cross-platform interface compatibility.
func killSignalName(force bool) string {
	return "TerminateProcess"
}

// killCmdStr lists precisely the captured PIDs in the confirmation.
func killCmdStr(children []collector.ProcDetail, force bool) string {
	var b strings.Builder
	b.WriteString("taskkill")
	for _, child := range children {
		fmt.Fprintf(&b, " /PID %d", child.PID)
	}
	b.WriteString(" /F")
	return b.String()
}
