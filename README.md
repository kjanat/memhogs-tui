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

The default is **process-tree grouping** on Linux, macOS, Windows, FreeBSD amd64,
and OpenBSD amd64. An application includes its observed descendants, including
helpers running different executables. Zed's group therefore includes its
Node-based adapters and language servers. Tree traversal stops at shells and
operating-system session managers, keeping separate application launches apart.

Choose another grouping explicitly when needed:

```sh
memhogs-tui                     # applications and their descendants (default)
memhogs-tui --group tree        # explicitly select the default
memhogs-tui --group application # independent executable families (Linux)
memhogs-tui --group executable  # all collected processes using each executable
```

Tree identity uses the root PID and creation time. Names and workspace labels can
change without changing identity. Parent PIDs and creation times come from native
process enumeration on each platform. Unknown or reused parents break the chain;
unrelated namesakes are never joined to fill that gap. If a root exits, surviving
children can become separate groups on the next sample.

Optional Linux application-instance grouping follows verified same-executable
families across sandbox helpers and respects distinct sessions and application
mains. A renderer's package alias does not create a separate instance, while
independent main processes remain separate even with identical packages. In that
mode a Node-based adapter has its own group with Zed as its launcher. Default tree
mode includes that adapter inside Zed's group, including in the captured scope
when terminating the tree. The `application` mode reports its Linux requirement
on other platforms; `tree` and `executable` are available on all supported OSes.

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
| `Enter`, `→` / `l`, `←` / `h` | Toggle/expand a group or process branch; left folds its branch or selects its parent                                  |
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
CPU sorting puts measured values first, highest usage first. Expanded trees sort
siblings while keeping parents before their descendants. The memory eligibility
threshold still applies to the list.

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
with the group columns. Drag a `│` boundary in the table header to adjust that
column's width. Widths stay fixed through sorting and refreshes for the current
run; shrinking the terminal compresses them, and enlarging it restores your
chosen widths. Right-click the header or press `r` to return to automatic sizing.
Click a row to select it, and use the mouse wheel over the table or sidebar to
scroll that area.

On Linux, every row uses common context: its working
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
Python rows show the script entry point or `python -m` module instead of virtualenv
and uv cache paths, for example `serena start-mcp-server` or `blender-mcp`.
Process labels use compact `project [tool(via launcher)]` notation, for example
`memhogs-tui [blender-mcp(via uv)]`.
Narrow table columns put the Python tool before workspace and launcher context
so the tool name survives truncation.
Terminals are shown when readable. Processes with neither resident nor swapped
memory, including zombies, remain in Linux's ancestry graph but contribute no
memory rows or captured kill targets. Their metadata can still identify an
application root or intermediary. Ownership is recomputed from each snapshot;
there is no persistent process registry. Child sorting follows RSS, swap or commit, name, or CPU;
process-count sorting uses RSS within each group.

Expanded application and tree groups render a nested process hierarchy with
indentation and branch guides. Descendants stay below their own parent even when
they use more memory or CPU. Each process branch can fold independently with
`Enter` or the arrow keys. A root's `(self)` row shows its own counters; the group
header shows the aggregate. Process rows, including folded branches, retain
individual-PID inspection and termination. Executable grouping keeps its flat
ranking across instances.

Claude, Codex, and Zed trees add expandable **project sections** with their own
RSS, swap or commit, process-count, and CPU totals. Expand an application, then a
project to see its sessions, language servers, MCP servers, and other dependencies
in their process hierarchy. Project identity uses the full directory inside the
owning process instance; identical repository names and independent launches stay
separate. Executable grouping retains its flat ranking.

Project attribution prefers verified session directories, structured absolute
Codex workspace arguments, then observed workspace/current directories. Git
subdirectories resolve to their repository or worktree root. Dependencies inherit
the workspace of a verified Claude or Codex ancestor; Zed's other tools use their
own workspace evidence. Zed's editor processes and processes without sufficient
evidence stay under **Shared / unassigned**, rather than being charged to an
arbitrary open project. Package caches do not become projects. The sidebar shows
the full project path and evidence; `c` includes the project owner and process
context. Selecting a project for termination captures only its displayed member
PIDs, with the project path and owning application disclosed in the confirmation.

On Linux, Claude rows put the recorded session start directory first, followed
by the current session name and a short session UUID, when matching metadata
exists in `~/.claude/sessions/`.
The PID and process creation time must both match. Press `c` for the full UUID,
name, recorded spawn directory, current working directory, and CLI/SDK
entrypoint. Search also matches
session names, UUIDs, and project paths. Missing or mismatched metadata displays
`session unavailable`, alongside the available common process context;
the collector reads no conversation transcripts.
