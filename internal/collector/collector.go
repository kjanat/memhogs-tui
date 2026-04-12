//go:build linux

// Package collector reads Linux /proc to gather point-in-time memory usage
// snapshots grouped by application name.
//
// Each [Snapshot] aggregates per-process RSS, swap, OOM scores, and system-wide
// memory pressure into typed structures ready for display.
// The collector is stateless; call [Collect] on each tick.
package collector

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Snapshot is a point-in-time view of system memory state.
type Snapshot struct {
	At     time.Time  // when the snapshot was taken
	Apps   []AppStat  // top applications sorted by RSS descending
	System SystemInfo // system-wide memory counters
	PSI    *PSIInfo   // memory pressure; nil if /proc/pressure/memory is unavailable
	OOM    []OOMEntry // top OOM kill candidates sorted by score descending
}

// AppStat is aggregated memory for a named application group.
// Name serves as both the display label and the grouping key.
type AppStat struct {
	Name      string       // short name: exe basename + first non-flag arg (e.g. "node playwright-mcp")
	RSSKB     int64        // total resident set size in KiB across all grouped processes
	SwapKB    int64        // total swap usage in KiB
	ProcCount int          // number of processes in this group
	MemPct    float64      // RSS as a percentage of total physical RAM
	Children  []ProcDetail // individual processes, sorted by RSS descending
}

// ProcDetail holds per-PID data for an individual process within an [AppStat] group.
type ProcDetail struct {
	PID     int    // Linux process ID
	Cmdline string // full command line for display when the group is expanded
	RSSKB   int64  // resident set size in KiB
	SwapKB  int64  // swap usage in KiB
}

// SystemInfo holds system-wide memory counters from /proc/meminfo.
// All values are in KiB.
type SystemInfo struct {
	MemTotalKB     int64 // total physical RAM
	MemAvailableKB int64 // available RAM (accounts for reclaimable caches)
	MemUsedKB      int64 // MemTotalKB − MemAvailableKB
	SwapTotalKB    int64 // total swap space
	SwapFreeKB     int64 // unused swap
	SwapUsedKB     int64 // SwapTotalKB − SwapFreeKB
	ZswapPoolKB    int64 // compressed pool size; 0 if zswap is disabled
	ZswapDataKB    int64 // original uncompressed data backed by zswap
}

// PSIInfo holds memory pressure stall information from /proc/pressure/memory.
// Each field is a percentage (0–100) averaged over the named window.
type PSIInfo struct {
	SomeAvg10  float64 // some-stalled 10 s average
	SomeAvg60  float64 // some-stalled 60 s average
	SomeAvg300 float64 // some-stalled 300 s average
	FullAvg10  float64 // full-stalled 10 s average
	FullAvg60  float64 // full-stalled 60 s average
	FullAvg300 float64 // full-stalled 300 s average
}

// OOMEntry is a single process's OOM kill priority.
type OOMEntry struct {
	Score int    // kernel OOM score (0–1000, higher = more likely to be killed)
	RSSKB int64  // resident set size in KiB
	Name  string // display name from [procName]
	PID   int    // Linux process ID
}

// Collect gathers a complete memory [Snapshot] by reading /proc.
// It returns an error only if /proc/meminfo is unreadable;
// individual process read failures are silently skipped.
func Collect() (*Snapshot, error) {
	sys, err := readMemInfo()
	if err != nil {
		return nil, fmt.Errorf("meminfo: %w", err)
	}
	return &Snapshot{
		At:     time.Now(),
		Apps:   collectApps(sys.MemTotalKB),
		System: sys,
		PSI:    collectPSI(),
		OOM:    collectOOM(),
	}, nil
}

func readMemInfo() (SystemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return SystemInfo{}, err
	}
	var info SystemInfo
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			info.MemTotalKB = val
		case "MemAvailable":
			info.MemAvailableKB = val
		case "SwapTotal":
			info.SwapTotalKB = val
		case "SwapFree":
			info.SwapFreeKB = val
		case "Zswap":
			info.ZswapPoolKB = val
		case "Zswapped":
			info.ZswapDataKB = val
		}
	}
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB
	info.SwapUsedKB = info.SwapTotalKB - info.SwapFreeKB
	return info, nil
}

type appGroup struct {
	rssKB    int64
	swapKB   int64
	count    int
	children []ProcDetail
}

