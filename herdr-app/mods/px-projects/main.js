// Projects: our own project manager's views over simple-herdr-projects (`shp`), on every machine
// linked to herdr-core. Three faces of one mod:
//   the Tasks panel (the layout's "tasks" node): Needs you / Working / Ready / Backlog / Done, with
//     a reply for an open question, Start, Accept and Reopen, and a new task in any shp space;
//   the Monitor tab (props {view: "monitor"}): the operations monitor, one rack per machine;
//   the Accept dialog (props {accept: task}): Enter accepts, Esc cancels.
// It only reads shared("shp"), which px-status's poller keeps (mods/px-projects/shp.js).

import { View, div, svg, image } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import { theme, props, shared, agents, run, openOverlay, closeOverlay } from "herdr";
import { group, detail, subtitle, openAsk, openTask, workspaceOf, shp, allTasks, openMonitor } from "./shp.js";

const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : String(x));
const ID_FONT = "JetBrains Mono";
const keyOf = (task) => `${task.machine ?? ""}/${task.id}`;
const oneLine = (s, n) => { const x = String(s ?? "").replace(/\s+/g, " ").trim(); return x.length > n ? `${x.slice(0, n - 1)}…` : x; };

function chip(id, text, color) {
  return h_flex().id(id).h(20).flex_none().px(6).gap(5).items_center().whitespace_nowrap().border_1().border_color(color)
    .text_size(10.5).font_semibold().text_color(color).child(div().size(5).flex_none().bg(color)).child(caps(text));
}

export default class Projects extends View {
  init(_props, cx) {
    this.view = props()?.accept ? "accept" : props()?.view === "monitor" ? "monitor" : "tasks";
    this.busy = null;   // the key of the task an action runs on
    this.error = null;  // why shp refused the last action
    if (this.view === "accept") { this.handle = cx.focus_handle(); this.handle.focus(); return; }
    if (this.view === "monitor") { this.tick = 0; cx.timer.every(240, (cx) => { this.tick++; cx.notify(); }); return; }
    this.replyTo = null;  // the key of the task whose question is being answered
    this.adding = false;  // the new-task line is open
    this.addPlace = null; // its machine (null: this computer) and space
    this.addSpace = null;
    this.reply = InputState.new({ placeholder: "Answer… (Enter sends, Esc closes)" });
    this.reply.on("submit", (_e, cx) => this.say(cx));
    this.title = InputState.new({ placeholder: "New task title… (Enter adds, Esc closes)" });
    this.title.on("submit", (_e, cx) => this.add(cx));
    this.folded = new Set(JSON.parse(localStorage.getItem("folded") ?? '["done"]'));
  }

  // Run an shp command for a task, showing why it refused; the poller shows the result.
  act(task, argv, cx, after) {
    if (this.busy) return;
    this.busy = keyOf(task); this.error = null; cx.notify();
    cx.spawn(async (cx) => {
      const why = await shp(task.machine, argv);
      this.busy = null; this.error = why ? `${task.id}: ${why}` : null;
      if (!why && after) after();
      cx.notify();
    });
  }

  say(cx) {
    const task = allTasks().find((t) => keyOf(t) === this.replyTo);
    const text = this.reply.value().trim();
    if (!task || !text) return;
    const ask = openAsk(task);
    this.act(task, ["say", ...(ask?.id && (task.asks ?? []).length > 1 ? ["--ask", String(ask.id)] : []), task.id, text], cx,
      () => { this.reply.set_value(""); this.replyTo = null; });
  }

  add(cx) {
    const title = this.title.value().trim();
    if (!title || !this.addSpace || this.busy) return;
    const machine = this.addPlace;
    this.busy = "add"; this.error = null; cx.notify();
    cx.spawn(async (cx) => {
      const why = await shp(machine, ["add", "--space", this.addSpace, title]);
      this.busy = null; this.error = why;
      if (!why) { this.title.set_value(""); this.adding = false; }
      cx.notify();
    });
  }

  // ---------- Tasks panel

  button(t, id, label, color, onClick) {
    return chip(id, label, color).cursor_pointer().hover((s) => s.bg(color).text_color(t.background))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((e, cx) => { cx.stop_propagation(); onClick(cx); });
  }

