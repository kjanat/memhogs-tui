package collector

import "strings"

// Arguments returns observed argv, with a display-only fallback for rewritten
// process titles and platforms without structured arguments.
func (p ProcDetail) Arguments() []string {
	if len(p.Args) == 1 && strings.Contains(p.Args[0], " --") {
		return strings.Fields(p.Args[0])
	}
	if len(p.Args) > 0 {
		return p.Args
	}
	return strings.Fields(p.Cmdline)
}
