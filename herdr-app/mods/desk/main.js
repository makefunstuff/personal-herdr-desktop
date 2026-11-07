// Desk: a pomodoro timer, focus stats and today's notes, drawn as pixel widgets.
// Linux port without the voice computer: the timer is Herdr's shared "pomodoro" state and the counts are
// "pomodoro.stats", the same ones the Win 3.1 layout's hourglass (px-pomo) uses, so both show one timer and
// one history. Notes are ~/.config/herdr-app/desk-notes/daily/<date>.md.
import * as fs from "fs/promises";
import { View, div, image } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import { appHome, theme, shared, setShared, layout, playSound, notify } from "herdr";

const NOTES = () => `${appHome()}/desk-notes/daily`;
const KEY = "pomodoro", STATS = "pomodoro.stats", MIN = 60 * 1000, WORK = 25 * MIN, BREAK = 5 * MIN;
const pad = (n) => String(n).padStart(2, "0");
const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const mmss = (s) => `${pad(Math.floor(Math.max(0, s) / 60))}:${pad(Math.floor(Math.max(0, s) % 60))}`;
const hm = (min) => (min >= 60 ? `${Math.floor(min / 60)}h ${pad(Math.round(min % 60))}m` : `${Math.round(min)}m`);
const wb = () => shared("layout") === "workbench";
const caps = (s) => (wb() ? String(s).toUpperCase() : String(s));
const findNode = (n, id) => { if (!n || typeof n !== "object") return null; if (n.id === id) return n;
  for (const c of n.children ?? []) { const f = findNode(c, id); if (f) return f; } return null; };
const idle = () => ({ phase: "idle", end: 0, left: WORK, overlay: null });
const state = () => shared(KEY) ?? idle();
const daysAgo = (n) => { const d = new Date(); d.setHours(12, 0, 0, 0); d.setDate(d.getDate() - n); return d; };

// One finished pomodoro of `min` minutes, today. Keeps px-pomo's {days: {date: count}} and adds {min: {date: minutes}}.
function record(min) {
  const st = shared(STATS) ?? { days: {} }, k = ymd(new Date());
  const days = st.days ?? {}, mins = st.min ?? {};
  setShared(STATS, { ...st, days: { ...days, [k]: (days[k] ?? 0) + 1 }, min: { ...mins, [k]: (mins[k] ?? (days[k] ?? 0) * 25) + min } });
}
function statsNow() {
  const st = shared(STATS) ?? { days: {} }, days = st.days ?? {}, mins = st.min ?? {};
  const count = (d) => days[ymd(d)] ?? 0, focus = (d) => mins[ymd(d)] ?? count(d) * 25;
  let n = count(daysAgo(0)) > 0 ? 0 : 1, streak = 0;
  while (count(daysAgo(n)) > 0) { streak++; n++; }
  let week = 0; for (let i = 0; i < 7; i++) week += focus(daysAgo(i));
  return {
    today: { count: count(daysAgo(0)), focus_min: focus(daysAgo(0)) }, week: { focus_min: week }, streak_days: streak,
    days: Array.from({ length: 14 }, (_, i) => { const d = daysAgo(13 - i); return { date: ymd(d), count: count(d) }; }),
    any: Object.keys(days).length > 0,
  };
}

// A day button's label: Today, Yesterday, else the weekday and date ("Mon 02").
function dayName(d) {
  const at = new Date(`${d}T12:00`), now = new Date(), y = new Date(now); y.setDate(now.getDate() - 1);
  if (d === ymd(now)) return "Today";
  if (d === ymd(y)) return "Yesterday";
  return `${"Sun Mon Tue Wed Thu Fri Sat".split(" ")[at.getDay()]} ${d.slice(8)}`;
}

export default class Desk extends View {
  init(_props, cx) {
    this.tick = 0; this.note = null; this.day = ymd(new Date()); this.days = []; this.err = null;
    this.input = InputState.new({ value: "", placeholder: "Add a note…  [ ] for a to-do" });
    this.input.on("submit", (_e, cx) => cx.spawn(async (cx) => { await this.addNote(); cx.notify(); }));
    // Editing one note in place: its words only (a checkbox keeps its box, a timed note its time).
    this.editing = null; // line number in the day's file
    this.editInput = InputState.new({ value: "", placeholder: "Edit the note (Enter saves, Esc cancels)" });
    this.editInput.on("submit", (_e, cx) => cx.spawn(async (cx) => { await this.saveEdit(cx); }));
    cx.timer.every(1000, (cx) => {
      this.tick++; this.step();
      const ph = state().phase, shown = !findNode(layout(), "desk")?.hidden;
      if (shown && (ph === "work" || ph === "break" || this.tick % 4 === 0)) cx.notify();
    });
    cx.spawn(async (cx) => { for (;;) { await this.loadNotes(); cx.notify(); await new Promise((ok) => cx.timer.after(10000, ok)); } });
  }