  actions(t, task) {
    const k = keyOf(task), busy = this.busy === k;
    const out = h_flex().flex_none().gap(4).items_center();
    if (busy) return out.child(div().text_size(10.5).text_color(t.dim).child(caps("…")));
    if (task.status === "needs") out.child(this.button(t, `reply-${k}`, "Reply", t.blocked, (cx) => {
      this.replyTo = this.replyTo === k ? null : k; if (this.replyTo) this.reply.focus(); cx.notify(); }));
    if (task.status === "todo" || task.status === "queued") out.child(this.button(t, `start-${k}`, "Start", t.accent, (cx) => this.act(task, ["start", task.id], cx)));
    if (task.status === "ready") {
      out.child(this.button(t, `reopen-${k}`, "Reopen", t.muted, (cx) => this.act(task, ["reopen", task.id], cx)));
      out.child(this.button(t, `accept-${k}`, "Accept", t.done, () =>
        openOverlay({ mod: "px-projects", kind: "dialog", modal: true, width: 440, height: 210, props: { accept: task } })));
    }
    return out;
  }

  row(t, task) {
    const k = keyOf(task), space = workspaceOf(task), info = detail(task), under = subtitle(task), old = task.status === "done";
    return v_flex().id(`task-${k}`).w_full().flex_none()
      .child(h_flex().w_full().px(12).py(5).gap(10).items_center()
        .when(Boolean(space), (e) => e.cursor_pointer().hover((s) => s.bg(t.hover)).on_click(() => openTask(task)))
        .child(v_flex().flex_1().min_w(0).gap(2)
          .child(h_flex().min_w(0).gap(8).items_center()
            .child(div().flex_none().w(64).font_family(ID_FONT).text_size(11.5).text_color(task.status === "needs" ? t.blocked : t.dim).child(String(task.id)))
            .child(div().flex_1().min_w(0).truncate().text_size(12.5).text_color(old ? t.muted : t.foreground).child(String(task.title ?? ""))))
          .when(Boolean(under), (e) => e.child(div().pl(72).min_w(0).truncate().text_size(11).text_color(task.status === "needs" ? t.foreground : t.muted).child(under))))
        .when(Boolean(info), (e) => e.child(div().flex_none().text_size(10.5).text_color(t.dim).child(caps(info))))
        .child(this.actions(t, task)))
      .when(this.replyTo === k, (e) => e.child(h_flex().w_full().pl(84).pr(12).pb(6).gap(6).items_center()
        .on_key_down((ev, cx) => { if (ev.key === "escape") { this.replyTo = null; cx.stop_propagation(); cx.notify(); } })
        .child(Input.new(this.reply).flex_1().h(28).px(4).rounded(0).border_1().border_color(t.blocked).bg(t.input).text_size(12).text_color(t.foreground))));
  }

  section(t, place, s, out) {
    const color = t[s.color] ?? t.dim, fkey = `${place.name ?? ""}:${s.key}`, fold = this.folded.has(s.key === "done" ? "done" : fkey);
    out.push(h_flex().id(`sec-${fkey}`).w_full().px(12).pt(10).pb(4).gap(8).items_center().cursor_pointer()
      .on_click((_e, cx) => {
        const id = s.key === "done" ? "done" : fkey;
        this.folded.has(id) ? this.folded.delete(id) : this.folded.add(id);
        localStorage.setItem("folded", JSON.stringify([...this.folded])); cx.notify();
      })
      .child(div().size(6).bg(color))
      .child(div().text_size(10.5).font_semibold().text_color(t.muted).child(`${caps(s.label)} · ${s.tasks.length}`))
      .child(div().flex_1().h(1).bg(t.border))
      .child(svg(`mods/px-chrome/${GS()}/chevron-${fold ? "right" : "down"}.svg`).size(11).flex_none()));
    if (!fold) for (const task of s.tasks.slice(0, 80)) out.push(this.row(t, task));
  }

