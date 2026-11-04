#!/usr/bin/env python3
"""Keep Herdr desktop's layout for this Mac's Shepherdr session in step with its agents.

One Herdr space per Shepherdr space that has a live agent, one tab per live agent (coordinator first, then
tasks). Only spaces whose id starts with "spshep" are ours; anything made by hand is kept as it is.
Runs once and exits (the Herdr status bar calls it every few seconds). Talks to herdr-core over
`HC_SESSION=shepherdr hc connect` (JSON-RPC, one request per line)."""
import hashlib, json, os, subprocess, sys, urllib.request

SESSION = os.environ.get("SHEP_SESSION", "shepherdr")
API = os.environ.get("SHEP_API", "http://127.0.0.1:7787/api")
HC = os.path.expanduser("~/.local/bin/hc")


def rpc(*calls):
    env = {k: v for k, v in os.environ.items() if not k.startswith("HC_")}
    env["HC_SESSION"] = SESSION
    lines = "".join(json.dumps({"jsonrpc": "2.0", "id": i, "method": m, "params": p}) + "\n" for i, (m, p) in enumerate(calls))
    out = subprocess.run([HC, "connect"], input=lines, capture_output=True, text=True, timeout=15, env=env).stdout
    res = {}
    for line in out.splitlines():
        try:
            r = json.loads(line)
        except ValueError:
            continue
        if "id" in r:
            res[r["id"]] = r
    return [res.get(i, {}) for i in range(len(calls))]


def hid(prefix, key):
    return prefix + hashlib.sha1(key.encode()).hexdigest()[:24]


def main():
    listing, doc = rpc(("terminal.list", {}), ("doc.get", {"name": "herdr-app"}))
    if "result" not in listing or "result" not in doc:
        sys.exit("herdr-core: " + json.dumps(listing.get("error") or doc.get("error")))
    terms = listing["result"].get("terminals", [])
    value = doc["result"].get("value") or {"spaces": []}
    version = doc["result"].get("version", 0)
    try:
        places = {p["id"]: p for p in json.load(urllib.request.urlopen(f"{API}/snapshot", timeout=5)).get("places", [])}
    except Exception:
        places = {}

    # live agents, grouped by Shepherdr space
    groups = {}
    for t in terms:
        lab = t.get("labels") or {}
        space = lab.get("shepherdr.space")
        if not space or t.get("state", "live") != "live" or lab.get("shepherdr.role") == "browser":
            continue
        groups.setdefault(space, []).append(t)

    old = {s["id"]: s for s in value.get("spaces", []) if s.get("id", "").startswith("spshep")}
    ours = []
    for space, ts in sorted(groups.items(), key=lambda kv: (not kv[0].endswith(":~"), places.get(kv[0], {}).get("name", kv[0]))):
        name = places.get(space, {}).get("name") or ("home" if space.endswith(":~") else space.rsplit(":", 1)[-1])
        ts.sort(key=lambda t: (t["labels"].get("shepherdr.role") != "coordinator", t["labels"].get("shepherdr.task", "")))
        sid = hid("spshep", space)
        prev_tabs = {}
        for tab in (old.get(sid) or {}).get("tabs", []):
            for c in (tab.get("root") or {}).get("children", []):
                term = (c.get("content") or {}).get("terminal")
                if term:
                    prev_tabs[term] = tab
        tabs = []
        for t in ts:
            addr = t.get("address") or t.get("id")
            if addr in prev_tabs:
                tabs.append(prev_tabs[addr])
                continue
            lab = t["labels"]
            label = "coordinator" if lab.get("shepherdr.role") == "coordinator" else (lab.get("shepherdr.task") or lab.get("shepherdr.role") or addr)
            tid = hid("tbshep", addr)
            tabs.append({"id": tid, "name": label, "root": {"id": tid, "type": "row",
                         "children": [{"id": hid("pshep", addr), "type": "block", "content": {"terminal": addr}}]}})
        ours.append({"id": sid, "name": name, "folder": t.get("cwd") or os.path.expanduser("~"), "tabs": tabs})

    spaces = [s for s in value.get("spaces", []) if not s.get("id", "").startswith("spshep")] + ours
    if spaces == value.get("spaces", []):
        print(json.dumps({"changed": False, "spaces": len(ours)}))
        return
    new = dict(value, spaces=spaces)
    (res,) = rpc(("doc.set", {"name": "herdr-app", "value": new, "version": version}))
    print(json.dumps({"changed": True, "spaces": len(ours), "result": res.get("result"), "error": res.get("error")}))


if __name__ == "__main__":
    main()
