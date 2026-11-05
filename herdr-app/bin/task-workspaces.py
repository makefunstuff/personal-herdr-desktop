#!/usr/bin/env python3
"""One Herdr workspace per started Shepherdr 3.1 task, kept in step with herdr-task's lists.

Shepherdr 3.1 starts a task's agent as an hc child of its space's Assistant, with no tab, pane or
Herdr space of its own ("a space can hold thousands of tasks"), so no sidebar can show it. This
gives each started task the workspace Shepherdr 2.1 had: a real Herdr space whose folder is the
task's worktree, under the space it belongs to, with the task's own tabs - Chat (the worker's
conversation), Terminal (the live hc terminal itself, never a new one), Files, Changes and Pull
request (this repo's px-files, px-changes, px-pr, which follow the space's folder).

The desktop's spaces are herdr-core's "herdr-app" document, as Shepherdr 2.1's desktop-layout
kept them: read and written over `herdr-core connect` (terminal.list, doc.get, doc.set). Only
spaces whose id starts with sptask are ours; workspaces made by hand are never touched. A task's
workspace is removed once its task has been done, archived or gone for two passes in a row, and a
list herdr-task was writing when it was read is never taken as empty, so a flicker never closes a
space. Its folder, name and tabs follow the task: renaming them by hand is undone on the next pass.

It reads this computer's ~/.herdr/tasks lists and this computer's window; a task in a space on
another machine is not mirrored here.

A new workspace is written with no tabs first and given them a moment later: Shepherdr's tab bar
(lib/coordinator.js watchNewSpaces) gives every new space an Assistant tab, and skips one with no
tabs, so this way a task workspace never gets an Assistant. The tabs come once the tab bar has
noted the space (shared.json's "coordinator.seen") or, with the window open, after a few seconds.

While the window is open, a write from here is a layout change it didn't make: Herdr shows its
"Layout changed elsewhere" notice (the document is shared by connected screens). It is only written
when a task appears, goes or is renamed, or its worker is replaced - never for a status or progress
change.

A service, not a mod: a mod only lives inside an open window and draws the document it is handed.
The document itself has to be written, also while the window is closed (that is what herdr-core
keeps), so this runs as a user service and the sidebars only draw what it wrote.

Writes ~/.config/herdr-app/task-workspaces.json for this repo's sidebars (px-spaces, px-agents):
which workspace a task has, the space to nest it under, its id, status and progress.

  task-workspaces.py                 one pass (a hand run; --dry-run says what it would do)
  task-workspaces.py --every 3       keep going, once every 3 s (the user service, install.sh)
  task-workspaces.py --session NAME  another hc session (default: this computer's)
  task-workspaces.py --dry-run       say what it would do, change nothing
"""
import fcntl, glob, hashlib, json, os, subprocess, sys, time

HOME = os.path.expanduser("~")
APP_HOME = os.environ.get("HERDR_APP_HOME") or os.path.join(HOME, ".config", "herdr-app")
TASKS = os.path.join(HOME, ".herdr", "tasks")
REGISTRY = os.path.join(APP_HOME, "task-workspaces.json")
SHARED = os.path.join(APP_HOME, "shared.json")
CORE = os.environ.get("HC_BIN") or "herdr-core"
PREFIX = "sptask"
# The tabs every task workspace gets, in this order. Chat first: a click opens it.
CHAT, TERM, FILES, CHANGES, PR = "Chat", "Terminal", "Files", "Changes", "PR"
# A task's state while it is started and not finished (herdr-task's words: todo, queued, running,
# ready, done). Ready stays: its worker is still there, waiting for the person.
LIVE_STATES = ("running", "ready")
# A new workspace's tabs wait this long (seconds) when shared.json doesn't say the tab bar has seen
# it - a tab bar that doesn't give spaces Assistants (px-tabs) never writes that, and the window
# has to be open anyway for anyone to look at the sidebar.
FILL_AFTER = 6


def log(*parts):
    print(time.strftime("%Y-%m-%d %H:%M:%S"), *parts, flush=True)


