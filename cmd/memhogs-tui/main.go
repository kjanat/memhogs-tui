// Memhogs is a TUI memory monitor for Linux, macOS, FreeBSD, OpenBSD, and
// Windows. It shows grouped memory usage by application with fold/unfold,
// sorting, filtering, and process kill support.
//
// Usage:
//
//	memhogs-tui [-i SECONDS] [SECONDS]
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

	"memhogs.kjanat.dev/internal/tui"
)

// Set via -ldflags at build time:
//
//	go build -ldflags "-X main.version=v0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	showVersion := flag.Bool("v", false, "print version and exit")
	interval := flag.Int("i", 3, "refresh interval (seconds)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "memhogs %s (%s, %s)\n\n", version, commit, date)
		fmt.Fprintf(os.Stderr, "Usage: memhogs [-i SECONDS] [SECONDS]\n\n")
		fmt.Fprintf(os.Stderr, "TUI memory monitor — grouped memory usage by application.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("memhogs %s (commit %s, built %s)\n", version, commit, date)
		return
	}

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
