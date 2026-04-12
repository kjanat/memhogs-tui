//go:build darwin

// Darwin implementation: uses sysctl + proc_pidinfo for memory snapshots.

package collector

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// proc_pidinfo constants for SYS_PROC_INFO syscall.
const (
	procInfoCallPidInfo = 1 // PROC_INFO_CALL_PIDINFO
	procPidTaskInfo     = 4 // PROC_PIDTASKINFO flavor
)

// procTaskInfo mirrors struct proc_taskinfo from <sys/proc_info.h>.
// Used with SYS_PROC_INFO + PROC_PIDTASKINFO to get accurate RSS.
type procTaskInfo struct {
	VirtualSize      uint64
	ResidentSize     uint64 // RSS in bytes — the field we need
	TotalUser        uint64
	TotalSystem      uint64
	ThreadsUser      uint64
	ThreadsSystem    uint64
	Policy           int32
	Faults           int32
	Pageins          int32
	CowFaults        int32
	MessagesSent     int32
	MessagesReceived int32
	SyscallsMach     int32
	SyscallsUnix     int32
	Csw              int32
	Threadnum        int32
	Numrunning       int32
	Priority         int32
}

// xswUsage mirrors struct xsw_usage from <sys/sysctl.h>.
// Returned by sysctl("vm.swapusage") as raw bytes.
type xswUsage struct {
	Total uint64
	Avail uint64
	Used  uint64
}

// Collect gathers a complete memory [Snapshot] using macOS sysctl and
// proc_pidinfo APIs. PSI and OOM are not available on macOS.
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
	totalBytes, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return SystemInfo{}, fmt.Errorf("hw.memsize: %w", err)
	}

	pagesize := int64(unix.Getpagesize())
	info := SystemInfo{
		MemTotalKB: int64(totalBytes / 1024),
	}

	// Available ≈ free + speculative + purgeable pages
	var availPages int64
	if v, err := unix.SysctlUint32("vm.page_free_count"); err == nil {
		availPages += int64(v)
	}
	if v, err := unix.SysctlUint32("vm.page_speculative_count"); err == nil {
		availPages += int64(v)
	}
	if v, err := unix.SysctlUint32("vm.page_purgeable_count"); err == nil {
		availPages += int64(v)
	}
	info.MemAvailableKB = availPages * pagesize / 1024
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB

	// Swap via vm.swapusage
	if raw, err := unix.SysctlRaw("vm.swapusage"); err == nil && len(raw) >= int(unsafe.Sizeof(xswUsage{})) {
		var sw xswUsage
		sw.Total = binary.LittleEndian.Uint64(raw[0:8])
		sw.Avail = binary.LittleEndian.Uint64(raw[8:16])
		sw.Used = binary.LittleEndian.Uint64(raw[16:24])
		info.SwapTotalKB = int64(sw.Total / 1024)
		info.SwapFreeKB = int64(sw.Avail / 1024)
		info.SwapUsedKB = int64(sw.Used / 1024)
	}

	return info, nil
}

func collectApps(totalKB int64) []AppStat {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}

	groups := make(map[string]*appGroup)
	for i := range procs {
		kp := &procs[i]
		pid := int(kp.Proc.P_pid)
		if pid == 0 {
			continue
		}

		rss, swap := procMem(int32(pid))
		name := procName(kp, pid)
		if name == "" {
			continue
		}

		g, ok := groups[name]
		if !ok {
			g = &appGroup{}
			groups[name] = g
		}
		g.rssKB += rss
		g.swapKB += swap
		g.count++
		g.children = append(g.children, ProcDetail{
			PID:     pid,
			Cmdline: cmdlineDisplay(pid),
			RSSKB:   rss,
			SwapKB:  swap,
		})
	}

	return buildAppStats(groups, totalKB)
}

// procMem reads RSS and swap via SYS_PROC_INFO + PROC_PIDTASKINFO.
// This avoids the int16 truncation in KinfoProc.Eproc.Xrssize.
func procMem(pid int32) (rssKB, swapKB int64) {
	var ti procTaskInfo
	size := int32(unsafe.Sizeof(ti))
	r, _, e := unix.Syscall6(
		unix.SYS_PROC_INFO,
		procInfoCallPidInfo,
		uintptr(pid),
		procPidTaskInfo,
		0,
		uintptr(unsafe.Pointer(&ti)),
		uintptr(size),
	)
	if e != 0 || int32(r) < size {
		return 0, 0
	}
	return int64(ti.ResidentSize / 1024), 0 // macOS has no per-process swap metric
}

// procName returns the display name for a process.
// Uses P_comm from kinfo_proc, falling back to cmdline when truncated.
func procName(kp *unix.KinfoProc, pid int) string {
	comm := commString(kp.Proc.P_comm)
	if len(comm) >= 15 || genericComm(comm) {
		if full := cmdlineName(pid); len(full) > len(comm) {
			return full
		}
	}
	return comm
}

// commString extracts a Go string from P_comm's fixed-size byte array.
func commString(comm [17]byte) string {
	n := bytes.IndexByte(comm[:], 0)
	if n < 0 {
		n = len(comm)
	}
	return string(comm[:n])
}

// genericComm returns true for thread names that hide the real application.
func genericComm(name string) bool {
	switch name {
	case "MainThread", "Web Content", "WebExtensions",
		"Isolated Web Co", "Socket Thread",
		"StreamTrans", "Timer", "worker",
		"pool-thread", "Thread", "main":
		return true
	}
	return false
}

// cmdlineName returns exe basename + first non-flag arg basename.
func cmdlineName(pid int) string {
	args := procArgs(pid)
	if len(args) == 0 {
		return ""
	}
	exe := filepath.Base(args[0])

	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if sub := filepath.Base(arg); sub != "" && sub != "." {
			return exe + " " + sub
		}
	}
	return exe
}

// cmdlineDisplay returns the full command for display when a group is expanded.
// Strips path from argv[0], squashes home dir to ~.
func cmdlineDisplay(pid int) string {
	args := procArgs(pid)
	if len(args) == 0 {
		return ""
	}
	args[0] = filepath.Base(args[0])
	s := strings.Join(args, " ")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	return s
}

// procArgs reads the full argv for a process via kern.procargs2.
// Format: [int32 argc] [exe path \0] [null padding] [argv0 \0] [argv1 \0] ...
// Only returns argc arguments — stops before environment variables.
func procArgs(pid int) []string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return nil
	}

	// Read argc to know where argv ends (env vars follow after)
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	data := raw[4:]

	// Find end of executable path
	pathEnd := bytes.IndexByte(data, 0)
	if pathEnd < 0 {
		return nil
	}

	// Skip padding nulls after exe path
	rest := data[pathEnd:]
	start := 0
	for start < len(rest) && rest[start] == 0 {
		start++
	}
	rest = rest[start:]

	// Split by null bytes, but only take argc entries (skip env vars)
	args := make([]string, 0, argc)
	for range argc {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			if len(rest) > 0 {
				args = append(args, string(rest))
			}
			break
		}
		if end > 0 {
			args = append(args, string(rest[:end]))
		}
		rest = rest[end+1:]
	}
	return args
}
