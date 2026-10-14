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
	seen := make(map[string]bool)
	// Map each /proc/meminfo key to the field it fills; absent keys stay zero.
	dst := map[string]*int64{
		"MemTotal":     &info.MemTotalKB,
		"MemAvailable": &info.MemAvailableKB,
		"SwapTotal":    &info.SwapTotalKB,
		"SwapFree":     &info.SwapFreeKB,
		"Zswap":        &info.ZswapPoolKB,
		"Zswapped":     &info.ZswapDataKB,
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		field, ok := dst[strings.TrimSuffix(fields[0], ":")]
		if !ok {
			continue
		}
		if val, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			*field = val
			seen[strings.TrimSuffix(fields[0], ":")] = true
		}
	}
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB
	info.SwapUsedKB = info.SwapTotalKB - info.SwapFreeKB
	info.SwapKnown = seen["SwapTotal"] && seen["SwapFree"]
	return info, nil
}

type collectedProcess struct {
	name   string
	detail ProcDetail
}

func collectApps(totalKB int64) []AppStat {
	processes := scanProcesses("/proc")
	enrichProcessContexts(processes, "/proc")
	groups := make(map[string]*appGroup)
	for _, process := range processes {
		group, ok := groups[process.name]
		if !ok {
			group = &appGroup{}
			groups[process.name] = group
		}
		group.rssKB += process.detail.RSSKB
		group.swapKB += process.detail.SwapKB
		group.count++
		group.children = append(group.children, process.detail)
	}
	return buildAppStats(groups, totalKB)
}

func scanProcesses(root string) map[int]*collectedProcess {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	processes := make(map[int]*collectedProcess)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		name, process, ok := readProcess(filepath.Join(root, entry.Name()))
		if !ok {
			continue
		}
		process.PID = pid
		if name == "claude" {
			process.Session = claudeSession(pid, process.StartTime)
		}
		processes[pid] = &collectedProcess{name: name, detail: process}
	}
	return processes
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
	return formatCmdline(splitCmdline(dir))
}

func formatCmdline(rawArgs []string) string {
	args := slices.Clone(rawArgs)
	if len(args) == 0 {
		return ""
	}
	// Some processes rewrite all argv into a single string. Applying Base
	// to that string would discard flags before its final path separator.
	if !strings.ContainsAny(args[0], " \t\r\n") {
		args[0] = filepath.Base(args[0])
	}
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

func procMem(dir string) (rss, swap int64, swapKnown bool) {
	data, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return 0, 0, false
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			rss = parseKB(line)
		case strings.HasPrefix(line, "VmSwap:"):
			swap = parseKB(line)
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				_, err := strconv.ParseInt(fields[1], 10, 64)
				swapKnown = err == nil
			}
		}
	}
	return rss, swap, swapKnown
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

const (
	// minOOMScore is the lowest oom_score worth listing; below it a process
	// is a negligible kill candidate.
	minOOMScore = 10
	// maxOOMEntries caps the collected list to the worst N candidates.
	maxOOMEntries = 10
)

func collectOOM() []OOMEntry {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	var oom []OOMEntry
	for _, e := range entries {
		if entry, ok := oomEntry(e); ok {
			oom = append(oom, entry)
		}
	}

	slices.SortFunc(oom, func(a, b OOMEntry) int {
		return cmp.Compare(b.Score, a.Score)
	})
	if len(oom) > maxOOMEntries {
		oom = oom[:maxOOMEntries]
	}
	return oom
}

// oomEntry reads one /proc entry into an OOMEntry, reporting false for
// non-PID dirs, unreadable scores, or processes below the display threshold.
func oomEntry(e os.DirEntry) (OOMEntry, bool) {
	if !e.IsDir() {
		return OOMEntry{}, false
	}
	pid := e.Name()
	if len(pid) == 0 || pid[0] < '0' || pid[0] > '9' {
		return OOMEntry{}, false
	}

	dir := filepath.Join("/proc", pid)
	raw, err := os.ReadFile(filepath.Join(dir, "oom_score"))
	if err != nil {
		return OOMEntry{}, false
	}
	score, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || score < minOOMScore {
		return OOMEntry{}, false
	}

	name := procName(dir)
	if name == "" {
		return OOMEntry{}, false
	}
	rss, _, _ := procMem(dir)
	pidNum, _ := strconv.Atoi(pid)

	return OOMEntry{Score: score, RSSKB: rss, Name: name, PID: pidNum}, true
}

// ProcessStartTime returns Linux's start-time ticks from /proc/PID/stat.
func ProcessStartTime(pid int) (uint64, error) {
	return readStartTime(filepath.Join("/proc", strconv.Itoa(pid)))
}

func readStartTime(dir string) (uint64, error) {
	start, _, err := readProcessIdentity(dir)
	return start, err
}

func readProcessIdentity(dir string) (start uint64, parent int, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return 0, 0, err
	}
	return parseProcessIdentity(string(raw))
}

func parseStartTime(stat string) (uint64, error) {
	start, _, err := parseProcessIdentity(stat)
	return start, err
}

func parseProcessIdentity(stat string) (start uint64, parent int, err error) {
	// comm (field 2) can contain spaces and parentheses; the final ')' ends it.
	_, rest, ok := strings.CutLast(stat, ")")
	fields := strings.Fields(rest)
	if !ok || len(fields) <= 19 {
		return 0, 0, fmt.Errorf("invalid process stat")
	}
	parent, err = strconv.Atoi(fields[1]) // field 4
	if err != nil {
		return 0, 0, err
	}
	start, err = strconv.ParseUint(fields[19], 10, 64) // field 22
	return start, parent, err
}

// readProcess verifies that metadata and counters belong to one process lifetime.
func readProcess(dir string) (string, ProcDetail, bool) {
	start, parent, err := readProcessIdentity(dir)
	if err != nil {
		return "", ProcDetail{}, false
	}
	name := procName(dir)
	if name == "" {
		return "", ProcDetail{}, false
	}
	rss, swap, known := procMem(dir)
	if rss <= 0 && swap <= 0 {
		return "", ProcDetail{}, false
	} // zombies and tasks with neither resident nor swapped memory
	cwd, terminal := processContext(dir)
	args := splitCmdline(dir)
	cmdline := formatCmdline(args)
	executable, _ := os.Readlink(filepath.Join(dir, "exe"))
	raw, err := os.ReadFile(filepath.Join(dir, "stat"))
	at := time.Now()
	end, identityErr := parseStartTime(string(raw))
	if err != nil || identityErr != nil || start != end {
		return "", ProcDetail{}, false
	}
	p := ProcDetail{StartTime: start, ParentPID: parent, Executable: executable, Args: args, Cmdline: cmdline, Cwd: cwd, Terminal: terminal, RSSKB: rss, SwapKB: swap, SwapKnown: known}
	if seconds, ok := parseCPUTime(string(raw), clockTicks()); ok {
		p.CPUTime, p.CPUAt = seconds, at
	}
	return name, p, true
}

func processContext(dir string) (cwd, terminal string) {
	cwd, _ = os.Readlink(filepath.Join(dir, "cwd"))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if cwd == home {
			cwd = "~"
		} else if strings.HasPrefix(cwd, home+"/") {
			cwd = "~" + strings.TrimPrefix(cwd, home)
		}
	}
	input, _ := os.Readlink(filepath.Join(dir, "fd", "0"))
	if strings.HasPrefix(input, "/dev/pts/") || strings.HasPrefix(input, "/dev/tty") {
		terminal = strings.TrimPrefix(input, "/dev/")
	}
	return cwd, terminal
}
