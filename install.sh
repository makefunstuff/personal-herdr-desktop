#!/bin/sh
# personal-herdr-desktop: install the themes, mods and Desk into Herdr desktop, herdr-doc, the Neovim
# theme, typing sounds and the time tracker; on macOS also dictation (Option+L, the voice computer in dictation-only mode), on Linux (with Omarchy) the workbench theme.
# Everything it replaces is backed up next to the original as <file>.bak.<time>. Re-run after pulling to update.
#
#   ./install.sh               everything for this system
#   ./install.sh --no-voice    macOS: without dictation
#   ./install.sh --timetrack   also start the time tracker (samples the focused window's title every 30 s, kept locally)
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"
SRC="$REPO/herdr-app"
DST="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
TS=$(date +%s)
OS=$(uname -s); VOICE=0; [ "$OS" = Darwin ] && VOICE=1; TIMETRACK=0
for a in "$@"; do case $a in --no-voice) VOICE=0 ;; --timetrack) TIMETRACK=1 ;; esac; done

[ -d "$DST" ] || { echo "No Herdr app folder at $DST: start Herdr once first."; exit 1; }
# Backups sit next to the original as <file>.bak.<time>, except in mods/, lib/ and core/ (a folder in mods/ is loaded
# as a mod; lib/ and core/ are Shepherdr's): those go to backups/<time>/, by the same path.
backup() {
  [ -e "$1" ] || return 0
  case $1 in
    # Into its folder there, never over what this run already kept (the first copy is the one from before the run).
    "$DST"/mods/*|"$DST"/lib/*|"$DST"/core/*) r=${1#"$DST"/}; mkdir -p "$DST/backups/$TS/$(dirname "$r")"; cp -Rn "$1" "$DST/backups/$TS/$(dirname "$r")/" ;;
    *) cp -R "$1" "$1.bak.$TS" ;;
  esac
}
put() { mkdir -p "$(dirname "$2")"; if [ -e "$2" ] && ! cmp -s "$1" "$2"; then backup "$2"; fi; cp "$1" "$2"; }

echo "== Herdr: themes, layouts, art, sounds, fonts, icons"
for f in $(cd "$SRC" && find themes layouts backgrounds pixel-icons workbench-icons fortress-icons sounds fonts -type f); do
  put "$SRC/$f" "$DST/$f"
done
for f in "$SRC"/programs-*.json; do put "$f" "$DST/$(basename "$f")"; done

echo "== Herdr: mods"
mkdir -p "$DST/mods"
for m in "$SRC"/mods/*; do
  name=$(basename "$m"); [ -d "$DST/mods/$name" ] && backup "$DST/mods/$name"
  rm -rf "$DST/mods/$name"; cp -R "$m" "$DST/mods/$name"
done

echo "== Herdr: layouts keep only the panels whose mods are installed"
python3 - "$DST" "$SRC" "$TS" <<'PY'
import copy, json, os, shutil, sys
home, src, ts = sys.argv[1:4]
missing = lambda c: isinstance(c, dict) and (m := (c.get("content") or {}).get("mod")) and not os.path.isdir(f"{home}/mods/{m}")
def strip(n):
    if isinstance(n, dict):
        # Blocks in a split or stack (children) and floating ones (floating) alike.
        for key in ("children", "floating"):
            if key in n:
                keep = [c for c in n[key] if not missing(c)]
                if len(keep) != len(n[key]):
                    n[key] = keep
                    if key == "children" and n.get("active") and n["active"] not in [c.get("id") for c in keep]: n["active"] = keep[0]["id"] if keep else None
        for v in n.values(): strip(v)
    elif isinstance(n, list):
        for v in n: strip(v)
def find(n, id):
    if isinstance(n, dict):
        if n.get("id") == id: return n
        for key in ("children", "floating"):
            for c in n.get(key, []):
                if (f := find(c, id)): return f
    return None
# The tab bar is this repo's px-tabs (Shepherdr's tabs-tasks once stood in for it: back to px-tabs when it's gone),
# and panels whose mods are all gone (Shepherdr's Tasks and Artifacts) go with them.
def drop(n, ids):
    if isinstance(n, dict):
        for key in ("children", "floating"):
            if key in n:
                n[key] = [c for c in n[key] if not (isinstance(c, dict) and c.get("id") in ids)]
                if key == "children" and n.get("active") in ids: n["active"] = n[key][0]["id"] if n[key] else None
                for c in n[key]: drop(c, ids)
for name in ("workbench", "fortress", "pixel"):
    p = f"{home}/layouts/{name}.json"
    if os.path.exists(p):
        d = json.load(open(p))
        tabs = find(d, "tabs")
        if tabs and (tabs.get("content") or {}).get("mod") == "tabs-tasks" and not os.path.isdir(f"{home}/mods/tabs-tasks"): tabs["content"]["mod"] = "px-tabs"
        strip(d)
        drop(d, {i for i, m in (("tasks", "tasks"), ("artifacts", "artifacts")) if not os.path.isdir(f"{home}/mods/{m}")})
        json.dump(d, open(p, "w"), indent=2)
PY

if [ "$VOICE" = 1 ]; then
echo "== Herdr: the dictation key (only where free)"
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
  (cd "$REPO/voice-computer" && mkdir -p "$HOME/.local/bin" && go build -o "$HOME/.local/bin/vc" ./cmd/vc)
  # "Herdr Voice.app" only when cmd/herdr-voice changed: a new ad-hoc build loses the microphone permission.
  # An app already there with no record is taken as current.
  vsum=$(cd "$REPO/voice-computer" && find cmd/herdr-voice -type f | sort | xargs shasum -a 256 | shasum -a 256 | cut -c1-64)
  vstamp="$HOME/.local/share/personal-herdr-desktop/herdr-voice.sha256"; mkdir -p "$(dirname "$vstamp")"
  if [ -d "$HOME/Applications/Herdr Voice.app" ] && [ ! -f "$vstamp" ]; then echo "$vsum" > "$vstamp"; fi
  [ "$(cat "$vstamp" 2>/dev/null)" = "$vsum" ] || { (cd "$REPO/voice-computer" && sh install/app.sh) && echo "$vsum" > "$vstamp"; }
  # Dictation only: Option+L writes speech down into an agent's chat or terminal (LFM3-Audio on this Mac). The voice
  # assistant (talking, brain, announcements, memory) stays off: dictation_only in its config, no mic button or talk keys.
  python3 "$REPO/voice-computer/herdr/install.py" --dictation-only
  python3 - "$HOME/.config/herdr-voice-computer/config.json" <<'PY3'
import json, os, sys
p = sys.argv[1]; os.makedirs(os.path.dirname(p), exist_ok=True)
c = json.load(open(p)) if os.path.exists(p) else {}
if "dictation_only" not in c:
    c["dictation_only"] = True; json.dump(c, open(p, "w"), indent=2); print("  dictation only (dictation_only in", p + ")")
PY3
  echo "  The speech model downloads on first start (LFM3-Audio-2B, about 1.5 GB)."
fi

L="$REPO/linux"; M="$REPO/macos"; O="$REPO/omarchy"
echo "== herdr-doc and its Claude Code skill"
put "$L/bin/herdr-doc" "$HOME/.local/bin/herdr-doc"; chmod +x "$HOME/.local/bin/herdr-doc"
[ -d "$HOME/.claude" ] && put "$L/skills/herdr-doc/SKILL.md" "$HOME/.claude/skills/herdr-doc/SKILL.md"
if [ -d "$HOME/.config/nvim" ]; then
  echo "== Neovim: the herdr colorscheme (follows Herdr's layout) and workbench (your colorscheme stays as it is)"
  for f in colors/herdr.lua colors/workbench.lua lua/herdr_theme.lua lua/config/herdr_desktop.lua; do put "$L/nvim/$f" "$HOME/.config/nvim/$f"; done
  grep -rqs "config.herdr_desktop" "$HOME/.config/nvim/lua" "$HOME/.config/nvim/init.lua" || echo "  herdr-nvim on Herdr Desktop: call require('config.herdr_desktop').setup() after require('herdr-nvim').setup() (see README)"
fi
mkdir -p "$HOME/.local/share/herdr-typing/mxblue"; cp "$L"/typing/mxblue/*.wav "$L"/typing/mxblue/*.txt "$HOME/.local/share/herdr-typing/mxblue/"

if [ "$OS" = Darwin ]; then
  echo "== macOS: typing sounds (Cherry MX Blue) and the time tracker, as LaunchAgents"
  put "$M/bin/herdr-typing-toggle" "$HOME/.local/bin/herdr-typing-toggle"; chmod +x "$HOME/.local/bin/herdr-typing-toggle"
  # Built again only when their source changed: macOS keeps a permission (Input Monitoring, Accessibility) for
  # this exact build, and a new one has to be allowed again.
  build() {
    sum=$(shasum -a 256 "$M/src/$1.swift" | cut -c1-64); stamp="$HOME/.local/share/personal-herdr-desktop/$1.sha256"
    [ -x "$HOME/.local/bin/$1" ] && [ "$(cat "$stamp" 2>/dev/null)" = "$sum" ] && return 0
    command -v swiftc >/dev/null || { echo "  $1: swiftc is needed (xcode-select --install)"; return 1; }
    mkdir -p "$(dirname "$stamp")"
    swiftc -O -o "$HOME/.local/bin/$1.new" "$M/src/$1.swift" && mv "$HOME/.local/bin/$1.new" "$HOME/.local/bin/$1" && echo "$sum" > "$stamp" && echo "  built $1"
  }
  agent() {
    plist="$HOME/Library/LaunchAgents/dev.personal-herdr-desktop.$1.plist"; mkdir -p "$HOME/Library/LaunchAgents" "$HOME/Library/Logs"
    sed -e "s#NAME#$1#g" -e "s#HOME#$HOME#g" "$M/launchd/agent.plist" > "$plist.new"
    if cmp -s "$plist.new" "$plist" && launchctl print "gui/$(id -u)/dev.personal-herdr-desktop.$1" >/dev/null 2>&1; then
      rm "$plist.new"; launchctl kickstart -k "gui/$(id -u)/dev.personal-herdr-desktop.$1"; return 0
    fi
    mv "$plist.new" "$plist"; launchctl bootout "gui/$(id -u)/dev.personal-herdr-desktop.$1" 2>/dev/null || true
    for i in 1 2 3 4 5; do launchctl bootstrap "gui/$(id -u)" "$plist" 2>/dev/null && { echo "  $1: running (LaunchAgent)"; return 0; }; sleep 1; done
    echo "  $1: launchctl bootstrap failed"
  }
  build herdr-keyclick && agent herdr-keyclick
  build herdr-timetrack && [ "$TIMETRACK" = 1 ] && agent herdr-timetrack
  # cmd-alt-k mutes the typing sounds (px-tabs runs herdr-typing-toggle), where it's free.
  python3 - "$DST/keys.json" "$TS" <<'PY2'
import json, os, shutil, sys
p = sys.argv[1]; cur = json.load(open(p)) if os.path.exists(p) else {}
g = cur.get("global", {})
taken = {k for v in cur.values() if isinstance(v, dict) for k in v}
if "cmd-alt-k" not in taken or g.get("cmd-alt-k") == "tabs-tasks.typing-toggle":
    if os.path.exists(p): shutil.copy(p, f"{p}.bak.{sys.argv[2]}")
    cur.setdefault("global", {})["cmd-alt-k"] = "px-tabs.typing-toggle"; json.dump(cur, open(p, "w"), indent=2)
    print("  cmd-alt-k: typing sounds on and off")
elif g.get("cmd-alt-k") != "px-tabs.typing-toggle": print("  cmd-alt-k is taken: run herdr-typing-toggle to mute")
PY2
fi

if [ "$OS" = Linux ]; then
  echo "== Linux: time tracker, typing sounds (Cherry MX Blue)"
  for f in herdr-timetrack herdr-keyclick herdr-typing-toggle; do put "$L/bin/$f" "$HOME/.local/bin/$f"; chmod +x "$HOME/.local/bin/$f"; done
  for u in herdr-keyclick herdr-timetrack; do put "$L/systemd/$u.service" "$HOME/.config/systemd/user/$u.service"; done
  if [ -f "$HOME/.config/hypr/hyprland.lua" ]; then
    put "$L/hypr/herdr-typing.lua" "$HOME/.config/hypr/herdr-typing.lua"
    grep -q 'require("hypr.herdr-typing")' "$HOME/.config/hypr/hyprland.lua" || { backup "$HOME/.config/hypr/hyprland.lua"; echo 'require("hypr.herdr-typing")' >> "$HOME/.config/hypr/hyprland.lua"; }
  fi
  systemctl --user daemon-reload
  systemctl --user enable --now herdr-keyclick.service
  [ "$TIMETRACK" = 1 ] && systemctl --user enable --now herdr-timetrack.service
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
  - Typing sounds: SUPER+ALT+K (Linux) or cmd-alt-k in Herdr (macOS) mutes; HERDR_KEYS_CUTOFF=<Hz> softens or brightens them.
  - macOS: allow herdr-keyclick under Input Monitoring and herdr-timetrack under Accessibility (System Settings > Privacy & Security).
EOF
[ "$VOICE" = 1 ] && cat <<'EOF'
  - Dictation: `vc start`, then Option+L in an agent's chat or terminal (again to finish). macOS asks once whether "Herdr Voice" may use the microphone: Allow.
    Web search through DeepSeek: set "search": {"deepseek_key_file": "<path>"} in ~/.config/herdr-voice-computer/config.json.
EOF
exit 0
