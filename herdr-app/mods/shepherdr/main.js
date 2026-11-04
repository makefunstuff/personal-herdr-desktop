// Shepherdr in Herdr desktop: spaces, tasks, Needs you and coordinator/task chats from this computer's
// Shepherdr (http://127.0.0.1:7787), without the web UI. Open it as a tab: tab.new {mod: "shepherdr"}.
import { View, div, svg, image } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import * as fs from "fs/promises";
import { run, theme, agents, exec, props, shared, appHome, panes, spaces, home } from "herdr";
// Show Shepherdr's tab if it's open anywhere, else open it here (same as the status bar's buttons).
function openShepherdr(view) {
  const name = view === "monitor" ? "Monitor" : "Shepherdr";
  const modTabs = new Set(panes().filter((p) => p.mod === "shepherdr").map((p) => p.tab));
  for (const s of spaces()) {
    const tab = (s.tabs ?? []).find((t) => modTabs.has(t.id) && t.name === name);
    if (tab) { if (!s.active) run("space.select", { id: s.id }); run("tab.select", { id: tab.id }); return; }
  }
  run("tab.new", { name, mod: "shepherdr", props: view === "monitor" ? { view: "monitor" } : {} });
}
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
const glyph = (name, size = 13) => svg(`mods/px-chrome/${GS()}/${name}.svg`).size(size).flex_none();
const crook = (size = 14) => svg(`mods/px-tabs/${GS()}/shepherdr.svg`).size(size).flex_none();
// light markdown: drop emphasis marks and heading hashes, keep the words
const plain = (md) => String(md ?? "").replace(/^#{1,6}\s+/gm, "").replace(/\*\*(.+?)\*\*/g, "$1").replace(/`([^`]+)`/g, "$1");

// Each Shepherdr this tab controls. `herdr`: the Herdr machine that shows its agents' terminals.
// `via`: run curl on that machine through herdr-core (Shepherdr only listens on its own loopback); none: fetch here.
// The Shepherdrs this panel shows. This computer's always; others come from ~/.config/herdr-app/shepherdr-sources.json:
// [{"id": "gpu", "name": "gpu-box", "herdr": "gpu-shepherdr", "via": "gpu-shepherdr"}]
// (herdr: the Herdr machine showing its agents; via: the Herdr machine to run curl on, since Shepherdr listens on loopback).
// The board from before 2.1 was migrated into this computer's Shepherdr on 2026-11-04. To look at its files again
// read-only, add { id: "legacy", name: "Board before 2.1", herdr: null, via: null, legacy: `${home()}/.shepherdr-legacy` }.
// Shepherdr 3.0 keeps its tasks in ~/.herdr/tasks/<slug>.json (herdr-task); this panel reads them (read-only) and
// takes each task's live status from its agent. The full Shepherdr 3.0 window is the "shepherdr" layout.
const SOURCES = [{ id: "local", name: "This computer", herdr: null, via: null, tasks3: `${home()}/.herdr/tasks` }];

async function tasks3Snapshot(dir) {
  const files = (await fs.readdir(dir).catch(() => [])).filter((f) => f.endsWith(".json") && !f.startsWith("."));
  const live = new Map((agents() ?? []).map((a) => [a.address, a]));
  const places = [], tasks = [];
  for (const f of files) {
    let d; try { d = JSON.parse(await fs.readFile(`${dir}/${f}`, "utf8")); } catch { continue; }
    if (!Array.isArray(d.tasks)) continue;
    const pid = `t3:${d.slug}`, prefix = d.prefix || "T";
    places.push({ id: pid, chat: pid, name: d.name, folder: d.folder, status: "idle" });
    for (const t of d.tasks) {
      const ag = [...(t.agents ?? [])].reverse().map((a) => live.get(a.address)).find(Boolean);
      const st = t.state === "done" ? "done" : t.state === "ready" ? "ready" : t.state === "queued" ? "queued" : t.state === "todo" ? "todo"
        : !ag ? "idle" : ag.status === "blocked" ? "need" : ag.status === "working" ? "work" : ag.status === "done" ? "ready" : "work";
      tasks.push({ id: `${prefix}-${t.id}`, name: t.title, place: pid, status: st, age: "", archived: t.archived ?? null,
        agent: { chat: `${prefix}-${t.id}/Agent`, address: ag?.address ?? null }, report: null,
        worktree: t.worktree, branch: t.branch, desc: t.prompt });
    }
    const ks = tasks.filter((k) => k.place === pid && !k.archived);
    places[places.length - 1].status = ks.some((k) => k.status === "need") ? "need" : ks.some((k) => k.status === "work") ? "work" : "idle";
  }
  return { places, tasks, machines: [], asks: {}, needs: [], artifacts: [], tasks3: true };
}

// ---- read-only view of the legacy (2.0) home: nothing here ever writes to it
async function readJson(path) { return JSON.parse(await fs.readFile(path, "utf8")); }
const legacyFile = (key) => `${key.replace(/[:/]/g, "_")}.json`;
async function legacySnapshot(dir) {
  const [agents, board, asks] = await Promise.all([readJson(`${dir}/agents.json`), readJson(`${dir}/tasks.json`),
    readJson(`${dir}/asks.json`).catch(() => ({}))]);
  const now = Date.now(), ago = (ms) => { const m = Math.round((now - ms) / 60000); return m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h` : `${Math.floor(m / 1440)}d`; };
  const places = Object.entries(agents).filter(([k]) => k.startsWith("p:")).map(([k, a]) => ({ id: k, chat: k, name: a.workspace || k.split(":").pop() || "home", status: "idle", folder: a.cwd }));
  const openAsks = {};
  const tasks = (board.tasks ?? []).map((t) => {
    const done = Boolean(t.doneAt || t.closed), stopped = Boolean(t.stopped);
    const status = done ? "done" : t.told === "need" ? "need" : stopped ? "idle" : t.report?.progress != null ? "work" : "todo";
    return { id: t.id, name: t.title, place: t.project, status, report: t.report ?? null, age: ago(Number(t.updated || t.created || now)),
      archived: t.cleaned ? Number(t.cleaned) : null, branch: t.branch, worktree: t.worktree, desc: t.desc, agent: { chat: `${t.id}/Agent` } };
  });
  for (const p of places) {
    const ks = tasks.filter((k) => k.place === p.id && !k.archived);
    p.status = ks.some((k) => k.status === "need") ? "need" : ks.some((k) => k.status === "work") ? "work" : "idle";
  }
  // open questions: the last unanswered question in each chat
  for (const chat of [...places.map((p) => p.chat), ...tasks.map((k) => k.agent.chat)]) {
    try {
      const log = await readJson(`${dir}/chatlog/${legacyFile(chat)}`);
      const q = [...(log.posted ?? [])].reverse().find((m) => m.question && !(log.answers ?? {})[m.question.id]);
      if (q) openAsks[chat] = q.question.id;
    } catch {}
  }
  return { places, tasks, machines: [], asks: openAsks, needs: [], artifacts: [], legacy: true };
}
async function legacyChat(dir, key) {
  let log = { posted: [], answers: {} };
  try { log = await readJson(`${dir}/chatlog/${legacyFile(key)}`); } catch {}
  const messages = (log.posted ?? []).map((m) => {
    let text = m.text ?? "";
    if (m.question) {
      const answered = (log.answers ?? {})[m.question.id] != null;
      text = `${m.question.kind ?? "Question"}: ${m.question.title}` + (m.question.choices ?? []).map((c, i) => `\n  ${i + 1}. ${c.label}${c.description ? ` — ${c.description}` : ""}`).join("")
        + (answered ? "\n(answered)" : "\n(open: answer it in the task's agent terminal)");
    }
    return { id: m.id, who: m.who === "you" || m.by === "human" ? "you" : "agent", text, at: m.at };
  });
  return { chat: { messages, finished: true } };
}
async function loadSources() {
  try {
    const extra = JSON.parse(await fs.readFile(`${appHome()}/shepherdr-sources.json`, "utf8"));
    for (const x of Array.isArray(extra) ? extra : []) if (x?.id && !SOURCES.some((s) => s.id === x.id)) SOURCES.push({ via: null, ...x });
  } catch {}
}
const API = "http://127.0.0.1:7787/api";
const STATUS_WORD = { need: "needs you", ready: "ready", merging: "merging", work: "working", idle: "idle", queued: "queued", todo: "to do", done: "done", off: "offline" };
const OPEN = ["need", "ready", "merging", "work", "queued", "todo"];

async function call(src, path, body) {
  if (src.tasks3) {
    if (body !== undefined) throw new Error("Use the Shepherdr layout (Ctrl+Shift+L) to act on tasks");
    if (path === "snapshot") return tasks3Snapshot(src.tasks3);
    if (path.startsWith("chat?chat=")) {
      const key = decodeURIComponent(path.slice(10));
      const snap = await tasks3Snapshot(src.tasks3), k = snap.tasks.find((x) => x.agent.chat === key);
      const text = k ? `${k.name}\n\n${k.worktree ? `Worktree: \`${k.worktree}\` (branch ${k.branch})\n\n` : ""}${k.agent.address ? `Its agent is ${k.agent.address}: the task row opens its terminal.` : "No live agent: start it from the Shepherdr layout (Ctrl+Shift+L)."}\n\n${k.desc ?? ""}` : "Open the Shepherdr layout (Ctrl+Shift+L) for this space's Assistant.";
      return { chat: { messages: [{ id: "t3", who: "agent", text, at: 0 }], finished: true } };
    }
    return {};
  }
  if (src.legacy) {
    if (body !== undefined) throw new Error("The board from before 2.1 is read-only here; answer in the task's agent terminal");
    if (path === "snapshot") return legacySnapshot(src.legacy);
    if (path.startsWith("chat?chat=")) return legacyChat(src.legacy, decodeURIComponent(path.slice(10)));
    if (path.startsWith("question") || path.startsWith("artifact")) return {};
    throw new Error("Not available for the board from before 2.1");
  }
  const url = `${API}/${path}`;
  if (!src.via) {
    const r = await fetch(url, body === undefined ? undefined : { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok || j?.error) throw new Error(j?.error ?? `HTTP ${r.status}`);
    return j;
  }
  const args = ["-sS", "-m", "20", "-w", "\n%{http_code}"];
  if (body !== undefined) args.push("-X", "POST", "-H", "content-type: application/json", "--data-binary", JSON.stringify(body));
  args.push(url);
  const r = await exec("curl", args, { on: src.via });
  const out = String(r.stdout ?? ""), at = out.lastIndexOf("\n");
  const code = Number(out.slice(at + 1)), text = out.slice(0, at);
  if (!code) throw new Error(oneLine(r.stderr || "no answer", 120));
  const j = (() => { try { return JSON.parse(text); } catch { return {}; } })();
  if (code >= 400 || j?.error) throw new Error(j?.error ?? `HTTP ${code}`);
  return j;
}
const get = (src, path) => call(src, path);
const act = (src, action) => call(src, "action", action);
const taskChat = (k) => k?.agent?.chat ?? k?.chat ?? (k?.id ? `${k.id}/Agent` : null);
const oneLine = (s, n = 140) => { const t = String(s ?? "").replace(/\s+/g, " ").trim(); return t.length > n ? t.slice(0, n - 1) + "…" : t; };
const clock = (ms) => ms ? new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "";

export default class Shepherdr extends View {
  init(_props, cx) {
    this.state = Object.fromEntries(SOURCES.map((s) => [s.id, { snap: null, err: null }]));
    this.sel = null; // { src, key }
    this.chat = null; this.sending = false; this.note = null; this.mine = new Set(); this.docs = {}; this.view = null;
    this.tick = 0;
    if (props()?.view === "monitor") cx.spawn(async (cx) => { for (;;) { await new Promise((ok) => cx.timer.after(240, ok)); this.tick++; cx.notify(); } });
    if (props()?.view === "monitor") cx.spawn(async () => { for (;;) { if (this.state.local?.snap) await this.pollMacGpu(); await new Promise((ok) => cx.timer.after(5000, ok)); } });
    this.input = InputState.new({ value: "", placeholder: "Message… (Enter sends)" });
    this.input.on("submit", (_e, cx) => cx.spawn(async (cx) => { await this.send(); cx.notify(); }));
    this.handle = cx.focus_handle();
    cx.spawn(async (cx) => {
      await loadSources();
      for (const x of SOURCES) this.state[x.id] ??= { snap: null, err: null };
      cx.notify();
    for (const src of SOURCES) {
        cx.spawn(async (cx) => {
          for (;;) {
            const st = this.state[src.id];
            try { st.snap = await get(src, "snapshot"); st.err = null; }
            catch (e) { st.err = e.message; }
            // props {board: {src, place: name}}: open on that space's task board
            const b = props()?.board;
            if (b && !this.boardDone && b.src === src.id && st.snap) {
              const pl = st.snap.places.find((p) => p.name === b.place);
              if (pl) { this.sel = { src: src.id, key: pl.chat }; this.view = { tasks: true }; this.boardDone = true; }
            }
            if (!this.sel && st.snap?.places?.[0]) this.sel = { src: src.id, key: st.snap.places[0].chat };
            cx.notify();
            await new Promise((ok) => cx.timer.after(src.via ? 4000 : 2500, ok));
          }
        });
      }
    });
    cx.spawn(async (cx) => {
      for (;;) {
        await this.loadChat();
        cx.notify();
        await new Promise((ok) => cx.timer.after(2500, ok));
      }
    });
  }
  src(id) { return SOURCES.find((s) => s.id === id); }
  async loadChat() {
    const sel = this.sel; if (!sel) return;
    try { const c = (await get(this.src(sel.src), `chat?chat=${encodeURIComponent(sel.key)}`)).chat; if (this.sel === sel) this.chat = c; } catch {}
  }

  open(src, key, cx) {
    // 3.0 tasks: a task with a live agent opens that agent's terminal (its space and tab), like Herdr's agent list.
    const k = (this.state[src]?.snap?.tasks ?? []).find((x) => x.agent?.chat === key);
    if (k?.agent?.address) { const a = (agents() ?? []).find((x) => x.address === k.agent.address);
      if (a) { run("space.select", { id: a.space }); run("tab.select", { id: a.tab }); run("agent.focus", { terminal: a.address }); } }
    this.sel = { src, key }; this.chat = null; this.note = null; this.view = null;
    cx.spawn(async (cx) => {
      await this.loadChat();
      act(this.src(src), { type: "read", chats: { [key]: Date.now() } }).catch(() => {});
      cx.notify();
    });
    this.input.focus();
    cx.notify();
  }

  async send() {
    const text = this.input.value().trim();
    const sel = this.sel;
    if (!text || !sel || this.sending) return;
    this.sending = true;
    try {
      const cid = `hd${Date.now()}`; this.mine.add(cid);
      await act(this.src(sel.src), { type: "send", chat: sel.key, text, cid, human: true });
      this.input.set_value(""); this.note = null;
      await this.loadChat();
    } catch (e) { this.note = `Not sent: ${e.message}`; }
    this.sending = false;
  }

  answer(q, i, cx) {
    const sel = this.sel;
    cx.spawn(async (cx) => {
      try { await act(this.src(sel.src), { type: "answer", chat: sel.key, question: q.id, choice: i }); this.note = null; await this.loadChat(); }
      catch (e) { this.note = `Answer not sent: ${e.message}`; }
      cx.notify();
    });
  }

  // What a chat is: its space, and its task when it is a task's agent.
  whatIs(sel) {
    const snap = sel && this.state[sel.src]?.snap; if (!snap) return {};
    const place = snap.places.find((p) => p.chat === sel.key);
    if (place) return { place, label: place.name };
    const task = snap.tasks.find((t) => taskChat(t) === sel.key);
    const tplace = task && snap.places.find((p) => p.id === task.place);
    return { task, place: tplace, label: task ? `${task.id} · ${task.name}` : sel.key };
  }
  // Its terminal in Herdr: the agent under that Shepherdr's Herdr machine, in the space of the same name;
  // a task's by its id in the tab name, a coordinator's the space's first tab.
  terminalFor(sel) {
    const { place, task } = this.whatIs(sel); if (!place) return null;
    const m = this.src(sel.src).herdr;
    // herdr null: this computer's own core (addresses without a machine prefix).
    const here = (agents() ?? []).filter((a) => (m ? a.address?.startsWith(`${m}/`) : !String(a.address ?? "").includes("/")) && a.space_name === place.name);
    if (task) return here.find((a) => (a.tab_name ?? "").includes(task.id) || (a.title ?? "").includes(task.id)) ?? null;
    return here.sort((a, b) => a.tab_index - b.tab_index)[0] ?? null;
  }

  // ------------------------------------------------------------------ left column
  dot(t, status, size = 7) {
    const c = { need: t.blocked, ready: t.done, merging: t.working, work: t.working, queued: t.dim, todo: t.dim, done: t.done, off: t.blocked, idle: t.border };
    return div().w(size).h(size).flex_none().bg(c[status] ?? t.dim);
  }
  row(t, id, src, key, left, right, opts = {}) {
    const sel = key && this.sel && this.sel.src === src && this.sel.key === key;
    const bar = sel ? t.accent : opts.alert ? t.blocked : null;
    return h_flex().id(id).w_full().min_w(0).h(opts.h ?? 24).pl(opts.indent ?? 10).pr(10).gap(7).items_center().cursor_pointer()
      .border_l(2).border_color(bar ?? "#00000000")
      .when(sel, (e) => e.bg(t.selected))
      .when(!sel, (e) => e.hover((s) => s.bg(t.hover)))
      .on_click((_e, cx) => key && this.open(src, key, cx))
      .children(left)
      .when(Boolean(right), (e) => e.child(div().flex_none().text_size(10.5).text_color(opts.alert ? t.blocked : t.dim).child(right)));
  }
  heading(t, text, extra, big) {
    return h_flex().w_full().px(10).pt(big ? 16 : 10).pb(4).items_center().text_size(big ? 12.5 : 11).text_color(big ? t.foreground : t.muted)
      .when(big, (e) => e.font_semibold().border_t(2).border_color(t.border))
      .child(div().flex_1().min_w(0).truncate().child(text)).when(Boolean(extra), (e) => e.child(div().text_color(t.blocked).child(extra)));
  }
  folded(key) { try { return JSON.parse(localStorage.getItem("fold") ?? "{}")[key] ?? null; } catch { return null; } }
  setFold(key, v, cx) {
    let f = {}; try { f = JSON.parse(localStorage.getItem("fold") ?? "{}"); } catch {}
    f[key] = v; localStorage.setItem("fold", JSON.stringify(f)); cx.notify();
  }
  // "6h", "1h 20m", "45m"
  dur(min) { if (min == null) return null; const m = Math.max(0, Math.round(min)); return m >= 60 ? `${Math.floor(m / 60)}h${m % 60 ? ` ${m % 60}m` : ""}` : `${m}m`; }
  // a running task's own estimate, as "35% · 4h"
  est(k) { const r = k.report; if (!r) return null; const parts = [r.progress != null ? `${r.progress}%` : null, r.eta != null ? (r.eta === 0 ? "<1m" : this.dur(r.eta)) : null].filter(Boolean); return parts.length ? parts.join(" · ") : null; }
  // when a queued task should start: the longest remaining estimate among the tasks it waits for (chains followed)
  startsIn(s, k, seen = new Set()) {
    if (!k.after?.length || seen.has(k.id)) return null; seen.add(k.id);
    let worst = 0, known = false;
    for (const a of k.after) {
      const id = String(a).replace(/^.*:/, ""), dep = (s.tasks ?? []).find((x) => x.id === id);
      if (!dep || dep.status === "done") continue;
      const own = dep.report?.eta ?? null, before = dep.status === "queued" || dep.status === "todo" ? this.startsIn(s, dep, seen) : 0;
      if (own == null || before == null) return null;
      worst = Math.max(worst, before + own); known = true;
    }
    return known ? worst : 0;
  }
  counts(t, s, p) {
    const ks = (s.tasks ?? []).filter((k) => k.place === p.id && !k.archived);
    const all = (s.tasks ?? []).filter((k) => k.place === p.id);
    const q = ks.filter((k) => k.status === "queued" || k.status === "todo").length, d = ks.filter((k) => k.status === "done").length;
    const a = all.filter((k) => k.archived).length;
    const parts = [q ? `${q} queued` : null, d ? `${d} done` : null, a ? `${a} archived` : null].filter(Boolean);
    return parts.length ? [div().flex_none().text_size(10).text_color(t.dim).child(parts.join(" · "))] : [];
  }
  // What waits on the owner: Shepherdr's needs plus its open questions (`asks`, keyed by chat), one per chat.
  waiting(snap, src) {
    const out = [...(snap?.needs ?? [])], seen = new Set(out.map((n) => n.chat));
    for (const [chat, qid] of Object.entries(snap?.asks ?? {})) {
      if (seen.has(chat)) continue; seen.add(chat);
      const place = (snap.places ?? []).find((p) => p.chat === chat);
      const task = (snap.tasks ?? []).find((k) => taskChat(k) === chat);
      const title = src ? this.askTitle(src, chat, qid) : null;
      out.push({ id: "ask", chat, place: place?.id ?? task?.place, task: task?.id,
        text: title ?? (place ? `${place.name} coordinator asks you something` : "asks you something"), at: 0 });
    }
    return out;
  }
  // An open question's title, read once from its chat and cached by question id.
  askTitle(src, chat, qid) {
    this.askTitles ??= {};
    const key = `${src.id}:${qid}`;
    if (!(key in this.askTitles)) {
      this.askTitles[key] = null;
      get(src, `chat?chat=${encodeURIComponent(chat)}`).then((r) => {
        const m = (r?.chat?.messages ?? []).find((x) => x.question?.id === qid);
        this.askTitles[key] = m?.question?.title ?? null;
      }).catch(() => {});
    }
    return this.askTitles[key];
  }

  sourceRows(t, src, out) {
    const st = this.state[src.id], s = st.snap;
    const needs = this.waiting(s, src);
    const closed = this.folded(`src:${src.id}`) === true;
    out.push(h_flex().id(`h-${src.id}`).w_full().px(10).pt(10).pb(5).gap(6).items_center().cursor_pointer()
      .border_t(1).border_color(t.border).hover((e) => e.bg(t.hover))
      .on_click((_e, cx) => this.setFold(`src:${src.id}`, !closed, cx))
      .child(glyph(closed ? "chevron-right" : "chevron-down", 10))
      .child(div().flex_1().min_w(0).truncate().text_size(11).font_semibold().text_color(t.muted).child(src.name.toUpperCase()))
      .when(Boolean(st.err) && Boolean(s || src.via), (e) => e.child(div().text_size(11).text_color(t.blocked).child(s ? "stale" : "offline")))
      .when(needs.length > 0, (e) => e.child(div().px(6).bg(t.blocked).text_color(t.background).text_size(11).font_semibold().child(String(needs.length)))));
    if (closed) return;
    if (!s) { const none = st.err && !src.via;
      out.push(div().px(10).py(4).text_size(12).text_color(none ? t.dim : st.err ? t.blocked : t.dim)
        .child(none ? "No Shepherdr running on this computer." : st.err ? `Can't reach it: ${oneLine(st.err, 90)}` : "Connecting…")); return; }
    const last = s.last ?? {}, read = s.read ?? {};
    const unread = (key) => (last[key] ?? 0) > (read[key] ?? read["*"] ?? 0);
    // Needs you first: the one thing to act on.
    needs.forEach((n, i) => {
      const what = n.question?.title ?? n.secret?.name ?? n.text ?? "Waiting";
      out.push(this.row(t, `${src.id}-need-${i}`, src.id, n.chat, [this.dot(t, "need"),
        div().flex_1().min_w(0).truncate().text_size(12).text_color(t.foreground).child(`${n.task ? n.task + " · " : ""}${oneLine(what, 80)}`)], "needs you", { alert: true }));
    });
    const quiet = [];
    for (const p of s.places ?? []) {
      const tasks = (s.tasks ?? []).filter((x) => x.place === p.id && !x.archived && OPEN.includes(x.status))
        .sort((a, b) => OPEN.indexOf(a.status) - OPEN.indexOf(b.status));
      const busy = p.status !== "idle" || tasks.length || unread(p.chat) || (this.sel?.src === src.id && this.sel.key === p.chat);
      if (!busy) { quiet.push(p); continue; }
      out.push(this.row(t, `${src.id}-p-${p.id}`, src.id, p.chat, [this.dot(t, p.status),
        div().flex_1().min_w(0).truncate().text_size(13).when(unread(p.chat), (e) => e.font_semibold()).child(p.name),
        ...this.counts(t, s, p)],
        p.status === "idle" ? null : (STATUS_WORD[p.status] ?? p.status)));
      for (const k of tasks) {
        out.push(this.row(t, `${src.id}-t-${k.id}`, src.id, taskChat(k), [this.dot(t, k.status),
          div().flex_none().text_size(11.5).text_color(t.dim).child(k.id),
          div().flex_1().min_w(0).truncate().text_size(12.5).when(Boolean(unread(taskChat(k))), (e) => e.font_semibold()).child(k.name)],
          (k.status === "work" || k.status === "merging") ? (this.est(k) ?? STATUS_WORD[k.status])
            : (k.status === "queued" || k.status === "todo") ? (() => { const m = this.startsIn(s, k); return m == null ? STATUS_WORD[k.status] : m === 0 ? "next" : `starts ~${this.dur(m)}`; })()
            : STATUS_WORD[k.status], { indent: 26, h: 24 }));
      }
    }
    if (quiet.length) {
      const open = this.folded(`quiet:${src.id}`) === false;
      out.push(h_flex().id(`q-${src.id}`).w_full().h(24).px(10).gap(6).items_center().cursor_pointer().text_size(11.5).text_color(t.dim)
        .hover((e) => e.bg(t.hover)).on_click((_e, cx) => this.setFold(`quiet:${src.id}`, open, cx))
        .child(div().w(10).child(open ? "▾" : "▸")).child(`${quiet.length} quiet space${quiet.length > 1 ? "s" : ""}`));
      if (open) for (const p of quiet) out.push(this.row(t, `${src.id}-p-${p.id}`, src.id, p.chat, [this.dot(t, p.status),
        div().flex_1().min_w(0).truncate().text_size(12.5).text_color(t.muted).child(p.name)], null, { indent: 26, h: 24 }));
    }
  }
  sidebar(t) {
    const out = [];
    const needs = SOURCES.reduce((n, src) => n + this.waiting(this.state[src.id].snap).length, 0);
    out.push(h_flex().w_full().h(36).flex_none().px(10).gap(8).items_center().bg(t.chrome).border_b(1).border_color(t.border)
      .child(div().text_color(t.accent).child(crook(14))).child(div().flex_1().text_size(12.5).font_semibold().child("Shepherdr"))
      .child(h_flex().id("open-monitor").h(20).px(6).items_center().border_1().border_color(t.border).text_size(10.5).cursor_pointer()
        .hover((e) => e.bg(t.hover)).on_click(() => openShepherdr("monitor")).child("Monitor"))
      .when(needs > 0, (e) => e.child(div().px(6).bg(t.blocked).text_color(t.background).text_size(10.5).font_semibold().child(`${needs} need you`))));
    for (const src of SOURCES) this.sourceRows(t, src, out);
    const compact = Boolean(props()?.compact);
    return v_flex().id("shep-side").when(!compact, (e) => e.w(330).flex_none().h_full().border_r(2))
      .when(compact, (e) => e.w_full().flex_1().border_b(2)).min_h(0).overflow_y_scroll().bg(t.chrome).border_color(t.border).pb(10).children(out);
  }

  // ------------------------------------------------------------------ chat
  message(t, m, i) {
    const you = m.who === "you";
    const box = v_flex().w_full().px(14).py(4).gap(3);
    if (m.newSession) box.child(h_flex().gap(8).items_center().py(4).child(div().flex_1().h(1).bg(t.border))
      .child(div().text_size(10.5).text_color(t.dim).child("new session")).child(div().flex_1().h(1).bg(t.border)));
    if (m.card) {
      return box.child(h_flex().gap(8).items_center().py(1)
        .child(this.dot(t, m.card.status ?? (m.card.health ? "need" : "idle"), 6))
        .child(div().flex_1().min_w(0).truncate().text_size(11.5).text_color(t.muted).child(oneLine(m.card.text, 200)))
        .child(div().flex_none().text_size(10.5).text_color(t.dim).child(clock(m.at))));
    }
    const user = this.state[this.sel?.src]?.snap?.user;
    const body = v_flex().w_full().gap(4).pl(10).border_l(2).border_color(you ? t.accent : t.border)
      .child(h_flex().gap(8).items_center().text_size(10.5)
        .child(div().font_semibold().text_color(you ? t.accent : t.muted).child(you ? (user || "you") : "agent"))
        .child(div().text_color(t.dim).child(clock(m.at)))
        .when(Boolean(m.waiting), (e) => e.child(div().text_color(t.working).child(m.waitsFor === "start" ? "starting…" : "waiting"))));
    if (m.answered) body.child(div().text_size(11.5).text_color(t.dim).child(m.answered));
    if (m.text) body.child(div().w_full().text_size(12.5).line_height(1.45).text_color(t.foreground).child(plain(m.text)));
    if (m.file) body.child(h_flex().gap(6).items_center().text_size(11.5).text_color(t.muted).child(glyph("file-text", 12)).child(m.file.name));
    if (m.artifact) body.child(this.artifactCard(t, m.artifact, false));
    if (m.question) body.child(this.question(t, m.question, i));
    if (m.secret) body.child(div().text_size(11.5).text_color(t.blocked).child(`asks for a secret: ${m.secret.name ?? ""} · shepherdr secret set`));
    return box.child(body);
  }
  question(t, q, i) {
    const open = q.sent === undefined && !q.closed;
    const card = v_flex().w_full().gap(6).p(10).border_1().border_color(open ? t.accent : t.border).bg(t.surface)
      .child(h_flex().gap(8).items_center()
        .child(div().px(5).text_size(10).font_semibold().bg(open ? t.accent : t.border).text_color(open ? t.accent_foreground : t.foreground).child(q.kind.toUpperCase()))
        .child(div().flex_1().min_w(0).text_size(12.5).font_semibold().child(q.title)));
    if (q.plan) card.child(div().text_size(11.5).text_color(t.muted).child(oneLine(plain(q.plan), 600)));
    if (q.steps?.length) card.child(v_flex().gap(2).children(q.steps.map((s, n) => div().text_size(11.5).text_color(t.muted).child(`${n + 1}. ${s}`))));
    if (!open) return card.child(div().text_size(11).text_color(t.dim).child(q.closed ? `closed · ${q.closed}` : `answered · ${q.choices[q.sent]?.label ?? q.answer ?? "yes"}`));
    if (q.multi) return card.child(div().text_size(11).text_color(t.dim).child("several choices: answer in its terminal"));
    return card.child(v_flex().gap(3).children(q.choices.map((c, n) =>
      h_flex().id(`q-${i}-${n}`).px(8).h(26).gap(8).items_center().border_1().border_color(t.border).bg(t.raised).cursor_pointer()
        .hover((s) => s.bg(t.hover).border_color(t.accent)).on_click((_e, cx) => this.answer(q, n, cx))
        .child(div().w(16).flex_none().text_size(11).font_semibold().text_color(t.accent).child(String(n + 1)))
        .child(div().flex_none().text_size(12).child(c.label))
        .when(Boolean(c.description), (e) => e.child(div().flex_1().min_w(0).truncate().text_size(11).text_color(t.dim).child(c.description))))));
  }
  // ------------------------------------------------------------------ artifacts (herdr.doc/1 drawn natively)
  artifactMeta(id) { return (this.state[this.sel?.src]?.snap?.artifacts ?? []).find((a) => a.id === id); }
  docFor(src, id, v) {
    const key = `${src}:${id}:${v ?? ""}`;
    if (!(key in this.docs)) {
      this.docs[key] = undefined;
      get(this.src(src), `artifacts/${encodeURIComponent(id)}/data${v ? `?v=${v}` : ""}`)
        .then((r) => { this.docs[key] = r?.data ?? null; }).catch(() => { this.docs[key] = null; });
    }
    return this.docs[key];
  }
  async openInBrowser(src, id, v) {
    const s = this.src(src);
    try {
      if (!s.via) { await exec("xdg-open", [`http://127.0.0.1:7787/api/artifacts/${encodeURIComponent(id)}?full=1${v ? `&v=${v}` : ""}`]); return; }
      // the other machine's Shepherdr only listens there: fetch the page and its data, keep a local copy, open that
      const a = await get(s, `artifacts/${encodeURIComponent(id)}/raw${v ? `?v=${v}` : ""}`);
      const html = String(a?.html ?? "");
      const boot = `<script>window.shepherdr={data:${JSON.stringify(a?.data ?? null)},send(){},sent:0};</script>`;
      const dir = `${appHome()}/data/shepherdr-artifacts`; await fs.mkdir(dir, { recursive: true });
      const file = `${dir}/${src}-${id}-v${a?.v ?? v ?? 0}.html`;
      await fs.writeFile(file, html.includes("<head>") ? html.replace("<head>", "<head>" + boot) : boot + html);
      await exec("xdg-open", [file]);
    } catch (e) { this.note = `Can't open it: ${e.message}`; }
  }
  artifactCard(t, post, full) {
    const src = this.sel?.src, meta = this.artifactMeta(post.id);
    const v = post.v ?? meta?.v, d = this.docFor(src, post.id, v);
    const btn = (id, text, on) => h_flex().id(id).h(20).px(6).items_center().border_1().border_color(t.border).text_size(10.5)
      .cursor_pointer().hover((s) => s.bg(t.hover)).on_click(on).child(text);
    const head = h_flex().w_full().gap(8).items_center().px(8).py(5).bg(t.chrome).border_b(1).border_color(t.border)
      .child(glyph("layers", 12)).child(div().flex_1().min_w(0).truncate().text_size(12).font_semibold().child(d?.title ?? meta?.title ?? post.id))
      .when(Boolean(d?.status), (e) => e.child(this.tag(t, d.status)))
      .child(div().flex_none().text_size(10.5).text_color(t.dim).child(`v${v ?? "?"}`))
      .when(!full, (e) => e.child(btn(`ao-${post.id}-${v}`, "Expand", (_e, cx) => { this.view = { artifact: post.id, v }; cx.notify(); })))
      .child(btn(`ab-${post.id}-${v}`, "Browser", (_e, cx) => cx.spawn(async (cx) => { await this.openInBrowser(src, post.id, v); cx.notify(); })));
    const card = v_flex().w_full().border_1().border_color(t.border).bg(t.surface).child(head);
    if (d === undefined) return card.child(div().p(8).text_size(11).text_color(t.dim).child("Loading…"));
    if (!d || d.format !== "herdr.doc/1") return card.child(div().p(8).text_size(11).text_color(t.dim)
      .child("An HTML artifact: open it in the browser."));
    const blocks = d.blocks ?? [];
    const shown = full ? blocks : blocks.slice(0, 6);
    const body = v_flex().w_full().p(10).gap(8);
    if (d.subtitle) body.child(div().text_size(11).text_color(t.dim).child(d.subtitle));
    for (const b of shown) { try { const el = this.block(t, b); if (el) body.child(el); } catch {} }
    if (!full && blocks.length > shown.length) body.child(div().text_size(11).text_color(t.dim).child(`${blocks.length - shown.length} more…`));
    return card.child(body);
  }
  tag(t, status) {
    const c = { ready: t.done, ok: t.done, done: t.done, work: t.working, warn: t.working, need: t.blocked, bad: t.blocked, fail: t.blocked, info: t.accent }[status] ?? t.dim;
    return div().flex_none().px(5).bg(c).text_color(t.background).text_size(10).font_semibold().child(String(status).toUpperCase());
  }
  block(t, b) {
    const tone = (x) => ({ ok: t.done, warn: t.working, bad: t.blocked, info: t.accent })[x] ?? t.foreground;
    const S = (v) => (v == null ? "" : String(v));
    switch (b?.type) {
      case "heading": return div().w_full().pt(4).pb(2).border_b(1).border_color(t.border).text_size(11).font_semibold().text_color(t.muted).child(S(b.text).toUpperCase());
      case "text": return div().w_full().text_size(12.5).line_height(1.45).child(S(b.text));
      case "divider": return div().w_full().h(1).bg(t.border);
      case "stats": return h_flex().w_full().gap(6).flex_wrap().children((b.items ?? []).map((i) =>
        v_flex().min_w(84).px(8).py(5).border_1().border_color(t.border).bg(t.background)
          .child(div().text_size(18).font_semibold().text_color(tone(i.tone)).child(S(i.value)))
          .child(div().text_size(10).text_color(t.dim).child(S(i.label).toUpperCase()))
          .when(Boolean(i.note), (e) => e.child(div().text_size(10).text_color(t.dim).child(S(i.note))))));
      case "kv": return v_flex().w_full().gap(2).children((b.items ?? []).map(([k, v]) =>
        h_flex().gap(10).text_size(12).child(div().w(110).flex_none().truncate().text_color(t.dim).child(S(k))).child(div().flex_1().min_w(0).child(S(v)))));
      case "list": return v_flex().w_full().gap(2).children((b.items ?? []).map((i, n) =>
        h_flex().gap(8).text_size(12.5).child(div().w(16).flex_none().text_color(t.dim).child(b.style === "number" ? `${n + 1}.` : "·")).child(div().flex_1().min_w(0).child(S(i)))));
      case "checks": return v_flex().w_full().gap(2).children((b.items ?? []).map((i) => {
        const [m, c] = i.ok === true ? ["✓", t.done] : i.ok === false ? ["✗", t.blocked] : ["–", t.dim];
        return h_flex().gap(8).text_size(12.5).child(div().w(14).flex_none().font_semibold().text_color(c).child(m))
          .child(div().flex_1().min_w(0).child(S(i.text))).when(Boolean(i.note), (e) => e.child(div().flex_none().text_size(11).text_color(t.dim).child(S(i.note))));
      }));
      case "steps": return v_flex().w_full().gap(2).children((b.items ?? []).map((i, n) => {
        const [m, c] = { done: ["✓", t.done], now: ["▶", t.working], skip: ["–", t.dim] }[i.state] ?? [String(n + 1), t.dim];
        return h_flex().gap(8).text_size(12.5).child(div().w(14).flex_none().font_semibold().text_color(c).child(m))
          .child(div().flex_1().min_w(0).text_color(i.state === "todo" ? t.muted : t.foreground).child(S(i.text)));
      }));
      case "table": {
        const cols = b.columns ?? [], al = b.align ?? [];
        const cell = (v, i, head) => div().flex_1().min_w(0).truncate().text_size(head ? 10.5 : 12).text_color(head ? t.dim : t.foreground)
          .when(al[i] === "right", (e) => e.text_right()).child(S(head ? S(v).toUpperCase() : v));
        return v_flex().w_full().child(h_flex().gap(8).pb(2).border_b(1).border_color(t.border).children(cols.map((c, i) => cell(c, i, true))))
          .children((b.rows ?? []).slice(0, 50).map((r) => h_flex().gap(8).py(1).border_b(1).border_color(t.surface).children(r.map((c, i) => cell(c, i, false)))));
      }
      case "bars": case "progress": {
        const items = b.type === "progress" ? [{ label: b.label, value: b.value, tone: b.tone }] : (b.items ?? []);
        const max = b.max || Math.max(1, ...items.map((i) => +i.value || 0));
        return v_flex().w_full().gap(3).children(items.map((i) => h_flex().gap(8).items_center().text_size(11.5)
          .child(div().w(100).flex_none().truncate().text_color(t.muted).child(S(i.label)))
          .child(div().flex_1().h(9).bg(t.background).child(div().h_full().w(`${Math.max(0, Math.min(100, ((+i.value || 0) / max) * 100))}%`).bg(i.tone ? tone(i.tone) : t.accent)))
          .child(div().w(70).flex_none().text_right().text_color(t.dim).child(`${S(i.value)}${b.unit ? " " + b.unit : ""}`))));
      }
      case "code": case "diff": {
        const lines = S(b.text).split("\n").slice(0, 120);
        const col = (l) => b.type !== "diff" ? t.foreground : l.startsWith("+") ? t.done : l.startsWith("-") ? t.blocked : l.startsWith("@@") ? t.accent : t.muted;
        return v_flex().w_full().gap(0)
          .when(Boolean(b.file), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(S(b.file))))
          .child(v_flex().w_full().p(6).bg(t.background).border_l(2).border_color(t.border)
            .children(lines.map((l) => div().w_full().text_size(11.5).text_color(col(l)).child(l || " "))));
      }
      case "callout": return div().w_full().px(8).py(6).bg(t.background).border_l(3).border_color(tone(b.tone ?? "info")).text_size(12.5).child(S(b.text));
      case "links": return v_flex().gap(2).children((b.items ?? []).map((i, n) => div().id(`lnk-${n}-${S(i.url).slice(0, 40)}`).text_size(12).text_color(t.accent)
        .cursor_pointer().on_click(() => exec("xdg-open", [S(i.url)]).catch(() => {})).child(S(i.label || i.url))));
      default: return null;
    }
  }
  artifactsView(t) {
    const sel = this.sel, snap = this.state[sel?.src]?.snap;
    const { place } = this.whatIs(sel);
    const list = (snap?.artifacts ?? []).filter((a) => !place || a.place === place.id || place.home).sort((a, b) => b.at - a.at);
    if (this.view?.artifact) {
      return v_flex().w_full().p(10).gap(8)
        .child(h_flex().id("art-back").gap(6).items_center().text_size(11).text_color(t.muted).cursor_pointer().on_click((_e, cx) => { this.view = { list: true }; cx.notify(); })
          .child(glyph("chevron-left", 10)).child("ARTIFACTS"))
        .child(this.artifactCard(t, { id: this.view.artifact, v: this.view.v }, true));
    }
    if (!list.length) return div().p(20).text_size(12).text_color(t.dim).child("No artifacts here yet.");
    return v_flex().w_full().py(4).children(list.map((a) => h_flex().id(`al-${a.id}`).w_full().h(30).px(12).gap(8).items_center().cursor_pointer()
      .hover((s) => s.bg(t.hover)).on_click((_e, cx) => { this.view = { artifact: a.id, v: a.v }; cx.notify(); })
      .child(glyph("layers", 12)).child(div().flex_1().min_w(0).truncate().text_size(12.5).child(a.title))
      .child(div().flex_none().text_size(10.5).text_color(t.dim).child(`v${a.v} · ${a.when}`))));
  }

  // A space's tasks as a board: what needs you, what works, what's ready, the backlog, and what's done.
  tasksView(t) {
    const sel = this.sel, snap = this.state[sel?.src]?.snap;
    const { place } = this.whatIs(sel);
    const inPlace = (k) => (!place || place.home ? (!place || k.place === place.id || place.level < 2) : k.place === place.id);
    const archived = (snap?.tasks ?? []).filter((k) => k.archived && inPlace(k)).sort((a, b) => (b.archived ?? 0) - (a.archived ?? 0));
    const all = (snap?.tasks ?? []).filter((k) => !k.archived && inPlace(k));
    const groups = [
      ["NEEDS YOU", all.filter((k) => k.status === "need"), t.blocked],
      ["WORKING", all.filter((k) => k.status === "work" || k.status === "merging"), t.working],
      ["READY", all.filter((k) => k.status === "ready"), t.done],
      ["BACKLOG", all.filter((k) => k.status === "queued" || k.status === "todo"), t.muted],
      ["DONE", all.filter((k) => k.status === "done").sort((a, b) => (b.doneAt ?? 0) - (a.doneAt ?? 0)), t.dim],
      ["ARCHIVED", archived, t.raised],
    ];
    const when = (ms) => { if (!ms) return ""; const d = new Date(ms), now = new Date();
      return d.toDateString() === now.toDateString() ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : d.toLocaleDateString([], { day: "numeric", month: "short" }); };
    const out = [];
    if (!all.length && !archived.length) return div().p(20).text_size(12).text_color(t.dim).child("No tasks in this space yet.");
    for (const [name, list, col] of groups) {
      if (!list.length) continue;
      const fkey = `${name === "DONE" ? "done" : "arch"}:${sel.src}:${place?.id}`;
      const foldable = name === "DONE" || name === "ARCHIVED";
      const fold = foldable && (name === "ARCHIVED" ? this.folded(fkey) !== false : this.folded(fkey) === true);
      out.push(h_flex().id(`tg-${name}`).w_full().px(12).pt(10).pb(4).gap(8).items_center()
        .when(foldable, (e) => e.cursor_pointer().on_click((_e, cx) => this.setFold(fkey, !fold, cx)))
        .child(div().size(6).bg(col)).child(div().text_size(10.5).font_semibold().text_color(t.muted).child(`${name} · ${list.length}`))
        .child(div().flex_1().h(1).bg(t.border))
        .when(foldable, (e) => e.child(div().text_size(10).text_color(t.dim).child(fold ? "show" : "hide"))));
      if (fold) continue;
      const old = name === "DONE" || name === "ARCHIVED";
      for (const k of list.slice(0, 60)) {
        const r = k.report;
        const startM = name === "BACKLOG" ? this.startsIn(snap, k) : null;
        const right = old ? [k.outcome && k.outcome !== "done" ? k.outcome.toUpperCase() : "", when(name === "ARCHIVED" ? (k.doneAt ?? k.archived) : k.doneAt)].filter(Boolean).join(" · ")
          : name === "BACKLOG" ? [startM == null ? null : startM === 0 ? "next" : `starts ~${this.dur(startM)}`, k.after?.length ? `after ${k.after.join(", ")}` : (k.status === "todo" ? "to do" : "queued")].filter(Boolean).join(" · ")
          : this.est(k) ?? k.age ?? "";
        out.push(v_flex().id(`tb-${k.id}`).w_full().px(12).py(4).gap(1).cursor_pointer().hover((e) => e.bg(t.hover))
          .on_click((_e, cx) => { if (taskChat(k)) { this.view = null; this.open(sel.src, taskChat(k), cx); } })
          .child(h_flex().gap(8).items_center()
            .child(div().w(64).flex_none().text_size(11).text_color(old ? t.dim : t.foreground).child(k.id))
            .child(div().flex_1().min_w(0).truncate().text_size(12).text_color(old ? t.muted : t.foreground).child(k.name))
            .child(div().flex_none().text_size(10.5).text_color(t.dim).child(right))
            .when(Boolean(k.pr), (e) => e.child(div().flex_none().text_size(10.5).text_color(t.accent).child(`#${k.pr.number}`))))
          .when(Boolean(r?.msg) && !old && name !== "BACKLOG", (e) => e.child(div().pl(72).truncate().text_size(11).text_color(t.dim).child(r.msg)))
          .when(old && Boolean(k.said || k.facts), (e) => e.child(div().pl(72).truncate().text_size(11).text_color(t.dim).child(oneLine(k.said || k.facts, 120)))));
      }
      if (old && list.length > 60) out.push(div().px(12).text_size(10.5).text_color(t.dim).child(`${list.length - 60} older not shown`));
    }
    return v_flex().w_full().pb(10).children(out);
  }

  chatPane(t) {
    const sel = this.sel, c = this.chat;
    const { label, task } = this.whatIs(sel);
    const term = sel ? this.terminalFor(sel) : null;
    const btn = (id, g, text, on, danger) => h_flex().id(id).h(22).px(7).gap(5).items_center().border_1().border_color(danger ? t.blocked : t.border)
      .text_size(11).text_color(danger ? t.blocked : t.foreground).cursor_pointer().hover((s) => s.bg(t.hover)).on_click(on).child(glyph(g, 11)).child(text);
    const state = c?.working ? (c.background ? "background" : "working") : c?.finished ? "finished" : null;
    const head = h_flex().w_full().h(36).flex_none().px(12).gap(8).items_center().border_b(1).border_color(t.border).bg(t.chrome)
      .when(Boolean(sel), (e) => e.child(div().flex_none().px(5).border_1().border_color(t.border).text_size(10).text_color(t.dim).child(this.src(sel.src).name)))
      .child(div().flex_1().min_w(0).truncate().text_size(13).font_semibold().child(label ?? "Shepherdr"))
      .when(Boolean(state), (e) => e.child(h_flex().gap(5).items_center().child(this.dot(t, state === "finished" ? "idle" : "work", 6))
        .child(div().text_size(10.5).text_color(state === "finished" ? t.dim : t.working).child(state))))
      .when(Boolean(sel), (e) => e.child(btn("tasks", "files", this.view?.tasks ? "Chat" : "Tasks", (_e, cx) => { this.view = this.view?.tasks ? null : { tasks: true }; cx.notify(); }))
        .child(btn("arts", "layers", this.view && !this.view.tasks ? "Chat" : "Artifacts", (_e, cx) => { this.view = this.view && !this.view.tasks ? null : { list: true }; cx.notify(); })))
      .when(Boolean(term), (e) => e.child(btn("go-term", "terminal", "Terminal", () => run("agent.focus", { terminal: term.address }))))
      .when(Boolean(c?.working), (e) => e.child(btn("stop", "x", "Stop", (_e, cx) => cx.spawn(async (cx) => {
        try { await act(this.src(sel.src), { type: "stop", chat: sel.key }); } catch (er) { this.note = er.message; } cx.notify(); }), true)));
    const msgs = c?.messages ?? [];
    const body = v_flex().id(`shep-chat-${sel?.src}-${sel?.key}`).flex_1().min_h(0).w_full().overflow_y_scroll().py(8);
    const empty = (text) => v_flex().flex_1().items_center().justify_center().gap(8).p(24).text_color(t.dim).child(crook(22)).child(div().text_size(12).child(text));
    if (sel && this.view?.tasks) body.child(this.tasksView(t));
    else if (sel && this.view) body.child(this.artifactsView(t));
    else if (!sel) body.child(empty("Pick a space or task."));
    else if (!c) body.child(empty("Loading…"));
    else if (!msgs.length) body.child(empty("No messages yet. Write below to start its coordinator."));
    else body.children(msgs.slice(-80).map((m, i) => this.message(t, m, i)));
    if (c?.live?.step) body.child(h_flex().px(14).py(4).gap(6).items_center().child(this.dot(t, "work", 5))
      .child(div().flex_1().min_w(0).truncate().text_size(11).text_color(t.dim).child(oneLine(c.live.step, 160))));
    // Shepherdr lists every screen's undelivered messages; show only the ones this panel sent.
    for (const f of (c?.failed ?? []).filter((f) => this.mine.has(f.cid))) body.child(div().px(14).py(2).text_size(11).text_color(t.blocked).child(`not delivered · ${f.error}`));
    const foot = v_flex().w_full().flex_none().px(10).py(8).gap(5).border_t(1).border_color(t.border).bg(t.chrome)
      .when(Boolean(this.note), (e) => e.child(div().text_size(11).text_color(t.blocked).child(this.note)))
      .child(h_flex().gap(0).items_center().border_1().border_color(t.border).bg(t.input)
        .child(div().pl(8).pr(4).text_size(13).text_color(t.accent).child("›"))
        .child(Input.new(this.input).flex_1().h(30).px(4).rounded(0).border_0().bg(t.input).text_size(12.5).text_color(t.foreground))
        .child(h_flex().id("send").h(30).px(12).items_center().bg(t.accent).text_color(t.accent_foreground).text_size(11.5).font_semibold().cursor_pointer()
          .hover((s) => s.opacity(0.9)).on_click((_e, cx) => cx.spawn(async (cx) => { await this.send(); cx.notify(); }))
          .child(this.sending ? "…" : "Send")));
    return v_flex().flex_1().min_w(0).min_h(0).when(!props()?.compact, (e) => e.h_full()).bg(t.background).child(head).child(body).when(Boolean(sel), (e) => e.child(foot));
  }

  // ------------------------------------------------------------------ Monitor: an operations console of what agents do
  seg(t, frac, n, color, moving) {
    const on = Math.round(Math.max(0, Math.min(1, frac)) * n);
    const head = moving && on > 0 ? this.tick % on : -1;
    const w = n > 20 ? 3 : 4, h = n > 20 ? 8 : 6;
    return h_flex().gap(1).children(Array.from({ length: n }, (_, i) => div().w(w).h(h)
      .when(i > 0 && i % Math.max(1, Math.round(n / 4)) === 0, (e) => e.ml(2))
      .bg(i === head ? t.foreground : i < on ? color : t.raised)));
  }
  sprite(st, paused) {
    const k = this.tick;
    const name = paused || st === "need" ? `wait${Math.floor(k / 3) % 2}` : st === "work" || st === "merging" ? `work${k % 4}`
      : st === "ready" || st === "done" ? `ready${Math.floor(k / 4) % 2}` : `queue${Math.floor(k / 5) % 2}`;
    return image(`mods/shepherdr/sprites/w32-${name}.svg`).w(64).h(64).flex_none();
  }
  cliIcon(cli) {
    const c = String(cli ?? "").toLowerCase().includes("codex") ? "codex" : "claude";
    const L = shared("layout");
    const path = L === "workbench" ? `workbench-icons/${c}.svg` : L === "fortress" ? `fortress-icons/${c === "codex" ? "book" : "dwarf"}.svg`
      : L === "pixel" ? (c === "codex" ? "win31-icons/dosapps.png" : "win31-icons/computer.png") : `pixel-icons/${c === "codex" ? "agent" : "claude"}.svg`;
    return image(path).size(14).flex_none();
  }
  meter(t, m) {
    const c = m.level === "over" ? t.blocked : m.level === "busy" ? t.working : t.done;
    return h_flex().gap(6).items_center().text_size(10.5)
      .child(div().w(40).text_color(t.dim).child(m.label ?? ({ memory: "MEM", disk: "DISK", cpu: "CPU", gpu: "GPU" })[String(m.name).toLowerCase()] ?? String(m.name).toUpperCase().replace(/\s+/g, "").slice(0, 5)))
      .child(this.seg(t, (m.pct ?? 0) / 100, 20, c))
      .child(div().w(m.value ? 56 : 34).text_right().text_color(c).child(m.value ?? `${Math.round(m.pct ?? 0)}%`));
  }
  // A Herdr agent as a Monitor node: its space and tab, status, program; a click shows its terminal.
  agentNode(t, a) {
    const col = a.status === "blocked" ? t.blocked : a.status === "working" ? t.working : t.done;
    const word = ({ blocked: "WAIT", working: "RUN", done: "DONE" })[a.status] ?? String(a.status).toUpperCase();
    const k = this.tick;
    const sprite = a.status === "blocked" ? `wait${Math.floor(k / 3) % 2}` : a.status === "working" ? `work${k % 4}` : `ready${Math.floor(k / 4) % 2}`;
    return v_flex().id(`hnode-${a.address}`).relative().w(320).flex_none().border_1().border_color(t.border).bg(t.chrome)
      .cursor_pointer().hover((e) => e.bg(t.surface))
      .on_click(() => { run("space.select", { id: a.space }); run("tab.select", { id: a.tab }); run("agent.focus", { terminal: a.address }); })
      .child(h_flex().h(24).px(8).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().text_size(11).font_semibold().text_color(t.foreground).child(a.address))
        .child(div().flex_1().truncate().text_size(10).text_color(t.dim).child(String(a.space_name ?? "").toUpperCase()))
        .child(h_flex().gap(5).items_center().px(5).h(16).border_1().border_color(col)
          .child(div().size(5).bg(a.status === "working" && this.tick % 4 >= 2 ? t.raised : col))
          .child(div().text_size(10).font_semibold().text_color(col).child(word))))
      .child(h_flex().p(8).gap(10).items_center()
        .child(image(`mods/shepherdr/sprites/${sprite}.svg`).w(40).h(40).flex_none())
        .child(v_flex().flex_1().min_w(0).gap(3)
          .child(div().truncate().text_size(12).child(a.tab_name || a.title || "agent"))
          .child(div().truncate().text_size(10.5).text_color(t.dim).child(`${a.program?.name ?? a.app ?? "agent"} · HERDR`))));
  }

  node(t, src, k) {
    const r = k.report, st = k.status, paused = r?.paused;
    const col = st === "need" || paused ? t.blocked : st === "work" || st === "merging" ? t.working : st === "ready" ? t.done : t.dim;
    const word = paused ? "WAIT" : ({ need: "WAIT", work: "RUN", merging: "MERGE", ready: "READY", queued: "QUEUE", todo: "IDLE" })[st] ?? String(st).toUpperCase();
    const pct = r?.progress ?? null;
    const eta = r?.eta == null ? "--" : r.eta === 0 ? "<1 MIN" : `~${r.eta} MIN`;
    const plan = k.plan ?? [];
    const corner = (pos) => div().absolute().w(7).h(7).border_color(col)
      .when(pos.includes("t"), (e) => e.top(-1).border_t(2)).when(pos.includes("b"), (e) => e.bottom(-1).border_b(2))
      .when(pos.includes("l"), (e) => e.left(-1).border_l(2)).when(pos.includes("r"), (e) => e.right(-1).border_r(2));
    return v_flex().id(`node-${src.id}-${k.id}`).relative().w(320).flex_none().border_1().border_color(t.border).bg(t.chrome)
      .cursor_pointer().hover((e) => e.bg(t.surface))
      .child(corner("tl")).child(corner("tr")).child(corner("bl")).child(corner("br"))
      .on_click((_e, cx) => { if (taskChat(k)) { this.open(src.id, taskChat(k), cx); if (props()?.view === "monitor") this.monitorChat = true; } })
      .child(h_flex().h(24).px(8).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().text_size(11).font_semibold().text_color(t.foreground).child(k.id))
        .child(div().flex_1().truncate().text_size(10).text_color(t.dim).child(String(this.placeName(src, k.place)).toUpperCase()))
        .child(h_flex().gap(5).items_center().px(5).h(16).border_1().border_color(col)
          .child(div().size(5).bg((st === "work" || st === "merging") && !paused && this.tick % 4 >= 2 ? t.raised : col))
          .child(div().text_size(10).font_semibold().text_color(col).child(word))))
      .child(v_flex().p(8).gap(6)
        .child(h_flex().gap(10).items_start()
          .child(v_flex().p(3).bg(t.background).border_1().border_color(t.raised).child(this.sprite(st, paused)))
          .child(v_flex().flex_1().min_w(0).gap(4)
            .child(h_flex().gap(6).items_center().child(this.cliIcon(k.agent?.cli)).child(div().flex_1().min_w(0).truncate().text_size(12.5).font_semibold().child(k.name)))
            .child(div().h(32).text_size(11.5).text_color(r?.msg ? t.foreground : t.dim).child(oneLine(r?.msg ?? k.sub ?? "no report yet", 80)))))
        .child(h_flex().gap(8).items_center()
          .child(this.seg(t, (pct ?? 0) / 100, 40, col, (st === "work" || st === "merging") && !paused))
          .child(div().text_size(11).font_semibold().text_color(col).child(pct == null ? "--%" : `${pct}%`)))
        .child(h_flex().gap(10).text_size(10.5).text_color(t.dim)
          .child(div().child(`ETA ${eta}`)).child(div().child(`AGE ${String(k.age ?? "").toUpperCase()}`))
          .when(Boolean(k.pr), (e) => e.child(div().text_color(t.accent).child(`PR #${k.pr.number}`))))
        .when(plan.length > 0, (e) => e.child(h_flex().gap(3).items_center()
          .children(plan.slice(0, 16).map((_, i) => div().w(8).h(8).bg(pct != null && i < Math.round((pct / 100) * plan.length) ? col : t.raised)))
          .child(div().pl(4).text_size(10).text_color(t.dim).child(`${plan.length} STEPS`)))));
  }
  placeName(src, id) { return (this.state[src.id]?.snap?.places ?? []).find((p) => p.id === id)?.name ?? ""; }
  meters(src, h) {
    const out = [];
    for (const m of h?.meters ?? []) {
      out.push(m);
      const vram = /memory (\d+)%/.exec(String(m.detail ?? ""));
      if (/^gpu/i.test(m.name) && vram) {
        const pct = +vram[1], n = String(m.name).replace(/\D/g, "");
        out.push({ name: `VRAM${n}`, label: `VRAM${n}`, pct, level: pct >= 95 ? "over" : pct >= 80 ? "busy" : "ok" });
      }
    }
    if (!src.via && this.macGpu) this.macGpu.forEach((g, i) => {
      const n = this.macGpu.length > 1 ? String(i) : "";
      out.push({ name: `GPU${n}`, label: `GPU${n}`, pct: g.util, level: g.util >= 90 ? "busy" : "ok" });
    });
    return out;
  }
  // This computer's GPUs on Linux (no ioreg): busy % per card from the DRM driver's RC6 (idle) residency,
  // /sys/class/drm/cardN/gt/gt0/rc6_residency_ms, sampled between polls; amdgpu's gpu_busy_percent when present.
  async pollMacGpu() {
    try {
      const r = await exec("sh", ["-c", 'for c in /sys/class/drm/card[0-9]*; do case "$c" in *-*) continue;; esac; ' +
        'b=$(cat "$c/device/gpu_busy_percent" 2>/dev/null); r=$(cat "$c/gt/gt0/rc6_residency_ms" 2>/dev/null || cat "$c/power/rc6_residency_ms" 2>/dev/null); ' +
        'echo "${c##*/} ${b:--} ${r:--} $(date +%s%3N)"; done']);
      const now = {}, out = [];
      for (const line of String(r.stdout ?? "").trim().split("\n").filter(Boolean)) {
        const [card, busy, rc6, at] = line.split(" ");
        now[card] = { rc6: Number(rc6), at: Number(at) };
        let util = null;
        if (busy !== "-") util = Number(busy);
        else if (rc6 !== "-" && this.gpuPrev?.[card]) {
          const dt = now[card].at - this.gpuPrev[card].at, idle = now[card].rc6 - this.gpuPrev[card].rc6;
          if (dt > 0) util = Math.max(0, Math.min(100, 100 - (idle / dt) * 100));
        }
        if (util != null) out.push({ card, util });
      }
      this.gpuPrev = now;
      this.macGpu = out.length ? out : null;
    } catch { this.macGpu = null; }
  }

  // A machine's health as equal cells: CPU, MEM, DISK, then one cell per GPU with its busy % and its memory stacked.
  cells(src, h) {
    const ms = this.meters(src, h), out = [];
    for (let i = 0; i < ms.length; i++) {
      const m = ms[i], name = String(m.label ?? m.name);
      if (/^(VRAM|GPUMEM)/i.test(name)) continue;
      const mate = /^gpu/i.test(name) ? ms.slice(i + 1).find((x) => /^(VRAM|GPUMEM)/i.test(String(x.label ?? x.name))
        && String(x.label ?? x.name).replace(/\D/g, "") === name.replace(/\D/g, "")) : null;
      out.push({ main: m, mem: mate });
    }
    return out;
  }
  bar(t, m) {
    const pct = Math.max(0, Math.min(100, m.pct ?? 0));
    const c = m.level === "over" ? t.blocked : m.level === "busy" ? t.working : t.muted;
    return h_flex().w_full().gap(1).children(Array.from({ length: 24 }, (_, i) => div().flex_1().h(5).bg(i < Math.round(pct / 100 * 24) ? c : t.raised)));
  }
  cell(t, { main, mem }) {
    const label = (m) => m.label ?? ({ memory: "MEM", disk: "DISK", cpu: "CPU", gpu: "GPU" })[String(m.name).toLowerCase()] ?? String(m.name).toUpperCase().replace(/\s+/g, "");
    const val = (m) => m.value ?? `${Math.round(m.pct ?? 0)}%`;
    const tone = (m) => m.level === "over" ? t.blocked : m.level === "busy" ? t.working : t.foreground;
    const line = (m, l) => v_flex().w_full().gap(3)
      .child(h_flex().w_full().text_size(10.5).child(div().flex_1().text_color(t.dim).child(l)).child(div().text_color(tone(m)).child(val(m))))
      .child(this.bar(t, m));
    return v_flex().w(150).flex_none().gap(6).child(line(main, label(main))).when(Boolean(mem), (e) => e.child(line(mem, "MEM")));
  }

  monitorView(t) {
    const out = [];
    let run = 0, wait = 0, queued = 0;
    for (const src of SOURCES) {
      const s = this.state[src.id].snap;
      const head = h_flex().w_full().h(28).px(12).gap(14).items_center().bg(t.chrome).border_b(1).border_t(1).border_color(t.border)
        .child(div().text_size(12).font_semibold().text_color(t.accent).child(`SYS · ${src.name.toUpperCase()}`));
      if (!s) { const none = this.state[src.id].err && !src.via;
        out.push(head.child(div().text_size(11).text_color(none ? t.dim : t.blocked).child(none ? "NO SHEPHERDR HERE" : this.state[src.id].err ? "OFFLINE" : "CONNECTING"))); continue; }
      out.push(head);
      for (const m of s.machines ?? []) {
        const h = m.health;
        const busy = (h?.meters ?? []).some((x) => x.level === "busy" || x.level === "over");
        out.push(h_flex().w_full().px(12).py(10).gap(20).items_start().border_b(1).border_color(t.border)
          .child(h_flex().w(170).flex_none().gap(10).items_center()
            .child(image(`mods/shepherdr/sprites/rack${Math.floor(this.tick / (busy ? 1 : 4)) % 2}.svg`).w(20).h(28).flex_none())
            .child(v_flex().gap(2).min_w(0)
              .child(div().truncate().text_size(11.5).font_semibold().text_color(m.status === "off" ? t.blocked : t.foreground).child(String(m.name).toUpperCase()))
              .child(div().text_size(10).text_color(t.dim).child(h ? `SLOTS ${h.working}/${h.max ?? "∞"}${h.held ? ` · HELD ${h.held}` : ""}` : "NO DATA"))))
          .child(h_flex().flex_1().gap(18).items_start().flex_wrap().children(this.cells(src, h).map((c) => this.cell(t, c)))));
      }
      const tasks = (s.tasks ?? []).filter((k) => !k.archived);
      const live = tasks.filter((k) => ["need", "work", "merging", "ready"].includes(k.status))
        .sort((a, b) => ["need", "work", "merging", "ready"].indexOf(a.status) - ["need", "work", "merging", "ready"].indexOf(b.status));
      const q = tasks.filter((k) => k.status === "queued" || k.status === "todo");
      run += live.filter((k) => k.status === "work" || k.status === "merging").length;
      wait += this.waiting(s).length; queued += q.length;
      // Herdr's own agents on this computer (worktree spaces, hand-started agents): the ongoing work outside Shepherdr's tasks.
      const herdr = src.via ? [] : (agents() ?? []).filter((a) => !String(a.address ?? "").includes("/") && ["working", "blocked", "done"].includes(a.status))
        .sort((a, b) => ["blocked", "working", "done"].indexOf(a.status) - ["blocked", "working", "done"].indexOf(b.status));
      run += herdr.filter((a) => a.status === "working").length; wait += herdr.filter((a) => a.status === "blocked").length;
      out.push(live.length || herdr.length ? h_flex().w_full().p(12).gap(12).flex_wrap()
          .children(live.map((k) => this.node(t, src, k))).children(herdr.map((a) => this.agentNode(t, a)))
        : div().px(12).py(10).text_size(11).text_color(t.dim).child("NO ACTIVE NODES"));
      const waits = this.waiting(s, src);
      if (waits.length) out.push(v_flex().w_full().px(12).pb(10).gap(2)
        .child(div().text_size(10.5).font_semibold().text_color(t.blocked).child(`WAITING ON YOU · ${waits.length}`))
        .children(waits.slice(0, 12).map((n, i) => {
          const place = (s.places ?? []).find((p) => p.id === n.place);
          return h_flex().id(`mw-${src.id}-${i}`).gap(10).text_size(11).cursor_pointer().hover((e) => e.bg(t.hover))
            .on_click((_e, cx) => { this.open(src.id, n.chat, cx); this.monitorChat = true; })
            .child(div().size(6).bg(t.blocked)).child(div().w(70).text_color(t.muted).child(n.task ?? "coordinator"))
            .child(div().flex_1().min_w(0).truncate().child(n.question?.title ?? n.text ?? "waiting on you"))
            .child(div().text_color(t.dim).child(String(place?.name ?? "").toUpperCase()));
        })));
      if (q.length) out.push(v_flex().w_full().px(12).pb(10).gap(2)
        .child(div().text_size(10.5).font_semibold().text_color(t.dim).child(`QUEUE · ${q.length}`))
        .children(q.slice(0, 12).map((k) => h_flex().gap(10).text_size(11)
          .child(div().w(70).text_color(t.muted).child(k.id)).child(div().flex_1().min_w(0).truncate().child(k.name))
          .child(div().text_color(t.dim).child((() => { const m = this.startsIn(s, k);
            const open = (k.after ?? []).filter((a) => { const d = (s.tasks ?? []).find((x) => x.id === String(a).replace(/^.*:/, "")); return d && d.status !== "done"; });
            return [m == null ? null : m === 0 ? "NEXT" : `STARTS ~${this.dur(m).toUpperCase()}`, open.length ? `AFTER ${open.join(", ")}` : null].filter(Boolean).join(" · ") || String(k.status).toUpperCase(); })())))));
    }
    const top = h_flex().w_full().h(36).flex_none().px(12).gap(16).items_center().bg(t.chrome).border_b(1).border_color(t.accent)
      .child(div().text_color(t.accent).child(crook(14))).child(div().text_size(13).font_semibold().child("OPERATIONS MONITOR"))
      .child((() => {
        const msgs = SOURCES.flatMap((src) => (this.state[src.id].snap?.tasks ?? []).filter((k) => k.report?.msg && !k.archived).map((k) => `${k.id} ▸ ${k.report.msg}`));
        const m = msgs.length ? msgs[Math.floor(this.tick / 16) % msgs.length] : "ALL QUIET";
        return h_flex().flex_1().min_w(0).px(10).h(22).items_center().bg(t.background).border_1().border_color(t.border)
          .child(div().text_size(11).text_color(t.done).child(this.tick % 4 < 2 ? "▶ " : "▷ ")).child(div().flex_1().min_w(0).truncate().text_size(11).text_color(t.foreground).child(oneLine(m, 120)));
      })())
      .child(div().text_size(11).text_color(t.working).child(`RUN ${run}`))
      .child(div().text_size(11).text_color(wait ? t.blocked : t.dim).child(`WAIT ${wait}`))
      .child(div().text_size(11).text_color(t.dim).child(`QUEUE ${queued}`))
      .child(div().text_size(10.5).text_color(t.dim).child(new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })));
    return v_flex().id("monitor").size_full().min_h(0).bg(t.background).text_color(t.foreground)
      .child(top).child(v_flex().id("monitor-body").flex_1().min_h(0).overflow_y_scroll().children(out));
  }

  render() {
    const t = theme();
    if (props()?.view === "monitor") {
      const mon = this.monitorView(t);
      if (!this.monitorChat || !this.sel) return mon;
      return h_flex().id("monitor-wrap").size_full().min_h(0).child(div().flex_1().min_w(0).h_full().child(mon))
        .child(v_flex().w(420).flex_none().h_full().border_l(2).border_color(t.border)
          .child(h_flex().id("mon-close").h(22).px(10).items_center().justify_end().bg(t.chrome).text_size(10.5).text_color(t.dim).cursor_pointer()
            .on_click((_e, cx) => { this.monitorChat = false; cx.notify(); }).child("CLOSE ✕"))
          .child(this.chatPane(t)));
    }
    const root = props()?.compact ? v_flex() : h_flex();
    return root.id("shepherdr").size_full().min_h(0).track_focus(this.handle).text_color(t.foreground).bg(t.background)
      .child(this.sidebar(t)).child(this.chatPane(t));
  }
}
