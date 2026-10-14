package collector

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func cpuProcess(pid int, seconds float64, at time.Time) ProcDetail {
	return ProcDetail{PID: pid, StartTime: 10, CPUTime: seconds, CPUAt: at}
}

func requireCPU(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Fatal(message)
	}
}

func TestProcessCPU(t *testing.T) {
	at := time.Unix(100, 0)
	before, after := cpuProcess(1, 10, at), cpuProcess(1, 13, at.Add(2*time.Second))
	for _, tt := range []struct {
		name  string
		edit  func(*ProcDetail)
		want  float64
		known bool
	}{
		{"multiple cores", func(*ProcDetail) {}, 150, true},
		{"idle", func(p *ProcDetail) { p.CPUTime = 10 }, 0, true},
		{"actual elapsed after pause", func(p *ProcDetail) { p.CPUAt = at.Add(10 * time.Second) }, 30, true},
		{"reused PID", func(p *ProcDetail) { p.StartTime++ }, 0, false},
		{"unknown identity", func(p *ProcDetail) { p.StartTime = 0 }, 0, false},
		{"missing counter", func(p *ProcDetail) { p.CPUAt = time.Time{} }, 0, false},
		{"counter reset", func(p *ProcDetail) { p.CPUTime = 9 }, 0, false},
		{"no elapsed time", func(p *ProcDetail) { p.CPUAt = at }, 0, false},
		{"older observation", func(p *ProcDetail) { p.CPUAt = at.Add(-time.Second) }, 0, false},
		{"negative counter", func(p *ProcDetail) { p.CPUTime = -1 }, 0, false},
		{"invalid counter", func(p *ProcDetail) { p.CPUTime = math.NaN() }, 0, false},
		{"infinite counter", func(p *ProcDetail) { p.CPUTime = math.Inf(1) }, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := after
			tt.edit(&current)
			got, known := processCPU(current, before)
			if known != tt.known || got != tt.want {
				t.Fatalf("got %.2f %v, want %.2f %v", got, known, tt.want, tt.known)
			}
		})
	}
	if _, known := processCPU(after, ProcDetail{}); known {
		t.Fatal("first sample must be unavailable")
	}
}

func TestCPUGroupChurnAndSnapshotImmutability(t *testing.T) {
	at := time.Unix(100, 0)
	previous := &Snapshot{Apps: []AppStat{{Name: "old", Children: []ProcDetail{cpuProcess(1, 10, at), cpuProcess(2, 1000, at), cpuProcess(3, 3, at)}}}}
	current := &Snapshot{Apps: []AppStat{
		{Name: "renamed", Children: []ProcDetail{cpuProcess(1, 12, at.Add(2*time.Second)), cpuProcess(3, 4, at.Add(2*time.Second))}},
		{Name: "new", Children: []ProcDetail{cpuProcess(4, 400, at.Add(2*time.Second))}},
		{Name: "empty"},
	}}
	sampled := WithCPU(current, previous)
	app := sampled.Apps[0]
	requireCPU(t, app.CPUKnown && app.CPUPct == 150, "wrong group CPU after exit/group rename")
	requireCPU(t, app.Children[0].CPUPct == 100 && app.Children[1].CPUPct == 50, "wrong child CPU")
	requireCPU(t, !sampled.Apps[1].CPUKnown && !sampled.Apps[2].CPUKnown, "new/empty group has no complete baseline")
	first := WithCPU(current, nil)
	requireCPU(t, !first.Apps[0].CPUKnown && !first.Apps[0].Children[0].CPUKnown, "first sample CPU must be unavailable")
	requireCPU(t, !previous.Apps[0].Children[0].CPUKnown, "previous snapshot modified")
	requireCPU(t, reflect.DeepEqual(current, first), "input snapshot modified")
	requireCPU(t, WithCPU(nil, previous) == nil, "unexpected nil snapshot")
	current.Apps[0].Children = append(current.Apps[0].Children, cpuProcess(5, 0, at.Add(2*time.Second)))
	if WithCPU(current, previous).Apps[0].CPUKnown {
		t.Fatal("partial group CPU presented as complete")
	}
}
