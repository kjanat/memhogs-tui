//go:build !linux && !windows && !darwin && !(freebsd && amd64) && !(openbsd && amd64)

package collector

import (
	"fmt"
	"runtime"
)

// Collect is not implemented on this platform.
func Collect() (*Snapshot, error) {
	return nil, fmt.Errorf("memhogs: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}
