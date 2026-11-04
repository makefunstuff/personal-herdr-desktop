#!/usr/bin/env python3
"""Puts the voice computer into Herdr: the voice mod, the mic glyphs and button in px-tabs (a marked
block), the keys (only when free) and the voice pill as a floating block in the px layouts. Every file
it changes is backed up beside itself first as FILE.bak.voice-computer.<time>. Run it again to update;
--uninstall takes it all out again (the backups stay)."""
import json, os, shutil, sys, time

HOME = os.path.expanduser("~/.config/herdr-app")
HERE = os.path.dirname(os.path.abspath(__file__))
STAMP = str(int(time.time()))
KEYS = {"cmd-shift-m": "voice.talk", "cmd-shift-y": "voice.confirm"}
LAYOUTS = ["workbench", "pixel", "fortress", "win31"]
FLOAT = {"id": "voice", "content": {"mod": "voice"}, "edge": "bottom", "align": "end"}
BEGIN, END = "// >>> voice (herdr-voice-computer)", "// <<< voice"

HELPER = BEGIN + """: the mic button. Click: talk (again: send); right-click: the voice window.
const voiceMic = (t) => {
  const v = shared("voice") ?? { state: "off" };
  const c = ({ off: t.dim, ready: v.quiet ? t.dim : t.muted, listening: t.accent, thinking: t.working, speaking: t.done, confirm: t.blocked })[v.state] ?? t.dim;
  return div().id("voice-mic").size(26).flex_none().flex().items_center().justify_center().cursor_pointer().relative()
    .text_color(c).when(v.state === "listening" || v.state === "confirm", (e) => e.bg(t.selected)).hover((s) => s.bg(t.hover))
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).on_click(() => run("voice.talk"))
    .on_mouse_down("right", (_e, cx) => { cx.stop_propagation(); run("voice.overlay"); })
    .child(svg(`mods/px-tabs/${GS()}/mic.svg`).size(15))
    .when(v.inbox > 0, (e) => e.child(div().absolute().top(3).right(3).size(5).bg(t.blocked)));
};
""" + END + "\n"
CHILD = "      " + BEGIN + "\n      .child(voiceMic(t))\n      " + END + "\n"

def backup(path):
    if os.path.exists(path):
        shutil.copy2(path, f"{path}.bak.voice-computer.{STAMP}")

def write(path, text):
    old = open(path).read() if os.path.exists(path) else None
    if old != text:
        backup(path)
        with open(path, "w") as f:
            f.write(text)
        print("changed", path)

def strip_blocks(src):
    out, skip = [], False
    for line in src.splitlines(keepends=True):
        if line.strip().startswith(BEGIN):
            skip = True
        if not skip:
            out.append(line)
        if skip and line.strip().startswith(END):
            skip = False
    return "".join(out)

def tab_bars(uninstall):
    # The mic goes before the + of every tab bar installed: px-tabs, and Shepherdr 3.x's tabs-tasks, which the
    # layouts use once Shepherdr is installed. Re-run after a Shepherdr update (it rewrites tabs-tasks).
    found = False
    for name in ("px-tabs", "tabs-tasks"):
        p = os.path.join(HOME, "mods", name, "main.js")
        if not os.path.exists(p):
            continue
        src = strip_blocks(open(p).read())
        if not uninstall:
            anchor = "const GS = () =>"
            if anchor not in src:
                print(f"{name}: no glyph helper to put the mic beside; left as it was")
                continue
            i = src.index("\n", src.index(anchor)) + 1
            src = src[:i] + HELPER + src[i:]
            anchor = '      .child(button("tab-new", '
            if anchor not in src:
                print(f"{name}: no + button to put the mic before; left as it was")
                continue
            src = src.replace(anchor, CHILD + anchor, 1)
            found = True
        write(p, src)
    if not uninstall and not found:
        sys.exit("no tab bar took the mic (px-tabs or tabs-tasks)")
    for style in ("g", "gw", "gf"):
        dst = os.path.join(HOME, "mods/px-tabs", style, "mic.svg")
        if uninstall:
            if os.path.exists(dst):
                os.remove(dst)
        else:
            write(dst, open(os.path.join(HERE, "glyphs", style, "mic.svg")).read())

def mod(uninstall):
    dst = os.path.join(HOME, "mods/voice")
    if uninstall:
        if os.path.isdir(dst):
            shutil.rmtree(dst)
        return
    os.makedirs(dst, exist_ok=True)
    for f in ("main.js", "mod.json"):
        write(os.path.join(dst, f), open(os.path.join(HERE, "mods/voice", f)).read())

def used_keys(keys):
    return {k for section in keys.values() if isinstance(section, dict) for k in section}

def keys_json(uninstall):
    p = os.path.join(HOME, "keys.json")
    keys = json.load(open(p))
    g = keys.setdefault("global", {})
    if uninstall:
        for k, v in KEYS.items():
            if g.get(k) == v:
                del g[k]
    else:
        used = used_keys(keys)
        for k, v in KEYS.items():
            if g.get(k) == v:
                continue
            if k in used:
                print(f"keys: {k} is taken ({g.get(k)}); not bound")
                continue
            g[k] = v
    write(p, json.dumps(keys, indent=2, ensure_ascii=False) + "\n")

def layouts(uninstall):
    for name in LAYOUTS:
        p = os.path.join(HOME, "layouts", name + ".json")
        if not os.path.exists(p):
            continue
        root = json.load(open(p))
        fl = [f for f in root.get("floating", []) if f.get("id") != "voice"]
        if not uninstall:
            fl.append(FLOAT)
        if fl:
            root["floating"] = fl
        else:
            root.pop("floating", None)
        write(p, json.dumps(root, indent=2, ensure_ascii=False) + "\n")

if __name__ == "__main__":
    un = "--uninstall" in sys.argv
    mod(un)
    tab_bars(un)
    keys_json(un)
    layouts(un)
