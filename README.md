# personal-herdr-desktop

My customizations for the [Herdr](https://herdr.dev) desktop app on macOS: three themed layouts with their own art,
icons and sounds, Shepherdr inside Herdr (panel, task board, operations monitor, native artifacts), a Desk with a
pomodoro timer and notes, and a local voice assistant running on LFM3 models.

Mac only for now. The parts that are macOS-specific are listed under [Porting](#porting) for a later Linux port.

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
- Other machines' Shepherdrs: `~/.config/herdr-app/shepherdr-sources.json` (see the example file).

**Desk** (`mods/desk`, DESK button in the status bar): pomodoro timer with a pixel tomato, focus stats and a 14-day
chart, today's note with clickable checkboxes; a timer chip in the status bar.

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

Requirements: Herdr desktop; for voice also Go, `brew install llama.cpp sox`. On macOS 15 with Homebrew's llama.cpp,
the voice computer sets `GGML_METAL_BF16_DISABLE=1` so Metal works.

The Windows 3.1 layout lives in [omarchy-herdr-win31](https://github.com/makefunstuff/omarchy-herdr-win31); its
Microsoft assets are fetched on your machine, never stored in a repo.

## Porting

Portable as is: themes, layouts, mods, art, sounds, the artifact format, the sync script, the voice computer's core.
macOS-specific: `Herdr Voice.app` (microphone permission), `afplay`/`rec`, `osascript` media keys, `ioreg` GPU stats in
the Monitor, Homebrew paths.

## Licence

Apache-2.0 (the `px-*` mods are modified from Herdr's own mods). The voice computer is MIT (its own LICENSE). Fonts:
PxPlus IBM VGA 8x16 (CC BY-SA 4.0), Pixelated MS Sans Serif (CC BY-SA 3.0); see `herdr-app/fonts/` and NOTICE.
