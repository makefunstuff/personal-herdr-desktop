// Desk: the secretary's pomodoro timer, focus stats and today's notes, drawn as pixel widgets.
// Data comes from the voice computer (vc status / vc pomo stats --json) and ~/Work/desk-notes/daily/*.md;
// every action goes through `vc` (vc pomo …, vc note …), so the voice and these buttons stay in step.
import * as fs from "fs/promises";
import { View, div, image } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import { exec, home, theme, shared } from "herdr";

const VC = () => `${home()}/.local/bin/vc`;
const NOTES = () => `${home()}/Work/desk-notes/daily`;
const pad = (n) => String(n).padStart(2, "0");
const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const mmss = (s) => `${pad(Math.floor(Math.max(0, s) / 60))}:${pad(Math.floor(Math.max(0, s) % 60))}`;
const hm = (min) => (min >= 60 ? `${Math.floor(min / 60)}h ${pad(Math.round(min % 60))}m` : `${Math.round(min)}m`);
const wb = () => shared("layout") === "workbench";
const caps = (s) => (wb() ? String(s).toUpperCase() : String(s));

async function vc(args) {
  const r = await exec(VC(), args);
  if (r.code && r.code !== 0) throw new Error((r.stderr || r.stdout || "vc failed").trim().split("\n").pop());
  return String(r.stdout ?? "");
}

export default class Desk extends View {
  init(_props, cx) {
    this.tick = 0; this.pomo = null; this.stats = null; this.note = null; this.day = ymd(new Date());
    this.days = []; this.err = null; this.vcUp = true;
    this.input = InputState.new({ value: "", placeholder: "Add a note… (Enter)" });
    this.input.on("submit", (_e, cx) => cx.spawn(async (cx) => { await this.addNote(); cx.notify(); }));
    cx.spawn(async (cx) => { for (;;) { await new Promise((ok) => cx.timer.after(250, ok)); this.tick++; cx.notify(); } });
    cx.spawn(async (cx) => { for (;;) { await this.loadStatus(); cx.notify(); await new Promise((ok) => cx.timer.after(2000, ok)); } });
    cx.spawn(async (cx) => { for (;;) { await this.loadStats(); await this.loadNotes(); cx.notify(); await new Promise((ok) => cx.timer.after(10000, ok)); } });
  }

  async loadStatus() {
    try { const s = JSON.parse(await vc(["status"])); this.pomo = s.pomo ?? null; this.vcUp = true; this.err = null; }
    catch (e) { this.vcUp = false; this.pomo = null; }
  }
  async loadStats() {
    try { this.stats = JSON.parse(await vc(["pomo", "stats", "--json"])); } catch { this.stats = null; }
  }
  async loadNotes() {
    try { this.note = await fs.readFile(`${NOTES()}/${this.day}.md`, "utf8"); } catch { this.note = null; }
    try { this.days = (await fs.readdir(NOTES())).filter((f) => /^\d{4}-\d\d-\d\d\.md$/.test(f)).map((f) => f.slice(0, 10)).sort().reverse().slice(0, 7); } catch { this.days = []; }
  }
  act(args, cx) {
    cx.spawn(async (cx) => {
      try { await vc(args); this.err = null; } catch (e) { this.err = e.message; }
      await this.loadStatus(); await this.loadStats(); await this.loadNotes(); cx.notify();
    });
  }
  async addNote() {
    const text = this.input.value().trim(); if (!text) return;
    try { await vc(["note", "add", text]); this.input.set_value(""); this.err = null; } catch (e) { this.err = e.message; }
    await this.loadNotes();
  }

