# personal-herdr-desktop

My customizations for the [Herdr](https://herdr.dev) desktop app on macOS and Linux: three themed layouts with their own
art, icons and sounds, Shepherdr inside Herdr (panel, task board, operations monitor, native artifacts), pull requests and
reviews, documents Herdr draws natively, a time tracker, a Desk with a pomodoro timer and notes, and a local voice
assistant on LFM3 models (macOS). On Linux with [Omarchy](https://omarchy.org) the whole desktop follows the workbench
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

**Shepherdr in Herdr** (`mods/shepherdr`), for one or more [Shepherdr](https://github.com/eliasstravik/shepherdr) instances:
- A panel (crook button in the tab bar) and a full tab: spaces with folding of quiet ones, what needs you (including
  open questions), chats with answer buttons, a task board per space (needs you / working / ready / backlog with
  start estimates / done / archived), estimates from agents' own reports.
- **Operations Monitor**: machines as aligned meter cells (CPU, memory, disk, each GPU with its VRAM, Mac GPU), one
  animated node per running task (pixel robot, progress conveyor, ETA), queue, ticker.
- **Artifacts** in `herdr.doc/1` ([spec](herdr-app/shepherdr-doc/SPEC.md)): agents write JSON blocks, Herdr draws them
  natively, the stock `render.html` draws them on the web and phone.
- `bin/shepherdr-desktop-sync.py` shows a Shepherdr's agents as Herdr spaces and tabs.
- **Task workspaces** (`bin/task-workspaces.py`, a user service: `herdr-task-workspaces`): each started Shepherdr 3.x
  task becomes a real Herdr workspace on its worktree, under the space of the repo it is from (the same place the
  sidebar already puts worktrees). Its tabs are the task's Chat, its agent's live terminal (the one it runs in, no new
  one), Files, Changes and Pull request; clicking it in the sidebar opens its Chat. It goes when the task is done,
  archived or removed (two passes, so a flicker never closes a space). Workspaces made by hand are never touched; the
  service reads this computer's tasks only, and a task workspace follows its task, so renaming its rows by hand is
  undone. While the window is open each of these writes is a layout change it didn't make, so Herdr shows its own
  "Layout changed elsewhere" notice when a task appears, goes or is renamed (never for status or progress).
- Other machines' Shepherdrs: `~/.config/herdr-app/shepherdr-sources.json` (see the example file).

**Desk** (`mods/desk`, DESK button in the status bar): pomodoro timer with a pixel tomato, focus stats and a 14-day
chart, today's note with clickable checkboxes; a timer chip in the status bar.

**Pull requests** (`mods/px-pr`, PR in the tab bar): the PR of the branch you're on, or one picked in Reviews, in the
panel. Description, open threads by file with the code they're on, resolved ones folded, reviews and comments as one
timeline; react, reply, comment, approve or request changes; FILES shows every diff, click a line to draft a comment and
submit them as one review; CHECKS lists failing ones first. ASK sends a comment to the agent working on that branch
(found through Shepherdr 3.0 tasks), or to one agent in a "Chores" space for everything that isn't your own work.

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

**Shepherdr 3.x patches** (`herdr-app/shepherdr-patches`, applied by the installer when Shepherdr 3.0.0's or 3.1's mods
are there, to the mods and to Shepherdr's own `lib/` and `core/` sources so `shepherdr doctor` stays whole). With 3.x
installed, the px layouts use its tab bar (`tabs-tasks`, which runs its service, inbox and new spaces' Assistants), with
this repo's PR, Reviews, Docs, Time, Desk and Pair buttons patched in; its own `shepherdr` layout gets those panels too: its views follow the px layouts' look (square corners, the theme's status colours, capitals on workbench), and
its chat works with Herdr zoomed (measured sizes converted to the units widths are given in, so the column is centred
and the newest messages stay in view), shows the agent's reasoning as folded "Thinking" lines, no longer goes blank while a folded (Show more) reply
is on screen (a handler the list refuses), and recovers from a blank list without re-laying out every message.

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

**Voice computer** ([voice-computer/](voice-computer/)): a warm, talkative assistant for Herdr on local models through
llama.cpp: LFM3-Omni-2B brain, LFM3-Audio-2B ears and voice, EmbeddingGemma recall. Talk with `cmd-shift-m`.
Commands (focus agents, switch themes, show panels), briefings and "what needs me", reminders, pomodoro, notes,
web search (DeepSeek, or DuckDuckGo/Wikipedia), long-term memory, stateful conversation, delegation to Shepherdr
coordinators with the right notes attached, always asking before it sends anything.

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

Both: themes, layouts, mods, art, sounds, the document formats, the sync scripts (the user service `herdr-task-workspaces`
for task workspaces), herdr-doc, typing sounds and the time
tracker (macOS: Swift LaunchAgents in `macos/`, built with `swiftc`; allow herdr-keyclick under Input Monitoring and
herdr-timetrack under Accessibility; `cmd-alt-k` mutes the clicks). macOS only: the voice computer (`Herdr Voice.app`,
`afplay`/`rec`, `osascript`, Homebrew). Linux only: Omarchy theme. The
installer drops layout panels whose mods aren't installed (Shepherdr 3.0's tasks, chat and artifacts come with it).

## Licence

Apache-2.0 (the `px-*` mods are modified from Herdr's own mods). The voice computer is MIT (its own LICENSE). Fonts:
PxPlus IBM VGA 8x16 (CC BY-SA 4.0), Pixelated MS Sans Serif (CC BY-SA 3.0); see `herdr-app/fonts/` and NOTICE. Cherry MX
Blue key sounds: Mechvibes, MIT.