  // The timer moves on here unless the layout has px-pomo's hourglass ("pomodoro" block), which does it there.
  step() {
    if (findNode(layout(), "pomodoro")) return;
    const s = state();
    if ((s.phase !== "work" && s.phase !== "break") || Date.now() < s.end) return;
    if (s.phase === "work") {
      setShared(KEY, { phase: "break", end: Date.now() + BREAK, left: BREAK, planned: BREAK, overlay: null });
      record(Math.round((s.planned ?? WORK) / MIN));
      playSound("done");
      notify({ title: "Pomodoro done", body: "Break time: stand up, stretch, look away.", timeout: 6000 });
    } else {
      setShared(KEY, idle());
      notify({ title: "Break is over", body: "Back to work: Start on the Desk begins the next one.", timeout: 6000 });
    }
  }
  pomo(cmd, n) {
    const s = state(), now = Date.now();
    if (cmd === "start") setShared(KEY, { phase: "work", end: now + n * MIN, left: n * MIN, planned: n * MIN, overlay: null });
    else if (cmd === "break") setShared(KEY, { phase: "break", end: now + BREAK, left: BREAK, planned: BREAK, overlay: null });
    else if (cmd === "pause" && (s.phase === "work")) setShared(KEY, { ...s, phase: "paused", left: s.end - now, end: 0 });
    else if (cmd === "pause" && s.phase === "break") setShared(KEY, idle());
    else if (cmd === "resume" && s.phase === "paused") setShared(KEY, { ...s, phase: "work", end: now + s.left });
    else if (cmd === "add" && (s.phase === "work" || s.phase === "break")) setShared(KEY, { ...s, end: s.end + n * MIN, planned: (s.planned ?? WORK) + n * MIN });
    else if (cmd === "stop") setShared(KEY, idle());
  }

  async loadNotes() {
    // Past midnight, a Desk that was showing today moves on to the new day.
    const today = ymd(new Date());
    if (this.today && today !== this.today && this.day === this.today) this.day = today;
    this.today = today;
    try { this.note = await fs.readFile(`${NOTES()}/${this.day}.md`, "utf8"); } catch { this.note = null; }
    try { this.days = (await fs.readdir(NOTES())).filter((f) => /^\d{4}-\d\d-\d\d\.md$/.test(f)).map((f) => f.slice(0, 10)).sort().reverse().slice(0, 7); } catch { this.days = []; }
    if (!this.days.includes(today)) this.days = [today, ...this.days].slice(0, 7); // today is a day even before its first note
  }
  async writeNote(day, text) {
    await fs.mkdir(NOTES(), { recursive: true }).catch(() => {});
    await fs.writeFile(`${NOTES()}/${day}.md`, text);
  }
  async toggleLine(n, cx) {
    try {
      const lines = (await fs.readFile(`${NOTES()}/${this.day}.md`, "utf8")).split("\n");
      const l = lines[n]; if (l == null) return;
      lines[n] = l.startsWith("- [ ]") ? "- [x]" + l.slice(5) : l.startsWith("- [x]") ? "- [ ]" + l.slice(5) : l;
      await this.writeNote(this.day, lines.join("\n")); this.err = null;
    } catch (e) { this.err = `Note not saved: ${e.message}`; }
    await this.loadNotes(); cx.notify();
  }
  // Words that start with a checkbox in any of the usual spellings ([ ], [], -[], - [ ], [x]): { done, text }.
  boxOf(text) {
    const m = /^-?\s*\[( |x|X)?\]\s*(.*)$/.exec(text.trim());
    return m ? { done: (m[1] ?? "").toLowerCase() === "x", text: m[2] } : null;
  }
  // A note's line, split into what stays (its "- [ ] " or "- 12:05 ") and its words.
  parts(l) {
    const cb = /^(- \[(?: |x)\] ?)(.*)$/.exec(l); if (cb) return [cb[1], cb[2]];
    const tm = /^(- \d\d:\d\d )(.*)$/.exec(l); if (tm) return [tm[1], tm[2]];
    const b = /^(- )(.*)$/.exec(l); if (b) return [b[1], b[2]];
    return ["", l];
  }
  startEdit(n, cx) {
    const l = (this.note ?? "").split("\n")[n]; if (l == null) return;
    this.editing = n; this.editInput.set_value(this.parts(l)[1]); this.editInput.focus?.(); cx.notify();
  }
  // Rewrites line n of the day's file (null text: removes it). The file is read again first, so a line added
  // meanwhile (voice, another Desk) is kept; the line must still be what was shown.
  async rewriteLine(n, shown, text, cx) {
    try {
      const lines = (await fs.readFile(`${NOTES()}/${this.day}.md`, "utf8")).split("\n");
      if (lines[n] !== shown) { this.err = "The note changed meanwhile; try again."; }
      else {
        const box = text === null ? null : this.boxOf(text);
        if (text === null) lines.splice(n, 1);
        else if (box) lines[n] = `- [${box.done ? "x" : " "}] ${box.text}`; // written as a checkbox: it becomes one
        else lines[n] = this.parts(shown)[0] + text;
        await this.writeNote(this.day, lines.join("\n")); this.err = null;
      }
    } catch (e) { this.err = `Note not saved: ${e.message}`; }
    await this.loadNotes(); cx.notify();
  }
  async saveEdit(cx) {
    const n = this.editing; if (n == null) return;
    const shown = (this.note ?? "").split("\n")[n], text = this.editInput.value().trim();
    this.editing = null;
    if (shown == null) { cx.notify(); return; }
    await this.rewriteLine(n, shown, text ? text : null, cx); // emptied: the note goes
  }

