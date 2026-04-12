package collector

import (
	"cmp"
	"slices"
)

// buildAppStats converts grouped process data into a sorted, capped slice of
// [AppStat] entries. Groups with RSS ≤ 1 MiB are excluded. Results are sorted
// by RSS descending and capped at 25 entries.
func buildAppStats(groups map[string]*appGroup, totalKB int64) []AppStat {
	apps := make([]AppStat, 0, len(groups))
	for name, g := range groups {
		if g.rssKB <= 1024 { // skip <1 MB
			continue
		}
		pct := 0.0
		if totalKB > 0 {
			pct = float64(g.rssKB) / float64(totalKB) * 100
		}
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