def rpc(calls, session=None):
    """herdr-core connect, one JSON-RPC request per line; returns the replies in order."""
    env = dict(os.environ)
    if session:
        env["HC_SESSION"] = session
    lines = "".join(json.dumps({"jsonrpc": "2.0", "id": i, "method": m, "params": p}) + "\n" for i, (m, p) in enumerate(calls))
    try:
        out = subprocess.run([CORE, "connect"], input=lines, capture_output=True, text=True, timeout=25, env=env).stdout
    except (OSError, subprocess.SubprocessError) as e:
        raise RuntimeError(f"herdr-core connect: {e}")
    res = {}
    for line in out.splitlines():
        try:
            r = json.loads(line)
        except ValueError:
            continue
        if "id" in r:
            res[r["id"]] = r
    return [res.get(i, {"error": {"message": "no reply"}}) for i in range(len(calls))]


def hid(prefix, key, n=30):
    return prefix + hashlib.sha1(key.encode("utf-8", "replace")).hexdigest()[:n]


def read_tasks():
    """herdr-task's lists on this computer: ([{folder, name, prefix, slug, machine, tasks}], {unreadable slugs}).

    herdr-task rewrites a list on every status and progress change, so a file can be caught mid-write:
    it must never read as "this list has no tasks". Those slugs come back separately and their tasks'
    workspaces are left alone this pass.
    """
    lists, unreadable = [], set()
    for path in sorted(glob.glob(os.path.join(TASKS, "*.json"))):
        slug = os.path.basename(path)[:-len(".json")]
        try:
            with open(path, encoding="utf-8") as f:
                d = json.load(f)
        except (OSError, ValueError):
            unreadable.add(slug)
            continue
        if isinstance(d, dict) and d.get("slug") and isinstance(d.get("tasks"), list):
            lists.append(d)
        else:
            unreadable.add(slug)
    return lists, unreadable


def text(v, n=200):
    v = " ".join(str(v or "").split())
    return v if len(v) <= n else v[: n - 1].rstrip() + "\u2026"


def workers_by_task(terminals):
    """Live task agents by herdr.task label, reviewers apart: {key: [terminal]}."""
    out = {}
    for k in terminals:
        lab = k.get("labels") or {}
        key = lab.get("herdr.task")
        if not key or k.get("state") != "live":
            continue
        if lab.get("herdr.role") in ("reviewer", "browser"):
            continue
        out.setdefault(key, []).append(k)
    return out


def repo_root(path):
    """The checkout a worktree belongs to (its .git points at <repo>/.git/worktrees/<name>)."""
    try:
        path = os.path.realpath(path)
        dotgit = os.path.join(path, ".git")
        if os.path.isdir(dotgit):
            return path
        with open(dotgit, encoding="utf-8") as f:
            line = f.readline().strip()
    except OSError:
        return None
    if not line.startswith("gitdir:"):
        return None
    gitdir = line[len("gitdir:"):].strip()
    if not os.path.isabs(gitdir):
        gitdir = os.path.normpath(os.path.join(path, gitdir))
    gitdir = os.path.normpath(gitdir)
    while gitdir and gitdir != "/" and os.path.basename(gitdir) != ".git":
        gitdir = os.path.dirname(gitdir)
    repo = os.path.realpath(os.path.dirname(gitdir)) if gitdir and gitdir != "/" else None
    return repo if repo and os.path.isdir(repo) else None


