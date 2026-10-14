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

On Linux the default is **application-instance grouping**. Processes whose
observed executable family belongs to one application root share a group;
separate roots stay separate even when they have the same name, executable,
or workspace. Group identity uses the root PID and creation time. Inferred
application names and workspace labels can change without changing that identity.
For example, ChatGPT and Bitwarden using the same Electron binary have separate
totals and termination scopes. Package paths, rather than friendly class names,
provide application evidence. Verified ancestry and the nearest main process
establish the running instance; a renderer's package-path alias does not create
a new instance. Separate main processes stay separate even with identical packages.

Choose the grouping explicitly when needed:

```sh
memhogs-tui --group application  # independent executable families (Linux default)
memhogs-tui --group executable   # all collected processes using each executable
memhogs-tui --group tree         # include different executables in an observed tree (Linux)
```

Application grouping follows observed same-executable ancestors, including across
sandbox helpers, and respects distinct recorded sessions and application mains.
It does not automatically include every helper running a different executable;
tree grouping does that. Tree traversal stops at shells and session managers.
For example, a Node-based Claude ACP adapter launched by Zed has its own
application group, with Zed shown as its launcher. Use `--group tree` to account
for that adapter and other descendants inside Zed's group instead. Terminating
that tree therefore has a wider scope than terminating the adapter alone.
Missing ancestry stays separate rather than being joined by a matching display
name. If a root exits or becomes unreadable, its children can become separate
groups on the next sample; no ownership is invented from past labels.
On other supported platforms the default remains executable/process-name
grouping; application and tree modes report their Linux requirement.

Instance rows include their root PID and available workspace. The header shows
the grouping mode, and termination confirmation states whether the selection is
one process, one application instance, a tree, or a broad executable group.
Group identities carry the strategy and either the verified root lifetime or
executable identity, separately from display labels and package hints. Linux
executable identity includes the observed path, device, and inode; unreadable
executables fall back to individual process lifetimes. Other platforms retain
their available executable/process-name grouping.

| Key                           | Action                                                                                                                |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `↑` / `k`, `↓` / `j`          | Move selection; the table scrolls to keep it visible                                                                  |
| `Page Up`, `Page Down`        | Move one visible page through table rows, or scroll details when the sidebar has focus                                |
| `Home`, `End`                 | Select the first or last visible-list row, or jump to the start or end of details                                     |
| `Tab`, `Esc`                  | Switch between table and details; `Esc` returns from details to the table                                             |
| `Enter`, `→` / `l`, `←` / `h` | Toggle, expand, or collapse a process group                                                                           |
| `s`                           | Cycle RSS, swap (commit on Windows), process count, name, and CPU sorting                                             |
| `/`, `Enter`, `Esc`           | Search group names and child command lines, accept, or clear the filter                                               |
| `p`                           | Pause or resume collection                                                                                            |
| `c`                           | Inspect full commands and available directory/terminal information; arrows or Page Up/Down scroll, `c` / `Esc` closes |
| `x`, `X`                      | Confirm termination or force termination of the selected group or child                                               |
| `y`, `n` / `Esc`              | Confirm or cancel termination                                                                                         |
| `?`, `q` / `Ctrl+C`           | Toggle help or quit                                                                                                   |

Sorting and searching cover every collected group above 1 MiB RSS or with
nonzero swap or commit, and expanded
groups include every collected child. Selection follows the group identity or the
child's PID and creation time across refreshes. Termination captures the listed
PIDs when the dialog opens, checks creation time before signaling each process,
and reports failures for individual PIDs. Processes that join a group afterward
are outside that captured scope. On Windows both termination keys use
`TerminateProcess`.

Confirmation shows the grouping mode, exact captured process count, and PID list.
An application or tree group with inconsistent ownership evidence cannot be
terminated in bulk; select a child process instead. Explicit executable grouping
retains its broad scope, which can include several applications or launches.

Creation-time checks reduce PID-reuse risk. On Linux, Go uses a pidfd when the
kernel and runtime support it; other Unix platforms and Linux fallback paths can
still have a race between checking identity and sending the signal.

Memory values use binary units: KiB, MiB, and GiB. Group RSS adds each child's
resident memory, including shared pages counted in multiple processes; it does
not measure memory uniquely attributable to the application.

