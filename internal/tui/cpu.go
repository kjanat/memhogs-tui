package tui

import "fmt"

func cpuText(percent float64, known bool) string {
	if !known {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", percent)
}