def desired(lists, terminals, spaces):
    """The task workspaces that should exist: [{id, key, ref, ...}] in a stable order."""
    workers = workers_by_task(terminals)
    by_folder = {}
    for s in spaces:
        if s.get("machine") or not s.get("folder"):
            continue
        by_folder.setdefault(os.path.realpath(s["folder"]), s["id"])
    out = []
    for lst in lists:
        slug = lst["slug"]
        folder = lst.get("folder") or ""
        for t in lst["tasks"]:
            if t.get("archived") or t.get("state") not in LIVE_STATES:
                continue
            key = f"{slug}#{t['id']}"
            live = workers.get(key) or []
            ws = t.get("worktree") if os.path.isdir(t.get("worktree") or "") else folder
            if not ws or not os.path.isdir(ws):
                continue
            # The last agent started is the task's own (reviewers are kept apart above). Its live
            # terminal is the one to show; with none, the task's chat still reads from its log.
            agent = (t.get("agents") or [{}])[-1]
            live_addr = next((k["address"] for k in live if k.get("address") == agent.get("address")), None) \
                or (live[-1].get("address") if live else None)
            addr = live_addr or agent.get("address")
            status = None
            for k in live:
                if k.get("address") == addr and (k.get("status") or {}).get("state"):
                    status = k["status"]["state"]
            progress = t.get("progress") or {}
            done = progress.get("done")
            num = lambda v: isinstance(v, (int, float)) and not isinstance(v, bool)
            ref = f"{lst.get('prefix') or 'TASK'}-{t['id']}"
            # Where it belongs: the space of the checkout its worktree is from (where the person
            # looks for that repo), else the space it was started from.
            root = repo_root(ws) if ws != folder else (repo_root(folder) or folder)
            parent = by_folder.get(os.path.realpath(root)) if root else None
            parent = parent or (t.get("space") if isinstance(t.get("space"), str) else None)
            out.append({
                "id": hid(PREFIX, key),
                "key": key,
                "ref": ref,
                "title": text(t.get("title"), 300),
                "name": text(f"{ref} \u00b7 {t.get('title')}", 90),
                "folder": ws,
                "branch": t.get("branch") or None,
                "space": t.get("space") if isinstance(t.get("space"), str) else None,
                "parent": parent,
                "terminal": addr,
                "live": bool(live_addr),
                "agent": (agent.get("agent") or None),
                "log": (agent.get("log") or None),
                "status": "ready" if (t.get("state") == "ready" and status not in ("working", "blocked"))
                          else (status or t.get("status") or ("working" if t.get("state") == "running" else "idle")),
                "progress": round(done) if num(done) else None,
                "doing": text(progress.get("text"), 120) or None,
                "left": round(progress["left"] / 60000) if num(progress.get("left")) else None,
                "chat": hid("tb", f"{hid(PREFIX, key)}:{CHAT}", 30),
                "started": t.get("started") or None,
                "key_ref": {"machine": None, "folder": folder, "slug": slug, "id": t["id"]},
            })
    return out


def tabs_of(t):
    """The task workspace's tabs, as the app's tab tree. Chat first: a click opens it."""
    tabs = []
    props = {"thread": True, "task": t["ref"], "title": t["title"], "key": t["key_ref"]}
    if t.get("terminal"):
        props["terminal"] = t["terminal"]
    if t.get("log"):
        props["log"] = {"path": t["log"], "kind": t.get("agent")}
    content = {CHAT: {"mod": "chat", "props": props}}
    if t.get("live"):
        content[TERM] = {"terminal": t["terminal"]}
    content[FILES] = {"mod": "px-files"}
    content[CHANGES] = {"mod": "px-changes"}
    content[PR] = {"mod": "px-pr"}
    for name, c in content.items():
        tid = hid("tb", f"{t['id']}:{name}", 30)
        tabs.append({"id": tid, "name": name, "root": {"id": tid, "type": "row",
                     "children": [{"id": hid("p", f"{t['id']}:{name}", 30), "type": "block", "content": c}]}})
    return tabs


def same_tabs(old, new):
    """Equal tab lists, by id, name and the content each tab shows (the app may add fields)."""
    def shape(tabs):
        return [(t.get("id"), t.get("name"), json.dumps(term_blocks(t.get("root")), sort_keys=True)) for t in tabs or []]
    return shape(old) == shape(new)


def term_blocks(node):
    if not isinstance(node, dict):
        return None
    if node.get("type") == "block":
        c = node.get("content") or {}
        if c.get("terminal"):
            return {"terminal": c["terminal"]}
        if c.get("mod") == "chat":
            p = c.get("props") or {}
            return {"mod": "chat", "terminal": p.get("terminal"), "task": p.get("task"), "title": p.get("title")}
        return {"mod": c.get("mod")}
    return [term_blocks(c) for c in node.get("children") or []]


