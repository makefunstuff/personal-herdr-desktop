#!/bin/sh
# personal-herdr-desktop: install the themes, mods, Shepherdr panel and Desk into Herdr desktop; on macOS also the voice
# computer, on Linux the typing sounds, time tracker, herdr-doc, the Neovim theme and (with Omarchy) the workbench theme.
# Everything it replaces is backed up next to the original as <file>.bak.<time>. Re-run after pulling to update.
#
#   ./install.sh               everything for this system
#   ./install.sh --no-voice    macOS: without the voice computer
#   ./install.sh --timetrack   Linux: also start the time tracker (samples the focused window's title every 30 s, kept locally)
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"
SRC="$REPO/herdr-app"
DST="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
TS=$(date +%s)
OS=$(uname -s); VOICE=0; [ "$OS" = Darwin ] && VOICE=1; TIMETRACK=0
for a in "$@"; do case $a in --no-voice) VOICE=0 ;; --timetrack) TIMETRACK=1 ;; esac; done

[ -d "$DST" ] || { echo "No Herdr app folder at $DST: start Herdr once first."; exit 1; }
backup() { [ -e "$1" ] && cp -R "$1" "$1.bak.$TS" || true; }
put() { mkdir -p "$(dirname "$2")"; if [ -e "$2" ] && ! cmp -s "$1" "$2"; then backup "$2"; fi; cp "$1" "$2"; }

echo "== Herdr: themes, layouts, art, sounds, fonts, icons"
for f in $(cd "$SRC" && find themes layouts backgrounds pixel-icons workbench-icons fortress-icons sounds fonts shepherdr-doc bin -type f); do
  put "$SRC/$f" "$DST/$f"
done
for f in "$SRC"/programs-*.json; do put "$f" "$DST/$(basename "$f")"; done
chmod +x "$DST/bin/shepherdr-desktop-sync.py"
[ -f "$DST/shepherdr-sources.json" ] || cp "$SRC/shepherdr-sources.example.json" "$DST/shepherdr-sources.example.json"

echo "== Herdr: mods"
mkdir -p "$DST/mods"
for m in "$SRC"/mods/*; do
  name=$(basename "$m"); [ -d "$DST/mods/$name" ] && backup "$DST/mods/$name"
  rm -rf "$DST/mods/$name"; cp -R "$m" "$DST/mods/$name"
done

if [ -d "$DST/mods/chat" ] && command -v patch >/dev/null; then
  echo "== Shepherdr 3.0: square corners and theme colours in these layouts, chat fixes (zoom, reasoning shown)"
  P="$SRC/shepherdr-patches"
  for u in "$DST"/mods/*/ui.js; do
    patch -s -R --dry-run "$u" < "$P/ui.js.patch" >/dev/null 2>&1 && continue   # already in
    patch -s --dry-run "$u" < "$P/ui.js.patch" >/dev/null 2>&1 && { backup "$u"; patch -s "$u" < "$P/ui.js.patch"; } || echo "  $u: not Shepherdr 3.0.0's, left as is"
  done
  if ! (cd "$DST/mods" && patch -s -R -p1 --dry-run < "$P/chat.patch" >/dev/null 2>&1); then
    (cd "$DST/mods" && patch -s -p1 --dry-run < "$P/chat.patch" >/dev/null 2>&1) && { backup "$DST/mods/chat"; (cd "$DST/mods" && patch -s -p1 < "$P/chat.patch"); } \
      || echo "  mods/chat: not Shepherdr 3.0.0's, chat patch left out"
  fi
fi

echo "== Herdr: layouts keep only the panels whose mods are installed (Shepherdr 3.0's tasks, chat, artifacts...)"
python3 - "$DST" <<'PY'
import json, os, sys
home = sys.argv[1]
def strip(n):
    if isinstance(n, dict):
        if "children" in n:
            keep = [c for c in n["children"] if not (isinstance(c, dict) and (m := c.get("content", {}).get("mod")) and not os.path.isdir(f"{home}/mods/{m}"))]
            if len(keep) != len(n["children"]):
                n["children"] = keep
                if n.get("active") and n["active"] not in [c.get("id") for c in keep]: n["active"] = keep[0]["id"] if keep else None
        for v in n.values(): strip(v)
    elif isinstance(n, list):
        for v in n: strip(v)
for name in ("workbench", "fortress", "pixel"):
    p = f"{home}/layouts/{name}.json"
    if os.path.exists(p):
        d = json.load(open(p)); strip(d); json.dump(d, open(p, "w"), indent=2)
PY

if [ "$VOICE" = 1 ]; then
echo "== Herdr: voice keys (only where free)"
python3 - "$SRC/keys.example.json" "$DST/keys.json" "$TS" <<'PY'
import json, os, shutil, sys
want = json.load(open(sys.argv[1]))["global"]; path = sys.argv[2]
cur = json.load(open(path)) if os.path.exists(path) else {"global": {}}
g = cur.setdefault("global", {})
added = {k: v for k, v in want.items() if k not in g}
taken = [k for k in want if k in g and g[k] != want[k]]
if added:
    if os.path.exists(path): shutil.copy(path, f"{path}.bak.{sys.argv[3]}")
    g.update(added); json.dump(cur, open(path, "w"), indent=2)
print("added:", added or "nothing", "| already bound to something else:", taken or "none")
PY
fi

if [ "$VOICE" = 1 ]; then
  echo "== Voice computer"
  command -v go >/dev/null || { echo "Go is needed for the voice computer (brew install go), or run with --no-voice."; exit 1; }
  for t in llama-server llama-tts rec afplay; do command -v $t >/dev/null || echo "  missing: $t (brew install llama.cpp sox)"; done
  (cd "$REPO/voice-computer" && mkdir -p "$HOME/.local/bin" && go build -o "$HOME/.local/bin/vc" ./cmd/vc && sh install/app.sh)
  echo "  Models download on first start (LFM3-Omni-2B, LFM3-Audio-2B, EmbeddingGemma: about 3.5 GB)."
