#!/usr/bin/env python3
"""Shepherdr server tasks as Herdr Desktop spaces, with Shepherdr's own agent in them.

The Shepherdr server (bun, :7787) runs each task's agent in its own herdr-core session ("shepherdr"),
which the Herdr window never shows. This keeps the window in step with it, so the Spaces list (which
nests a worktree's space under its repo's) shows exactly the server's tasks:

- a task whose worktree exists and that was started (Shepherdr has an agent terminal for it, or it is
  neither "todo" nor "done") gets a space in its worktree, named "<ID> <name>";
- that space's "agent" tab is Shepherdr's agent itself (hc attach over the shepherdr socket), never
  a second agent; when Shepherdr relaunches the agent, the tab follows the new terminal;
- it gets an "nvim" tab in the worktree;
- spaces are never closed here: a finished task keeps its space under its repo (px-spaces nests it by
  the worktree path even once the worktree is removed) until the person closes it.

Tabs other than "agent"/"nvim" are left alone.

  shepherdr-spaces.py              one pass
  shepherdr-spaces.py --every 10   keep going (the user service)
  shepherdr-spaces.py --dry-run    say what it would do
"""
import json, os, subprocess, sys, time, urllib.request

HOME = os.path.expanduser("~")
APP = os.environ.get("HERDR_APP_BIN") or os.path.join(HOME, ".local/share/herdr-app/herdr-app")
API = os.environ.get("SHEPHERDR_URL", "http://127.0.0.1:7787")
RUNTIME = os.environ.get("XDG_RUNTIME_DIR") or f"/run/user/{os.getuid()}"
SOCK = os.environ.get("SHEPHERDR_SOCKET") or os.path.join(RUNTIME, "herdr-core", "shepherdr.sock")
STATE = os.path.join(os.environ.get("HERDR_APP_HOME") or os.path.join(HOME, ".config/herdr-app"), "shepherdr-spaces.json")
DRY = "--dry-run" in sys.argv
_labels = {}  # (session, address) -> hc get result; labels and argv don't change for a terminal


def log(*a):
    print(time.strftime("%H:%M:%S"), *a, flush=True)


def run(argv, env=None):
    r = subprocess.run(argv, capture_output=True, text=True, timeout=30, env=env)
    try:
        return json.loads(r.stdout or "null")
    except ValueError:
        return None


def herdr(*args):
    if DRY and args[0] == "do":
        log("would:", " ".join(args[1:]))
        return {}
    out = run([APP, *args])
    if args[0] == "do":
        log(" ".join(a if len(a) < 60 else a[:57] + "…" for a in args[1:]), "->", json.dumps(out)[:120])
    return out or {}


def hc_get(addr, sock=None):
    key = (sock, addr)
    if key not in _labels:
        env = dict(os.environ, HC_SOCKET=sock) if sock else None
        _labels[key] = run(["hc", "get", addr], env) or {}
    return _labels[key]


def snapshot():
    with urllib.request.urlopen(f"{API}/api/snapshot", timeout=10) as r:
        return json.load(r)


def shepherdr_agents():
    """task id -> the address of Shepherdr's agent for it: the newest live one, else the newest."""
    env = dict(os.environ, HC_SOCKET=SOCK)
    best = {}
    for t in (run(["hc", "ls", "--all"], env) or {}).get("terminals", []):
        if not str(t.get("name", "")).startswith("shep-"):
            continue
        g = hc_get(t["address"], SOCK)
        lab = g.get("labels") or {}
        if lab.get("shepherdr.role") != "task" or not lab.get("shepherdr.task"):
            continue
        live = t.get("state") == "live"
        rank = (live, g.get("created_ms") or 0)
        cur = best.get(lab["shepherdr.task"])
        if not cur or rank > cur[0]:
            best[lab["shepherdr.task"]] = (rank, t["address"], live)
    return {k: (v[1], v[2]) for k, v in best.items()}


def attached_to(term):
    """The Shepherdr terminal a window terminal is attached to (hc attach over the shepherdr socket), else None."""
    argv = hc_get(term).get("argv") or []
    if "attach" in argv and any("shepherdr.sock" in a for a in argv):
        i = argv.index("attach")
        return argv[i + 1] if i + 1 < len(argv) else None
    return None


def load_state():
    try:
        return json.load(open(STATE))
    except (OSError, ValueError):
        return {"made": {}}


def save_state(s):
    if not DRY:
        tmp = STATE + ".tmp"
        json.dump(s, open(tmp, "w"), indent=1)
        os.replace(tmp, STATE)


def short(name, n=26):
    name = " ".join(str(name).split())
    return name if len(name) <= n else name[: n - 1].rstrip() + "…"


def one_pass():
    snap = snapshot()
    agents = shepherdr_agents()
    spaces = [s for s in (herdr("ls").get("spaces") or []) if not s.get("machine")]
    by_folder = {os.path.realpath(s["folder"]): s for s in spaces if s.get("folder")}
    state = load_state()
    made = state.setdefault("made", {})  # space id -> task id, spaces this made

    for t in snap.get("tasks", []):
        tid, folder = t.get("id"), t.get("folder")
        if not tid or not folder or t.get("archived"):
            continue
        agent = agents.get(tid)
        if not os.path.isdir(folder) or (not agent and t.get("status") in ("todo", "done")):
            continue
        sp = by_folder.get(os.path.realpath(folder))
        attach = ["env", f"HC_SOCKET={SOCK}", "hc", "attach", agent[0]] if agent and agent[1] else None
        if not sp:
            name = f"{tid} {short(t.get('name') or '')}".strip()
            r = herdr("do", "space.new", "--name", name, "--folder", folder,
                      *(["--tab", "agent", "--argv", json.dumps(attach)] if attach else []))
            if DRY or not r.get("space"):
                continue
            made[r["space"]] = tid
            sp = next((s for s in (herdr("ls").get("spaces") or []) if s["id"] == r["space"]), {"id": r["space"], "tabs": []})
        tabs = sp.get("tabs") or []
        terms = lambda tab: [x["terminal"] for x in tab.get("terminals", []) if x.get("terminal")]
        if attach:
            addr = agent[0]
            mine = [tab for tab in tabs if any(attached_to(x) for x in terms(tab))]
            if not any(attached_to(x) == addr for tab in mine for x in terms(tab)):
                for tab in mine:  # attached to an earlier launch of this task's agent
                    herdr("do", "tab.close", "--id", tab["id"])
                herdr("do", "tab.new", "--name", "agent", "--space", sp["id"], "--folder", folder, "--argv", json.dumps(attach))
        if not any(tab.get("name") == "nvim" for tab in tabs):
            herdr("do", "tab.new", "--name", "nvim", "--space", sp["id"], "--folder", folder, "--argv", json.dumps(["nvim"]))

    # Spaces this made stay after their task is done (the Spaces list keeps them under their repo,
    # also once the worktree is removed); closing one is the person's call. Forget ones closed.
    ids = {s["id"] for s in spaces}
    for sid in [k for k in made if k not in ids]:
        made.pop(sid)
    save_state(state)


def main():
    every = None
    if "--every" in sys.argv:
        every = float(sys.argv[sys.argv.index("--every") + 1])
    while True:
        try:
            one_pass()
        except Exception as e:  # server down, window closed: try again next pass
            log("pass skipped:", e)
        if not every:
            break
        time.sleep(every)


if __name__ == "__main__":
    main()
