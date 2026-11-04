import { View, div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { spaces, agents, theme, run, window, shared, panes, layout, exec, home } from "herdr";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
// The workbench layout writes its labels in capitals (DESK, SHEPHERDR, MONITOR, BUS 1 · THIS COMPUTER).
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : x);

// Shepherdr's tabs: show the open one (in whatever space it is), else open it here. Never duplicates.
export function openShepherdr(view) {
  const name = view === "monitor" ? "Monitor" : "Shepherdr";
  const modTabs = new Set(panes().filter((p) => p.mod === "shepherdr").map((p) => p.tab));
  for (const s of spaces()) {
    const tab = (s.tabs ?? []).find((t) => modTabs.has(t.id) && t.name === name);
    if (tab) { if (!s.active) run("space.select", { id: s.id }); run("tab.select", { id: tab.id }); return; }
  }
  run("tab.new", { name, mod: "shepherdr", props: view === "monitor" ? { view: "monitor" } : {} });
}
const shepOpen = (view) => { const name = view === "monitor" ? "Monitor" : "Shepherdr";
  const modTabs = new Set(panes().filter((p) => p.mod === "shepherdr").map((p) => p.tab));
  return spaces().some((s) => (s.tabs ?? []).some((t) => modTabs.has(t.id) && t.name === name)); };

const findNode = (n, id) => { if (!n || typeof n !== "object") return null; if (n.id === id) return n;
  for (const c of n.children ?? []) { const f = findNode(c, id); if (f) return f; } return null; };
const mmss = (s) => `${String(Math.floor(Math.max(0, s) / 60)).padStart(2, "0")}:${String(Math.floor(Math.max(0, s) % 60)).padStart(2, "0")}`;

