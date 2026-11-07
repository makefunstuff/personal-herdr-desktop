// Workspace spaces: stable colored avatars, worktrees under their checkout,
// grouped by machine when more than one is known. After a space's name, the repo
// its first terminal is in now, only where unusual: branch, ahead/behind. Agent state lives in the agents list, not here. Click to
// switch, right-click for its menu, drag to reorder.

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { spaces, machines, panes, run, openOverlay, exec, hc, theme, shared, setShared, watchFolder, window, layout, readTextFile, appHome } from "herdr";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
// The id prefix bin/task-workspaces.py gives the workspaces it keeps for started tasks.
const TASK_PREFIX = "sptask";
// Our projects manager's tasks (shp, read by px-status into shared("shp")): each started task has its own
// space in its worktree, which git already nests under the repo; this gives that row the task's id, state
// and progress. Its states in the sidebar's words:
const SHP_STATUS = { needs: "blocked", working: "working", merging: "working", ready: "ready", done: "done" };
// Its space: the recorded workspace id (remote ones read "machine/id"), else the space in its worktree
// or named "<ID> · <title>" (tasks imported from Shepherdr keep the space it made).
const tail = (path) => String(path ?? "").replace(/^~|^\/(Users|home)\/[^/]+/, "");
const sameTask = (s, task) => (task.workspace && (s.id === task.workspace || String(s.id).endsWith(`/${task.workspace}`)))
  || (task.worktree && tail(s.folder) === tail(task.worktree)) || s.name === `${task.id} · ${task.title}`;
function shpTasks(sp) {
  const out = {};
  for (const task of (shared("shp")?.places ?? []).flatMap((p) => p.tasks)) {
    const space = sp.find((s) => (s.machine ?? null) === (task.machine ?? null) && sameTask(s, task));
    if (!space) continue;
    const ask = (task.asks ?? [])[0];
    out[space.id] = { shp: true, ref: task.id, title: task.title, status: SHP_STATUS[task.status] ?? "idle", progress: task.progress,
      left: task.left, doing: task.status === "needs" && ask?.text ? ask.text : task.step };
  }
  return out;
}

function find(node, id) {
  if (node?.id === id) return node;
  for (const child of node?.children ?? []) {
    const found = find(child, id);
    if (found) return found;
  }
  return null;
}

// The sidebar's rail goes by its width, not a flag: dragging its edge narrows it too.
const narrow = () => (find(layout(), "sidebar")?.size?.px ?? Infinity) < 160;

// Where a space is now: its first terminal's live folder, else where it was made.
// `ps`: panes(), read once by the caller (each read copies every pane).
function place(s, ps) {
  const tabs = new Set((s.tabs ?? []).map((c) => c.id));
  return ps.find((p) => tabs.has(p.tab) && p.cwd)?.cwd || s.folder;
}
const rowHeight = ({ tree, task }) => (tree || task ? 28 : 30);
const where = (s, ps) => JSON.stringify([s.machine ?? null, place(s, ps)]);
const base = (path) => path.replace(/\/+$/, "").split("/").pop();
// Changes made outside Herdr (an editor's save, a commit in another app): looked
// for this often, in repos the list shows, while the window is in front.
const FALLBACK = 60000;

// What a stored color name looks like on an avatar (the same in every mod
// that draws space avatars): one strong color per hue, light and dark alike.
const MARK = { ansi6: "#22a99a", ansi3: "#e3814f", ansi4: "#5b8def", ansi5: "#8b7cf6", ansi2: "#3fae6a", ansi1: "#d9649a" };

// A space speaks up only when something in it needs you.
function glyph(status) {
  return status === "blocked" || status === "done" ? "●" : "·";
}

function tone(t, status) {
  return t[status] ?? t.unknown;
}

