// Memhogs is a TUI memory monitor for Linux, macOS, FreeBSD, OpenBSD, and
// Windows. It shows grouped memory usage by application with fold/unfold,
// sorting, filtering, and process kill support.
//
// Usage:
//
//	memhogs-tui [-i SECONDS] [--group application|executable|tree] [SECONDS]
//
// The flags are:
//
//	-i
//	    Refresh interval in seconds (default: 3).
//	-group
//	    Group by application instance, executable, or process tree.
//
// A bare numeric argument is treated as the refresh interval.
// The WATCH environment variable also sets the interval for backwards
// compatibility.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"memhogs.kjanat.dev/internal/collector"
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
	grouping := flag.String("group", string(collector.DefaultGrouping()), "group by tree, executable, or application instance (application: Linux)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "memhogs %s (%s, %s)\n\n", version, commit, date)
		fmt.Fprintf(os.Stderr, "Usage: memhogs [-i SECONDS] [--group application|executable|tree] [SECONDS]\n\n")
		fmt.Fprintf(os.Stderr, "TUI memory monitor — grouped memory usage by application.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("memhogs %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	mode := collector.Grouping(*grouping)
	if err := collector.ValidateGrouping(mode); err != nil {
		fmt.Fprintf(os.Stderr, "memhogs: %v\n", err)
		os.Exit(2)
	}

	explicitInterval := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "i" {
			explicitInterval = true
		}
	})
	watch, watchSet := os.LookupEnv("WATCH")
	d, err := resolveInterval(*interval, explicitInterval, flag.Args(), watch, watchSet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memhogs: %v\n", err)
		os.Exit(2)
	}

	m := tui.New(tui.Config{
		Interval: d,
		Grouping: mode,
	})

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "memhogs: %v\n", err)
		os.Exit(1)
	}
}

// resolveInterval gives explicit -i precedence over a positional interval,
// then WATCH, then the default. Only the selected source is interpreted.
func resolveInterval(seconds int, explicit bool, args []string, watch string, watchSet bool) (time.Duration, error) {
	if len(args) > 1 {
		return 0, fmt.Errorf("expected at most one interval argument")
	}
	source := "-i"
	value := strconv.Itoa(seconds)
	if !explicit {
		switch {
		case len(args) == 1:
			source, value = "interval argument", args[0]
		case watchSet:
			source, value = "WATCH", watch
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/int64(time.Second) {
		return 0, fmt.Errorf("%s must be a positive whole number of seconds within the supported duration range", source)
	}
	return time.Duration(n) * time.Second, nil
}
