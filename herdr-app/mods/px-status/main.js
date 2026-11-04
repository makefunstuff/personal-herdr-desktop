import { View, div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { spaces, agents, theme, run, window, shared, panes, layout, exec, home } from "herdr";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";

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
    // the pomodoro chip: read the voice computer's timer every 2 s (its countdown is drawn every second)
    this.pomo = null;
    cx.spawn(async (cx) => { for (;;) {
      try { const r = await exec(`${home()}/.local/bin/vc`, ["status"]); this.pomo = JSON.parse(String(r.stdout)).pomo ?? null; } catch { this.pomo = null; }
      cx.notify(); await new Promise((ok) => cx.timer.after(2000, ok)); } });
    cx.spawn(async (cx) => { for (;;) { await new Promise((ok) => cx.timer.after(1000, ok)); if (this.pomo?.state === "focus" || this.pomo?.state === "break") cx.notify(); } });
    // Keep this Mac's Shepherdr agents visible in Herdr (one space per Shepherdr space, one tab per agent).
    cx.spawn(async () => { for (;;) {
      await exec("python3", [`${home()}/.config/herdr-app/bin/shepherdr-desktop-sync.py`]).catch(() => null);
      await new Promise((ok) => cx.timer.after(5000, ok)); } });
  }
  render() {
    const t = theme();
    const space = spaces().find(s => s.active);
    const list = agents();
    const needs = list.filter(a => a.status === "blocked").length;
    const label = (icon, text) => h_flex().gap(6).items_center().child(svg(`mods/px-chrome/${GS()}/${icon}.svg`).size(13)).child(text);
    return h_flex().size_full().px(14).gap(16).items_center().bg(t.chrome).border_t(1).border_color(t.border).text_size(11.8).text_color(t.muted)
      .child(label(space?.machine ? "server" : "monitor", space?.machine ?? "This computer"))
      .when(Boolean(space?.branch), el => el.child(label("branch", space.branch)))
      .child(h_flex().gap(6).items_center().child(div().size(6).rounded(0).bg(t.dim)).child(`${list.length} agents`)
        .when(needs > 0, el => el.child(div().text_color(t.blocked).child(`· ${needs} needs you`))))
      .child(div().flex_1())
      .when(Boolean(this.pomo && this.pomo.state !== "idle"), el => {
        const p = this.pomo, left = p.ends_at ? (p.ends_at - Date.now()) / 1000 : (p.left_s ?? 0);
        const c = p.state === "break" ? t.done : p.state === "paused" ? t.dim : t.accent;
        const word = { focus: "FOCUS", break: "BREAK", paused: "PAUSED" }[p.state] ?? String(p.state).toUpperCase();
        return el.child(h_flex().id("st-pomo").h(20).px(7).gap(6).items_center().border_1().border_color(c).text_color(c).cursor_pointer()
          .hover(s => s.bg(t.hover)).on_click(() => run("layout.show", { id: "desk" }))
          .child(div().size(6).bg(c)).child(`${word} ${mmss(left)}`).when(Boolean(p.task), e => e.child(div().max_w(140).truncate().text_color(t.muted).child(p.task))));
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
          .on_click(() => openShepherdr(v))
          .child(svg(v === "monitor" ? `mods/px-chrome/${GS()}/layers.svg` : `mods/px-tabs/${GS()}/shepherdr.svg`).size(12))
          .child(text).when(open, el => el.child(div().size(5).bg(t.accent)));
      }))
      .child(div().id("palette").text_color(t.dim).on_click(() => run("app.palette")).child(`${window()?.platform === "macos" ? "⌘" : "Ctrl"} Shift P  commands`));
  }
}
