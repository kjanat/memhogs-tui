package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func sampledCPUModel(t *testing.T) (Model, *collector.Snapshot) {
	t.Helper()
	at := time.Unix(100, 0)
	apps := []collector.AppStat{testApp("a", 4096, 1, 2), testApp("b", 2048, 3), testApp("unknown", 1024, 4)}
	for i := range apps {
		for j := range apps[i].Children {
			apps[i].Children[j].CPUAt = at
			apps[i].Children[j].CPUTime = 100
		}
	}
	m := testModel()
	m = m.applySnapshot(&collector.Snapshot{Apps: apps})
	if m.snap.Apps[0].CPUKnown {
		t.Fatal("first sample CPU should be unavailable")
	}
	m.expanded["a"] = true
	m.cursor = 1 // PID 1: selection must survive child CPU reordering
	next := collector.WithCPU(m.snap, nil)
	next.Apps[0].Children[0].CPUTime += .5
	next.Apps[0].Children[1].CPUTime += 2
	next.Apps[1].Children[0].CPUTime += 4
	for i := range next.Apps {
		for j := range next.Apps[i].Children {
			next.Apps[i].Children[j].CPUAt = at.Add(2 * time.Second)
		}
	}
	next.Apps[2].Children[0].StartTime++ // PID reuse remains unavailable
	m = m.applySnapshot(next)
	return m, next
}

func TestCPUSamplingSortingAndSelection(t *testing.T) {
	m, next := sampledCPUModel(t)
	for range 4 {
		m, _ = updateModel(m, press('s'))
	}
	require(t, m.sort == SortCPU && strings.Contains(m.tableTitle(), "sort:CPU"), "CPU sort missing from key cycle")
	sorted := m.sortedApps()
	require(t, sorted[0].Name == "b" && sorted[0].CPUPct == 200, "wrong group CPU sorting: %+v", sorted)
	require(t, sorted[1].CPUPct == 125 && !sorted[2].CPUKnown, "wrong summed or unavailable CPU")
	rows := m.visibleRows(sorted)
	if rows[m.cursor].Child.PID != 1 || m.sortedChildren(sorted[1])[0].PID != 2 {
		t.Fatal("CPU sort lost selected identity or did not reorder children")
	}
	if m.prevSnap.Apps[0].Children[0].CPUTime != 100 || next.Apps[0].CPUKnown {
		t.Fatal("sampling mutated a snapshot")
	}
	zero := collector.AppStat{Name: "idle", CPUKnown: true}
	m.snap.Apps = append(m.snap.Apps, zero)
	sorted = m.sortedApps()
	if sorted[len(sorted)-1].Name != "unknown" {
		t.Fatal("unavailable CPU must follow measured zero")
	}
}

func TestCPUColumnVisibleAndAligned(t *testing.T) {
	app := testApp("workers", 4096, 1, 2)
	app.CPUPct, app.CPUKnown = 150, true
	app.Children[0].CPUPct, app.Children[0].CPUKnown = 150, true
	m := testModel(app)
	m.expanded[app.Name] = true
	rows := m.visibleRows(m.sortedApps())
	for _, width := range []int{61, 80, 120} {
		lines := strings.Split(ansi.Strip(m.viewTable(rows, width, 8)), "\n")
		column := strings.Index(lines[1], "CPU%") + len("CPU%")
		if column < 4 || !strings.Contains(lines[3], "150.0%") || !strings.Contains(lines[4], "150.0%") || !strings.Contains(lines[5], "n/a") {
			t.Fatalf("CPU values lost at width %d:\n%s", width, strings.Join(lines, "\n"))
		}
		for _, line := range lines[3:6] {
			value := "150.0%"
			if !strings.Contains(line, value) {
				value = "n/a"
			}
			end := strings.Index(line, value) + len(value)
			if ansi.StringWidth(line[:end]) != column {
				t.Fatalf("CPU column not right aligned: %q", line)
			}
		}
	}
	require(t, strings.Contains(ansi.Strip(m.viewDetail(app, 40)), "150.0%"), "CPU missing from group details")
	require(t, strings.Contains(ansi.Strip(m.viewDetailChild(app.Name, app.Children[0], 40)), "150.0%"), "CPU missing from process details")
}
