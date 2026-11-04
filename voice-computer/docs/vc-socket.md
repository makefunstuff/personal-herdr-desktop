# How `vc` and the Herdr mod talk to vcd

vcd listens on `~/.local/state/herdr-voice-computer/vc.sock` (mode 600): one JSON line per request,
JSON lines back. The Herdr mod uses HTTP on `127.0.0.1:8792` instead (mods can't open sockets):
`GET /state?after=<version>` (a long poll, up to 25 s, answers the state once its `version` is newer)
and `POST /cmd {"cmd", "arg"}`, each with the header `X-VC-Token: <~/.local/state/herdr-voice-computer/token>`
(mode 600, new on every start).

| `vc` command | request |
|---|---|
| `vc talk` | `{"cmd": "talk"}`: opens the mic, or sends what it heard |
| `vc ptt start\|stop` | `{"cmd": "ptt", "arg": "start"}` |
| `vc turn --wav F` / `--text T` | `{"cmd": "turn", "arg": "/path.wav"}` / `{"cmd": "text", "arg": "words"}` |
| `vc cancel`, `vc hide`, `vc undo`, `vc words` | `{"cmd": "cancel"}` … |
| `vc confirm [<card>]`, `vc decline [<card>]` | `{"cmd": "confirm"}` |
| `vc quiet on\|off\|toggle` | `{"cmd": "quiet", "arg": "on"}`: announcements off (on) |
| `vc mute on\|off\|toggle` | the computer's speech |
| `vc say <text>` | speak a line |
| `vc status`, `vc watch` | the state (watch: then every change, and `level` lines) |

The state line is the original's (`state`, `off`, `why`, `heard`, `answer`, `note`, `turns`, `done`,
`card`, `choices`, `muted`, `replies_muted` = quiet) plus `name`, `inbox` (what waits on you),
`reminders`, `toast` (`seq` grows; the mod posts it in Herdr), `last` (the last turn's timings, never
words) and `version`. `off` may be `mic`: macOS gives the microphone's silence (`why` says what to allow).