  // The new-task line: machine and space chips, then the title.
  adder(t, places) {
    const answered = places.filter((p) => p.read && !p.error);
    const withSpaces = answered.filter((p) => p.spaces.length);
    if (!withSpaces.length) return div().px(12).py(8).text_size(11).text_color(t.dim)
      .child("No shp spaces yet. Add one in a terminal: shp space add <name> --folder <repo>");
    if (!withSpaces.some((p) => p.name === this.addPlace)) this.addPlace = withSpaces[0].name;
    const place = withSpaces.find((p) => p.name === this.addPlace);
    if (!place.spaces.includes(this.addSpace)) this.addSpace = place.spaces[0];
    const pick = (id, label, on, click) => h_flex().id(id).h(20).px(6).items_center().border_1().text_size(10.5).cursor_pointer()
      .border_color(on ? t.accent : t.border).text_color(on ? t.accent : t.muted).hover((s) => s.bg(t.hover)).on_click(click).child(caps(label));
    return v_flex().w_full().px(12).py(8).gap(6).border_b(1).border_color(t.border).bg(t.chrome)
      .on_key_down((ev, cx) => { if (ev.key === "escape") { this.adding = false; cx.stop_propagation(); cx.notify(); } })
      .when(withSpaces.length > 1, (e) => e.child(h_flex().gap(4).flex_wrap().children(withSpaces.map((p) =>
        pick(`am-${p.name ?? "here"}`, p.name ?? "This computer", p.name === this.addPlace, (_e, cx) => { this.addPlace = p.name; cx.notify(); })))))
      .child(h_flex().gap(4).flex_wrap().children(place.spaces.map((s) =>
        pick(`as-${s}`, s, s === this.addSpace, (_e, cx) => { this.addSpace = s; cx.notify(); }))))
      .child(Input.new(this.title).w_full().h(28).px(4).rounded(0).border_1().border_color(t.accent).bg(t.input).text_size(12).text_color(t.foreground));
  }

