// Package collector gathers point-in-time memory usage snapshots grouped by
// application instance, executable, or process tree.
//
// Each [Snapshot] aggregates per-process RSS, swap, OOM scores, and system-wide
// memory pressure into typed structures ready for display.
// The collector is stateless; call [Collect] on each tick.
package collector

import "time"

// Snapshot is a point-in-time view of system memory state.
type Snapshot struct {
	At            time.Time  // when the snapshot was taken
	Apps          []AppStat  // all eligible applications sorted by RSS descending
	ProcessCommit bool       // process commit is collected instead of swap (Windows)
	System        SystemInfo // system-wide memory counters
	PSI           *PSIInfo   // memory pressure; nil if unavailable on this platform
	OOM           []OOMEntry // top OOM kill candidates sorted by score descending
}

// AppStat aggregates one explicitly identified group. Name is a display label;
// GroupID is the stable key for selection, history, and termination scope.
type AppStat struct {
	GroupID     GroupID         // typed identity, independent of display name and current membership
	Name        string          // inferred application name or executable label; never an identity
	Root        ProcessIdentity // observed root for application/tree grouping; zero for executable groups
	RootProcess *ProcDetail     // observed root metadata, even when it has no displayed memory
	Directory   string          // root workspace clue for display, never part of group identity
	RSSKB       int64           // total resident set size in KiB across all grouped processes
	SwapKB      int64           // total swap usage in KiB
	SwapKnown   bool            // true only when swap is available for every child
	CommitKB    int64           // Windows process commit charge in KiB
	CommitKnown bool            // true only when commit is available for every child
	ProcCount   int             // number of processes in this group
	MemPct      float64         // RSS as a percentage of total physical RAM
	CPUPct      float64         // interval CPU usage; 100% is one logical CPU
	CPUKnown    bool            // true only when CPU usage is available for every child
	Children    []ProcDetail    // individual processes, sorted by RSS descending
}

// ProcessIdentity identifies one process lifetime, including across PID reuse.
type ProcessIdentity struct {
	PID       int
	StartTime uint64
}

// ProcDetail holds per-PID data for an individual process within an [AppStat] group.
type ProcDetail struct {
	PID          int                // OS process ID
	ParentPID    int                // observed parent PID; zero if unavailable
	StartTime    uint64             // opaque OS creation time; PID plus StartTime identifies a process
	Executable   string             // executable path, when readable
	ExecutableID ExecutableIdentity // executable file identity, when readable
	Args         []string           // original argv, when available; never execute this display metadata
	Cmdline      string             // full command line for display when the group is expanded
	Cwd          string             // working directory, when readable
	Terminal     string             // controlling input terminal, when readable
	UID          string             // effective account ID; empty when unavailable
	User         string             // resolved account name; empty when unavailable
	Session      *SessionInfo       // verified Claude session metadata; nil if unavailable
	Context      *ProcessContext    // common application, workspace, and ancestry context
	RSSKB        int64              // resident set size in KiB
	SwapKB       int64              // swap usage in KiB
	SwapKnown    bool               // distinguishes unavailable swap from zero
	CommitKB     int64              // Windows process commit charge in KiB
	CommitKnown  bool               // distinguishes unavailable commit from zero
	CPUTime      float64            // cumulative user + kernel CPU time in seconds, excluding child processes
	CPUAt        time.Time          // counter observation time; zero when unavailable
	CPUPct       float64            // interval CPU usage; 100% is one logical CPU
	CPUKnown     bool               // true after two valid observations of this process lifetime
}

// ProcessContext describes observed ownership and workspace clues. Sources are
// retained so a current directory or open repository is never called a spawn directory.
type ProcessContext struct {
	Application       string            // concrete application inferred from executable/package arguments
	ApplicationPID    int               // process providing that application identity
	ApplicationStart  uint64            // creation time of that application root
	PackagePath       string            // package argument observed on this process; never inherited
	ApplicationKey    string            // normalized package path; independent of a friendly display name
	ApplicationSource string            // evidence used for the label
	ProjectDir        string            // observed workspace; may differ from Cwd
	ProjectSource     string            // cwd, application ancestor, or repository socket
	Origin            string            // nearest distinct application or launcher in the parent chain
	OriginPID         int               // PID of that ancestor
	Chromium          bool              // Chromium role flags observed in this executable's process family
	Ancestors         []ProcessAncestor // immediate parent first; bounded and cycle checked
	Resources         []string          // relevant open resource paths; contents are never read
}

// ProcessAncestor records a verified process identity from the same scan.
type ProcessAncestor struct {
	PID        int
	StartTime  uint64
	Name       string
	Executable string
	Cwd        string
}

// SessionInfo identifies an active Claude conversation without reading its transcript.
type SessionInfo struct {
	ID         string // full session UUID
	Name       string // current session display name
	ProjectDir string // session's original project directory
	Entrypoint string // Claude entrypoint, such as cli or sdk-ts
}

// SystemInfo holds system-wide memory counters.
// All values are in KiB.
type SystemInfo struct {
	MemTotalKB        int64 // total physical RAM
	MemAvailableKB    int64 // available RAM (accounts for reclaimable caches)
	MemUsedKB         int64 // MemTotalKB − MemAvailableKB
	SwapTotalKB       int64 // total swap space
	SwapFreeKB        int64 // unused swap
	SwapUsedKB        int64 // SwapTotalKB − SwapFreeKB
	SwapKnown         bool  // true when system swap counters were read successfully
	CommitLimitKB     int64 // Windows maximum commit available to this process
	CommitAvailableKB int64 // Windows remaining commit available to this process
	CommitKnown       bool  // true when Windows commit counters are available
	ZswapPoolKB       int64 // compressed pool size; 0 if zswap is disabled or unsupported
	ZswapDataKB       int64 // original uncompressed data backed by zswap
}

// PSIInfo holds memory pressure stall information.
// Each field is a percentage (0–100) averaged over the named window.
// Linux-only; nil on other platforms.
type PSIInfo struct {
	SomeAvg10  float64 // some-stalled 10 s average
	SomeAvg60  float64 // some-stalled 60 s average
	SomeAvg300 float64 // some-stalled 300 s average
	FullAvg10  float64 // full-stalled 10 s average
	FullAvg60  float64 // full-stalled 60 s average
	FullAvg300 float64 // full-stalled 300 s average
}

// OOMEntry is a single process's OOM kill priority.
// Linux-only; empty slice on other platforms.
type OOMEntry struct {
	Score int    // kernel OOM score (0–1000, higher = more likely to be killed)
	RSSKB int64  // resident set size in KiB
	Name  string // display name
	PID   int    // OS process ID
}

// appGroup accumulates per-process data while building [AppStat] entries.
// Used internally by platform-specific collectApps implementations.
type appGroup struct {
	name        string
	root        ProcessIdentity
	rootProcess *ProcDetail
	directory   string
	rssKB       int64
	swapKB      int64
	count       int
	children    []ProcDetail
}
