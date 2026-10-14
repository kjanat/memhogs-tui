# memhogs-tui

Arch froze again. Next time, have this open.

```sh
# Install
go install memhogs.kjanat.dev/cmd/memhogs-tui@latest

# Run
go run memhogs.kjanat.dev/cmd/memhogs-tui@latest
```

After installation, run `memhogs-tui` to refresh every three seconds. Set a
different interval with `memhogs-tui -i 5` or `memhogs-tui 5`. An explicit `-i`
takes precedence over the positional interval, followed by the legacy `WATCH`
environment variable and the default. Intervals must be positive whole seconds;
invalid values and values that overflow a Go duration are rejected.

| Key                           | Action                                                                                                                |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `↑` / `k`, `↓` / `j`          | Move selection; the table scrolls to keep it visible                                                                  |
| `Enter`, `→` / `l`, `←` / `h` | Toggle, expand, or collapse a process group                                                                           |
| `s`                           | Cycle RSS, swap (commit on Windows), process count, and name sorting                                                  |
| `/`, `Enter`, `Esc`           | Search group names and child command lines, accept, or clear the filter                                               |
| `p`                           | Pause or resume collection                                                                                            |
| `c`                           | Inspect full commands and available directory/terminal information; arrows or Page Up/Down scroll, `c` / `Esc` closes |
| `x`, `X`                      | Confirm termination or force termination of the selected group or child                                               |
| `y`, `n` / `Esc`              | Confirm or cancel termination                                                                                         |
| `?`, `q` / `Ctrl+C`           | Toggle help or quit                                                                                                   |

Sorting and searching cover every collected group above 1 MiB RSS or with
nonzero swap or commit, and expanded
groups include every collected child. Selection follows the group name or the
child's PID and creation time across refreshes. Termination captures the listed
PIDs when the dialog opens, checks creation time before signaling each process,
and reports failures for individual PIDs. Processes that join a group afterward
are outside that captured scope. On Windows both termination keys use
`TerminateProcess`.

Memory values use binary units: KiB, MiB, and GiB. Group RSS adds each child's
resident memory, including shared pages counted in multiple processes; it does
not measure memory uniquely attributable to the application.

| Metric                  | Linux                                | macOS       | FreeBSD amd64           | OpenBSD amd64 | Windows       |
| ----------------------- | ------------------------------------ | ----------- | ----------------------- | ------------- | ------------- |
| Process resident memory | RSS                                  | RSS         | RSS                     | RSS           | Working set   |
| Process swap            | `/proc/PID/status`                   | Unavailable | Kernel swap RSS         | Unavailable   | Unavailable   |
| Process commit          | Unavailable                          | Unavailable | Unavailable             | Unavailable   | Commit charge |
| System swap             | Available                            | Available   | Available via `swapctl` | Available     | Unavailable   |
| Memory PSI / OOM scores | Available when exposed by the kernel | Unavailable | Unavailable             | Unavailable   | Unavailable   |

Unavailable process counters display `n/a`; unavailable system swap displays
`unavailable`. Windows commit charge is displayed separately from swap. Its
system panel labels the commit limit and remaining commit available to the
current process. Collection errors remain visible and are retried on subsequent
refreshes.

Expanded rows align each process's RSS, swap or commit, PID, and memory percentage
with the group columns. Recognizable Chromium launch flags become short roles
such as renderer, GPU process, or network service. These roles do not identify
individual browser tabs or sites. A process without a `--type` flag is labeled
`browser (no --type)` when its group is a recognized Chromium browser. Other
applications retain their command summaries. On Linux, interactive shell rows
show their working directories and input terminals when readable. Processes
with neither resident nor swapped memory, including zombies, are excluded from
Linux memory groups. Child sorting follows RSS, swap or commit, or name;
process-count sorting uses RSS within each group.

On Linux, Claude rows put the recorded session start directory first, followed
by the current session name and a short session UUID, when matching metadata
exists in `~/.claude/sessions/`.
The PID and process creation time must both match. Press `c` for the full UUID,
name, recorded spawn directory, current working directory, and CLI/SDK
entrypoint. Search also matches
session names, UUIDs, and project paths. Missing or mismatched metadata displays
`session unavailable`, with the available working directory labeled `cwd`;
the collector reads no conversation transcripts.
