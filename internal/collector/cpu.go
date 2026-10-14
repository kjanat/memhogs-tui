package collector

import (
	"math"
	"slices"
)

// WithCPU returns a copy of current with CPU usage measured against previous.
// It matches individual process lifetimes across groups, so group churn cannot
// turn an exited process's lifetime CPU counter into a negative rate or a spike.
// Unreadable counters and processes without a baseline remain unavailable.
// Neither input snapshot is modified.
func WithCPU(current, previous *Snapshot) *Snapshot {
	if current == nil {
		return nil
	}
	baseline := make(map[int]ProcDetail)
	if previous != nil {
		for _, app := range previous.Apps {
			for _, p := range app.Children {
				baseline[p.PID] = p
			}
		}
	}
	result := *current
	result.Apps = slices.Clone(current.Apps)
	for i := range result.Apps {
		app := &result.Apps[i]
		app.Children = slices.Clone(app.Children)
		app.CPUPct, app.CPUKnown = 0, len(app.Children) > 0
		for j := range app.Children {
			p := &app.Children[j]
			p.CPUPct, p.CPUKnown = processCPU(*p, baseline[p.PID])
			app.CPUPct += p.CPUPct
			app.CPUKnown = app.CPUKnown && p.CPUKnown
		}
	}
	return &result
}

func processCPU(current, previous ProcDetail) (float64, bool) {
	if current.PID <= 0 || current.PID != previous.PID || current.StartTime == 0 || current.StartTime != previous.StartTime {
		return 0, false
	}
	if !hasCPUObservation(current) || !hasCPUObservation(previous) {
		return 0, false
	}
	elapsed := current.CPUAt.Sub(previous.CPUAt).Seconds()
	delta := current.CPUTime - previous.CPUTime
	if elapsed <= 0 || delta < 0 {
		return 0, false
	}
	return 100 * delta / elapsed, true
}

func hasCPUObservation(p ProcDetail) bool {
	return !p.CPUAt.IsZero() && validCPUTime(p.CPUTime)
}

func validCPUTime(seconds float64) bool {
	return seconds >= 0 && !math.IsInf(seconds, 0) && !math.IsNaN(seconds)
}