func collectApps(totalKB int64) []AppStat {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	groups := make(map[string]*appGroup)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid := e.Name()
		if len(pid) == 0 || pid[0] < '0' || pid[0] > '9' {
			continue
		}

		dir := filepath.Join("/proc", pid)
		name := procName(dir)
		if name == "" {
			continue
		}

		rss, swap := procMem(dir)
		g, ok := groups[name]
		if !ok {
			g = &appGroup{}
			groups[name] = g
		}
		g.rssKB += rss
		g.swapKB += swap
		g.count++

		pidNum, _ := strconv.Atoi(pid)
		g.children = append(g.children, ProcDetail{
			PID:     pidNum,
			Cmdline: cmdlineDisplay(dir),
			RSSKB:   rss,
			SwapKB:  swap,
		})
	}

	apps := make([]AppStat, 0, len(groups))
	for name, g := range groups {
		if g.rssKB <= 1024 { // skip <1 MB
			continue
		}
		pct := 0.0
		if totalKB > 0 {
			pct = float64(g.rssKB) / float64(totalKB) * 100
		}
		// Sort children by RSS descending
		slices.SortFunc(g.children, func(a, b ProcDetail) int {
			return cmp.Compare(b.RSSKB, a.RSSKB)
		})
		apps = append(apps, AppStat{
			Name:      name,
			RSSKB:     g.rssKB,
			SwapKB:    g.swapKB,
			ProcCount: g.count,
			MemPct:    pct,
			Children:  g.children,
		})
	}

	slices.SortFunc(apps, func(a, b AppStat) int {
		return cmp.Compare(b.RSSKB, a.RSSKB)
	})
	if len(apps) > 25 {
		apps = apps[:25]
	}
	return apps
}

// procName returns the display name for a process.
// Prefers cmdline over comm when comm is kernel-truncated (>=15 chars)
// or is a generic thread name that hides the real application.
func procName(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "comm"))
	if err != nil {
		return ""
	}
	comm := strings.TrimSpace(string(raw))
	if len(comm) >= 15 || genericComm(comm) {
		if full := cmdlineName(dir); len(full) > len(comm) {
			return full
		}
	}
	return comm
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

// cmdlineName returns a short group name: exe basename + first non-flag arg basename.
// e.g. "node playwright-mcp", "chrome", "python3 myapp.py"
func cmdlineName(dir string) string {
	args := splitCmdline(dir)
	if len(args) == 0 {
		return ""
	}
	exe := basename(args[0])

	// Find first non-flag argument, use its basename
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if sub := basename(arg); sub != "" {
			return exe + " " + sub
		}
	}
	return exe
}

// cmdlineDisplay returns the full command for display when a group is expanded.
// Strips path from argv[0], squashes home dir to ~.
func cmdlineDisplay(dir string) string {
	args := splitCmdline(dir)
	if len(args) == 0 {
		return ""
	}
	args[0] = basename(args[0])
	s := strings.Join(args, " ")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	return s
}

func splitCmdline(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil || len(data) == 0 {
		return nil
	}
	data = bytes.TrimRight(data, "\x00")
	parts := bytes.Split(data, []byte{0})
	args := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := string(p); s != "" {
			args = append(args, s)
		}
	}
	return args
}

func basename(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func procMem(dir string) (rss, swap int64) {
	data, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return 0, 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			rss = parseKB(line)
		case strings.HasPrefix(line, "VmSwap:"):
			swap = parseKB(line)
		}
	}
	return rss, swap
}

func parseKB(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseInt(fields[1], 10, 64)
	return v
}

func collectPSI() *PSIInfo {
	data, err := os.ReadFile("/proc/pressure/memory")
	if err != nil {
		return nil
	}
	psi := &PSIInfo{}
	for i, line := range strings.SplitN(string(data), "\n", 3) {
		kv := parsePSIFields(line)
		switch i {
		case 0: // some
			psi.SomeAvg10 = kv["avg10"]
			psi.SomeAvg60 = kv["avg60"]
			psi.SomeAvg300 = kv["avg300"]
		case 1: // full
			psi.FullAvg10 = kv["avg10"]
			psi.FullAvg60 = kv["avg60"]
			psi.FullAvg300 = kv["avg300"]
		}
	}
	return psi
}

func parsePSIFields(line string) map[string]float64 {
	m := make(map[string]float64)
	for field := range strings.FieldsSeq(line) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			m[k] = f
		}
	}
	return m
}

func collectOOM() []OOMEntry {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	var oom []OOMEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid := e.Name()
		if len(pid) == 0 || pid[0] < '0' || pid[0] > '9' {
			continue
		}

		dir := filepath.Join("/proc", pid)
		raw, err := os.ReadFile(filepath.Join(dir, "oom_score"))
		if err != nil {
			continue
		}
		score, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || score < 10 {
			continue
		}

		name := procName(dir)
		if name == "" {
			continue
		}
		rss, _ := procMem(dir)
		pidNum, _ := strconv.Atoi(pid)

		oom = append(oom, OOMEntry{
			Score: score,
			RSSKB: rss,
			Name:  name,
			PID:   pidNum,
		})
	}

	slices.SortFunc(oom, func(a, b OOMEntry) int {
		return cmp.Compare(b.Score, a.Score)
	})
	if len(oom) > 10 {
		oom = oom[:10]
	}
	return oom
}
