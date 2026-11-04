// Time: where the person's time goes, from herdr-timetrack's samples (~/.local/share/herdr-timetrack/<day>.jsonl, one
// every 30 s: the focused window, and Herdr's space and tab). Today and this week: active time, by project (Herdr space),
// by category (Herdr, GitHub, Google Cloud, Jira, Slack, meetings, mail, browsing…), by ticket (ABC-123 keys found in
// titles, spaces and tabs), and the day as a strip. Idle and locked samples don't count. Local only; reads, never sends.
import * as fs from "fs/promises";
import { View, div } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { theme, shared, home } from "herdr";

const STEP = 30; // seconds a sample stands for
const caps = (s) => (shared("layout") === "workbench" ? String(s).toUpperCase() : String(s));
const pad = (n) => String(n).padStart(2, "0");
const day = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const hm = (s) => { const m = Math.round(s / 60); return m < 60 ? `${m}m` : `${Math.floor(m / 60)}h ${pad(m % 60)}m`; };
// Issue keys (ABC-123), minus things that look like them (UTF-8, ISO-8859, SHA-256…).
const TICKET = /\b(?!(?:UTF|ISO|SHA|MD|AES|RFC|CVE|TLS|SSL|HTTP|IPV|X)-)[A-Z][A-Z0-9]{1,9}-\d{1,6}\b/g;

// What a sample was: category, project, tickets.
function classify(r) {
  const app = (r.app || "").toLowerCase(), title = r.title || "";
  const tickets = [...new Set([...(title.match(TICKET) ?? []), ...((r.space || "").match(TICKET) ?? []), ...((r.tab || "").match(TICKET) ?? [])])];
  let cat = "Other", project = null;
  if (app === "herdr-app") { cat = "Herdr"; project = r.space || "Herdr"; }
  else if (app === "slack") cat = "Slack";
  else if (app.includes("hey") || /gmail|outlook|mail/i.test(title)) cat = "Mail";
  else if (/chromium|chrome|firefox|brave|safari|arc|edge|zen/.test(app)) {
    cat = /meet\.google|Meet -|zoom/i.test(title) ? "Meetings" : /pull request|github/i.test(title) ? "GitHub" : /google cloud|console\.cloud|gcp|kubernetes engine|cloud monitoring/i.test(title) ? "Google Cloud"
      : /jira|atlassian|confluence/i.test(title) ? "Jira" : /grafana|sentry|argo/i.test(title) ? "Observability" : /slack/i.test(title) ? "Slack" : "Browsing";
  } else if (/alacritty|ghostty|kitty|foot|terminal|iterm|wezterm/.test(app)) cat = "Terminal";
  if (!project && tickets.length) project = tickets[0];
  return { cat, project, tickets };
}

const CAT_TONE = { Herdr: "accent", GitHub: "done", "Google Cloud": "working", Jira: "info", Slack: "violet", Meetings: "blocked", Mail: "muted", Observability: "working", Browsing: "dim", Terminal: "accent", Other: "dim" };

export default class Time extends View {
  init(_p, cx) {
    this.days = {}; this.range = "today"; this.error = null;
    cx.spawn(async (cx) => { await this.load(); cx.notify(); });
    cx.timer.every(60000, (cx) => cx.spawn(async (cx) => { await this.load(); cx.notify(); }));
  }

  async load() {
    const now = new Date(), out = {};
    for (let i = 0; i < 7; i++) {
      const d = new Date(now); d.setDate(now.getDate() - i);
      const k = day(d);
      try {
        const text = await fs.readFile(`${home()}/.local/share/herdr-timetrack/${k}.jsonl`, "utf8");
        out[k] = text.split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter(Boolean);
      } catch { out[k] = []; }
    }
    this.days = out; this.error = null;
  }

  sum(samples) {
    const active = samples.filter((r) => !r.idle && !r.locked);
    const by = (f) => { const m = new Map(); for (const r of active) for (const k of [].concat(f(r) ?? [])) m.set(k, (m.get(k) ?? 0) + STEP); return [...m.entries()].sort((a, b) => b[1] - a[1]); };
    return {
      active: active.length * STEP, idle: samples.filter((r) => r.idle).length * STEP,
      cats: by((r) => classify(r).cat), projects: by((r) => classify(r).project), tickets: by((r) => classify(r).tickets),
      first: samples[0]?.t ?? null, last: samples[samples.length - 1]?.t ?? null,
    };
  }

  col(t, cat) { const k = CAT_TONE[cat] ?? "dim"; return { accent: t.accent, done: t.done, working: t.working, info: t.ansi12 ?? t.accent, violet: t.ansi13 ?? t.muted, blocked: t.blocked, muted: t.muted, dim: t.dim }[k]; }

  bars(t, rows, total, colour) {
    return v_flex().w_full().gap(3).children(rows.slice(0, 10).map(([k, v]) => h_flex().w_full().gap(8).items_center()
      .child(div().w(150).flex_none().truncate().text_size(11).text_color(t.foreground).child(k))
      .child(h_flex().flex_1().h(8).bg(t.raised).child(div().h_full().w(`${Math.max(1, (v / Math.max(1, total)) * 100)}%`).bg(colour ? colour(k) : t.accent)))
      .child(div().w(62).flex_none().text_right().text_size(11).text_color(t.muted).child(hm(v)))));
  }
  bar(t, label, right) {
    return h_flex().w_full().h(22).mt(12).flex_none().items_center().border_b(1).border_color(t.border)
      .child(div().h_full().px(8).flex().items_center().bg(t.raised).text_size(10).font_semibold().text_color(t.foreground).child(caps(label)))
      .child(div().flex_1()).when(Boolean(right), (e) => e.child(div().px(6).text_size(10).text_color(t.dim).child(right)));
  }

