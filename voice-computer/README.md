# Herdr Voice Computer

A voice secretary for the [Herdr](https://herdr.dev) desktop app on macOS, running entirely on local
LFM3 models through llama.cpp. Press a key, talk, and it moves Herdr for you, tells you what your
agents are doing and what waits on you, takes notes, sets reminders and, after you confirm, passes
your words to an agent. It is a port of
[omarchy-voice-computer](https://github.com/makefunstuff/omarchy-voice-computer) (MIT): the same vcd,
turns, confirms, memory and personality, with Herdr in place of Hyprland.

- **Voice commands first.** A fixed grammar answers without any model in about 0.3 s from release
  (speech recognition included): "go to homelab", "focus the semantic grep agent", "next agent",
  "new tab in home with claude", "theme fortress", "show / hide the Shepherdr panel", "what needs me",
  "brief me", "what is homelab doing", "read it", "note: …", "remind me in 20 minutes to …",
  "list reminders", "cancel the reminder", "tell homelab to …", "yes" / "no", "stop", "quiet",
  "talk to me", volume and media. Names are matched loosely against Herdr's spaces and agents
  ("shepherder" is Shepherdr, "semantic rep" is semantic-grep-nvim).
- **A secretary for the rest.** Anything else goes to LFM3-1.7B with the same tools. It answers in one
  short plain sentence and never from its own knowledge: only from the context line (time, layout,
  focused space and agent, agent counts, quiet, reminders) and its tools' results.
- **Confirms.** Sending words to an agent is always asked first, out loud: say yes or no, press the
  confirm key, or use the voice window. A new tab in a space on another machine is asked too. Nothing
  is ever closed by voice.
- **Briefings and announcements.** vcd watches Herdr's agents and Shepherdr: "brief me" says what
  finished, what waits on you (and what it asks) and what still runs since the last briefing. With
  announcements on ("talk to me"; quiet is the default until you say it once), it says one line when
  an agent finishes or starts waiting, never over you or its own answers.
- **Memory.** The last `history` turns (20) stay in RAM only, show in the voice window and are replayed
  into a new session. Only notes you ask for are written down (`~/.local/share/herdr-voice-computer/notes.md`).
  Reminders live in memory. The log has timings, never words.

## Parts

- `vc` (`cmd/vc`): the CLI, and `vc daemon` is vcd. vcd supervises two `llama-server`s on loopback:
  the brain (`LiquidAI/LFM3-1.7B-GGUF` Q4_K_M, `--reasoning off`, port 8790) and the ears and mouth
  (`LiquidAI/LFM3-Audio-2B-GGUF` Q4_K_M with its mmproj, port 8791: `/v1/audio/transcriptions` and
  `/tts` streaming WAV). Every llama process gets `GGML_METAL_BF16_DISABLE=1`.
- `Herdr Voice.app` (`cmd/herdr-voice`, built by `install/app.sh` into `~/Applications`): a tiny
  launcher bundle with `NSMicrophoneUsageDescription`, so macOS asks for the microphone once for
  "Herdr Voice". `vc start` launches vcd through it.
- The Herdr mod `voice` (`herdr/mods/voice`) and a mic button in `px-tabs` (`herdr/install.py`).

## Install

```sh
go build -o ~/.local/bin/vc ./cmd/vc
install/app.sh                 # Herdr Voice.app (the microphone's owner)
python3 herdr/install.py       # the mod, the mic button, the keys, the voice pill (backs up every file)
vc start --wait
```

The first time you talk, macOS asks "Herdr Voice would like to access the microphone": click **Allow**.
If you missed it: System Settings → Privacy & Security → Microphone → turn on **Herdr Voice**, then
`vc restart`. `vc mic-check` records 1.5 s and says whether the mic gives sound.

## Use

| | |
|---|---|
| `cmd-shift-m` / click the mic in the tab bar | talk; press again to send |
| `cmd-shift-y` | yes to the open confirm |
| right-click the mic | the voice window: conversation, inbox, reminders, quiet switch |
| `vc start` / `vc stop` / `vc restart` / `vc status` | run it (no login items: the Herdr mod starts it when it loads) |
| `vc turn --say "brief me"` | a turn from macOS `say` (or `--wav F`, `--text T`), with its timings |
| `vc quiet on\|off`, `vc mute`, `vc latency` | |

## License

MIT, see [LICENSE](LICENSE).