export default class WorkspaceStatus extends View {
  init(_p, cx) {
    // The pomodoro chip draws the shared timer ("pomodoro", the same one px-pomo and Desk use); redraw once a second while it runs.
    cx.timer.every(1000, (cx) => { const ph = shared("pomodoro")?.phase; if (ph === "work" || ph === "break") cx.notify(); });
    // Keep this computer's Shepherdr agents visible in Herdr (one space per Shepherdr space, one tab per agent),
    // only while a Shepherdr answers here: without one nothing runs.
    cx.spawn(async (cx) => { for (;;) {
      const up = await fetch("http://127.0.0.1:7787/api/snapshot").then((r) => r.ok).catch(() => false);
      if (up) await exec("python3", [`${home()}/.config/herdr-app/bin/shepherdr-desktop-sync.py`]).catch(() => null);
      await new Promise((ok) => cx.timer.after(up ? 5000 : 30000, ok)); } });
  }
  render() {
    const t = theme();
    const space = spaces().find(s => s.active);
    const list = agents();
    const needs = list.filter(a => a.status === "blocked").length;
    const label = (icon, text) => h_flex().gap(6).items_center().child(svg(`mods/px-chrome/${GS()}/${icon}.svg`).size(13)).child(caps(text));
    return h_flex().size_full().px(14).gap(16).items_center().bg(t.chrome).border_t(1).border_color(t.border).text_size(11.8).text_color(t.muted)
      .child(label(space?.machine ? "server" : "monitor", space?.machine ?? "This computer"))
      .when(Boolean(space?.branch), el => el.child(label("branch", space.branch)))
      .child(h_flex().gap(6).items_center().child(div().size(6).rounded(0).bg(t.dim)).child(caps(`${list.length} ${list.length === 1 ? "agent" : "agents"}`))
        .when(needs > 0, el => el.child(div().text_color(t.blocked).child(caps(`· ${needs} needs you`)))))
      .child(div().flex_1())
      .when(!findNode(layout(), "pomodoro") && ["work", "break", "paused"].includes(shared("pomodoro")?.phase), el => {
        const p = shared("pomodoro"), left = (p.phase === "paused" ? p.left : p.end - Date.now()) / 1000;
        const c = p.phase === "break" ? t.done : p.phase === "paused" ? t.dim : t.accent;
        const word = { work: "FOCUS", break: "BREAK", paused: "PAUSED" }[p.phase];
        return el.child(h_flex().id("st-pomo").h(20).px(7).gap(6).items_center().border_1().border_color(c).text_color(c).cursor_pointer()
          .hover(s => s.bg(t.hover)).on_click(() => { if (findNode(layout(), "desk")) run("layout.show", { id: "desk" }); })
          .child(div().size(6).bg(c)).child(`${word} ${mmss(left)}`));
      })
      // Google Cloud (mods/px-gcp's summary): firing alerts in red, pods in trouble; a click opens its panel.
      .when(Boolean(findNode(layout(), "gcp") && shared("gcp.summary")), el => {
        const g = shared("gcp.summary"), duty = Boolean(shared("gcp.duty")), hot = g.alerts > 0;
        const c = hot ? t.blocked : t.done;
        return el.child(h_flex().id("st-gcp").h(20).px(7).gap(6).items_center().border_1().border_color(c).text_color(c).cursor_pointer()
          .when(hot && duty, e => e.bg(t.blocked).text_color(t.background))
          .hover(s => s.bg(t.hover)).on_click(() => { run("layout.select", { stack: "panel-body", child: "gcp" }); run("layout.show", { id: "panel" }); })
          .child(div().size(6).bg(hot && duty ? t.background : c)).child(`GCP ${g.alerts}`)
          .when(g.pods > 0, e => e.child(div().text_color(hot && duty ? t.background : t.working).child(`· ${g.pods} pods`)))
          .when(g.fresh > 0 && duty, e => e.child(div().font_semibold().child("!"))));
      })
      .when(Boolean(findNode(layout(), "desk")), el => {
        const open = !findNode(layout(), "desk").hidden, wbm = shared("layout") === "workbench";
        return el.child(h_flex().id("st-desk").h(20).px(7).gap(6).items_center().border_1().border_color(open ? t.accent : t.border)
          .text_color(open ? t.foreground : t.muted).cursor_pointer().hover(s => s.bg(t.hover).text_color(t.foreground))
          .on_click(() => run("layout.toggle", { id: "desk" }))
          .child(svg(`mods/px-chrome/${GS()}/file-text.svg`).size(12)).child(wbm ? "DESK" : "Desk")
          .when(open, e => e.child(div().size(5).bg(t.accent))));
      })
      .children(["shepherdr", "monitor"].map((v) => {
        const open = shepOpen(v), wb = shared("layout") === "workbench";
        const text = v === "monitor" ? (wb ? "MONITOR" : "Monitor") : (wb ? "SHEPHERDR" : "Shepherdr");
        return h_flex().id(`st-${v}`).h(20).px(7).gap(6).items_center().border_1().border_color(open ? t.accent : t.border)
          .text_color(open ? t.foreground : t.muted).cursor_pointer().hover(s => s.bg(t.hover).text_color(t.foreground))
          // Shepherdr: 3.0's Tasks panel in this layout when it has one; Monitor: the Monitor tab.
          .on_click(() => { if (v === "shepherdr" && findNode(layout(), "tasks")) { run("layout.select", { stack: "panel-body", child: "tasks" }); run("layout.show", { id: "panel" }); } else openShepherdr(v); })
          .child(svg(v === "monitor" ? `mods/px-chrome/${GS()}/layers.svg` : `mods/px-tabs/${GS()}/shepherdr.svg`).size(12))
          .child(text).when(open, el => el.child(div().size(5).bg(t.accent)));
      }))
      .child(div().id("palette").text_color(t.dim).on_click(() => run("app.palette")).child(caps(`${window()?.platform === "macos" ? "⌘" : "Ctrl"} Shift P  commands`)));
  }
}
