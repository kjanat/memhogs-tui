//go:build darwin || windows || (freebsd && amd64) || (openbsd && amd64)

package collector

import (
	"os"
	"testing"
)

// Run on the native OS: cross-compilation alone cannot validate kernel records.
func TestNativeCollectionIncludesCurrentProcessInDefaultTree(t *testing.T) {
	snapshot, err := Collect()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range snapshot.Apps {
		if app.GroupID.Mode != GroupTree {
			t.Fatal("default collection returned another grouping mode")
		}
		for _, process := range app.Children {
			if process.PID == os.Getpid() {
				if process.StartTime == 0 || process.ParentPID <= 0 || process.Context == nil {
					t.Fatalf("native process identity/ancestry missing: %+v", process)
				}
				return
			}
		}
	}
	t.Fatal("native collection omitted its own process")
}
