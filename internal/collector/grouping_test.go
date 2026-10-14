package collector

import (
	"runtime"
	"testing"
)

func TestGroupingValidation(t *testing.T) {
	if err := ValidateGrouping(DefaultGrouping()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Grouping{GroupApplication, GroupTree, GroupExecutable, "bad", ""} {
		err := ValidateGrouping(mode)
		want := mode == GroupExecutable || (runtime.GOOS == "linux" && (mode == GroupApplication || mode == GroupTree))
		if (err == nil) != want {
			t.Fatalf("%s accepted=%v, want %v", mode, err == nil, want)
		}
	}
	if _, err := CollectGrouped("bad"); err == nil {
		t.Fatal("collection accepted invalid grouping")
	}
}
