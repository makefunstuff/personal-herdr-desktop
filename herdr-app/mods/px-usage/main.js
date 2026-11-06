// Usage limits of the AI providers this machine uses, read by herdr-usage (Claude Code, Codex, API keys…).
// Every 5 minutes (herdr-usage caches for 4); a click on the header asks the providers again.

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, home, theme, shared } from "herdr";

// The workbench layout writes its headings and labels in capitals (SPACES, AGENTS, BUS 1 · THIS COMPUTER).
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : x);
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
// A segmented meter like the Monitor's: thin cells, grey until it nears its limit (amber), then red.
export function meterBar(t, pct, color, cells = 24) {
  const on = Math.round((Math.max(0, Math.min(100, pct ?? 0)) / 100) * cells);
  return h_flex().w_full().gap(1).children(Array.from({ length: cells }, (_, i) => div().flex_1().h(5).bg(i < on ? color : t.raised)));
}

const EVERY = 5 * 60 * 1000;
const INSTALL = "github.com/makefunstuff/herdr-android/tree/main/host/herdr-usage";

function until(at) {
  const m = Math.max(0, Math.round((at * 1000 - Date.now()) / 60000));
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h${m % 60}m` : `${Math.floor(h / 24)}d${h % 24}h`;
}

// "Self_Serve_Business_Prolite" -> "Business Prolite"; "max_5x" -> "Max 5x".
const planName = (plan) => String(plan ?? "").replace(/^self[_ ]serve[_ ]/i, "").split(/[_\s]+/).filter(Boolean)
  .map((w) => /^\d/.test(w) ? w : w[0].toUpperCase() + w.slice(1).toLowerCase()).join(" ");
// How old a provider's reading is, when it's older than an hour (a provider not used lately keeps its last one).
function age(readAt) {
  const m = Math.round((Date.now() / 1000 - (readAt ?? 0)) / 60);
  if (!readAt || m < 60) return "";
  return m < 24 * 60 ? `${Math.floor(m / 60)}h old` : `${Math.floor(m / 1440)}d old`;
}

// Why a reading failed, in words: Herdr gives a mod's runtime room for a fixed number of outstanding
// host tasks, and after many reloads of many mods it can run out — then no timer or call will start.
const whyNot = (e) => {
  const m = String(e?.message ?? e ?? "").split("\n")[0].trim();
  return /outstanding host task/i.test(m) ? "Herdr is out of room for its mods: reopen the window" : m || "herdr-usage didn't answer";
};

const hhmm = (at) => {
  const d = new Date(at * 1000);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
};

export default class Usage extends View {
  init(_props, cx) {
    this.data = null;
    this.error = null;
    this.loading = false;
    // Without the timer the limits stand still; the block still draws with the reason, never nothing.
    try {
      cx.timer.every(EVERY, (cx) => this.refresh(cx, false));
      this.refresh(cx, false);
    } catch (e) {
      this.loading = false;
      this.error = whyNot(e);
    }
  }

  refresh(cx, fresh) {
    if (this.loading) return;
    this.loading = true;
    cx.notify();
    try {
      this.fetch(cx, fresh);
    } catch (e) {
      this.loading = false;
      this.error = whyNot(e);
      cx.notify();
    }
  }

  // The read itself, in its own task: a runtime that can't start one is reported, not thrown.
  fetch(cx, fresh) {
    cx.spawn(async (cx) => {
      let r;
      const bin = `${home()}/.local/share/herdr-usage/herdr-usage`, args = fresh ? ["limits", "--fresh"] : ["limits"];
      let failure = null;
      // One retry: a busy machine can hit exec's 30 s limit once without anything being wrong.
      for (let i = 0; i < 2 && !r; i++) { try { r = await exec(bin, args); } catch (e) { failure = e; } }
      if (!r) {
        const why = String(failure?.message ?? failure ?? "");
        // Only a missing program says where it comes from; anything else shows as it is.
        this.error = /no such file|not found|ENOENT/i.test(why) ? `Needs herdr-usage in ~/.local/share/herdr-usage/ (${INSTALL})`
          : `herdr-usage didn't answer (${why.split("\n")[0] || "timed out"}); click the header to retry`;
        this.loading = false;
        cx.notify();
        return;
      }
      try {
        const o = JSON.parse(r.stdout);
        this.data = o;
        this.error = o.ok === false ? (o.error ?? "herdr-usage failed") : null;
      } catch {
        this.error = (r.stderr || r.stdout || "herdr-usage gave no reading").trim().split("\n").pop();
      }
      this.loading = false;
      cx.notify();
    });
  }

  // Grey while there's room; amber near the limit, red at it (the Monitor's meters).
  color(pct) {
    const t = theme();
    return pct >= 90 ? t.blocked : pct >= 70 ? t.working : t.muted;
  }
  tone(pct) {
    const t = theme();
    return pct >= 90 ? t.blocked : pct >= 70 ? t.working : t.foreground;
  }

  row(l) {
    const t = theme();
    const pct = Math.min(100, Math.max(0, l.percent ?? 0));
    return v_flex().gap(3)
      .child(h_flex().gap(6).items_center().text_size(10.5)
        .child(div().flex_1().truncate().text_color(t.dim).child(caps(l.name + (l.estimated ? " ~" : ""))))
        .when(Boolean(l.resets), (el) => el.child(div().text_size(10).text_color(t.dim).child(caps(until(l.resets)))))
        .child(div().w(34).flex_none().text_color(this.tone(pct)).child(`${Math.round(pct)}%`)))
      .child(meterBar(t, pct, this.color(pct)));
  }

  provider(p) {
    const t = theme();
    return v_flex().gap(5)
      .child(h_flex().gap(6).items_center().text_size(11)
        .child(div().flex_none().font_semibold().text_color(t.foreground).child(caps(p.name)))
        .when(Boolean(p.plan), (el) => el.child(div().flex_1().min_w(0).truncate().text_size(10).text_color(t.dim).child(caps(planName(p.plan)))))
        .when(Boolean(age(p.readAt)), (el) => el.child(div().flex_none().text_size(10).text_color(t.working).child(caps(age(p.readAt))))))
      .when(Boolean(p.error), (el) => el.child(div().text_size(10.5).text_color(t.blocked).child(p.error)))
      .children((p.limits ?? []).map((l) => this.row(l)));
  }

  render(cx) {
    const t = theme();
    const shown = (this.data?.providers ?? []).filter((p) => p.limits?.length || p.error);
    return v_flex().size_full().min_h(0).bg(t.chrome).border_t(1).border_color(t.border).text_size(13)
      .child(h_flex().id("usage-head").px(16).h(41).pt(14).pb(5).flex_none().items_center().justify_between()
        .tooltip("Refresh usage limits")
        .on_click((_e, cx) => this.refresh(cx, true))
        .child(div().text_size(11.5).font_medium().text_color(t.dim).child(caps("Usage")))
        .child(this.loading
          ? svg(`mods/px-chrome/${GS()}/spinner.svg`).size(13).text_color(t.dim).spin(900)
          : div().text_size(10).text_color(t.dim).hover((s) => s.text_color(t.foreground))
            .child(this.data?.at ? hhmm(this.data.at) : "")))
      .child(v_flex().id("usage-list").flex_1().min_h(0).overflow_y_scrollbar().px(16).pb(10).gap(10)
        .when(Boolean(this.error), (el) => el.child(div().text_size(11).text_color(t.blocked).child(this.error)))
        .when(!this.error && this.data && shown.length === 0, (el) => el.child(div().text_size(11).text_color(t.dim).child(caps("No limits found."))))
        .children(shown.map((p) => this.provider(p))));
  }
}
