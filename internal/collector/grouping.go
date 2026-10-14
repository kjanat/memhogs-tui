package collector

import (
	"fmt"
	"runtime"
)

// Grouping selects the accounting and group-termination scope.
type Grouping string

// Grouping strategies supported by the collectors.
const (
	GroupApplication Grouping = "application"
	GroupExecutable  Grouping = "executable"
	GroupTree        Grouping = "tree"
)

// DefaultGrouping includes applications and their observed descendants.
func DefaultGrouping() Grouping {
	return GroupTree
}

// ValidateGrouping rejects unknown modes and modes without platform support.
func ValidateGrouping(mode Grouping) error {
	switch mode {
	case GroupApplication:
		if runtime.GOOS != "linux" {
			return fmt.Errorf("%s grouping requires Linux application attribution; use tree or executable", mode)
		}
	case GroupExecutable, GroupTree:
	default:
		return fmt.Errorf("unknown grouping %q: choose application, executable, or tree", mode)
	}
	return nil
}

// Collect gathers a snapshot with the platform's default grouping.
func Collect() (*Snapshot, error) {
	return CollectGrouped(DefaultGrouping())
}

// CollectGrouped gathers a snapshot whose totals and child membership use mode.
func CollectGrouped(mode Grouping) (*Snapshot, error) {
	if err := ValidateGrouping(mode); err != nil {
		return nil, err
	}
	snapshot, err := collect(mode)
	if err != nil {
		return nil, err
	}
	resolveAccountNames(snapshot.Apps, lookupAccount)
	return snapshot, nil
}