  async addNote() {
    const text = this.input.value().trim(); if (!text) return;
    const day = this.day, now = new Date();
    const box = this.boxOf(text);
    const line = box ? `- [${box.done ? "x" : " "}] ${box.text}` : `- ${pad(now.getHours())}:${pad(now.getMinutes())} ${text}`;
    try {
      let cur = ""; try { cur = await fs.readFile(`${NOTES()}/${day}.md`, "utf8"); } catch { cur = `# ${day}\n`; }
      await this.writeNote(day, (cur.endsWith("\n") ? cur : cur + "\n") + line + "\n");
      this.input.set_value(""); this.err = null;
    } catch (e) { this.err = `Note not saved: ${e.message}`; }
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
    const p = state(), st = { work: "focus", break: "break", paused: "paused" }[p.phase] ?? "idle";
    const left = st === "paused" ? p.left / 1000 : st === "idle" ? 0 : (p.end - Date.now()) / 1000;
    const planned = (p.planned ?? (st === "break" ? BREAK : WORK)) / 1000;
    const frac = st === "idle" ? 0 : 1 - Math.max(0, left) / planned;
    const col = st === "break" ? t.done : st === "paused" ? t.dim : t.accent;
    const sprite = st === "break" ? `cup${this.tick % 2}` : st === "idle" ? `sleep${Math.floor(this.tick / 2) % 2}`
      : st === "paused" ? "tomato0" : `tomato${this.tick % 2}`;
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
          .child(div().truncate().text_size(11.5).text_color(t.dim).child(st === "idle" ? "Press Start: 25 minutes of focus." : st === "break" ? "Stand up, stretch, look away." : "Focusing."))))
      .child(h_flex().w_full().gap(1).children(Array.from({ length: segs }, (_, i) =>
        div().flex_1().h(6).bg(i < on ? col : t.raised).when(st === "focus" && i === on, (e) => e.bg(this.tick % 2 ? t.foreground : col)))))
      .child(h_flex().gap(6).flex_wrap()
        .when(st === "idle", (e) => e.child(this.btn(t, "p-start", "Start 25", (_e, cx) => this.pomo("start", 25), true))
          .child(this.btn(t, "p-50", "50", (_e, cx) => this.pomo("start", 50)))
          .child(this.btn(t, "p-brk", "Break", (_e, cx) => this.pomo("break"))))
        .when(st === "focus" || st === "break", (e) => e.child(this.btn(t, "p-pause", "Pause", (_e, cx) => this.pomo("pause"), true))
          .child(this.btn(t, "p-add", "+5", (_e, cx) => this.pomo("add", 5)))
          .child(this.btn(t, "p-stop", "Stop", (_e, cx) => this.pomo("stop"))))
        .when(st === "paused", (e) => e.child(this.btn(t, "p-res", "Resume", (_e, cx) => this.pomo("resume"), true))
          .child(this.btn(t, "p-stop2", "Stop", (_e, cx) => this.pomo("stop")))));
  }
  statsView(t) {
    const s = statsNow();
    if (!s.any) return div().px(12).text_size(11).text_color(t.dim).child("Stats appear after your first pomodoro.");
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
      })));
  }
  notesView(t) {
    const lines = (this.note ?? "").split("\n");
    const rows = [];
    // Hover shows the note's ×; a click on its words edits it.
    const row = (n, l, lead, words, dim, strike) => h_flex().id(`nl-${n}`).w_full().gap(8).px(4).py(3).items_start()
      .when(this.hoverNote === n, (e) => e.bg(t.hover))
      .on_hover((h, cx) => { if (h) this.hoverNote = n; else if (this.hoverNote === n) this.hoverNote = null; cx.notify(); })
      .child(lead)
      .child(div().id(`nw-${n}`).flex_1().min_w(0).text_size(12).cursor_text().text_color(dim ? t.dim : t.foreground).when(strike, (e) => e.line_through())
        .on_click((_e, cx) => this.startEdit(n, cx)).child(String(words)))
      .child(div().id(`nx-${n}`).w(14).flex_none().text_center().text_size(12).cursor_pointer()
        .text_color(this.hoverNote === n ? t.dim : t.background).hover((s) => s.text_color(t.blocked))
        .on_click((_e, cx) => { cx.stop_propagation(); cx.spawn(async (cx) => this.rewriteLine(n, l, null, cx)); }).child("×"));
    const editor = (n, lead) => h_flex().w_full().gap(8).px(4).py(2).items_center().bg(t.hover)
      .on_key_down((e, cx) => { if (e.key === "escape") { this.editing = null; cx.stop_propagation(); cx.notify(); } })
      .child(lead)
      .child(div().flex_1().min_w(0).border_b(1).border_color(t.accent)
        .child(Input.new(this.editInput).w_full().h(24).px(0).rounded(0).border_0().bg(t.hover).text_size(12).text_color(t.foreground)))
      .child(div().id(`ns-${n}`).px(4).text_size(10).font_semibold().text_color(t.accent).cursor_pointer().on_click((_e, cx) => cx.spawn(async (cx) => this.saveEdit(cx))).child(caps("save")));
    const box = (n, done) => div().id(`nb-${n}`).w(12).h(12).mt(2).flex_none().cursor_pointer().border_1()
      .border_color(done ? t.done : t.muted).bg(done ? t.done : t.input).hover((s) => s.border_color(t.accent))
      .on_click((_e, cx) => { cx.stop_propagation(); cx.spawn(async (cx) => this.toggleLine(n, cx)); });
    const time = (x) => div().w(36).flex_none().text_size(10.5).text_color(t.dim).child(x);
    lines.forEach((l, n) => {
      const cb = /^- \[( |x)\] ?(.*)$/.exec(l), bullet = /^- (.*)$/.exec(l);
      if (cb) {
        const done = cb[1] === "x";
        rows.push(this.editing === n ? editor(n, box(n, done)) : row(n, l, box(n, done), cb[2], done, done));
      } else if (bullet) {
        const m = /^(\d\d:\d\d) (.*)$/.exec(bullet[1]);
        rows.push(this.editing === n ? editor(n, time(m ? m[1] : "·")) : row(n, l, time(m ? m[1] : "·"), m ? m[2] : bullet[1], false, false));
      } else if (l.trim() && !l.startsWith("# ")) {
        rows.push(this.editing === n ? editor(n, time("")) : row(n, l, time(""), l, true, false));
      }
    });
    return v_flex().w_full().px(12).gap(2)
      .when(this.days.length > 1, (e) => e.child(h_flex().gap(4).pb(4).flex_wrap().children(this.days.map((d) =>
        h_flex().id(`day-${d}`).px(5).h(18).items_center().border_1().border_color(d === this.day ? t.accent : t.border)
          .text_size(10).text_color(d === this.day ? t.accent : t.dim).cursor_pointer()
          .on_click((_e, cx) => { this.day = d; cx.spawn(async (cx) => { await this.loadNotes(); cx.notify(); }); }).child(dayName(d))))))
      .children(rows.length ? rows : [div().py(4).text_size(11.5).text_color(t.dim).child(this.note === null ? "No notes yet today. Type one below." : "Empty.")])
      .child(h_flex().pt(6).items_center().border_1().border_color(t.border).bg(t.input)
        .child(div().pl(8).pr(4).text_color(t.accent).child("›"))
        .child(Input.new(this.input).flex_1().h(28).px(4).rounded(0).border_0().bg(t.input).text_size(12).text_color(t.foreground)));
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
        .when(Boolean(this.err), (e) => e.child(div().mx(12).mt(8).text_size(11).text_color(t.blocked).child(this.err)))
        .child(this.section(t, "Pomodoro", null)).child(this.timer(t))
        .child(this.section(t, "Focus")).child(this.statsView(t))
        .child(this.section(t, "Notes", (() => { const open = ((this.note ?? "").match(/^- \[ \]/gm) ?? []).length;
          return `${open ? `${open} to do · ` : ""}${this.day === ymd(now) ? "today" : this.day}`; })())).child(this.notesView(t)));
  }
}
