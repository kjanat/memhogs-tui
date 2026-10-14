package collector

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

func nativeProcess(pid, parent int, name string, start uint64, rss int64) *collectedProcess {
	return &collectedProcess{name: name, detail: ProcDetail{PID: pid, ParentPID: parent, StartTime: start, RSSKB: rss}}
}

func nativePIDs(app AppStat) []int {
	pids := make([]int, 0, len(app.Children))
	for _, p := range app.Children {
		pids = append(pids, p.PID)
	}
	slices.Sort(pids)
	return pids
}

func TestNativeDefaultTreeIncludesDifferentExecutables(t *testing.T) {
	for _, session := range []string{"launchd", "init", "explorer.exe", "services.exe", "systemd"} {
		t.Run(session, func(t *testing.T) {
			processes := map[int]*collectedProcess{
				1:  nativeProcess(1, 0, "system", 1, 0),
				2:  nativeProcess(2, 1, session, 2, 0),
				10: nativeProcess(10, 2, "zed-editor", 10, 4096),
				11: nativeProcess(11, 10, "node", 11, 2048),
				12: nativeProcess(12, 11, "language-server", 12, 1024),
				20: nativeProcess(20, 2, "zed-editor", 20, 3072),
				21: nativeProcess(21, 20, "node", 21, 2048),
			}
			apps := groupNativeProcesses(processes, 102400, DefaultGrouping())
			if len(apps) != 2 || !reflect.DeepEqual(nativePIDs(apps[0]), []int{10, 11, 12}) || !reflect.DeepEqual(nativePIDs(apps[1]), []int{20, 21}) {
				t.Fatalf("native tree split helpers or mixed independent roots: %+v", apps)
			}
			if apps[0].GroupID.Mode != GroupTree || apps[0].Root.PID != 10 || apps[0].RSSKB != 7168 || math.Abs(apps[0].MemPct-7) > 1e-9 {
				t.Fatalf("incorrect default tree identity/accounting: %+v", apps[0])
			}
		})
	}
}

func TestNativeTreeParentIdentityGuards(t *testing.T) {
	for _, tt := range []struct {
		name          string
		parentStart   uint64
		parentPresent bool
	}{
		{"missing", 1, false}, {"unreadable", 0, true}, {"PID reused", 30, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			processes := map[int]*collectedProcess{20: nativeProcess(20, 10, "node", 20, 4096)}
			if tt.parentPresent {
				processes[10] = nativeProcess(10, 1, "zed-editor", tt.parentStart, 2048)
			}
			apps := groupNativeProcesses(processes, 0, GroupTree)
			for _, app := range apps {
				if slices.Contains(nativePIDs(app), 20) && app.Root.PID != 20 {
					t.Fatal("child borrowed unverified or reused parent identity")
				}
			}
		})
	}
}

func TestNativeTreeRetainsZeroMemoryRootAndIntermediary(t *testing.T) {
	processes := map[int]*collectedProcess{
		10: nativeProcess(10, 1, "zed-editor", 10, 0),
		11: nativeProcess(11, 10, "helper", 11, 0),
		12: nativeProcess(12, 11, "node", 12, 4096),
	}
	apps := groupNativeProcesses(processes, 0, GroupTree)
	if len(apps) != 1 || apps[0].Root.PID != 10 || !reflect.DeepEqual(nativePIDs(apps[0]), []int{12}) {
		t.Fatalf("lost identity-only ancestry or added it to kill scope: %+v", apps)
	}
	if apps[0].RootProcess == nil || apps[0].RootProcess.PID != 10 {
		t.Fatal("root metadata unavailable")
	}
}

func TestNativeTreeStopsAtShellsAndPIDOne(t *testing.T) {
	for _, shell := range []string{"zsh", "-bash", "cmd.exe", "powershell.exe", "pwsh.exe", "WindowsTerminal.exe"} {
		t.Run(shell, func(t *testing.T) {
			processes := map[int]*collectedProcess{
				1:  nativeProcess(1, 0, "unknown-root", 1, 0),
				2:  nativeProcess(2, 1, shell, 2, 0),
				10: nativeProcess(10, 2, "editor", 10, 4096),
				20: nativeProcess(20, 2, "editor", 20, 2048),
			}
			apps := groupNativeProcesses(processes, 0, GroupTree)
			if len(apps) != 2 || apps[0].Root.PID != 10 || apps[1].Root.PID != 20 {
				t.Fatalf("crossed shell boundary: %+v", apps)
			}
			processes[10].detail.ParentPID = 1
			apps = groupNativeProcesses(processes, 0, GroupTree)
			if apps[0].Root.PID != 10 {
				t.Fatal("grouped application under PID 1")
			}
		})
	}
}

func TestNativeExplicitExecutableGroupingRemainsBroad(t *testing.T) {
	processes := map[int]*collectedProcess{
		10: nativeProcess(10, 1, "electron", 10, 4096),
		20: nativeProcess(20, 1, "electron", 20, 2048),
	}
	apps := groupNativeProcesses(processes, 0, GroupExecutable)
	if len(apps) != 1 || apps[0].ProcCount != 2 || apps[0].Root.PID != 0 {
		t.Fatalf("explicit executable grouping changed: %+v", apps)
	}
}