fi

if [ "$OS" = Linux ]; then
  L="$REPO/linux"; O="$REPO/omarchy"
  echo "== Linux: herdr-doc, time tracker, typing sounds (Cherry MX Blue)"
  for f in herdr-doc herdr-timetrack herdr-keyclick herdr-typing-toggle; do put "$L/bin/$f" "$HOME/.local/bin/$f"; chmod +x "$HOME/.local/bin/$f"; done
  mkdir -p "$HOME/.local/share/herdr-typing/mxblue"; cp "$L"/typing/mxblue/*.wav "$HOME/.local/share/herdr-typing/mxblue/"
  for u in herdr-keyclick herdr-timetrack; do put "$L/systemd/$u.service" "$HOME/.config/systemd/user/$u.service"; done
  if [ -f "$HOME/.config/hypr/hyprland.lua" ]; then
    put "$L/hypr/herdr-typing.lua" "$HOME/.config/hypr/herdr-typing.lua"
    grep -q 'require("hypr.herdr-typing")' "$HOME/.config/hypr/hyprland.lua" || { backup "$HOME/.config/hypr/hyprland.lua"; echo 'require("hypr.herdr-typing")' >> "$HOME/.config/hypr/hyprland.lua"; }
  fi
  systemctl --user daemon-reload
  systemctl --user enable --now herdr-keyclick.service
  [ "$TIMETRACK" = 1 ] && systemctl --user enable --now herdr-timetrack.service
  if [ -d "$HOME/.config/nvim" ]; then
    echo "== Neovim: the herdr colorscheme (follows Herdr's layout) and workbench"
    for f in colors/herdr.lua colors/workbench.lua lua/herdr_theme.lua lua/config/herdr_desktop.lua; do put "$L/nvim/$f" "$HOME/.config/nvim/$f"; done
    grep -rqs "config.herdr_desktop" "$HOME/.config/nvim/lua" "$HOME/.config/nvim/init.lua" || echo "  herdr-nvim on Herdr Desktop: call require('config.herdr_desktop').setup() after require('herdr-nvim').setup() (see README)"
  fi
  [ -d "$HOME/.claude" ] && put "$L/skills/herdr-doc/SKILL.md" "$HOME/.claude/skills/herdr-doc/SKILL.md"
  if command -v omarchy >/dev/null; then
    echo "== Omarchy: the workbench theme, icons and cursors, lock screen, screensaver, glyphs, sounds"
    mkdir -p "$HOME/.config/omarchy/themes" "$HOME/.local/share/icons"
    backup "$HOME/.config/omarchy/themes/workbench"; rm -rf "$HOME/.config/omarchy/themes/workbench"; cp -R "$O/themes/workbench" "$HOME/.config/omarchy/themes/"
    rm -rf "$HOME/.local/share/icons/Workbench"; cp -R "$O/icons/Workbench" "$HOME/.local/share/icons/"
    gtk-update-icon-cache -q "$HOME/.local/share/icons/Workbench" 2>/dev/null || true
    put "$O/fonts/WorkbenchGlyphs.ttf" "$HOME/.local/share/fonts/WorkbenchGlyphs.ttf"; fc-cache -f "$HOME/.local/share/fonts" >/dev/null
    W="$HOME/.local/share/workbench"
    put "$O/fonts/60-workbench-glyphs.conf" "$W/60-workbench-glyphs.conf"; put "$O/share/term-font" "$W/term-font"; chmod +x "$W/term-font"
    mkdir -p "$W/sounds"; cp "$O"/share/sounds/*.wav "$W/sounds/"
    put "$O/bin/workbench-screensaver" "$HOME/.local/bin/workbench-screensaver"; chmod +x "$HOME/.local/bin/workbench-screensaver"
    put "$O/hooks/theme-set.d/look-follows-theme" "$HOME/.config/omarchy/hooks/theme-set.d/look-follows-theme"
    chmod +x "$HOME/.config/omarchy/hooks/theme-set.d/look-follows-theme"
    lock=$(find "$HOME/.config/omarchy/plugins" -maxdepth 1 -name '*.lock' -type d 2>/dev/null | head -1)
    if [ -z "$lock" ]; then omarchy plugin clone omarchy.lock >/dev/null; lock=$(find "$HOME/.config/omarchy/plugins" -maxdepth 1 -name '*.lock' -type d | head -1); fi
    [ -n "$lock" ] && put "$O/lock/LockView.workbench.qml" "$lock/LockView.workbench.qml"
  fi
fi

cat <<'EOF'

Done. Next:
  - Switch layouts with ctrl-shift-l: workbench, fortress, pixel.
  - Omarchy: `omarchy theme set workbench` (lock screen, cursors, menu and terminal fonts follow the theme).
  - Typing sounds: SUPER+ALT+K mutes; HERDR_KEYS_CUTOFF=<Hz> in the service softens or brightens them.
  - Shepherdr on another machine: copy shepherdr-sources.example.json to shepherdr-sources.json and edit it.
EOF
[ "$VOICE" = 1 ] && cat <<'EOF'
  - Voice: `vc start`, then cmd-shift-m to talk. macOS asks once whether "Herdr Voice" may use the microphone: Allow.
    Web search through DeepSeek: set "search": {"deepseek_key_file": "<path>"} in ~/.config/herdr-voice-computer/config.json.
EOF
exit 0
