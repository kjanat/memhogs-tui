//go:build linux

// Linux implementation: reads /proc for memory snapshots.

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

	return buildAppStats(groups, totalKB)
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
	exe := filepath.Base(args[0])

	// Find first non-flag argument, use its basename
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
func cmdlineDisplay(dir string) string {
	args := splitCmdline(dir)
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
