//go:build !unix && !windows

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

func killCmdStrPID(pid int, force bool) string {
	return fmt.Sprintf("kill %d (unsupported platform)", pid)
}

func rescueCmdStr(name string) string {
	return fmt.Sprintf("kill %q (unsupported platform)", name)
}
