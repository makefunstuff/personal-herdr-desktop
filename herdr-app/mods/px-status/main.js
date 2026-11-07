import { View, div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { spaces, agents, theme, run, window, shared, panes, layout } from "herdr";
import { startPoller } from "./poll.js";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
// The workbench layout writes its labels in capitals (DESK, BUS 1 · THIS COMPUTER).
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : x);
const findNode = (n, id) => { if (!n || typeof n !== "object") return null; if (n.id === id) return n;
  for (const c of n.children ?? []) { const f = findNode(c, id); if (f) return f; } return null; };

// The Monitor tab (mods/px-projects with {view: "monitor"}): show it wherever it is open, else open it here.
function openMonitor() {
  const mine = new Set(panes().filter((p) => p.mod === "px-projects").map((p) => p.tab));
  for (const s of spaces()) {
    const tab = (s.tabs ?? []).find((t) => mine.has(t.id) && t.name === "Monitor");
    if (tab) { if (!s.active) run("space.select", { id: s.id }); run("tab.select", { id: tab.id }); return; }
  }
  run("tab.new", { name: "Monitor", mod: "px-projects", props: { view: "monitor" } });
}
const mmss = (s) => `${String(Math.floor(Math.max(0, s) / 60)).padStart(2, "0")}:${String(Math.floor(Math.max(0, s) % 60)).padStart(2, "0")}`;
export default class WorkspaceStatus extends View {
  init(_p, cx) {
    // Our projects manager (shp): one poller for every view, see mods/px-projects/shp.js.
    startPoller(cx);
    // The pomodoro chip draws the shared timer ("pomodoro", the same one px-pomo and Desk use); redraw once a second while it runs.
    cx.timer.every(1000, (cx) => { const ph = shared("pomodoro")?.phase; if (ph === "work" || ph === "break") cx.notify(); });
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
      // Tasks (shp): opens the Tasks panel, red when a task needs you; Monitor: the operations monitor tab.
      .when(Boolean(findNode(layout(), "tasks")), el => {
        const all = (shared("shp")?.places ?? []).flatMap(p => p.tasks);
        const waiting = all.filter(k => k.status === "needs").length, ready = all.filter(k => k.status === "ready").length;
        const c = waiting ? t.blocked : ready ? t.done : t.border;
        return el.child(h_flex().id("st-tasks").h(20).px(7).gap(6).items_center().border_1().border_color(c)
          .text_color(waiting ? t.blocked : t.muted).cursor_pointer().hover(s => s.bg(t.hover).text_color(t.foreground))
          .on_click(() => { run("layout.select", { stack: "panel-body", child: "tasks" }); run("layout.show", { id: "panel" }); })
          .child(svg(`mods/px-chrome/${GS()}/circle-check.svg`).size(12)).child(caps("Tasks"))
          .when(waiting > 0, e => e.child(div().font_semibold().child(String(waiting))))
          .when(!waiting && ready > 0, e => e.child(div().text_color(t.done).child(caps(`${ready} ready`)))))
          .child(h_flex().id("st-monitor").h(20).px(7).gap(6).items_center().border_1().border_color(t.border).text_color(t.muted)
            .cursor_pointer().hover(s => s.bg(t.hover).text_color(t.foreground)).on_click(() => openMonitor())
            .child(svg(`mods/px-chrome/${GS()}/layers.svg`).size(12)).child(caps("Monitor")));
      })
      .when(Boolean(findNode(layout(), "desk")), el => {
        const open = !findNode(layout(), "desk").hidden, wbm = shared("layout") === "workbench";
        return el.child(h_flex().id("st-desk").h(20).px(7).gap(6).items_center().border_1().border_color(open ? t.accent : t.border)
          .text_color(open ? t.foreground : t.muted).cursor_pointer().hover(s => s.bg(t.hover).text_color(t.foreground))
          .on_click(() => run("layout.toggle", { id: "desk" }))
          .child(svg(`mods/px-chrome/${GS()}/file-text.svg`).size(12)).child(wbm ? "DESK" : "Desk")
          .when(open, e => e.child(div().size(5).bg(t.accent))));
      })
      .child(div().id("palette").text_color(t.dim).on_click(() => run("app.palette")).child(caps(`${window()?.platform === "macos" ? "⌘" : "Ctrl"} Shift P  commands`)));
  }
}
