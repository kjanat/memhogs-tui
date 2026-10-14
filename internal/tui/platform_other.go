//go:build !unix && !windows

package tui

import (
	"fmt"
	"runtime"

	"memhogs.kjanat.dev/internal/collector"
)

func killProcess(process collector.ProcDetail, force bool) error {
	return fmt.Errorf("kill not supported on %s", runtime.GOOS)
}

func killSignalName(force bool) string {
	return "kill"
}

func killCmdStr(children []collector.ProcDetail, force bool) string {
	return "kill (unsupported platform)"
}