def app_open():
    """Whether the Herdr window is running (its tab bar ready to note new spaces)."""
    try:
        return subprocess.run(["pgrep", "-x", "herdr-app"], capture_output=True, timeout=5).returncode == 0 or \
            subprocess.run(["pgrep", "-f", "herdr-app$"], capture_output=True, timeout=5).returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False


def seen_by_tab_bar():
    """The space ids Shepherdr's tab bar has noted (shared.json: coordinator.seen), or None when no
    tab bar writes that list (px-tabs): a new workspace then waits a few seconds instead."""
    try:
        with open(SHARED, encoding="utf-8") as f:
            v = json.load(f).get("coordinator.seen")
    except (OSError, ValueError, AttributeError):
        return None
    return set(v) if isinstance(v, list) else None


def load_registry():
    try:
        with open(REGISTRY, encoding="utf-8") as f:
            d = json.load(f)
        return d if isinstance(d, dict) else {}
    except (OSError, ValueError):
        return {}


def save_registry(tasks, pending, missing, ours, old):
    """Write what the sidebars read, only when it changed (a pass every few seconds)."""
    kept = {k: v for k, v in pending.items() if k in tasks}
    gone = {k: v for k, v in missing.items() if k in ours}
    if old.get("tasks") == tasks and old.get("pending") == kept and old.get("missing") == gone:
        return
    tmp = REGISTRY + ".new"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump({"version": 1, "updated": int(time.time() * 1000), "tasks": tasks, "pending": kept, "missing": gone}, f, indent=1)
        f.write("\n")
    os.replace(tmp, REGISTRY)


