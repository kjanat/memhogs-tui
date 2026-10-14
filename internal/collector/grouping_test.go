package collector

import (
	"runtime"
	"testing"
)

func TestDefaultGrouping(t *testing.T) {
	if got := DefaultGrouping(); got != GroupTree {
		t.Fatalf("default grouping = %s, want tree", got)
	}
}

func TestGroupingValidation(t *testing.T) {
	if err := ValidateGrouping(DefaultGrouping()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Grouping{GroupApplication, GroupTree, GroupExecutable, "bad", ""} {
		err := ValidateGrouping(mode)
		want := mode == GroupExecutable || mode == GroupTree || (runtime.GOOS == "linux" && mode == GroupApplication)
		if (err == nil) != want {
			t.Fatalf("%s accepted=%v, want %v", mode, err == nil, want)
		}
	}
	if _, err := CollectGrouped("bad"); err == nil {
		t.Fatal("collection accepted invalid grouping")
	}
}