  // ---------------------------------------------------------------- pieces
  btn(t, id, label, on, primary) {
    return h_flex().id(id).h(24).px(9).items_center().border_1().border_color(primary ? t.accent : t.border)
      .bg(primary ? t.accent : t.raised).text_color(primary ? t.accent_foreground : t.foreground).text_size(11).font_semibold()
      .cursor_pointer().hover((s) => s.opacity(0.85)).on_click(on).child(caps(label));
  }
  section(t, title, right) {
    return h_flex().w_full().px(12).pt(12).pb(6).gap(8).items_center()
      .child(div().text_size(10.5).font_semibold().text_color(t.muted).child(caps(title)))
      .child(div().flex_1().h(1).bg(t.border))
      .when(Boolean(right), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(right)));
  }
  timer(t) {
    const p = this.pomo, st = p?.state ?? "idle";
    const left = p?.ends_at ? (p.ends_at - Date.now()) / 1000 : (p?.left_s ?? 0);
    const planned = p?.planned_s || 1500;
    const frac = st === "idle" ? 0 : 1 - Math.max(0, left) / planned;
    const col = st === "break" ? t.done : st === "paused" ? t.dim : t.accent;
    const sprite = st === "break" ? `cup${Math.floor(this.tick / 3) % 2}` : st === "idle" ? `sleep${Math.floor(this.tick / 4) % 2}`
      : st === "paused" ? "tomato0" : `tomato${Math.floor(this.tick / 2) % 2}`;
    const word = { focus: "Focus", break: "Break", paused: "Paused", idle: "Ready" }[st] ?? st;
    const segs = 32, on = Math.round(frac * segs);
    return v_flex().w_full().px(12).gap(8)
      .child(h_flex().gap(12).items_center()
        .child(v_flex().p(4).bg(t.background).border_1().border_color(t.border).child(image(`mods/desk/sprites/${sprite}.svg`).w(64).h(64)))
        .child(v_flex().flex_1().min_w(0).gap(2)
          .child(h_flex().gap(6).items_center()
            .child(div().size(6).bg(st === "focus" && this.tick % 4 < 2 ? t.raised : col))
            .child(div().text_size(10.5).font_semibold().text_color(col).child(caps(word))))
          .child(div().text_size(30).font_semibold().text_color(st === "idle" ? t.muted : t.foreground).child(st === "idle" ? mmss(1500) : mmss(left)))
          .child(div().truncate().text_size(11.5).text_color(t.dim).child(p?.task ? p.task : st === "idle" ? "Say “start a pomodoro”, or press Start." : "no task"))))
      .child(h_flex().w_full().gap(1).children(Array.from({ length: segs }, (_, i) =>
        div().flex_1().h(6).bg(i < on ? col : t.raised).when(st === "focus" && i === on, (e) => e.bg(this.tick % 2 ? t.foreground : col)))))
      .child(h_flex().gap(6).flex_wrap()
        .when(st === "idle", (e) => e.child(this.btn(t, "p-start", "Start 25", (_e, cx) => this.act(["pomo", "start", "25"], cx), true))
          .child(this.btn(t, "p-50", "50", (_e, cx) => this.act(["pomo", "start", "50"], cx)))
          .child(this.btn(t, "p-brk", "Break", (_e, cx) => this.act(["pomo", "break"], cx))))
        .when(st === "focus" || st === "break", (e) => e.child(this.btn(t, "p-pause", "Pause", (_e, cx) => this.act(["pomo", "pause"], cx), true))
          .child(this.btn(t, "p-add", "+5", (_e, cx) => this.act(["pomo", "add", "5"], cx)))
          .child(this.btn(t, "p-stop", "Stop", (_e, cx) => this.act(["pomo", "stop"], cx))))
        .when(st === "paused", (e) => e.child(this.btn(t, "p-res", "Resume", (_e, cx) => this.act(["pomo", "resume"], cx), true))
          .child(this.btn(t, "p-stop2", "Stop", (_e, cx) => this.act(["pomo", "stop"], cx)))));
  }
  statsView(t) {
    const s = this.stats;
    if (!s) return div().px(12).text_size(11).text_color(t.dim).child("Stats appear after your first pomodoro.");
    const today = s.today?.count ?? 0;
    const days = s.days ?? [];
    const max = Math.max(1, ...days.map((d) => d.count ?? 0));
    const H = 44;
    return v_flex().w_full().px(12).gap(10)
      .child(h_flex().gap(3).items_center().flex_wrap()
        .children(Array.from({ length: Math.max(4, Math.min(12, today + (today % 4 ? 4 - (today % 4) : 0) || 4)) }, (_, i) =>
          image(`mods/desk/sprites/mini-${i < today ? "on" : "off"}.svg`).size(16)))
        .child(div().pl(6).text_size(11).text_color(t.dim).child(`${today} today`)))
      .child(h_flex().gap(16)
        .child(v_flex().child(div().text_size(18).font_semibold().child(hm(s.today?.focus_min ?? 0))).child(div().text_size(10).text_color(t.dim).child(caps("focus today"))))
        .child(v_flex().child(div().text_size(18).font_semibold().child(hm(s.week?.focus_min ?? 0))).child(div().text_size(10).text_color(t.dim).child(caps("this week"))))
        .child(v_flex().child(div().text_size(18).font_semibold().text_color((s.streak_days ?? 0) > 1 ? t.accent : t.foreground).child(`${s.streak_days ?? 0}d`)).child(div().text_size(10).text_color(t.dim).child(caps("streak")))))
      .child(h_flex().w_full().h(H + 14).gap(3).items_end().children(days.map((d, i) => {
        const h = Math.round(((d.count ?? 0) / max) * H), isToday = i === days.length - 1;
        return v_flex().flex_1().items_center().gap(2)
          .child(v_flex().w_full().h(H).justify_end().child(div().w_full().h(Math.max(2, h)).bg(d.count ? (isToday ? t.accent : t.muted) : t.raised)))
          .child(div().text_size(9).text_color(isToday ? t.accent : t.dim).child("SMTWTFS"[new Date(d.date + "T12:00").getDay()]));
      })))
      .when((s.tasks ?? []).length > 0, (e) => e.child(v_flex().gap(2).children(s.tasks.slice(0, 4).map((k) =>
        h_flex().gap(8).text_size(11).child(div().flex_1().min_w(0).truncate().child(k.task ?? "no task")).child(div().text_color(t.dim).child(hm(k.focus_min)))))));
  }
  notesView(t) {
    const lines = (this.note ?? "").split("\n");
    const rows = [];
    lines.forEach((l, n) => {
      const cb = /^- \[( |x)\] ?(.*)$/.exec(l), bullet = /^- (.*)$/.exec(l);
      if (cb) {
        const done = cb[1] === "x";
        rows.push(h_flex().id(`nl-${n}`).w_full().gap(8).py(2).items_start().cursor_pointer().hover((s) => s.bg(t.hover))
          .on_click((_e, cx) => this.act(["note", "toggle", this.day, String(n + 1)], cx))
          .child(div().w(12).h(12).mt(2).flex_none().border_1().border_color(done ? t.done : t.border).bg(done ? t.done : t.input))
          .child(div().flex_1().min_w(0).text_size(12).text_color(done ? t.dim : t.foreground).when(done, (e) => e.line_through()).child(cb[2])));
      } else if (bullet) {
        const m = /^(\d\d:\d\d) (.*)$/.exec(bullet[1]);
        rows.push(h_flex().w_full().gap(8).py(2).items_start()
          .child(div().w(36).flex_none().text_size(10.5).text_color(t.dim).child(m ? m[1] : "·"))
          .child(div().flex_1().min_w(0).text_size(12).child(m ? m[2] : bullet[1])));
      } else if (l.trim() && !l.startsWith("# ")) {
        rows.push(div().w_full().py(2).text_size(12).text_color(t.muted).child(l));
      }
    });
    return v_flex().w_full().px(12).gap(2)
      .when(this.days.length > 1, (e) => e.child(h_flex().gap(4).pb(4).flex_wrap().children(this.days.map((d) =>
        h_flex().id(`day-${d}`).px(5).h(18).items_center().border_1().border_color(d === this.day ? t.accent : t.border)
          .text_size(10).text_color(d === this.day ? t.accent : t.dim).cursor_pointer()
          .on_click((_e, cx) => { this.day = d; cx.spawn(async (cx) => { await this.loadNotes(); cx.notify(); }); }).child(d.slice(5))))))
      .children(rows.length ? rows : [div().py(4).text_size(11.5).text_color(t.dim).child(this.note === null ? "No notes yet today. Say “note: …” or type below." : "Empty.")])
      .when(this.day === ymd(new Date()), (e) => e.child(h_flex().pt(6).items_center().border_1().border_color(t.border).bg(t.input)
        .child(div().pl(8).pr(4).text_color(t.accent).child("›"))
        .child(Input.new(this.input).flex_1().h(28).px(4).rounded(0).border_0().bg(t.input).text_size(12).text_color(t.foreground))));
  }

  render() {
    const t = theme();
    const now = new Date();
    return v_flex().id("desk").size_full().min_h(0).bg(t.chrome).text_color(t.foreground)
      .child(h_flex().w_full().h(36).flex_none().px(12).gap(8).items_center().border_b(1).border_color(t.border)
        .child(image(`mods/desk/sprites/mini-on.svg`).size(14))
        .child(div().flex_1().text_size(12.5).font_semibold().child(caps("Desk")))
        .child(div().text_size(10.5).text_color(t.dim).child(now.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" }))))
      .child(v_flex().id("desk-body").flex_1().min_h(0).overflow_y_scroll().pb(12)
        .when(!this.vcUp, (e) => e.child(div().m(12).p(8).border_1().border_color(t.blocked).text_size(11).text_color(t.blocked)
          .child("The voice computer isn't running: start it with vc start (or the mic button).")))
        .when(Boolean(this.err), (e) => e.child(div().mx(12).mt(8).text_size(11).text_color(t.blocked).child(this.err)))
        .child(this.section(t, "Pomodoro", this.pomo?.cycle ? `round ${this.pomo.cycle}` : null)).child(this.timer(t))
        .child(this.section(t, "Focus")).child(this.statsView(t))
        .child(this.section(t, "Notes", this.day === ymd(now) ? "today" : this.day)).child(this.notesView(t)));
  }
}
