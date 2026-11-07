# personal-herdr-desktop

My customizations for the [Herdr](https://herdr.dev) desktop app on macOS and Linux: three themed layouts with their own
art, icons and sounds, pull requests and
reviews, documents Herdr draws natively, a time tracker, a Desk with a pomodoro timer and notes, and local
dictation into agents with LFM3 (macOS). On Linux with [Omarchy](https://omarchy.org) the whole desktop follows the workbench
look too.

## What's in it

**Themes.** Each layout switches its colours, fonts, background, program icons, interface glyphs, Herdr logo and
alert sounds together (`ctrl-shift-l` cycles them).

| Layout | Look |
|---|---|
| `workbench` | Engineering-puzzle-game console: near-black panels, amber frames, circuit-board background, module-style pane headers, numbered space slots on "buses", chip-package program icons, dot-matrix glyphs, chime and buzz sounds. |
| `fortress` | Colony-sim inspired: pixel wooden floor, gold frames, VGA pixel font, item sprites for programs (pickaxe shell, dwarf Claude, barrel Docker…), framed button glyphs, pickaxe-clink and war-horn sounds. |
| `pixel` | DOS/EGA in dark navy, pixel icons, 8-bit beeps. |

All art and sounds are original and drawn as vectors, so they stay sharp on Retina screens.

**Projects: Tasks panel and Monitor** (`mods/px-projects`, over
[simple-herdr-projects](https://github.com/makefunstuff/simple-herdr-projects), `shp`). Every machine linked to
herdr-core, in one place:

- **Tasks panel** (TASKS in the tab bar and the status bar, with how many need you): Needs you, Working, Ready,
  Backlog and Done. A question gets a Reply line (`shp say`), a to-do gets Start, a ready task gets Reopen and Accept
  (confirmed first: checks, then merge or PR). + adds a task to any shp space on any machine. A row opens the task's
  space at its Chat.
- **Monitor** (MONITOR in the status bar, or from the panel): the operations monitor. Each machine is a rack with CPU,
  memory, disk and worker slots. Each running task is a node with its sprite, progress, step and review round, and
  plain Herdr agents get nodes too. Under them: what waits on you, the queue with its `after`s, and a ticker of every
  worker's step.
- **Sidebar**: a task's space shows as the task (id, title, state light, progress) under its repo.
- **Toasts**: once per task that comes to need you or becomes ready.

`px-status` holds the only poller: `shp list --json` per machine every 3 s and `shp tick` every 10 s, so queued work
starts. Every view reads its result, `shared("shp")`. The installer builds `shp` from a checkout beside this repo.

**Task workspaces, on machines still running Shepherdr 3.x** (until simple-herdr-projects replaces it) (`bin/task-workspaces.py`, a user service: `herdr-task-workspaces`; a service and not a mod
  because the document has to be written even with no window open, and mods only draw it): each started Shepherdr 3.x
  task becomes a real Herdr workspace on its worktree, under the space of the repo it is from (the same place the
  sidebar already puts worktrees). Its tabs are the task's Chat, its agent's live terminal (the one it runs in, no new
  one), one REVIEW tab per live reviewer, Files, Changes and Pull request, then any tab someone added (an nvim from
  Changes, say); clicking it in the sidebar opens its Chat. A worker starts each review round as its own labelled hc
  terminal, and its tab is named by the round (REVIEW 2 for `review-4-2`); the tab goes once that terminal is gone,
  and is never kept as a hand-added tab, so a window restart cannot duplicate or strand one. It goes when the task is
  done, archived or removed: two passes in a row, so a flicker never closes a space, and its live terminals are
  unlabelled first, because a live terminal keeps its `herdr.space` label and Herdr rebuilds the space (with an
  Assistant) from it - without that a removed space comes back and is removed again, over and over. The programs keep
  running; only their place in the window goes. (If one still comes back three times it is left in the document and out
  of the sidebar for half an hour, said once in the log, so a stuck label can never turn into a write loop.) Workspaces
  made by hand are never touched. The service reads this computer's tasks only, and a task workspace follows its task,
  so renaming its rows by hand is undone. While the window is open each of these writes is a layout change it didn't
  make, so Herdr shows its own "Layout changed elsewhere" notice when a task appears, goes or is renamed, or when its
  worker is replaced (never for a status or progress change).

**Desk** (`mods/desk`, DESK button in the status bar): pomodoro timer with a pixel tomato, focus stats and a 14-day
chart, today's note with clickable checkboxes; a timer chip in the status bar.

**Pull requests** (`mods/px-pr`, PR in the tab bar): the PR of the branch you're on, or one picked in Reviews, in the
panel. Description, open threads by file with the code they're on, resolved ones folded, reviews and comments as one
timeline; react, reply, comment, approve or request changes; FILES shows every diff, click a line to draft a comment and
submit them as one review; CHECKS lists failing ones first. ASK sends a comment to the agent working on that branch,
or to one agent in a "Chores" space for everything that isn't your own work.

**Reviews** (`mods/px-reviews`): PRs waiting for your review, assigned to you, reviewed by you and your own, from one
GitHub query; a toast for new requests; REVIEW hands one to the agent for a summary and draft comments.

**Changes** (`mods/px-changes`): the working tree's diff with line and file comments that go to the pane's agent; a
line number (or a file's `nvim` button) opens it there in Neovim: in the space's `nvim` tab when it has one, else a new one.

**Docs** (`mods/px-doc`, `herdr-doc`): investigation notes, runbooks and drafts as `herdr.doc/2` JSON
([spec](herdr-app/mods/px-doc/SPEC.md)) drawn natively in the Docs panel: callouts, findings with evidence, timelines,
tables, todos. `herdr-doc new|check|export|list`; a Claude Code skill tells agents to write these instead of HTML.

**Time** (`mods/px-time`, `herdr-timetrack`): where the day goes, from a local sampler of the focused window
(app, title, Herdr space and tab) every 30 s: active and idle time, by category, by Herdr space, by issue key, the day as
10-minute slots, the last 7 days. Opt-in: `./install.sh --timetrack` (Linux: a user service; macOS: a LaunchAgent,
`macos/src/herdr-timetrack.swift`, that reads window titles once you allow it under Accessibility).

**Sidebar**: AI usage limits (`mods/px-usage`, needs `herdr-usage`) and CPU / GPU / RAM / disk (`mods/px-sysmon`).

**Linux extras** (`linux/`): Herdr typing sounds (real Cherry MX Blue recordings from Mechvibes, softened; IBM Model M
from bucklespring if present; SUPER+ALT+K mutes), a Neovim colorscheme `herdr` that follows Herdr's layout live (and a
standalone `workbench` one). [herdr-nvim](https://github.com/ChmaraX/herdr-nvim) on Herdr Desktop
(`lua/config/herdr_desktop.lua`): the plugin asks the legacy herdr server for agents; this sends its comments through
`hc` to the agent in Neovim's own Herdr space (its tab's agent, else the space's only one or its `agent` tab, else a
picker). Hook it into the plugin's spec:

```lua
{ 'ChmaraX/herdr-nvim', event = 'VeryLazy', opts = {},
  config = function(_, opts) require('herdr-nvim').setup(opts); require('config.herdr_desktop').setup() end },
```

**Omarchy** (`omarchy/`): the `workbench` Omarchy theme (colours, three circuit wallpapers, bar), Workbench icons for
apps and cursors, a workbench lock screen, a circuit-board / logic-analyzer screensaver, dot-matrix glyphs for the bar,
console sounds for login, low battery and notifications. A theme-set hook switches lock view, cursors, menu and terminal
fonts with the theme, so `omarchy theme set workbench` changes everything at once.

**Dictation** ([voice-computer/](voice-computer/), macOS, opt-in: `./install.sh --voice`): `Option+L` in an agent's chat or terminal, speak, `Option+L`
again: LFM3-Audio-2B writes it down on this Mac and it lands in the chat's box or at the terminal's cursor, never sent
by itself. The voice computer runs in `dictation_only` mode for this; its assistant features (talking, a brain,
announcements, memory) are still in the code but off.

## Install

```sh
git clone https://github.com/makefunstuff/personal-herdr-desktop
cd personal-herdr-desktop
./install.sh            # or ./install.sh --no-voice
```

Requirements: Herdr desktop, `gh` for pull requests; for voice (macOS) also Go, `brew install llama.cpp sox`; on Linux
PipeWire (`pw-cat`) for the typing sounds. With [omarchy-herdr-win31](https://github.com/makefunstuff/omarchy-herdr-win31)
installed too, run this installer after it (both ship px-pane; this one has the Win 3.1 title bars as well). On macOS 15 with Homebrew's llama.cpp,
the voice computer sets `GGML_METAL_BF16_DISABLE=1` so Metal works.

The Windows 3.1 layout lives in [omarchy-herdr-win31](https://github.com/makefunstuff/omarchy-herdr-win31); its
Microsoft assets are fetched on your machine, never stored in a repo.

## Platforms

Both: themes, layouts, mods, art, sounds, the document formats, the task workspaces service (on machines with Shepherdr 3.x), herdr-doc, typing sounds and the time
tracker (macOS: Swift LaunchAgents in `macos/`, built with `swiftc`; allow herdr-keyclick under Input Monitoring and
herdr-timetrack under Accessibility; `cmd-alt-k` mutes the clicks). macOS only: the voice computer (`Herdr Voice.app`,
`afplay`/`rec`, `osascript`, Homebrew). Linux only: Omarchy theme. The
installer drops layout panels whose mods aren't installed.

## Licence

Apache-2.0 (the `px-*` mods are modified from Herdr's own mods). The voice computer is MIT (its own LICENSE). Fonts:
PxPlus IBM VGA 8x16 (CC BY-SA 4.0), Pixelated MS Sans Serif (CC BY-SA 3.0); see `herdr-app/fonts/` and NOTICE. Cherry MX
Blue key sounds: Mechvibes, MIT.
