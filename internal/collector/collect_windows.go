//go:build windows

package collector

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpsapi              = windows.NewLazySystemDLL("psapi.dll")
	modkernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procGetProcessMemInfo = modpsapi.NewProc("GetProcessMemoryInfo")
	procGlobalMemStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// Collect gathers a complete memory [Snapshot] using Windows APIs.
// PSI and OOM are not available on Windows and will be nil/empty.
func Collect() (*Snapshot, error) {
	sys, err := readMemInfo()
	if err != nil {
		return nil, fmt.Errorf("meminfo: %w", err)
	}
	return &Snapshot{
		At:     time.Now(),
		Apps:   collectApps(sys.MemTotalKB),
		System: sys,
	}, nil
}

func readMemInfo() (SystemInfo, error) {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, err := procGlobalMemStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return SystemInfo{}, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	info := SystemInfo{
		MemTotalKB:     int64(ms.TotalPhys / 1024),
		MemAvailableKB: int64(ms.AvailPhys / 1024),
		SwapTotalKB:    int64(ms.TotalPageFile / 1024),
		SwapFreeKB:     int64(ms.AvailPageFile / 1024),
	}
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB
	info.SwapUsedKB = info.SwapTotalKB - info.SwapFreeKB
	return info, nil
}

func collectApps(totalKB int64) []AppStat {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))

	if err := windows.Process32First(snap, &pe); err != nil {
		return nil
	}

	groups := make(map[string]*appGroup)
	for {
		pid := pe.ProcessID
		if pid == 0 { // System Idle Process
			if windows.Process32Next(snap, &pe) != nil {
				break
			}
			continue
		}

		exe := windows.UTF16ToString(pe.ExeFile[:])
		name := groupName(exe)
		rss, swap := procMem(pid)

		g, ok := groups[name]
		if !ok {
			g = &appGroup{}
			groups[name] = g
		}
		g.rssKB += rss
		g.swapKB += swap
		g.count++
		g.children = append(g.children, ProcDetail{
			PID:     int(pid),
			Cmdline: exe,
			RSSKB:   rss,
			SwapKB:  swap,
		})

		if windows.Process32Next(snap, &pe) != nil {
			break
		}
	}

	return buildAppStats(groups, totalKB)
}

// groupName strips the .exe suffix and lowercases for consistent grouping.
func groupName(exe string) string {
	name := filepath.Base(exe)
	if lower := strings.ToLower(name); strings.HasSuffix(lower, ".exe") {
		name = name[:len(name)-4]
	}
	return strings.ToLower(name)
}

// procMem reads WorkingSetSize (≈RSS) and PagefileUsage (≈swap/commit) for a PID.
func procMem(pid uint32) (rssKB, swapKB int64) {
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ,
		false, pid,
	)
	if err != nil {
		return 0, 0
	}
	defer windows.CloseHandle(h)

	var pmc processMemoryCounters
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	r, _, _ := procGetProcessMemInfo.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&pmc)),
		uintptr(pmc.CB),
	)
	if r == 0 {
		return 0, 0
	}
	return int64(pmc.WorkingSetSize / 1024), int64(pmc.PagefileUsage / 1024)
}