export default class Spaces extends View {
  init(_props, cx) {
    this.git = {}; // where() -> { name, branch, ahead, behind, repo, linked }, null: not a repo
    this.checked = {}; // where() -> when it was last looked at
    this.unsure = new Set(); // where()s git couldn't answer for (a machine away)
    this.pending = new Set(); // where()s to look at next
    this.busy = {}; // pane -> an agent working or a program in front of its shell
    this.ready = {}; // machine -> its layout was readable at the last look
    this.at = {}; // space -> its where() at the last look
    this.watching = new Set(); // local repos' git folders
    // Spaces made while this runs wait (at most a moment) for their git, so a
    // worktree appears under its repo instead of jumping there.
    this.shown = new Set(spaces().map((s) => s.id));
    this.born = {};
    this.pressed = null;
    this.over = null;
    this.machines = [];
    // Task workspaces (bin/task-workspaces.py's registry): the id of the workspace a started
    // Shepherdr task has, its space, its status and progress. A task workspace is a real Herdr
    // space in the worktree, filled with the task's Chat, Terminal, Files, Changes and PR tabs.
    this.tasks = {};
    this.chatWanted = null; // a task row clicked before its tabs were written (see openTask)
    this.loadTasks(cx);
    cx.timer.every(3000, cx => this.loadTasks(cx));
    this.folded = new Set(JSON.parse(localStorage.getItem("machines.folded") ?? "[]"));
    this.refreshMachines(cx);
    cx.timer.every(15000, cx => this.refreshMachines(cx));
    // `git status` walks the whole tree, on another machine a process there:
    // only when its answer could have changed (see `due`).
    cx.timer.every(250, (cx) => this.checkNew(cx));
  }

  // The task workspaces the sync service keeps, as { [space id]: task }. Read now and then: a new
  // task's workspace shows up within a few seconds and goes when the task is done.
  loadTasks(cx) {
    cx.spawn(async (cx) => {
      let next = {};
      try { next = JSON.parse((await readTextFile(`${appHome()}/task-workspaces.json`)) || "{}").tasks ?? {}; } catch {}
      if (JSON.stringify(next) === JSON.stringify(this.tasks)) return;
      this.tasks = next;
      this.openWantedChat();
      cx.notify();
    });
  }

  // A task row: its space, then its Chat. The service writes a task's space a moment before its
  // tabs, so a click in between is kept and made good when they arrive (openWantedChat).
  openTask(w, task) {
    run("space.select", { id: w.id });
    if (!task) return;
    if (task.shp) { const chat = (w.tabs ?? []).find((x) => x.name === "Chat"); if (chat) run("tab.select", { id: chat.id }); return; }
    if (task.chat && task.tabs !== false) run("tab.select", { id: task.chat });
    else this.chatWanted = { id: w.id, at: Date.now() };
  }

  openWantedChat() {
    const want = this.chatWanted;
    if (!want) return;
    const t = this.tasks[want.id];
    const ready = Boolean(t && t.tabs !== false && t.chat);
    if (!ready && Date.now() - want.at < 60000) return;   // its tabs aren't written yet: wait
    this.chatWanted = null;
    // Only if the row the person clicked is still the space on screen.
    if (ready && spaces().find((s) => s.id === want.id)?.active) run("tab.select", { id: t.chat });
  }

  checkNew(cx) {
    const now = Date.now();
    const sp = spaces();
    for (const s of sp) if (!this.shown.has(s.id)) this.born[s.id] ??= now;
    const due = this.due(now, sp, panes());
    if (due.size) this.refreshGit(due, cx);
    if (sp.some((s) => !this.shown.has(s.id))) cx.notify();
  }

  // Places whose git could have changed: new ones (a new space, a cd), where a
  // command or an agent's turn just ended (a checkout, a commit, edits), those on
  // a machine that is back, and repos in view whose fallback is due.
  due(now, sp, ps) {
    // Moved there: what it knew of that place may be old.
    const at = {};
    for (const s of sp) at[s.id] = where(s, ps);
    const keys = new Set(Object.values(at).filter((k) => JSON.parse(k)[1]));
    const due = new Set([...keys].filter((k) => !(k in this.git)));
    for (const [id, k] of Object.entries(at)) if (this.at[id] && this.at[id] !== k && keys.has(k)) due.add(k);
    this.at = at;
    const spaceOf = {};
    for (const s of sp) for (const t of s.tabs ?? []) spaceOf[t.id] = s;
    const busy = {};
    for (const p of ps) {
      if (!p.terminal || !spaceOf[p.tab]) continue;
      busy[p.pane] = p.status === "working" || !(p.program?.shell || p.program?.agent);
      if (this.busy[p.pane] && !busy[p.pane]) this.sameRepo(at[spaceOf[p.tab].id], keys).forEach((k) => due.add(k));
    }
    this.busy = busy;
    for (const m of machines()) {
      const ready = m.layout === "ready";
      if (ready && this.ready[m.name] === false) for (const k of keys) if (JSON.parse(k)[0] === m.name) due.add(k);
      this.ready[m.name] = ready;
    }
    if (window().active) {
      for (const k of keys) {
        const asks = this.git[k] || this.unsure.has(k);
        if (asks && !this.folded.has(JSON.parse(k)[0]) && now - (this.checked[k] ?? 0) > FALLBACK) due.add(k);
      }
    }
    return due;
  }

