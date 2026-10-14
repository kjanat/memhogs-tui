//go:build linux

package tui

import (
	"os/exec"
	"strings"
	"testing"

	"memhogs.kjanat.dev/internal/collector"
)

func TestKillRevalidatesProcessIdentity(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	start, err := collector.ProcessStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	process := collector.ProcDetail{PID: cmd.Process.Pid, StartTime: start + 1}
	if err := killProcess(process, false); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("PID reuse not rejected: %v", err)
	}
	for _, invalid := range []collector.ProcDetail{{PID: 0, StartTime: start}, {PID: -1, StartTime: start}, {PID: process.PID}} {
		if err := killProcess(invalid, true); err == nil {
			t.Fatal("unsafe identity accepted")
		}
	}
	process.StartTime = start
	if err := killProcess(process, false); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper exited without receiving signal")
	}
}

func TestDisplayedKillCommandUsesCapturedPIDs(t *testing.T) {
	processes := []collector.ProcDetail{{PID: 12}, {PID: 34}}
	if got := killCmdStr(processes, false); got != "kill -TERM 12 34" {
		t.Fatalf("got %q", got)
	}
	if got := killCmdStr(processes, true); got != "kill -KILL 12 34" {
		t.Fatalf("got %q", got)
	}
}
