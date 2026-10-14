//go:build !linux && !windows && !darwin && !(freebsd && amd64) && !(openbsd && amd64)

package collector

import (
	"fmt"
	"runtime"
)

// Collect is not implemented on this platform.
func collect(_ Grouping) (*Snapshot, error) {
	return nil, fmt.Errorf("memhogs: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

// ProcessStartTime reports that process identity is unsupported.
func ProcessStartTime(pid int) (uint64, error) {
	return 0, fmt.Errorf("process identity unsupported on %s", runtime.GOOS)
}