  // `key` and the other places in its repo (a commit there moves their ahead/behind).
  sameRepo(key, keys) {
    const repo = this.git[key]?.repo;
    return [key, ...(repo ? [...keys].filter((k) => this.git[k]?.repo === repo) : [])];
  }

  refreshMachines(cx) {
    cx.spawn(async cx => {
      try { const r = await hc(["machine", "ls", "--json"]); if (r.code === 0) this.machines = (JSON.parse(r.stdout).machines ?? []).filter((m) => !m.temporary); } catch {}
      cx.notify();
    });
  }

  // One look at a time, each place once however often it was asked for meanwhile.
  refreshGit(keys, cx) {
    for (const k of keys) this.pending.add(k);
    if (this.refreshing) return;
    this.refreshing = true;
    cx.spawn(async (cx) => {
      while (this.pending.size) {
        const keys = [...this.pending];
        this.pending.clear();
        for (const key of keys) await this.check(key, cx);
      }
      const used = new Set(Object.values(this.git).filter(Boolean).map((g) => JSON.parse(g.repo)).filter(([on]) => !on).map(([, repo]) => repo));
      for (const repo of this.watching) if (!used.has(repo)) this.watching.delete(repo);
      this.refreshing = false;
      cx.notify();
    });
  }

  // One place's git: branch, ahead/behind, its repo.
  async check(key, cx) {
    const [on, cwd] = JSON.parse(key);
    this.checked[key] = Date.now();
    this.unsure.delete(key);
    try {
      // No optional locks: a status that touched the index would wake the watch again.
      // Writes nothing under .git (index lock, fsmonitor cookie): watch() would wake on it.
      const r = await exec("git", ["--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--porcelain=v2", "--branch"], { cwd, on });
      if (r.code !== 0) {
        // Only git's own "no" is remembered as no repo; anything else is asked again.
        if (!/not a git repository/i.test(r.stderr)) this.unsure.add(key);
        this.git[key] = null;
        return;
      }
      // The repo's own .git folder names it; a worktree's top differs from it.
      const top = await exec("git", ["rev-parse", "--show-toplevel"], { cwd, on });
      const common = await exec("git", ["rev-parse", "--path-format=absolute", "--git-common-dir"], { cwd, on });
      const repo = common.stdout.trim();
      const name = repo.endsWith("/.git") ? base(repo.slice(0, -5)) : base(repo).replace(/\.git$/, "");
      const info = { name, branch: null, ahead: 0, behind: 0, repo: JSON.stringify([on, repo]), linked: repo !== `${top.stdout.trim()}/.git` };
      for (const line of r.stdout.split("\n")) {
        if (line.startsWith("# branch.head ")) info.branch = line.slice(14);
        else if (line.startsWith("# branch.ab ")) {
          const [a, b] = line.slice(12).split(" ");
          info.ahead = Math.abs(parseInt(a, 10)) || 0;
          info.behind = Math.abs(parseInt(b, 10)) || 0;
        }
      }
      this.git[key] = info;
      if (!on) this.watch(repo, cx);
    } catch {
      // Couldn't ask (the machine is away): keep what it showed, look again later.
      this.git[key] ??= null;
      this.unsure.add(key);
    }
  }

