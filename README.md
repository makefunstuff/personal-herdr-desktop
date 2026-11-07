# personal-herdr-desktop

My customizations for the [Herdr](https://herdr.dev) desktop app on macOS and Linux: three themed layouts with their own
art, icons and sounds, pull requests and
reviews, documents Herdr draws natively, a time tracker, a Desk with a pomodoro timer and notes, and a voice
assistant with local LFM3 speech (macOS). On Linux with [Omarchy](https://omarchy.org) the whole desktop follows the workbench
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

**Voice computer** ([voice-computer/](voice-computer/)): a warm, talkative, slightly cheeky assistant for Herdr.
LFM3-Audio-2B hears and speaks on this Mac (llama.cpp), the thinking is a configurable brain (DeepSeek's reasoner here,
with the local LFM3-Omni-2B as a backup that loads only when needed), EmbeddingGemma recalls notes and memory.
Talk with `cmd-shift-m`; dictate into any agent's chat or terminal with `Option+L`. Commands (focus agents, switch
themes, show panels), "what needs me", reminders, pomodoro, Desk notes, web search, websites read with agent-browser,
Mac apps read, opened and worked (through Claude Code with agent-desktop, always confirmed), messages to agents (always
confirmed). It learns lasting facts and your preferences from talking (`memory.md`) and grows a persona from your
conversations (`persona.md`), both plain files you can edit.

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

Both: themes, layouts, mods, art, sounds, the document formats, herdr-doc, typing sounds and the time
tracker (macOS: Swift LaunchAgents in `macos/`, built with `swiftc`; allow herdr-keyclick under Input Monitoring and
herdr-timetrack under Accessibility; `cmd-alt-k` mutes the clicks). macOS only: the voice computer (`Herdr Voice.app`,
`afplay`/`rec`, `osascript`, Homebrew). Linux only: Omarchy theme. The
installer drops layout panels whose mods aren't installed.

## Licence

Apache-2.0 (the `px-*` mods are modified from Herdr's own mods). The voice computer is MIT (its own LICENSE). Fonts:
PxPlus IBM VGA 8x16 (CC BY-SA 4.0), Pixelated MS Sans Serif (CC BY-SA 3.0); see `herdr-app/fonts/` and NOTICE. Cherry MX
Blue key sounds: Mechvibes, MIT.
