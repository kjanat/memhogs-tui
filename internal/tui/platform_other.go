//go:build !linux && !windows

package tui

import (
	"fmt"
	"runtime"
)

func killProcess(pid int, force bool) error {
	return fmt.Errorf("kill not supported on %s", runtime.GOOS)
}

func killSignalName(force bool) string {
	return "kill"
}

func killCmdStr(name string, force bool) string {
	return fmt.Sprintf("kill %q (unsupported platform)", name)
}

func rescueCmdStr(name string) string {
	return fmt.Sprintf("kill %q (unsupported platform)", name)
}
