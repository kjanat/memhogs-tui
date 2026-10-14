//go:build linux

package collector

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCPUTime(t *testing.T) {
	fields := strings.Fields("S 1 0 0 0 0 0 0 0 0 0 120 30 99999 99999 0 0 1 0 100")
	stat := "42 (name ) with ( parentheses)) " + strings.Join(fields, " ")
	if seconds, ok := parseCPUTime(stat, 250); !ok || seconds != .6 {
		t.Fatalf("must use user+system only with supplied tick rate: %g %v", seconds, ok)
	}
	for _, input := range []string{"", "42 (x) S", strings.Replace(stat, "120", "bad", 1), strings.Replace(stat, "30", "-1", 1)} {
		if _, ok := parseCPUTime(input, 100); ok {
			t.Fatalf("accepted malformed CPU counter %q", input)
		}
	}
	if _, ok := parseCPUTime(stat, 0); ok {
		t.Fatal("guessed missing tick rate")
	}
}

func TestAuxClockTicks(t *testing.T) {
	for _, size := range []int{4, 8} {
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			data := make([]byte, 8*size)
			for i, value := range []uint32{6, 4096, 17, 250, 0, 0, 17, 99} {
				if size == 4 {
					order.PutUint32(data[i*size:], value)
				} else {
					order.PutUint64(data[i*size:], uint64(value))
				}
			}
			if got := auxClockTicks(data, size, order); got != 250 {
				t.Fatalf("word size %d %T: %d", size, order, got)
			}
			for _, malformed := range [][]byte{nil, data[:len(data)-1], data[4*size:]} {
				if auxClockTicks(malformed, size, order) != 0 {
					t.Fatal("accepted missing, truncated, or post-AT_NULL tick rate")
				}
			}
		}
	}
	if auxClockTicks(make([]byte, 16), 3, binary.NativeEndian) != 0 {
		t.Fatal("accepted invalid word size")
	}
	if clockTicks() == 0 {
		t.Fatal("unable to read native AT_CLKTCK")
	}
}

func TestCPUObservesBusyProcess(t *testing.T) {
	// The shell loop stays within this child process and needs no external tools.
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	dir := filepath.Join("/proc", fmt.Sprint(cmd.Process.Pid))
	before := waitForShellCPU(t, dir)
	before.PID = cmd.Process.Pid
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		_, after, ok := readProcess(dir)
		requireCPU(t, ok && !after.CPUAt.IsZero(), "busy process not sampled")
		after.PID = cmd.Process.Pid
		if after.CPUTime > before.CPUTime {
			usage, known := processCPU(after, before)
			requireCPU(t, known && usage > 0, "busy CPU unavailable")
			t.Logf("busy child PID %d: %.1f%% CPU over %s", after.PID, usage, after.CPUAt.Sub(before.CPUAt))
			return
		}
	}
	t.Fatal("busy child CPU counter did not advance")
}

func waitForShellCPU(t *testing.T, dir string) ProcDetail {
	t.Helper()
	// Wait for exec so the counter belongs to the requested shell image.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, p, ok := readProcess(dir); ok && len(p.Args) > 0 && p.Args[0] == "/bin/sh" {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(time.Millisecond)
	}
}
