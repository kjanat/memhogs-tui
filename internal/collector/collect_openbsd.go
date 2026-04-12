//go:build openbsd && amd64

// OpenBSD implementation: uses sysctl + SysctlUvmexp for memory snapshots.
// kinfo_proc struct layout from OpenBSD 7.x <sys/sysctl.h>.

package collector

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// kinfoProc mirrors struct kinfo_proc from OpenBSD <sys/sysctl.h>.
type kinfoProc struct {
	Forw        uint64
	Back        uint64
	Paddr       uint64
	Addr        uint64
	Fd          uint64
	Stats       uint64
	Limit       uint64
	Vmspace     uint64
	Sigacts     uint64
	Sess        uint64
	Tsess       uint64
	Ru          uint64
	Eflag       int32
	Exitsig     int32
	Flag        int32
	Pid         int32
	Ppid        int32
	Sid         int32
	_           int32
	Tpgid       int32
	Uid         uint32
	Ruid        uint32
	Gid         uint32
	Rgid        uint32
	Groups      [16]uint32
	Ngroups     int16
	Jobc        int16
	Tdev        uint32
	Estcpu      uint32
	RtimeSec    uint32
	RtimeUsec   uint32
	Cpticks     int32
	Pctcpu      uint32
	Swtime      uint32
	Slptime     uint32
	Schedflags  int32
	Uticks      uint64
	Sticks      uint64
	Iticks      uint64
	Tracep      uint64
	Traceflag   int32
	Holdcnt     int32
	Siglist     int32
	Sigmask     uint32
	Sigignore   uint32
	Sigcatch    uint32
	Stat        int8
	Priority    uint8
	Usrpri      uint8
	Nice        uint8
	Xstat       uint16
	Acflag      uint16
	Comm        [24]byte
	Wmesg       [8]byte
	Wchan       uint64
	Login       [32]byte
	VmRssize    int32 // RSS in pages
	VmTsize     int32
	VmDsize     int32
	VmSsize     int32
	Uvalid      int64
	UstartSec   uint64
	UstartUsec  uint32
	UutimeSec   uint32
	UutimeUsec  uint32
	UstimeSec   uint32
	UstimeUsec  uint32
	UruMaxrss   uint64
	UruIxrss    uint64
	UruIdrss    uint64
	UruIsrss    uint64
	UruMinflt   uint64
	UruMajflt   uint64
	UruNswap    uint64
	UruInblock  uint64
	UruOublock  uint64
	UruMsgsnd   uint64
	UruMsgrcv   uint64
	UruNsignals uint64
	UruNvcsw    uint64
	UruNivcsw   uint64
	UctimeSec   uint32
	UctimeUsec  uint32
	Psflags     uint32
	Spare       int32
	Svuid       uint32
	Svgid       uint32
	Emul        [8]byte
	RlimRssCur  uint64
	Cpuid       uint64
	VmMapSize   uint64
	Tid         int32
	Rtableid    uint32
	Pledge      uint64
}

var sizeofKinfoProc = int(unsafe.Sizeof(kinfoProc{}))

const (
	kernProcAll = 0
)

// Collect gathers a complete memory [Snapshot] using OpenBSD sysctl.
// PSI and OOM are not available on OpenBSD.
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

	info := SystemInfo{
		MemTotalKB: int64(totalBytes / 1024),
	}

	// OpenBSD has SysctlUvmexp — the cleanest memory API of all BSDs
	if uvm, err := unix.SysctlUvmexp("vm.uvmexp"); err == nil {
		ps := int64(uvm.Pagesize)
		info.MemAvailableKB = int64(uvm.Free) * ps / 1024
		info.SwapTotalKB = int64(uvm.Swpages) * ps / 1024
		info.SwapUsedKB = int64(uvm.Swpginuse) * ps / 1024
		info.SwapFreeKB = info.SwapTotalKB - info.SwapUsedKB
	}
	info.MemUsedKB = info.MemTotalKB - info.MemAvailableKB

	return info, nil
}

func collectApps(totalKB int64) []AppStat {
	raw, err := unix.SysctlRaw("kern.proc", kernProcAll, 0, sizeofKinfoProc, 1024)
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

		rssKB := int64(kp.VmRssize) * pagesize / 1024
		name := comm

		g, ok := groups[name]
		if !ok {
			g = &appGroup{}
			groups[name] = g
		}
		g.rssKB += rssKB
		g.count++
		g.children = append(g.children, ProcDetail{
			PID:     pid,
			Cmdline: comm, // OpenBSD: kern.proc.args is restricted, use comm
			RSSKB:   rssKB,
		})
	}

	return buildAppStats(groups, totalKB)
}
