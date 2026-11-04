# Installing and running

Requirements (Apple Silicon, macOS 13+): Herdr, Go 1.27, Homebrew `llama.cpp` (`llama-server`),
`sox` (`rec`, `play`), and the two LFM3 models in the Hugging Face cache:
`LiquidAI/LFM3-1.7B-GGUF` (`LFM3-1.7B-Q4_K_M.gguf`) and `LiquidAI/LFM3-Audio-2B-GGUF`
(`LFM3-Audio-2B-Q4_K_M.gguf`, `mmproj-LFM3-Audio-2B-Q4_0.gguf`).

1. `go build -o ~/.local/bin/vc ./cmd/vc`
2. `install/app.sh`: builds `~/Applications/Herdr Voice.app` (ad-hoc signed, no Dock icon). It only
   starts `vc daemon`; it exists so macOS has an app to ask for the microphone. Processes Herdr
   starts can't get the mic: Herdr.app declares no microphone use, so macOS hands them silence and never asks.
3. `python3 herdr/install.py`: copies the `voice` mod, adds the mic glyph (`g/`, `gw/`, `gf/`) and a
   marked `// >>> voice … // <<< voice` block to `mods/px-tabs/main.js`, binds `cmd-shift-m` (talk) and
   `cmd-shift-y` (confirm) in `keys.json` when free, and adds the `voice` floating block to the
   workbench, pixel, fortress and win31 layouts. Each changed file is backed up first as
   `FILE.bak.voice-computer.<time>`. `--uninstall` takes it all out.
4. `vc start --wait`, then talk. Allow **Herdr Voice** when macOS asks for the microphone.

There is no login item: the voice mod runs `vc start` when it loads and finds vcd down. `vc stop`
stops vcd and both llama-servers. Only one vcd runs (a lock in the state folder).

## Configuration

`~/.config/herdr-voice-computer/config.json`, every key optional (see `internal/config/config.go`):
`name` (the secretary's, "Herdr"), `voice` (female/male), `history` (20), `max_sentences` (2),
`brain_server` / `audio_server` (`model`, `mmproj`, `port`, `args`), `stt` / `chat` / `tts` endpoints,
`notes_file`, `layouts`, `announce_gap_s`, `http_port`, and `shepherdrs`. This Mac's Shepherdr is read
on loopback. Another machine's is read only, like Herdr's shepherdr mod reads it (curl run there
through `hc exec --on`). It is off until you add it:

```json
{ "shepherdrs": [
    {"name": "this Mac", "url": "http://127.0.0.1:7787", "herdr_machine": "mac-shepherdr"},
    {"name": "gpubox", "url": "http://127.0.0.1:7787", "machine": "gpubox-shepherdr", "herdr_machine": "gpubox-shepherdr"} ] }
```

## Files

`~/.local/state/herdr-voice-computer/`: `vc.sock`, `token`, `vcd.pid`, `vcd.lock`, `vcd.log` (timings
only), `brain.log`, `audio.log`, `prefs.json` (quiet on or off). `~/.local/share/herdr-voice-computer/notes.md`:
your notes.