  tasksView(t) {
    const places = shared("shp")?.places ?? [];
    const tasks = places.flatMap((p) => p.tasks);
    const needs = tasks.filter((k) => k.status === "needs").length, ready = tasks.filter((k) => k.status === "ready").length;
    const out = [];
    for (const place of places) {
      if (places.length > 1 || place.error) out.push(h_flex().h(26).mt(4).px(12).gap(8).items_center().bg(t.chrome).border_b(1).border_color(t.border)
        .child(svg(`mods/px-chrome/${GS()}/${place.name ? "server" : "monitor"}.svg`).size(12).flex_none())
        .child(div().flex_1().truncate().text_size(10.5).font_semibold().text_color(t.muted).child(caps(place.name ?? "This computer")))
        .child(div().text_size(10.5).text_color(place.error ? t.dim : t.muted).child(place.error ? caps(place.connected ? "no shp" : "offline") : String(place.tasks.length))));
      if (place.error && place.connected && !/no such file|not found|executable/i.test(place.error))
        out.push(div().px(12).py(4).truncate().text_size(11).text_color(t.dim).child(place.error));
      for (const s of group(place.tasks)) this.section(t, place, s, out);
    }
    if (!tasks.length) out.push(div().px(12).py(12).text_size(11.5).text_color(t.dim)
      .child(places.length && places.every((p) => p.read) ? "No tasks yet. + adds one." : "Reading tasks…"));
    return v_flex().id("projects").size_full().min_h(0).bg(t.surface).text_color(t.foreground).text_size(13)
      .child(h_flex().h(42).flex_none().px(12).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().font_semibold().child(caps("Tasks")))
        .child(div().flex_1())
        .when(needs > 0, (e) => e.child(chip("p-needs", `${needs} need you`, t.blocked)))
        .when(ready > 0, (e) => e.child(chip("p-ready", `${ready} ready`, t.done)))
        .child(div().id("p-monitor").h(20).px(6).flex().items_center().border_1().border_color(t.border).text_size(10.5).text_color(t.muted)
          .cursor_pointer().hover((s) => s.bg(t.hover).text_color(t.foreground)).on_click(() => openMonitor()).child(caps("Monitor")))
        .child(div().id("p-add").size(22).flex().items_center().justify_center().cursor_pointer().text_color(this.adding ? t.accent : t.muted)
          .hover((s) => s.bg(t.hover).text_color(t.foreground))
          .on_click((_e, cx) => { this.adding = !this.adding; if (this.adding) this.title.focus(); cx.notify(); })
          .child(svg(`mods/px-chrome/${GS()}/plus.svg`).size(13))))
      .when(this.adding, (e) => e.child(this.adder(t, places)))
      .when(Boolean(this.error), (e) => e.child(h_flex().id("p-error").px(12).py(5).gap(8).items_center().bg(t.chrome).border_b(1).border_color(t.blocked)
        .child(div().flex_1().text_size(11).text_color(t.blocked).child(this.error))
        .child(div().id("p-error-x").text_size(11).text_color(t.dim).cursor_pointer().on_click((_e, cx) => { this.error = null; cx.notify(); }).child("✕"))))
      .child(v_flex().id("projects-list").flex_1().min_h(0).overflow_y_scrollbar().pb(10).children(out));
  }

  // ---------- Accept dialog

  accept(task, cx) {
    if (this.busy) return;
    this.busy = keyOf(task); this.error = null; cx.notify();
    cx.spawn(async (cx) => {
      const why = await shp(task.machine, ["accept", task.id, "--yes"]);
      if (!why) { closeOverlay(props().overlay); return; }
      this.busy = null; this.error = why; cx.notify();
    });
  }

  acceptView(t) {
    const task = props().accept, cancel = () => closeOverlay(props().overlay);
    const button = (id, label, bg, fg, border, onClick) => h_flex().id(id).h(32).px(14).items_center().border_1()
      .border_color(border).bg(bg).text_color(fg).cursor_pointer().hover((s) => s.opacity(0.88)).on_click(onClick).child(caps(label));
    return v_flex().id("accept").track_focus(this.handle).size_full().min_h(0).p(18).gap(12).bg(t.popover).text_color(t.foreground).text_size(13)
      .on_key_down((ev, cx) => {
        if (ev.key === "enter") this.accept(task, cx); else if (ev.key === "escape") cancel(); else return;
        cx.stop_propagation();
      })
      .child(h_flex().items_center().child(div().flex_1().text_size(15).font_semibold().child(`Accept ${task.id}${task.machine ? ` on ${task.machine}` : ""}?`))
        .child(div().text_size(11).text_color(t.dim).child("Esc")))
      .child(div().flex_1().min_h(0).text_color(t.muted).child(`${task.title} — runs its space's checks, then merges its branch or opens its pull request.`))
      .when(Boolean(this.error), (e) => e.child(div().text_size(11).text_color(t.blocked).child(this.error)))
      .child(h_flex().justify_end().gap(8).text_size(12)
        .child(button("no", "Cancel", t.raised, t.foreground, t.input, cancel))
        .child(button("yes", this.busy ? "Accepting…" : "Accept", t.solid, t.solid_foreground, t.solid, (_e, cx) => this.accept(task, cx))));
  }

  // ---------- Monitor

  seg(t, frac, n, color, moving) {
    const on = Math.round(Math.max(0, Math.min(1, frac)) * n), head = moving && on > 0 ? this.tick % on : -1;
    return h_flex().gap(1).children(Array.from({ length: n }, (_, i) => div().w(4).h(6)
      .when(i > 0 && i % Math.max(1, Math.round(n / 4)) === 0, (e) => e.ml(2))
      .bg(i === head ? t.foreground : i < on ? color : t.raised)));
  }

  sprite(status, size, big) {
    const k = this.tick, pre = big ? "w32-" : "";
    const name = status === "needs" ? `wait${Math.floor(k / 3) % 2}` : status === "working" || status === "merging" ? `work${k % 4}`
      : status === "ready" || status === "done" ? `ready${Math.floor(k / 4) % 2}` : `queue${Math.floor(k / 5) % 2}`;
    return image(`mods/px-projects/sprites/${pre}${name}.svg`).w(size).h(size).flex_none();
  }

  cell(t, label, pct) {
    const p = Math.max(0, Math.min(100, pct ?? 0)), c = p >= 95 ? t.blocked : p >= 80 ? t.working : t.muted;
    return v_flex().w(150).flex_none().gap(3)
      .child(h_flex().w_full().text_size(10.5).child(div().flex_1().text_color(t.dim).child(label)).child(div().text_color(p >= 80 ? c : t.foreground).child(`${Math.round(p)}%`)))
      .child(h_flex().w_full().gap(1).children(Array.from({ length: 24 }, (_, i) => div().flex_1().h(5).bg(i < Math.round((p / 100) * 24) ? c : t.raised))));
  }

  node(t, task) {
    const st = task.status, col = st === "needs" ? t.blocked : st === "working" || st === "merging" ? t.working : st === "ready" ? t.done : t.dim;
    const word = ({ needs: "WAIT", working: "RUN", merging: "MERGE", ready: "READY", queued: "QUEUE", todo: "IDLE" })[st] ?? String(st).toUpperCase();
    const moving = st === "working" || st === "merging", pct = task.progress ?? null;
    const corner = (pos) => div().absolute().w(7).h(7).border_color(col)
      .when(pos.includes("t"), (e) => e.top(-1).border_t(2)).when(pos.includes("b"), (e) => e.bottom(-1).border_b(2))
      .when(pos.includes("l"), (e) => e.left(-1).border_l(2)).when(pos.includes("r"), (e) => e.right(-1).border_r(2));
    const space = String(task.worktree ?? "").match(/worktrees\/([^/]+)\//)?.[1] ?? String(task.id).replace(/-\d+$/, "");
    const msg = subtitle(task) || (st === "ready" ? task.summary : "") || "no report yet";
    return v_flex().id(`node-${keyOf(task)}`).relative().w(320).flex_none().border_1().border_color(t.border).bg(t.chrome)
      .cursor_pointer().hover((e) => e.bg(t.surface)).on_click(() => openTask(task))
      .child(corner("tl")).child(corner("tr")).child(corner("bl")).child(corner("br"))
      .child(h_flex().h(24).px(8).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().font_family(ID_FONT).text_size(11).font_semibold().child(String(task.id)))
        .child(div().flex_1().truncate().text_size(10).text_color(t.dim).child(String(space).toUpperCase()))
        .child(h_flex().gap(5).items_center().px(5).h(16).border_1().border_color(col)
          .child(div().size(5).bg(moving && this.tick % 4 >= 2 ? t.raised : col))
          .child(div().text_size(10).font_semibold().text_color(col).child(word))))
      .child(v_flex().p(8).gap(6)
        .child(h_flex().gap(10).items_start()
          .child(v_flex().p(3).bg(t.background).border_1().border_color(t.raised).child(this.sprite(st, 64, true)))
          .child(v_flex().flex_1().min_w(0).gap(4)
            .child(div().truncate().text_size(12.5).font_semibold().child(String(task.title ?? "")))
            .child(div().h(32).text_size(11.5).text_color(msg === "no report yet" ? t.dim : st === "needs" ? t.blocked : t.foreground).child(oneLine(msg, 80)))))
        .child(h_flex().gap(8).items_center()
          .child(this.seg(t, (pct ?? 0) / 100, 40, col, moving))
          .child(div().text_size(11).font_semibold().text_color(col).child(pct == null ? "--%" : `${pct}%`)))
        .child(h_flex().gap(10).text_size(10.5).text_color(t.dim)
          .child(div().child(`LEFT ${task.left ? String(task.left).toUpperCase() : "--"}`))
          .when(Boolean(task.review), (e) => e.child(div().child(`REVIEW ${task.review}${task.blocking ? ` · ${task.blocking} BLOCKING` : ""}`)))
          .when(Boolean(task.pr), (e) => e.child(div().text_color(t.accent).child(`PR ${String(task.pr).replace(/^.*\/pull\//, "#")}`)))));
  }

  // A Herdr agent outside shp's tasks, as before: its space and tab, its state; a click shows it.
  agentNode(t, a) {
    const col = a.status === "blocked" ? t.blocked : a.status === "working" ? t.working : t.done;
    const word = ({ blocked: "WAIT", working: "RUN", done: "DONE" })[a.status] ?? String(a.status).toUpperCase();
    const k = this.tick, sprite = a.status === "blocked" ? `wait${Math.floor(k / 3) % 2}` : a.status === "working" ? `work${k % 4}` : `ready${Math.floor(k / 4) % 2}`;
    return v_flex().id(`hnode-${a.address}`).w(320).flex_none().border_1().border_color(t.border).bg(t.chrome)
      .cursor_pointer().hover((e) => e.bg(t.surface))
      .on_click(() => { run("space.select", { id: a.space }); run("tab.select", { id: a.tab }); run("agent.focus", { terminal: a.address }); })
      .child(h_flex().h(24).px(8).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().text_size(11).font_semibold().child(String(a.address)))
        .child(div().flex_1().truncate().text_size(10).text_color(t.dim).child(String(a.space_name ?? "").toUpperCase()))
        .child(h_flex().gap(5).items_center().px(5).h(16).border_1().border_color(col)
          .child(div().size(5).bg(a.status === "working" && this.tick % 4 >= 2 ? t.raised : col))
          .child(div().text_size(10).font_semibold().text_color(col).child(word))))
      .child(h_flex().p(8).gap(10).items_center()
        .child(image(`mods/px-projects/sprites/${sprite}.svg`).w(40).h(40).flex_none())
        .child(v_flex().flex_1().min_w(0).gap(3)
          .child(div().truncate().text_size(12).child(a.tab_name || a.title || "agent"))
          .child(div().truncate().text_size(10.5).text_color(t.dim).child(`${a.program?.name ?? a.app ?? "agent"} · HERDR`))));
  }

  monitorView(t) {
    const places = shared("shp")?.places ?? [];
    const order = ["needs", "working", "merging", "ready"];
    // Terminals a task runs in are shown as its task, not again as plain agents.
    const taskSpaces = new Set(places.flatMap((p) => p.tasks).map((k) => workspaceOf(k)?.id).filter(Boolean));
    const out = [];
    let runN = 0, waitN = 0, queueN = 0;
    for (const place of places) {
      const name = place.name ?? "This computer";
      const head = h_flex().w_full().h(28).px(12).gap(14).items_center().bg(t.chrome).border_b(1).border_t(1).border_color(t.border)
        .child(div().text_size(12).font_semibold().text_color(t.accent).child(`SYS · ${name.toUpperCase()}`));
      if (!place.connected || (place.error && !place.tasks.length)) {
        out.push(head.child(div().text_size(11).text_color(place.connected ? t.dim : t.blocked).child(!place.connected ? "OFFLINE" : place.read ? "NO SHP HERE" : "CONNECTING")));
        continue;
      }
      out.push(head);
      const tasks = place.tasks, h = place.health;
      const working = tasks.filter((k) => k.status === "working").length;
      const busy = h && (h.cpu >= 80 || h.mem >= 80);
      out.push(h_flex().w_full().px(12).py(10).gap(20).items_start().border_b(1).border_color(t.border)
        .child(h_flex().w(170).flex_none().gap(10).items_center()
          .child(image(`mods/px-projects/sprites/rack${Math.floor(this.tick / (busy ? 1 : 4)) % 2}.svg`).w(20).h(28).flex_none())
          .child(v_flex().gap(2).min_w(0)
            .child(div().truncate().text_size(11.5).font_semibold().child(name.toUpperCase()))
            .child(div().text_size(10).text_color(t.dim).child(`SLOTS ${working}/4${h?.cores ? ` · ${h.cores} CORES` : ""}`))))
        .child(h ? h_flex().flex_1().gap(18).items_start().flex_wrap().child(this.cell(t, "CPU", h.cpu)).child(this.cell(t, "MEM", h.mem)).child(this.cell(t, "DISK", h.disk))
          : div().text_size(10.5).text_color(t.dim).child("NO DATA")));
      const live = tasks.filter((k) => order.includes(k.status)).sort((a, b) => order.indexOf(a.status) - order.indexOf(b.status));
      const q = tasks.filter((k) => k.status === "queued" || k.status === "todo");
      const herdr = place.name ? [] : (agents() ?? []).filter((a) => !String(a.address ?? "").includes("/") && !taskSpaces.has(a.space)
        && ["working", "blocked", "done"].includes(a.status))
        .sort((a, b) => ["blocked", "working", "done"].indexOf(a.status) - ["blocked", "working", "done"].indexOf(b.status));
      runN += live.filter((k) => k.status === "working" || k.status === "merging").length + herdr.filter((a) => a.status === "working").length;
      waitN += live.filter((k) => k.status === "needs").length + herdr.filter((a) => a.status === "blocked").length;
      queueN += q.length;
      out.push(live.length || herdr.length ? h_flex().w_full().p(12).gap(12).flex_wrap().children(live.map((k) => this.node(t, k))).children(herdr.map((a) => this.agentNode(t, a)))
        : div().px(12).py(10).text_size(11).text_color(t.dim).child("NO ACTIVE NODES"));
      const waits = tasks.filter((k) => k.status === "needs");
      if (waits.length) out.push(v_flex().w_full().px(12).pb(10).gap(2)
        .child(div().text_size(10.5).font_semibold().text_color(t.blocked).child(`WAITING ON YOU · ${waits.length}`))
        .children(waits.map((k) => h_flex().id(`mw-${keyOf(k)}`).gap(10).text_size(11).cursor_pointer().hover((e) => e.bg(t.hover))
          .on_click(() => openTask(k))
          .child(div().size(6).bg(t.blocked)).child(div().w(70).text_color(t.muted).child(String(k.id)))
          .child(div().flex_1().min_w(0).truncate().child(subtitle(k) || "waiting on you")))));
      if (q.length) out.push(v_flex().w_full().px(12).pb(10).gap(2)
        .child(div().text_size(10.5).font_semibold().text_color(t.dim).child(`QUEUE · ${q.length}`))
        .children(q.slice(0, 12).map((k) => h_flex().gap(10).text_size(11)
          .child(div().w(70).text_color(t.muted).child(String(k.id))).child(div().flex_1().min_w(0).truncate().child(String(k.title ?? "")))
          .child(div().text_color(t.dim).child([k.after?.length ? `AFTER ${k.after.join(", ")}` : null, k.parent ? `UNDER ${k.parent}` : null].filter(Boolean).join(" · ")
            || String(k.status).toUpperCase())))));
    }
    if (!places.length) out.push(div().p(12).text_size(11).text_color(t.dim).child("CONNECTING"));
    const msgs = places.flatMap((p) => p.tasks).filter((k) => k.status !== "done" && subtitle(k)).map((k) => `${k.id} ▸ ${subtitle(k)}`);
    const ticker = msgs.length ? msgs[Math.floor(this.tick / 16) % msgs.length] : "ALL QUIET";
    const top = h_flex().w_full().h(36).flex_none().px(12).gap(16).items_center().bg(t.chrome).border_b(1).border_color(t.accent)
      .child(svg(`mods/px-chrome/${GS()}/layers.svg`).size(14).text_color(t.accent))
      .child(div().text_size(13).font_semibold().child("OPERATIONS MONITOR"))
      .child(h_flex().flex_1().min_w(0).px(10).h(22).items_center().bg(t.background).border_1().border_color(t.border)
        .child(div().text_size(11).text_color(t.done).child(this.tick % 4 < 2 ? "▶ " : "▷ "))
        .child(div().flex_1().min_w(0).truncate().text_size(11).child(oneLine(ticker, 120))))
      .child(div().text_size(11).text_color(t.working).child(`RUN ${runN}`))
      .child(div().text_size(11).text_color(waitN ? t.blocked : t.dim).child(`WAIT ${waitN}`))
      .child(div().text_size(11).text_color(t.dim).child(`QUEUE ${queueN}`))
      .child(div().text_size(10.5).text_color(t.dim).child(new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })));
    return v_flex().id("monitor").size_full().min_h(0).bg(t.background).text_color(t.foreground)
      .child(top).child(v_flex().id("monitor-body").flex_1().min_h(0).overflow_y_scroll().children(out));
  }

  render() {
    const t = theme();
    if (this.view === "accept") return this.acceptView(t);
    if (this.view === "monitor") return this.monitorView(t);
    return this.tasksView(t);
  }
}
