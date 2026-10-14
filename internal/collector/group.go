package collector

import (
	"cmp"
	"slices"
)

// buildAppStats converts grouped process data into a sorted slice of
// [AppStat] entries. Groups with RSS ≤ 1 MiB and no swap or commit are excluded.
// Results are sorted
// by RSS descending. Presentation limits belong in the TUI.
func buildAppStats(groups map[string]*appGroup, totalKB int64) []AppStat {
	apps := make([]AppStat, 0, len(groups))
	for name, g := range groups {
		pct := 0.0
		if totalKB > 0 {
			pct = float64(g.rssKB) / float64(totalKB) * 100
		}
		slices.SortFunc(g.children, func(a, b ProcDetail) int {
			return cmp.Or(cmp.Compare(b.RSSKB, a.RSSKB), cmp.Compare(a.PID, b.PID))
		})
		swapKnown, commitKnown := len(g.children) > 0, len(g.children) > 0
		var commitKB int64
		for _, child := range g.children {
			swapKnown = swapKnown && child.SwapKnown
			commitKnown = commitKnown && child.CommitKnown
			commitKB += child.CommitKB
		}
		if g.rssKB <= 1024 && g.swapKB <= 0 && commitKB <= 0 {
			continue
		}
		apps = append(apps, AppStat{
			Name:        name,
			RSSKB:       g.rssKB,
			SwapKB:      g.swapKB,
			SwapKnown:   swapKnown,
			CommitKB:    commitKB,
			CommitKnown: commitKnown,
			ProcCount:   g.count,
			MemPct:      pct,
			Children:    g.children,
		})
	}

	slices.SortFunc(apps, func(a, b AppStat) int {
		return cmp.Or(cmp.Compare(b.RSSKB, a.RSSKB), cmp.Compare(a.Name, b.Name))
	})
	return apps
}