def one_pass(session=None, dry_run=False):
    (listing, doc) = rpc([("terminal.list", {}), ("doc.get", {"name": "herdr-app"})], session)
    if "result" not in listing or "result" not in doc:
        raise RuntimeError("herdr-core: " + json.dumps((listing.get("error") or doc.get("error") or {})).strip())
    terminals = listing["result"].get("terminals") or []
    value = doc["result"].get("value") or {"spaces": []}
    version = doc["result"].get("version", 0)
    spaces = value.get("spaces") or []
    lists, unreadable = read_tasks()
    want = desired(lists, terminals, spaces)
    by_id = {t["id"]: t for t in want}
    have = {s["id"]: s for s in spaces if str(s.get("id", "")).startswith(PREFIX)}
    registry = load_registry()
    old_tasks = registry.get("tasks") or {}
    pending = dict(registry.get("pending") or {})
    missing = dict(registry.get("missing") or {})
    seen = seen_by_tab_bar()
    now = time.time()
    window = app_open()
    changed = False
    # Tabs for a workspace that already exists: at once when it's the same task, unless it is new.
    out_spaces = []
    filled, created, removed, kept_old = [], [], [], {}
    for s in spaces:
        sid = s.get("id")
        t = by_id.get(sid)
        if not str(sid or "").startswith(PREFIX):
            out_spaces.append(s)
            continue
        if not t:
            # A task's space only goes once the task is gone from a list that was read whole, two
            # passes in a row: herdr-task rewrites its list on every status change, so a file caught
            # mid-write, a state settling or a worktree being made must never close a space. Two
            # passes still leave the sidebar inside ten seconds.
            slug = str((old_tasks.get(sid) or {}).get("key") or "").split("#")[0]
            if slug in unreadable or (not slug and unreadable):
                out_spaces.append(s)
                if sid in old_tasks:
                    kept_old[sid] = old_tasks[sid]
                continue
            n = missing.get(sid, 0) + 1
            if n < 2:
                missing[sid] = n
                out_spaces.append(s)
                if sid in old_tasks:
                    kept_old[sid] = old_tasks[sid]
                continue
            missing.pop(sid, None)
            removed.append(s.get("name") or sid)
            changed = True
            continue
        missing.pop(sid, None)
        tabs = tabs_of(t)
        if not (s.get("tabs") or []):
            # Written with no tabs a moment ago (see the top): fill them once the tab bar has noted
            # the space. A tab bar that writes no such list gets a few seconds from the moment the
            # window opened - never from when the space was made with the window shut, or the tab
            # bar would see it fresh and give it an Assistant before this fills it.
            p = pending.get(sid)
            if not isinstance(p, dict):
                p = {"at": now, "window": window}      # a pending time from an older version
            elif window and not p.get("window"):
                p = {"at": now, "window": True}        # the window just opened: let the tab bar look
            pending[sid] = p
            waited = now - p["at"]
            if (seen is not None and sid in seen) or (window and waited >= (FILL_AFTER if seen is None else 30)):
                s = dict(s, tabs=tabs, name=t["name"], folder=t["folder"])
                pending.pop(sid, None)
                filled.append(t["ref"])
                changed = True
            else:
                s = dict(s, name=t["name"], folder=t["folder"])
        elif not same_tabs(s.get("tabs"), tabs) or s.get("name") != t["name"] or os.path.normpath(s.get("folder") or "") != os.path.normpath(t["folder"]):
            s = dict(s, tabs=tabs, name=t["name"], folder=t["folder"])
            pending.pop(sid, None)
            changed = True
        out_spaces.append(s)
    for t in want:
        if t["id"] in have:
            continue
        # New: no tabs yet, so the tab bar's watchNewSpaces leaves it alone (no Assistant chat).
        out_spaces.append({"id": t["id"], "name": t["name"], "folder": t["folder"], "tabs": []})
        pending[t["id"]] = now
        missing.pop(t["id"], None)
        created.append(t["ref"])
        changed = True
    written = True
    if changed and not dry_run:
        (res,) = rpc([("doc.set", {"name": "herdr-app", "value": dict(value, spaces=out_spaces), "version": version})], session)
        if "error" in res:
            log("doc.set refused (another writer):", json.dumps(res.get("error"))[:200])
            written = False
        else:
            for label, items in (("created", created), ("tabs", filled), ("removed", removed)):
                if items:
                    log(label + ":", ", ".join(items))
    if changed and dry_run:
        for label, items in (("would create", created), ("would fill", filled), ("would remove", removed)):
            if items:
                log(label + ":", ", ".join(items))
    # What the sidebars read: every task with a workspace, filled or still waiting for its tabs.
    tasks = {}
    for t in want:
        tasks[t["id"]] = {k: t[k] for k in ("id", "key", "ref", "title", "folder", "branch", "space", "parent",
                                            "terminal", "live", "agent", "status", "progress", "doing", "left", "started", "chat")}
        tasks[t["id"]]["tabs"] = not (t["id"] in pending)
    # A space kept for a pass while its task is only just gone keeps its task row meanwhile.
    for sid, old in kept_old.items():
        tasks.setdefault(sid, old)
    if not dry_run and written:
        save_registry(tasks, pending, missing, have, registry)
    return {"created": created, "filled": filled, "removed": removed, "changed": changed, "tasks": tasks}


def main(argv):
    session = None
    every = None
    dry = "--dry-run" in argv
    for i, a in enumerate(argv):
        if a == "--session" and i + 1 < len(argv):
            session = argv[i + 1]
        elif a.startswith("--session="):
            session = a.split("=", 1)[1]
        elif a == "--every" and i + 1 < len(argv):
            every = float(argv[i + 1])
        elif a.startswith("--every="):
            every = float(a.split("=", 1)[1])
    os.makedirs(APP_HOME, exist_ok=True)
    lock = open(os.path.join(APP_HOME, "task-workspaces.lock"), "w")
    while True:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            log("another pass is running; this one has nothing to do")  # it sees the same tasks
        else:
            try:
                one_pass(session, dry)
            except Exception as e:  # keep going: the window, the core or a file may be busy a moment
                log("error:", e)
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)  # between passes a one-shot run can have its turn
        if every is None:
            return 0
        time.sleep(max(0.5, every))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
