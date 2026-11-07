# PHD-5 audit: the desktop mods

`herdr-app check` passed for every mod before this work (the output is in the
proof artifact), and `herdr-app status` reported no layout, mod or overlay
errors. The bugs below are the ones that still fail in normal use, plus the
dead code and duplication removed afterwards. PHD-3, the sidebar's USAGE and
SYSTEM blocks, was checked first and still holds: all three layouts give
`usage` and `sysmon` a minimum equal to their size, and the live workbench
shows both blocks (before screenshot in the proof artifact).

## Bugs

### 1. Removing a worktree from a repo's main checkout closes its space first

**File:** `herdr-app/mods/px-worktree/main.js` (`_pxRender`, `key`, `work`, `remove`)

The dialog marks a main checkout as blocked and says so, but the check is only
used for the hint: Enter in the dialog still calls `work()`, which calls
`remove()`. `remove()` runs `space.close` before `git worktree remove`, and git
always refuses to remove the main checkout, so the space and its terminals are
closed while the checkout stays. The same happens for any failed `git worktree
remove`: the space is already gone.

**Reproduce:** right-click a space whose folder is a repo's main checkout,
choose Remove worktree, press Enter. The space closes, `git worktree remove`
fails, no worktree is removed.

**Fix:** refuse the remove before any side effect when the folder is not a
linked worktree; make `key()` and `work()` use the same guard.

### 2. A note typed while viewing a past day is written to today

**File:** `herdr-app/mods/desk/main.js` (`addNote`)

The day strip lets Desk show an earlier day. The note input stays visible on
that day (PHD-3 made it unconditional), but `addNote()` always builds
`ymd(new Date())` and then sets `this.day` to today. Typing a note while
looking at yesterday silently changes today's file and jumps the view.

**Reproduce:** open Desk, click Yesterday, type a note and press Enter. The
note appears in `desk-notes/daily/<today>.md`, not in yesterday's file.

**Fix:** write to the day Desk is showing (`this.day`); today behaves exactly
as before.

### 3. SYSTEM says "Linux only" on Linux until the second sample

**File:** `herdr-app/mods/px-sysmon/main.js` (`read`)

`s.linux` is `this.prev != null || s.cpu != null`. On Linux the CPU percentage
needs a previous sample, so the first reading has neither, and the block shows
"CPU, GPU and RAM: Linux only" for up to ten seconds, on Linux.

**Reproduce:** reopen the window (or wait for a reload) and look at SYSTEM
before the second sample.

**Fix:** remember that a `/proc/stat` `cpu` line was read; that is what makes
the machine Linux, independent of having a delta yet.

### 4. A task's sidebar line can show an already answered question

**File:** `herdr-app/mods/px-spaces/main.js` (`shpTasks`)

The Tasks panel and the poller's toasts pick the first *open* question
(`openAsk`), but the sidebar picks `asks[0]` whatever its state. A task with an
answered first question and a newer open one shows the old words in the
sidebar.

**Reproduce:** give an shp task two questions, answer the first, leave the
second open; the sidebar keeps showing the first. The Tasks panel shows the
second.

**Fix:** use the same "first open ask, else the first ask" rule in the sidebar.

## Dead code and duplication (refactor)

- `px-tabs`: `find` and `findNode` are the same layout walk; use one.
- `px-pr`: `reviewers()` and the `agent` argument of `ask()` are unused. The
  Ask action has routed through `toWork()`/Chores for a while; say that in the
  comment instead of keeping a dead path.
- `px-reviews`: `agent()` is unused.
- `px-spaces`: the exported `glyph`/`tone` are only used in this file.
- `px-usage`: `meterBar` is only used in this file.
- `px-projects/shp.js`: `herdrStatus` is unused. `px-status` has its own copy
  of `openMonitor` (a mod can only import from its own folder).
- Unused imports: `px-changes` (`Scrollbar`, `v_virtual_list`), `px-doc`
  (`notify`), `px-picker` (`openOverlay`), `px-pr` (`svg`, `notify`),
  `px-status` (`exec`, `home`), plus the unused `tip` argument in `px-pane`.
- `desk`: the `soft` helper is a no-op left from the wrapping work.

## Line counts, before

Only `*.js`; SVG and JSON are assets, not code.

| mod | lines |
|---|---:|
| desk | 298 |
| px-agents | 525 |
| px-changes (`main.js` + `hl.js`) | 502 |
| px-chrome | 86 |
| px-confirm | 52 |
| px-doc | 262 |
| px-files | 172 |
| px-help | 91 |
| px-machine-add | 368 |
| px-menu | 162 |
| px-new-menu | 74 |
| px-pane | 221 |
| px-peek | 76 |
| px-picker | 236 |
| px-pr | 608 |
| px-projects (`main.js` + `shp.js`) | 462 |
| px-prompt | 50 |
| px-reviews | 307 |
| px-spaces | 504 |
| px-status (`main.js` + `poll.js`) | 200 |
| px-sysmon | 169 |
| px-tabs | 143 |
| px-time | 142 |
| px-toast | 37 |
| px-usage | 155 |
| px-worktree | 367 |
| voice | 171 |

`bin/task-workspaces.py` (665) and `bin/shepherdr-spaces.py` (189) are the
Shepherdr-specific services; they are only touched where a bug requires it.

## What goes at the shp cutover (SHP-4/SHP-5)

These parts are intentionally left alone beyond bug fixes because
simple-herdr-projects replaces them:

- `herdr-app/bin/task-workspaces.py` and the `task-workspaces.json` registry
  it writes.
- The Shepherdr task registry reads in `px-agents` (`loadTasks`, `taskFor`,
  `taskAgents`) and `px-spaces` (`loadTasks`, `openTask`, the `sptask`
  nesting and the `task-workspaces.json` entries).
- `herdr-app/bin/shepherdr-spaces.py` and the Shepherdr patches in
  `install.sh`.
- `px-tabs`' Shepherdr panel toggles and Artifacts button, where the layout
  still has Shepherdr's nodes.
- The `~/.herdr/tasks` routing in `px-pr` and `px-reviews` (`workAgent`).

The replacements already on main are `px-projects` (Tasks/Monitor) and the
`shared("shp")` poller in `px-status/poll.js`.
