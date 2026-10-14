package collector

import (
	"reflect"
	"testing"
)

func TestResolveAccountNames(t *testing.T) {
	apps := []AppStat{
		{Children: []ProcDetail{{UID: "1000"}, {UID: "0"}, {UID: "missing"}}},
		{Children: []ProcDetail{{UID: "1000"}, {UID: "missing"}, {}}},
	}
	calls := make(map[string]int)
	resolveAccountNames(apps, func(uid string) string {
		calls[uid]++
		return map[string]string{"1000": "alice", "0": "root"}[uid]
	})
	if !reflect.DeepEqual(calls, map[string]int{"1000": 1, "0": 1, "missing": 1}) {
		t.Fatalf("lookups must be cached, including failures: %v", calls)
	}
	if apps[0].Children[0].User != "alice" || apps[1].Children[0].User != "alice" || apps[0].Children[1].User != "root" {
		t.Fatal("account names not attached to each matching process")
	}
	if apps[1].Children[1].User != "" || apps[1].Children[2].User != "" {
		t.Fatal("unknown or missing accounts must remain unavailable")
	}
}
