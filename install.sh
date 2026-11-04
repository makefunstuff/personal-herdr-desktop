#!/bin/sh
# personal-herdr-desktop: install the themes, mods, Shepherdr panel, Desk and voice computer into Herdr desktop (macOS).
# Everything it replaces is backed up next to the original as <file>.bak.<time>. Re-run after pulling to update.
#
#   ./install.sh            Herdr pieces + the voice computer
#   ./install.sh --no-voice Herdr pieces only
set -e
REPO="$(cd "$(dirname "$0")" && pwd)"
SRC="$REPO/herdr-app"
DST="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
TS=$(date +%s)
VOICE=1; [ "$1" = "--no-voice" ] && VOICE=0

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

if [ "$VOICE" = 1 ]; then
  echo "== Voice computer"
  command -v go >/dev/null || { echo "Go is needed for the voice computer (brew install go), or run with --no-voice."; exit 1; }
  for t in llama-server llama-tts rec afplay; do command -v $t >/dev/null || echo "  missing: $t (brew install llama.cpp sox)"; done
  (cd "$REPO/voice-computer" && mkdir -p "$HOME/.local/bin" && go build -o "$HOME/.local/bin/vc" ./cmd/vc && sh install/app.sh)
  echo "  Models download on first start (LFM3-Omni-2B, LFM3-Audio-2B, EmbeddingGemma: about 3.5 GB)."
fi

cat <<'EOF'

Done. Next:
  - Switch layouts with ctrl-shift-l: workbench, fortress, pixel.
  - Shepherdr on another machine: copy shepherdr-sources.example.json to shepherdr-sources.json and edit it.
  - Voice: `vc start`, then cmd-shift-m to talk. macOS asks once whether "Herdr Voice" may use the microphone: Allow.
    Web search through DeepSeek: set "search": {"deepseek_key_file": "<path>"} in ~/.config/herdr-voice-computer/config.json.
EOF