  // The day as 10-minute slots from the first to the last sample, coloured by the slot's main category.
  strip(t, samples) {
    if (!samples.length) return div();
    const start = new Date(samples[0].t * 1000); start.setMinutes(Math.floor(start.getMinutes() / 10) * 10, 0, 0);
    const slots = new Map();
    for (const r of samples) {
      const k = Math.floor((r.t * 1000 - start.getTime()) / 600000);
      const s = slots.get(k) ?? new Map();
      const c = r.locked ? "locked" : r.idle ? "idle" : classify(r).cat;
      s.set(c, (s.get(c) ?? 0) + 1); slots.set(k, s);
    }
    const n = Math.max(...slots.keys()) + 1;
    const cells = Array.from({ length: n }, (_, i) => {
      const s = slots.get(i); if (!s) return div().flex_1().h(18).bg(t.background);
      const top = [...s.entries()].sort((a, b) => b[1] - a[1])[0][0];
      return div().flex_1().h(18).bg(top === "idle" || top === "locked" ? t.raised : this.col(t, top));
    });
    const hours = []; for (let i = 0; i < n; i += 6) hours.push(i);
    return v_flex().w_full().gap(2)
      .child(h_flex().w_full().gap(1).children(cells))
      .child(h_flex().w_full().children(Array.from({ length: n }, (_, i) => div().flex_1().text_size(9).text_color(t.dim)
        .child(i % 6 === 0 ? `${pad(new Date(start.getTime() + i * 600000).getHours())}:00` : ""))));
  }

  render() {
    const t = theme(), today = day(new Date());
    const samples = this.range === "today" ? (this.days[today] ?? []) : Object.values(this.days).flat();
    const s = this.sum(samples), any = Object.values(this.days).some((x) => x.length);
    const tab = (id, label) => div().id(`rng-${id}`).h(20).px(8).flex().items_center().text_size(10).font_semibold().cursor_pointer()
      .when(this.range === id, (e) => e.bg(t.accent).text_color(t.accent_foreground)).when(this.range !== id, (e) => e.border_1().border_color(t.border).text_color(t.dim))
      .on_click((_e, cx) => { this.range = id; cx.notify(); }).child(caps(label));
    const body = v_flex().id("time-body").flex_1().min_h(0).overflow_y_scrollbar().px(10).pb(12);
    if (!any) body.child(v_flex().pt(12).gap(6).text_size(12).text_color(t.dim)
      .child(div().child("No samples yet. The tracker isn't running."))
      .child(div().text_size(11).child("Start it: systemctl --user enable --now herdr-timetrack, on a Mac ./install.sh --timetrack (samples the focused window every 30 s, kept in ~/.local/share/herdr-timetrack)")));
    else {
      body.child(h_flex().w_full().mt(10).gap(6)
        .child(v_flex().flex_1().px(8).py(5).border_1().border_color(t.accent).bg(t.chrome).child(div().text_size(9.5).text_color(t.dim).child(caps("active"))).child(div().text_size(20).font_semibold().text_color(t.accent).child(hm(s.active))))
        .child(v_flex().flex_1().px(8).py(5).border_1().border_color(t.border).bg(t.chrome).child(div().text_size(9.5).text_color(t.dim).child(caps("idle"))).child(div().text_size(20).font_semibold().text_color(t.muted).child(hm(s.idle))))
        .child(v_flex().flex_1().px(8).py(5).border_1().border_color(t.border).bg(t.chrome).child(div().text_size(9.5).text_color(t.dim).child(caps(this.range === "today" ? "since" : "days"))).child(div().text_size(20).font_semibold().text_color(t.foreground)
          .child(this.range === "today" ? (s.first ? `${pad(new Date(s.first * 1000).getHours())}:${pad(new Date(s.first * 1000).getMinutes())}` : "–") : String(Object.values(this.days).filter((x) => x.length).length)))));
      if (this.range === "today") { body.child(this.bar(t, "The day", "10-minute slots")); body.child(div().mt(6).child(this.strip(t, samples))); }
      else {
        body.child(this.bar(t, "Per day", null));
        const ds = Object.keys(this.days).sort();
        const max = Math.max(1, ...ds.map((k) => this.sum(this.days[k]).active));
        body.child(h_flex().w_full().mt(6).h(70).gap(4).items_end().children(ds.map((k) => { const a = this.sum(this.days[k]).active;
          return v_flex().flex_1().items_center().gap(2).child(div().text_size(9).text_color(t.dim).child(a ? hm(a) : ""))
            .child(div().w_full().h(Math.max(2, Math.round((a / max) * 46))).bg(k === today ? t.accent : t.muted))
            .child(div().text_size(9).text_color(k === today ? t.accent : t.dim).child("SMTWTFS"[new Date(`${k}T12:00`).getDay()])); })));
      }
      body.child(this.bar(t, "By category", null)); body.child(div().mt(6).child(this.bars(t, s.cats, s.active, (k) => this.col(t, k))));
      body.child(this.bar(t, "By project", "Herdr spaces, tickets")); body.child(div().mt(6).child(this.bars(t, s.projects, s.active)));
      if (s.tickets.length) { body.child(this.bar(t, "By ticket", "ABC-123 keys in titles")); body.child(div().mt(6).child(this.bars(t, s.tickets, s.active, () => t.done))); }
    }
    return v_flex().id("time").size_full().min_h(0).overflow_hidden().bg(t.surface).border_1().border_color(t.border).text_color(t.foreground).text_size(13)
      .child(h_flex().h(42).flex_none().px(14).gap(6).items_center().border_b(1).border_color(t.border)
        .child(div().font_semibold().child(caps("Time"))).child(div().flex_1()).child(tab("today", "Today")).child(tab("week", "7 days")))
      .child(body);
  }
}
