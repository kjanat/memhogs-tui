//go:build freebsd && amd64

// FreeBSD implementation: uses sysctl for memory snapshots.
// kinfo_proc struct layout from FreeBSD 13+14 <sys/user.h>.

package collector

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// kinfoProc mirrors struct kinfo_proc from FreeBSD <sys/user.h>.
// Field layout verified against FreeBSD 13 and 14 on amd64.
type kinfoProc struct {
	Structsize    int32
	Layout        int32
	Args          uintptr
	Paddr         uintptr
	Addr          uintptr
	Tracep        uintptr
	Textvp        uintptr
	Fd            uintptr
	Vmspace       uintptr
	Wchan         *byte
	Pid           int32
	Ppid          int32
	Pgid          int32
	Tpgid         int32
	Sid           int32
	Tsid          int32
	Jobc          int16
	SpareShort1   int16
	TdevFreebsd11 uint32
	Siglist       [16]byte // unix.Sigset_t
	Sigmask       [16]byte
	Sigignore     [16]byte
	Sigcatch      [16]byte
	Uid           uint32
	Ruid          uint32
	Svuid         uint32
	Rgid          uint32
	Svgid         uint32
	Ngroups       int16
	SpareShort2   int16
	Groups        [16]uint32
	Size          uint64
	Rssize        int64 // RSS in pages — no truncation on FreeBSD
	Swrss         int64 // swap RSS in pages
	Tsize         int64
	Dsize         int64
	Ssize         int64
	Xstat         uint16
	Acflag        uint16
	Pctcpu        uint32
	Estcpu        uint32
	Slptime       uint32
	Swtime        uint32
	Cow           uint32
	Runtime       uint64
	Start         unix.Timeval
	Childtime     unix.Timeval
	Flag          int64
	Kiflag        int64
	Traceflag     int32
	Stat          int8
	Nice          int8
	Lock          int8
	Rqindex       int8
	OncpuOld      uint8
	LastcpuOld    uint8
	Tdname        [17]byte
	Wmesg         [9]byte
	Login         [18]byte
	Lockname      [9]byte
	Comm          [20]byte
	Emul          [17]byte
	Loginclass    [18]byte
	Moretdname    [4]byte
	Sparestrings  [46]byte
	Spareints     [2]int32
	Tdev          uint64
	Oncpu         int32
	Lastcpu       int32
	Tracer        int32
	Flag2         int32
	Fibnum        int32
	CrFlags       uint32
	Jid           int32
	Numthreads    int32
	Tid           int32
	Pri           kinfoPrority
	Rusage        unix.Rusage
	RusageCh      unix.Rusage
	Pcb           uintptr
	Kstack        *byte
	Udata         *byte
	Tdaddr        uintptr
	Pd            uintptr
	Spareptrs     [5]*byte
	Sparelongs    [12]int64
	Sflag         int64
	Tdflags       int64
}

type kinfoPrority struct {
	Class  uint8
	Level  uint8
	Native uint8
	User   uint8
}

var sizeofKinfoProc = int(unsafe.Sizeof(kinfoProc{}))

// Collect gathers a complete memory [Snapshot] using FreeBSD sysctl.
// PSI and OOM are not available on FreeBSD.
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
	totalBytes, err := unix.SysctlUint64("hw.physmem")
	if err != nil {
		return SystemInfo{}, fmt.Errorf("hw.physmem: %w", err)
	}

	pagesize := int64(unix.Getpagesize())
	info := SystemInfo{
		MemTotalKB: int64(totalBytes / 1024),
	}

	if v, err := unix.SysctlUint32("vm.stats.vm.v_free_count"); err == nil {
		info.MemAvailableKB = int64(v) * pagesize / 1024
	}
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB

	// Swap via swapctl command — kvm_getswapinfo requires CGo
	info.SwapTotalKB, info.SwapUsedKB = readSwapInfo()
	info.SwapFreeKB = info.SwapTotalKB - info.SwapUsedKB

	return info, nil
}

// readSwapInfo parses "swapctl -sk" output for total and used swap in KiB.
// Returns (0, 0) if unavailable.
func readSwapInfo() (totalKB, usedKB int64) {
	out, err := exec.Command("/usr/sbin/swapctl", "-sk").Output()
	if err != nil {
		return 0, 0
	}
	// Format: "total: <total> <used> <avail> <capacity>"
	fields := strings.Fields(string(out))
	if len(fields) < 4 || fields[0] != "total:" {
		return 0, 0
	}
	total, _ := strconv.ParseInt(fields[1], 10, 64)
	used, _ := strconv.ParseInt(fields[2], 10, 64)
	return total, used
}

func collectApps(totalKB int64) []AppStat {
	raw, err := unix.SysctlRaw("kern.proc.all")
	if err != nil {
		return nil
	}
	if len(raw)%sizeofKinfoProc != 0 {
		return nil // struct size mismatch — kernel version incompatible
	}

	n := len(raw) / sizeofKinfoProc
	pagesize := int64(unix.Getpagesize())

	groups := make(map[string]*appGroup)
	for i := range n {
		kp := (*kinfoProc)(unsafe.Pointer(&raw[i*sizeofKinfoProc : (i+1)*sizeofKinfoProc][0]))
		pid := int(kp.Pid)
		if pid == 0 {
			continue
		}

		comm := unix.ByteSliceToString(kp.Comm[:])
		if comm == "" {
			continue
		}

		rssKB := kp.Rssize * pagesize / 1024
		swapKB := kp.Swrss * pagesize / 1024

		name := comm
		if len(comm) >= 19 {
			if full := cmdlineName(pid); len(full) > len(comm) {
				name = full
			}
		}

		g, ok := groups[name]
		if !ok {
			g = &appGroup{}
			groups[name] = g
		}
		g.rssKB += rssKB
		g.swapKB += swapKB
		g.count++
		g.children = append(g.children, ProcDetail{
			PID:     pid,
			Cmdline: cmdlineDisplay(pid),
			RSSKB:   rssKB,
			SwapKB:  swapKB,
		})
	}

	return buildAppStats(groups, totalKB)
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

// procArgs reads the full argv for a process via kern.proc.args.
func procArgs(pid int) []string {
	raw, err := unix.SysctlRaw("kern.proc.args", pid)
	if err != nil || len(raw) == 0 {
		return nil
	}
	raw = bytes.TrimRight(raw, "\x00")
	parts := bytes.Split(raw, []byte{0})
	args := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := string(p); s != "" {
			args = append(args, s)
		}
	}
	return args
}
