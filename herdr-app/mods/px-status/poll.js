// The poller for our projects manager (simple-herdr-projects, `shp`), on this computer and on every
// machine linked to herdr-core. px-status runs it, since the status bar is always on screen: it reads
// `shp list --json` per machine, runs `shp tick` so queued work starts, samples each machine's load,
// lists its shp spaces, toasts once per task that newly needs you or is ready, and publishes it all as
// shared("shp") = { places: [{ name, connected, tasks, spaces, error, read, health }] }.
// The Tasks panel and Monitor (mods/px-projects), the sidebar and the bars only read that.

import { hc, exec, notify, setShared } from "herdr";

const POLL = 3000;    // tasks
const TICK = 10000;   // shp tick: start queued work
const HEALTH = 15000; // machine load
const SPACES = 30000; // the machine's shp spaces
const lastLine = (text) => String(text ?? "").trim().split("\n").pop();
const openAsk = (task) => (task.asks ?? []).find((a) => !a.closed && !a.answered) ?? task.asks?.[0] ?? null;
// A toast's words: the open question, else the worker's step.
function subtitle(task) {
  const ask = openAsk(task);
  if (task.status === "needs" && ask?.text) return String(ask.text);
  return task.step ? String(task.step) : "";
}

function notice(task) {
  const status = String(task.status ?? "");
  if (status !== "needs" && status !== "ready") return null;
  const who = task.machine ? `${task.id} on ${task.machine}` : String(task.id);
  return {
    key: `${task.machine ?? ""}/${task.id}:${status}`,
    title: `${who} ${status === "ready" ? "is ready" : "needs you"}`,
    body: subtitle(task) || String(task.title ?? ""),
    level: status === "ready" ? "success" : "warning",
  };
}

// One machine's load as the Monitor draws it: CPU (load / cores), memory and the home disk, in %.
const SAMPLE = `n=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
l=$(uptime | sed 's/.*load averages*: *//' | tr -d ',' | awk '{print $1}')
if [ -r /proc/meminfo ]; then m=$(awk '/MemTotal/{t=$2}/MemAvailable/{a=$2}END{if(t)printf "%d",(t-a)*100/t}' /proc/meminfo)
else m=$(memory_pressure 2>/dev/null | awk '/free percentage/{gsub("%","",$5); print 100-$5}'); fi
d=$(df -P "$HOME" | awk 'NR==2{gsub("%","",$5); print $5}')
echo "$n $l \${m:-0} \${d:-0}"`;

const newPlace = (name, connected) => ({ name, connected, tasks: [], spaces: [], error: null, read: false, answered: false, health: null });

// Start the poller (px-status, one per window, calls this from its init; its loops end with it).
export function startPoller(cx) {
  const state = { places: [newPlace(null, true)], seen: {}, loading: new Set(), last: "" };
  const publish = () => {
    const places = state.places.map(({ name, connected, tasks, spaces, error, read, health }) => ({ name, connected, tasks, spaces, error, read, health }));
    const text = JSON.stringify(places);
    if (text === state.last) return;
    state.last = text;
    setShared("shp", { places });
  };
  const machines = async () => {
    try {
      const reply = await hc(["machine", "ls", "--json"]);
      if (reply.code !== 0) return;
      const linked = (JSON.parse(reply.stdout).machines ?? []).filter((m) => !m.temporary)
        .map((m) => ({ name: m.name, connected: m.state === "connected" }));
      state.places = [{ name: null, connected: true }, ...linked].map(({ name, connected }) =>
        Object.assign(state.places.find((p) => p.name === name) ?? newPlace(name, connected), { connected }));
    } catch { /* keep the machines already known */ }
  };
  const load = async (place) => {
    if (state.loading.has(place.name)) return;
    if (!place.connected) { Object.assign(place, { tasks: [], error: "Not connected.", read: true, answered: false }); return; }
    state.loading.add(place.name);
    try {
      const r = await exec("shp", ["list", "--json"], place.name ? { on: place.name } : {});
      // shp still prints the tasks it could read beside a bad task file.
      place.answered = r.code === 0 || Boolean(r.stdout.trim());
      const list = place.answered ? JSON.parse(r.stdout) : [];
      place.tasks = Array.isArray(list) ? list.map((t) => ({ ...t, machine: place.name })) : [];
      place.error = r.code === 0 ? null : lastLine(r.stderr) || `shp list exited ${r.code}`;
      if (r.code === 0) toast(place);
    } catch (e) {
      Object.assign(place, { tasks: [], answered: false, error: lastLine(e?.stderr || e?.message || e) || "No shp there." });
    } finally {
      state.loading.delete(place.name);
      place.read = true;
    }
  };
  // Toast what newly needs you or is ready on this machine; a task keeps its key while it stays so.
  const toast = (place) => {
    const prefix = `${place.name ?? ""}/`;
    const now = Object.fromEntries(Object.keys(state.seen).filter((k) => !k.startsWith(prefix)).map((k) => [k, true]));
    for (const task of place.tasks) {
      const one = notice(task);
      if (!one) continue;
      now[one.key] = true;
      if (!state.seen[one.key]) notify({ title: one.title, body: one.body, level: one.level });
    }
    state.seen = now;
  };
  const health = async (place) => {
    if (!place.answered) return;
    try {
      const r = await exec("sh", ["-c", SAMPLE], place.name ? { on: place.name } : {});
      const [n, load1, mem, disk] = String(r.stdout).trim().split(/\s+/).map(Number);
      place.health = { cpu: Math.min(100, Math.round((load1 / Math.max(1, n)) * 100)), mem, disk, cores: n };
    } catch { place.health = null; }
  };
  const spacesOf = async (place) => {
    if (!place.answered) return;
    try {
      const r = await exec("sh", ["-c", "ls ~/.herdr/simple/spaces/ 2>/dev/null | sed -n 's/\\.md$//p'"], place.name ? { on: place.name } : {});
      place.spaces = String(r.stdout).split("\n").map((s) => s.trim()).filter(Boolean);
    } catch { /* keep */ }
  };
  const every = (ms, fn) => cx.spawn(async () => { for (;;) { try { await fn(); } catch {} await new Promise((ok) => cx.timer.after(ms, ok)); } });
  every(POLL, async () => { await machines(); await Promise.all(state.places.map(load)); publish(); });
  every(TICK, async () => { for (const p of state.places) if (p.answered) await exec("shp", ["tick"], p.name ? { on: p.name } : {}).catch(() => null); });
  every(HEALTH, async () => { await Promise.all(state.places.map(health)); publish(); });
  every(SPACES, async () => { await Promise.all(state.places.map(spacesOf)); publish(); });
}
