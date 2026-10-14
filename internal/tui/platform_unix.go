//go:build unix

package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"memhogs.kjanat.dev/internal/collector"
)

// killProcess sends SIGTERM or SIGKILL to a single PID using the standard
// library's os.FindProcess + Signal.
func killProcess(process collector.ProcDetail, force bool) error {
	if process.PID <= 0 || process.StartTime == 0 {
		return fmt.Errorf("process identity unavailable")
	}
	p, err := os.FindProcess(process.PID)
	if err != nil {
		return err
	}
	defer func() { _ = p.Release() }()
	start, err := collector.ProcessStartTime(process.PID)
	if err != nil {
		return err
	}
	if start != process.StartTime {
		return fmt.Errorf("process identity changed; refusing signal")
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

// killCmdStr lists precisely the captured PIDs in the confirmation.
func killCmdStr(children []collector.ProcDetail, force bool) string {
	sig := "TERM"
	if force {
		sig = "KILL"
	}
	pids := make([]string, 0, len(children))
	for _, child := range children {
		pids = append(pids, strconv.Itoa(child.PID))
	}
	return "kill -" + sig + " " + strings.Join(pids, " ")
}
