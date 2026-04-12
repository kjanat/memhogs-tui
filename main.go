// Memhogs is a TUI memory monitor for Linux.
// It shows grouped memory usage by application with fold/unfold, sorting,
// filtering, and process kill support.
//
// Usage:
//
//	memhogs [-i SECONDS] [SECONDS]
//
// The flags are:
//
//	-i
//	    Refresh interval in seconds (default: 3).
//
// A bare numeric argument is treated as the refresh interval.
// The WATCH environment variable also sets the interval for backwards
// compatibility.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kjanat/memhogs-tui/internal/tui"
)

func main() {
	interval := flag.Int("i", 3, "refresh interval (seconds)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: memhogs [-i SECONDS] [SECONDS]\n\n")
		fmt.Fprintf(os.Stderr, "TUI memory monitor — grouped memory usage by application.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Positional arg as interval: memhogs 5
	if flag.NArg() > 0 {
		if n, err := strconv.Atoi(flag.Arg(0)); err == nil {
			*interval = n
		}
	}

	// WATCH env var for backwards compat
	if v, ok := os.LookupEnv("WATCH"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			*interval = n
		}
	}

	m := tui.New(tui.Config{
		Interval: time.Duration(*interval) * time.Second,
	})

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "memhogs: %v\n", err)
		os.Exit(1)
	}
}