  watch(repo, cx) {
    if (this.watching.has(repo)) return;
    this.watching.add(repo);
    cx.spawn(async (cx) => {
      try {
        while (this.watching.has(repo)) {
          await watchFolder(repo);
          const mine = JSON.stringify([null, repo]);
          if (this.watching.has(repo)) this.refreshGit(Object.keys(this.git).filter((k) => this.git[k]?.repo === mine), cx);
        }
      } catch { this.watching.delete(repo); }
    });
  }

  menu(w) {
    // A popup is as tall as its items; height is only the most.
    openOverlay({ frame: "none", mod: "px-menu", kind: "popup", width: 200, height: 260, props: { tab: "space", target: { id: w.id } } });
  }

  // Spaces in order, each worktree space moved under its main checkout's, each task's workspace
  // under the space the service names for it (its repo's space, else the one it was started from).
  ordered() {
    const sp = spaces(), ps = panes();
    const ready = (w) => this.shown.has(w.id) || where(w, ps) in this.git || Date.now() - (this.born[w.id] ??= Date.now()) > 1500;
    for (const w of sp) if (!this.shown.has(w.id) && ready(w)) this.shown.add(w.id);
    const shp = shpTasks(sp);
    const all = sp.map((w, i) => ({ w, i, g: this.git[where(w, ps)] ?? null, task: this.tasks[w.id] ?? shp[w.id] ?? null })).filter((r) => this.shown.has(r.w.id));
    // A task's workspace never reads as a space of its own. Without a task entry (a moment before
    // the service writes one, or a space Herdr rebuilt from a leftover terminal label after a
    // window restart) it would be an orphan at the top, so it is left out until it has its task
    // row. A task row also waits for the parent space the service names: it is never top-level.
    const known = new Set(sp.map((s) => s.id));
    // An shp task's space is a worktree space like any other (nested by git below), only labelled.
    const list = all.filter((r) => r.task?.shp || (!r.task && !String(r.w.id).startsWith(TASK_PREFIX)));
    const tasks = all.filter((r) => r.task && !r.task.shp && known.has(r.task.parent ?? r.task.space));
    const mains = new Set(list.filter((r) => r.g && !r.g.linked).map((r) => r.g.repo));
    // A task worktree whose folder git can't read (removed once its task finished) still nests under
    // its repo by path: ~/.shepherdr/worktrees/<repo>/… or ~/.herdr/worktrees/<repo>/….
    const byName = new Map(list.filter((r) => r.g && !r.g.linked).map((r) => [r.g.name, r.g.repo]));
    const wtRepo = (r) => { const m = String(r.w.folder ?? "").match(/\/\.(?:shepherdr|herdr)\/worktrees\/([^/]+)\//); return m ? byName.get(m[1]) : undefined; };
    for (const r of list) if (!r.g?.linked && !r.w.machine) { const repo = wtRepo(r); if (repo) r.g = { ...(r.g ?? { branch: null, ahead: 0, behind: 0, name: "", gone: true }), repo, linked: true }; }
    const nested = (r) => r.g?.linked && mains.has(r.g.repo);
    const out = [];
    const placed = new Set(); // repos whose worktrees are listed (under the first of its spaces)
    for (const r of list) {
      if (nested(r)) continue;
      out.push({ ...r, tree: null });
      if (!r.g || r.g.linked || placed.has(r.g.repo)) continue;
      placed.add(r.g.repo);
      const kids = list.filter((k) => nested(k) && k.g.repo === r.g.repo);
      kids.forEach((k, n) => out.push({ ...k, tree: n === kids.length - 1 ? "└─" : "├─" }));
    }
    const extra = new Map();
    for (const r of tasks) {
      const at = out.find((x) => x.w.id === (r.task.parent ?? r.task.space));
      if (!at) continue;   // its parent isn't on screen (yet): no top-level fallback, next frame
      if (!extra.has(at.w.id)) extra.set(at.w.id, []);
      extra.get(at.w.id).push(r);
    }
    const final = [];
    for (const r of out) {
      final.push(r);
      const kids = extra.get(r.w.id) ?? [];
      kids.forEach((k, n) => final.push({ ...k, task: k.task, tree: n === kids.length - 1 ? "└─" : "├─" }));
    }
    return final;
  }

  // The rows, under a header per machine once one isn't this computer.
  grouped(t, collapsed) {
    const rows = this.ordered();
    const draw = (r) => (collapsed ? this.compact(r, t) : this.row(r, t));
    const remote = [...new Set([...rows.map(r => r.w.machine).filter(Boolean), ...this.machines.map(m => m.name)])].sort();
    const loading = m => {
      const layout = machines().find(k => k.name === m)?.layout;
      return layout === "loading" || (!layout && this.machines.some(k => k.name === m && k.state === "connected"))
        || spaces().some(s => (s.machine ?? null) === m && !this.shown.has(s.id));
    };
    const loadingRow = m => div().id(`machine-loading-${m ?? "local"}`).h(28).flex_none().px(8).text_size(12).text_color(t.dim).child(collapsed ? "…" : "Loading…");
    if (!remote.length) return loading(null) && !rows.length ? [loadingRow(null)] : rows.map(draw);
    const machineNames = [null, ...remote];
    return machineNames.flatMap((m) => {
      const mine = rows.filter((r) => (r.w.machine ?? null) === m);
      const machine = this.machines.find(k => k.name === m);
      const connected = !m || machine?.state === "connected";
      const folded = this.folded.has(m);
      const head = h_flex().id(`machine-${m ?? "local"}`).h(26).flex_none().px(collapsed ? 2 : 8).gap(7).items_center().text_size(11).text_color(t.dim)
        .cursor_pointer().hover(s => s.bg(t.hover))
        .on_click((_e, cx) => {
          folded ? this.folded.delete(m) : this.folded.add(m);
          localStorage.setItem("machines.folded", JSON.stringify([...this.folded]));
          cx.notify();
        })
        .child(svg(`mods/px-chrome/${GS()}/${m ? "server" : "monitor"}.svg`).size(13).flex_none())
        .when(!collapsed, el => el.child(div().flex_1().truncate().child(shared("layout") === "workbench"
          ? `BUS ${machineNames.indexOf(m) + 1} \u00b7 ${(m ?? "This computer").toUpperCase()}` : (m ?? "This computer")))
          .when(!connected, el => el.child(div().flex_none().child((machine?.state ?? "asleep").replace(/_/g, " "))))
          .when(connected, el => el.child(div().id(`machine-add-${m ?? "local"}`).flex_none().px(4).rounded(0).hover(s => s.bg(t.hover).text_color(t.foreground))
            .on_click((_e, cx) => { cx.stop_propagation(); run("space.new", m ? { machine: m } : {}); }).child("+")))
          .child(div().size(6).rounded(0).flex_none().bg(connected ? t.ansi2 : t.dim))
          .child(svg(`mods/px-chrome/${GS()}/chevron-${folded ? "right" : "down"}.svg`).size(11).flex_none()));
      const empty = h_flex().id(`machine-new-${m ?? "local"}`).h(28).flex_none().px(8).gap(7).items_center().rounded(0)
        .text_size(12).text_color(t.dim).hover(s => s.bg(t.hover).text_color(t.foreground))
        .on_click(() => run("space.new", m ? { machine: m } : {}))
        .child(svg(`mods/px-chrome/${GS()}/plus.svg`).size(12)).when(!collapsed, el => el.child("New space there"));
      return [head, ...(folded ? [] : mine.length ? mine.map(draw) : [loading(m) ? loadingRow(m) : empty])];
    });
  }

  // Only what's unusual, after the name: "herdr \u00b7 spike \u21912 \u21931". The repo or
  // branch goes when the name already says it (a worktree is named after its branch),
  // and master/main go: nearly every space is on one.
  gitTail(g, tree, name, t) {
    const raw = g.gone ? "worktree removed" : g.branch ?? "detached";
    const same = (a) => a.toLowerCase() === name.toLowerCase();
    const branch = same(raw) || raw === "master" || raw === "main" ? "" : raw;
    const repo = tree || same(g.name) ? "" : g.name;
    const where = [repo, branch].filter(Boolean).join(" \u00b7 ");
    return h_flex()
      .flex_1()
      .min_w(0)
      .gap(5)
      .justify_end()
      .items_center()
      .text_size(11.5)
      .text_color(t.dim)
      .when(Boolean(where), (el) => el.child(div().min_w(0).truncate().child(where)))
      .when(g.ahead > 0, (el) => el.child(div().flex_none().text_color(t.ansi2).child(`\u2191${g.ahead}`)))
      .when(g.behind > 0, (el) => el.child(div().flex_none().text_color(t.ansi1).child(`\u2193${g.behind}`)));
  }

  row({ w, i, g, tree, task }, t) {
    const on = w.active;
    const isTask = Boolean(task);
    // A task workspace: its worker's own state (working, needs you, done) and how far it got.
    const status = isTask ? (({ working: "working", blocked: "blocked", done: "done", ready: "blocked" })[task.status] ?? "idle") : w.status;
    const led = isTask ? (t[status] ?? t.dim) : (w.status === "working" ? t.working : w.status === "blocked" ? t.blocked : w.status === "done" ? t.done : t.border);
    const colors = shared("space.colors") ?? {};
    const color = colors[w.id] ?? "ansi6";
    const fill = color.startsWith("#") ? color : (MARK[color] ?? t[color]);
    return v_flex()
      .relative()
      .id(`space-${w.id}`)
      .px(8)
      .h(rowHeight({ tree, task }))
      .flex_none()
      .justify_center()
      .rounded(0)
      .when(on, (el) => el.bg(t.selected))
      .when(!on, (el) => el.hover((s) => s.bg(t.hover)))
      .when(this.pressed === i && this.over !== i, (el) => el.opacity(0.5))
      .when(this.pressed !== null && this.over === i && this.pressed !== i, (el) => el.child(
        div().absolute().left(0).right(0).h(2).bg(t.accent)
          .when(this.pressed < i, line => line.bottom(0))
          .when(this.pressed > i, line => line.top(0)),
      ))
      .on_mouse_down("left", () => {
        this.pressed = i;
        this.over = i;
      })
      .on_mouse_move((_e, cx) => {
        const source = spaces()[this.pressed];
        const over = source && (source.machine ?? null) === (w.machine ?? null) ? i : null;
        if (this.pressed !== null && this.over !== over) {
          this.over = over;
          cx.notify();
        }
      })
      .on_mouse_up("left", (_e, cx) => {
        const from = this.pressed;
        const over = this.over;
        this.pressed = null;
        this.over = null;
        if (from !== null && over === i && from !== i) run("space.move", { id: spaces()[from].id, index: i + 1 });
        else if (from === i) this.openTask(w, task);
        cx.notify();
      })
      .on_mouse_down("right", () => this.menu(w))
      .when(shared("layout") === "workbench", (el) => el.border_l(3).border_color(on ? t.accent : "#00000000"))
      .when(shared("layout") === "workbench", (el) => el.child(
        // workbench: a numbered module slot: index cell, name in caps, status LED. A task workspace
        // takes the worktree row's shape: a branch, its id, title and progress.
        h_flex().gap(8).items_center()
          .when(Boolean(tree) || isTask, el => el.pl(26).child(svg(`mods/px-chrome/${GS()}/branch.svg`).size(12).text_color(t.dim)))
          .when(!tree && !isTask, el => el.child(h_flex().w(24).h(18).flex_none().items_center().justify_center().border_1()
            .border_color(on ? t.accent : t.border).bg(on ? t.accent : t.surface).text_size(10.5).font_semibold()
            .text_color(on ? t.accent_foreground : t.dim).child(String(i + 1).padStart(2, "0"))))
          .when(!isTask, el => el.child(div().flex_shrink(0).max_w(150).truncate().text_size(12.5).font_semibold().text_color(on ? t.accent : t.foreground).child(w.name.toUpperCase())))
          .when(isTask, el => el.child(div().flex_shrink(0).text_size(12.5).font_semibold().text_color(on ? t.accent : t.foreground).child(task.ref)))
          .when(!isTask, el => el.child(g ? this.gitTail(g, tree, w.name, t) : div().flex_1()))
          .when(isTask, el => el.child(this.taskTail(task, t)))
          .child(div().size(7).flex_none().bg(led))))
      .when(shared("layout") !== "workbench", (el) => el.child(
        h_flex()
          .gap(9)
          .items_center()
          .when(Boolean(tree) || isTask, el => el.pl(22).child(svg(`mods/px-chrome/${GS()}/branch.svg`).size(13).text_color(t.dim)))
          .when(!tree && !isTask, el => el.child(h_flex().size(18).flex_none().rounded(0).bg(fill).items_center().justify_center().text_size(10.5).font_bold().text_color("#ffffff").child([...w.name][0]?.toUpperCase() ?? "?")))
          .when(!isTask, el => el.child(div().flex_shrink(0).max_w(150).truncate().text_color(t.foreground).child(w.name)))
          .when(isTask, el => el.child(div().flex_shrink(0).text_color(on ? t.accent : t.foreground).child(task.ref)))
          .when(!isTask, el => el.child(g ? this.gitTail(g, tree, w.name, t) : div().flex_1()))
          .when(isTask, el => el.child(this.taskTail(task, t))),
      ));
  }

  // A task workspace's line after its id: the task's title, how far it got, and its worker's words.
  taskTail(task, t) {
    const pct = typeof task.progress === "number" ? `${task.progress}%` : null;
    const left = typeof task.left === "number" ? `~${task.left}m` : task.left ? String(task.left) : null;
    return h_flex().flex_1().min_w(0).gap(6).justify_end().items_center().text_size(11.5).text_color(t.dim)
      .child(div().flex_1().min_w(0).truncate().tooltip(`${task.ref} \u00b7 ${task.title}${task.doing ? ` \u00b7 ${task.doing}` : ""}`).child(task.title ?? ""))
      .when(Boolean(pct), el => el.child(div().flex_none().text_color(t.foreground).child(pct)))
      .when(!pct && Boolean(left), el => el.child(div().flex_none().child(left)));
  }

  compact({ w, i, task }, t) {
    // Narrow sidebar: a task keeps its id and its worker's state, not the plain space's.
    const status = task ? (({ working: "working", blocked: "blocked", done: "done", ready: "blocked" })[task.status] ?? "idle") : w.status;
    return h_flex()
      .id(`space-${w.id}`)
      .h(26)
      .flex_none()
      .justify_center()
      .gap(4)
      .when(w.active, (el) => el.bg(t.raised))
      .hover((s) => s.bg(t.hover))
      .on_click(() => this.openTask(w, task))
      .child(div().text_color(w.active ? t.foreground : t.muted).child(task?.ref ?? String(i + 1)))
      .child(div().text_color(tone(t, status)).child(glyph(status)));
  }

  render() {
    const t = theme();
    const collapsed = narrow();
    const palette = ["ansi6", "ansi3", "ansi4", "ansi5", "ansi2", "ansi1"];
    const sp = spaces();
    const ids = new Set(sp.map(p => p.id));
    const kept = Object.fromEntries(Object.entries(shared("space.colors") ?? {}).filter(([id]) => ids.has(id)));
    let changed = Object.keys(kept).length !== Object.keys(shared("space.colors") ?? {}).length;
    for (const p of sp) {
      if (kept[p.id] || p.name === "~") continue;
      const uses = c => Object.values(kept).filter(v => v === c).length;
      kept[p.id] = palette.reduce((best, c) => uses(c) < uses(best) ? c : best);
      changed = true;
    }
    if (changed) setShared("space.colors", kept);
    return v_flex().size_full().min_h(0).bg(t.chrome).text_size(13)
      .child(h_flex().h(31).pl(16).pr(10).items_center().justify_between().text_size(11.5).text_color(t.dim)
        .when(!collapsed, el => el.child(shared("layout") === "workbench" ? "SPACES" : "Spaces"))
        .child(div().id("space-new").size(26).flex_none().flex().items_center().justify_center().rounded(0).hover(s => s.bg(t.hover).text_color(t.foreground))
          .on_click(() => run("space.new")).child(svg(`mods/px-chrome/${GS()}/plus.svg`).size(14))))
      .child(v_flex().flex_1().min_h(0).px(8).pb(14).overflow_y_scrollbar().mode("always")
        .on_mouse_up("left", (_e, cx) => { this.pressed = null; cx.notify(); })
        .children(this.grouped(t, collapsed)));
  }
}