The `CPU%` column measures user and kernel CPU time between observations, for
both groups and individual processes. **100% means one fully occupied logical
CPU**; multithreaded processes and groups can exceed 100%. Group CPU adds its
current children's usage. The first observation of a process, unreadable
counters, and reused PIDs display `n/a` until a valid baseline exists. A group
displays `n/a` if any current child lacks that baseline. Rates use actual elapsed
time, including pauses or delayed refreshes, rather than the configured interval.
CPU sorting puts measured values first, highest usage first, including within
expanded groups. The memory eligibility threshold still applies to the list.

| Metric                  | Linux                                | macOS       | FreeBSD amd64           | OpenBSD amd64 | Windows       |
| ----------------------- | ------------------------------------ | ----------- | ----------------------- | ------------- | ------------- |
| Process resident memory | RSS                                  | RSS         | RSS                     | RSS           | Working set   |
| Process CPU usage       | User + kernel time                   | Task time   | Process runtime         | User + kernel | User + kernel |
| Process swap            | `/proc/PID/status`                   | Unavailable | Kernel swap RSS         | Unavailable   | Unavailable   |
| Process commit          | Unavailable                          | Unavailable | Unavailable             | Unavailable   | Commit charge |
| System swap             | Available                            | Available   | Available via `swapctl` | Available     | Unavailable   |
| Memory PSI / OOM scores | Available when exposed by the kernel | Unavailable | Unavailable             | Unavailable   | Unavailable   |

Unavailable process counters display `n/a`; unavailable system swap displays
`unavailable`. Windows commit charge is displayed separately from swap. Its
system panel labels the commit limit and remaining commit available to the
current process. Collection errors remain visible and are retried on subsequent
refreshes.

Expanded rows align each process's RSS, swap or commit, PID, memory percentage, and CPU percentage
with the group columns. On Linux, every row uses common context: its working
directory, executable, parent chain, and concrete application when a package
argument identifies a shared runtime's owner. This distinguishes Electron apps
and shows which editor or CLI launched a helper. Search matches this context too.
For orphaned processes whose cwd is uninformative, bounded descriptor inspection
can identify a repository from an open resource such as a `.git` socket. Resource
contents are never read. The full-command inspector shows the evidence and source
of the directory label; a current directory or open repository is not a recorded
spawn directory.

The sidebar puts **Process context** directly below the selected row's memory
and CPU counters. Group rows show their verified root process; child rows show
that individual process. It includes the account name and effective UID when
available, the launching application from observed ancestry, directory, wrapped
launch arguments, parent PID, executable, terminal, and available session details.
Broad executable groups ask you to select a child instead of borrowing one
process's owner for the entire group.

**Launch dir (session)** comes from verified session metadata. Otherwise the
sidebar labels the observed working directory **Current dir** and says that the
launch directory was not recorded. A process can change its working directory;
the original directory cannot be recovered from its current cwd alone. Argument
previews are bounded; `c` opens the complete command/context inspector.

Press `Tab` to focus and scroll details with arrows, Page Up/Down, or Home/End;
`Tab` or `Esc` returns to the table. RAM, available memory, swap, and the Linux
10-second PSI summary (`s` = some, `f` = full) stay pinned while details scroll.
Below 90 columns, `Tab` opens details across the terminal width. Account names
are resolved once per distinct UID in each scan on Unix; an unresolved account
still shows its UID. Windows account ownership is unavailable.

Recognizable Chromium launch flags add roles such as renderer, GPU process, or
network service. A main process is recognized from those flags in its observed
process family, including browsers absent from any application-name list. These
roles do not identify individual browser tabs or sites. Generic command summaries
keep leading actions and leave complete launch arguments in the `c` inspector.
Terminals are shown when readable. Processes with neither resident nor swapped
memory, including zombies, remain in Linux's ancestry graph but contribute no
memory rows or captured kill targets. Their metadata can still identify an
application root or intermediary. Ownership is recomputed from each snapshot;
there is no persistent process registry. Child sorting follows RSS, swap or commit, name, or CPU;
process-count sorting uses RSS within each group.

On Linux, Claude rows put the recorded session start directory first, followed
by the current session name and a short session UUID, when matching metadata
exists in `~/.claude/sessions/`.
The PID and process creation time must both match. Press `c` for the full UUID,
name, recorded spawn directory, current working directory, and CLI/SDK
entrypoint. Search also matches
session names, UUIDs, and project paths. Missing or mismatched metadata displays
`session unavailable`, alongside the available common process context;
the collector reads no conversation transcripts.
