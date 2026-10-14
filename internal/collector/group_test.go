package collector

import (
	"fmt"
	"testing"
)

func TestBuildAppStatsRetainsAllEligibleGroups(t *testing.T) {
	groups := make(map[GroupID]*appGroup)
	for i := range 40 {
		rss := int64(2048 + i*1024)
		groups[executableNameID(fmt.Sprintf("app-%02d", i))] = &appGroup{rssKB: rss, count: 1, children: []ProcDetail{{PID: i + 1, RSSKB: rss, SwapKnown: true}}}
	}
	groups[executableNameID("small")] = &appGroup{rssKB: 1024}
	apps := buildAppStats(groups, 100*1024)
	if len(apps) != 40 {
		t.Fatalf("got %d groups, want 40", len(apps))
	}
	if apps[39].Name != "app-00" || apps[39].MemPct != 2 {
		t.Fatalf("lowest RSS group lost or percentage wrong: %+v", apps[39])
	}
	for i := 1; i < len(apps); i++ {
		if apps[i-1].RSSKB < apps[i].RSSKB {
			t.Fatal("not sorted by RSS")
		}
	}
}

func TestBuildAppStatsUnknownAndKnownZeroMetrics(t *testing.T) {
	children := []ProcDetail{{PID: 2, RSSKB: 2048, SwapKnown: true, CommitKnown: true, CommitKB: 1024}, {PID: 1, RSSKB: 2048, SwapKnown: true, CommitKnown: true, CommitKB: 2048}}
	groups := map[GroupID]*appGroup{executableNameID("app"): {rssKB: 4096, count: 2, children: children}}
	a := buildAppStats(groups, 0)[0]
	if !a.SwapKnown || a.SwapKB != 0 || !a.CommitKnown || a.CommitKB != 3072 || a.MemPct != 0 || a.Children[0].PID != 1 {
		t.Fatalf("bad aggregate: %+v", a)
	}
	children[0].SwapKnown = false
	children[0].CommitKnown = false
	a = buildAppStats(groups, 0)[0]
	if a.SwapKnown || a.CommitKnown {
		t.Fatal("partial metric presented as complete")
	}
}

func TestBuildAppStatsDeterministicTies(t *testing.T) {
	groups := map[GroupID]*appGroup{executableNameID("z"): {rssKB: 2048}, executableNameID("a"): {rssKB: 2048}}
	apps := buildAppStats(groups, 0)
	if apps[0].Name != "a" {
		t.Fatal("RSS ties must sort by name")
	}
}

func TestBuildAppStatsRetainsSwappedOrCommittedGroupsWithLowRSS(t *testing.T) {
	groups := map[GroupID]*appGroup{
		executableNameID("swapped"):   {swapKB: 8192, count: 1, children: []ProcDetail{{PID: 1, SwapKB: 8192, SwapKnown: true}}},
		executableNameID("committed"): {count: 1, children: []ProcDetail{{PID: 2, CommitKB: 8192, CommitKnown: true}}},
		executableNameID("empty"):     {},
	}
	apps := buildAppStats(groups, 0)
	if len(apps) != 2 {
		t.Fatalf("got %d groups, want swapped and committed", len(apps))
	}
}
