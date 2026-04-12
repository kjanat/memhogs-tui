// Package collector gathers point-in-time memory usage snapshots grouped by
// application name.
//
// Each [Snapshot] aggregates per-process RSS, swap, OOM scores, and system-wide
// memory pressure into typed structures ready for display.
// The collector is stateless; call [Collect] on each tick.
package collector

import "time"

// Snapshot is a point-in-time view of system memory state.
type Snapshot struct {
	At     time.Time  // when the snapshot was taken
	Apps   []AppStat  // top applications sorted by RSS descending
	System SystemInfo // system-wide memory counters
	PSI    *PSIInfo   // memory pressure; nil if unavailable on this platform
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
	PID     int    // OS process ID
	Cmdline string // full command line for display when the group is expanded
	RSSKB   int64  // resident set size in KiB
	SwapKB  int64  // swap usage in KiB
}

// SystemInfo holds system-wide memory counters.
// All values are in KiB.
type SystemInfo struct {
	MemTotalKB     int64 // total physical RAM
	MemAvailableKB int64 // available RAM (accounts for reclaimable caches)
	MemUsedKB      int64 // MemTotalKB − MemAvailableKB
	SwapTotalKB    int64 // total swap space
	SwapFreeKB     int64 // unused swap
	SwapUsedKB     int64 // SwapTotalKB − SwapFreeKB
	ZswapPoolKB    int64 // compressed pool size; 0 if zswap is disabled or unsupported
	ZswapDataKB    int64 // original uncompressed data backed by zswap
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
	rssKB    int64
	swapKB   int64
	count    int
	children []ProcDetail
}
