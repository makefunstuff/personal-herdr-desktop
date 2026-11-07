import { View, div, svg, image } from "gpui-kit";
import { h_flex } from "gpui-base";
import { spaces, panes, run, openOverlay, theme, window, shared, layout, settings, readTextFile, appHome, focused, nextCommand, exec, home } from "herdr";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
// The workbench layout writes its labels in capitals (DESK, SHEPHERDR, MONITOR in the status bar).
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : x);
// A bar button like the status bar's DESK/SHEPHERDR/MONITOR: 1px border, the accent border and a small accent dot when on.
const barButton = (t, id, on, icon, label) => h_flex().id(id).h(22).px(7).gap(6).flex_none().items_center().rounded(0)
  .border_1().border_color(on ? t.accent : t.border).text_size(11.5).text_color(on ? t.foreground : t.muted).cursor_pointer()
  .hover(s => s.bg(t.hover).text_color(t.foreground)).on_mouse_down("left", (_e, cx) => cx.stop_propagation())
  .child(svg(icon).size(12)).child(caps(label)).when(on, e => e.child(div().size(5).bg(t.accent)));

function find(node, id) {
  if (node?.id === id) return node;
  for (const child of node?.children ?? []) {
    const found = find(child, id);
    if (found) return found;
  }
  return null;
}

// The layout node called id, if this layout has it (the Shepherdr panel's toggle shows only then).
const findNode = (n, id) => { if (!n || typeof n !== "object") return null; if (n.id === id) return n;
  for (const c of n.children ?? []) { const f = findNode(c, id); if (f) return f; } return null; };

export default class Tabs extends View {
  init(_props, cx) {
    this.pressed = null;
    this.pairMod = false; // Pair's panel is installed (mods/pair): its button shows
    cx.spawn(async (cx) => { this.pairMod = Boolean(await readTextFile(`${appHome()}/mods/pair/main.js`).catch(() => null)); cx.notify(); });
    this.panels = JSON.parse(localStorage.getItem("panels") ?? "{}");
    // cmd-alt-k (keys.json: px-tabs.typing-toggle) mutes and unmutes the typing sounds (herdr-keyclick).
    cx.spawn(async () => { for (;;) {
      const c = await nextCommand();
      if (c === "typing-toggle") await exec(`${home()}/.local/bin/herdr-typing-toggle`, []).catch(() => {});
    } });
  }

  // The side panel is remembered per space: switching shows that space's choice
  // (closed if it never had one), and opening or closing it records it.
  syncPanel(space, panel, body, enabled) {
    if (!space || !panel) return;
    const now = panel.hidden ? null : body?.active ?? null;
    if (space.id !== this.space) {
      const first = this.space === undefined;
      this.space = space.id; this.pending = null;
      if (!(first && !(space.id in this.panels))) {
        const stored = this.panels[space.id] ?? null, want = enabled.includes(stored) ? stored : null;
        if (want !== now) {
          this.pending = { want, until: Date.now() + 1000 };
          if (want) { run("layout.select", { stack: "panel-body", child: want }); run("layout.show", { id: "panel" }); }
          else run("layout.hide", { id: "panel" });
          return;
        }
      }
    }
    // Until the restore lands (or something else won), the old state isn't a choice.
    if (this.pending) {
      if (this.pending.want !== now && Date.now() < this.pending.until) return;
      this.pending = null;
    }
    if (this.panels[space.id] === now) return;
    this.panels[space.id] = now;
    localStorage.setItem("panels", JSON.stringify(this.panels));
  }

  render() {
    const t = theme();
    const space = spaces().find(p => p.active);
    const shown = space?.tabs ?? [];
    const stop = (_e, cx) => cx.stop_propagation();
    const button = (id, label, action) => div().id(id).size(26).flex_none().flex().items_center().justify_center().rounded(0).text_color(t.dim).hover(s => s.bg(t.hover).text_color(t.foreground))
      .on_mouse_down("left", stop).on_click(action).child(label);
    const item = (c, i) => {
      const program = panes().find(p => p.tab === c.id && p.program?.agent)?.program ?? panes().find(p => p.tab === c.id)?.program;
      const name = /^tb\d+$/.test(c.name) ? program?.name ?? "Terminal" : c.name;
      return h_flex().id(`tab-${c.id}`).h(29).px(10).gap(7).rounded(0).items_center().flex_none().text_size(12.8)
        .border(1).border_color(c.active ? t.border : t.chrome).bg(c.active ? t.raised : t.chrome).text_color(c.active ? t.foreground : t.muted)
        .hover(s => s.bg(t.hover))
        .on_mouse_down("left", (e, cx) => { cx.stop_propagation(); this.pressed = i; if (e.click_count === 2) run("tab.rename", { id: c.id }); })
        .on_mouse_up("left", () => {
          const from = this.pressed; this.pressed = null;
          if (from !== null && from !== i) run("tab.move", { id: shown[from].id, index: space.tabs.findIndex(x => x.id === c.id) + 1 });
          else run("tab.select", { id: c.id });
        })
        .on_mouse_down("right", (_e, cx) => { cx.stop_propagation(); openOverlay({ frame: "none", mod: "px-menu", kind: "popup", width: 200, props: { tab: "tab", target: { id: c.id, space: space.id } } }); })
        .when(Boolean(program?.icon), el => el.child(program.tint ? svg(program.icon).size(14).text_color(t.muted) : image(program.icon).size(14)))
        .child(div().max_w(170).truncate().child(caps(c.zoomed ? `${name} Z` : name)))
        .when(c.status === "working", el => el.child(svg(`mods/px-chrome/${GS()}/spinner.svg`).size(12).flex_none().text_color(t.working).spin(900)))
        .when(c.status === "blocked" || c.status === "done", el => el.child(div().size(6).rounded(0).bg(t[c.status])))
        .when(c.active, el => el.child(div().id(`close-${c.id}`).text_color(t.dim).on_mouse_down("left", stop).on_click((_e, cx) => { cx.stop_propagation(); run("tab.close", { id: c.id }); }).child(svg(`mods/px-pane/${GS()}/close.svg`).size(12))));
    };
    const root = layout(), sidebar = find(root, "sidebar"), w = window();
    // Old saved rails and a dragged-narrow sidebar can be smaller than the window buttons.
    const lightsPadding = w?.platform === "macos" && w.titlebar === "none" && !w.fullscreen && root?.traffic_lights
      ? Math.max(10, (root.traffic_lights[0] + 64) / settings().zoom - (sidebar?.hidden ? 0 : sidebar?.size?.px ?? 0))
      : 10;
    const panel = find(root, "panel"), body = find(root, "panel-body");
    // The welcome's "workspace.panels" picks Files/Changes; the Jira panel is always on where the layout has one.
    const picked = shared("workspace.panels") ?? ["files", "changes"];
    const enabled = picked.includes("jira") || !findNode(layout(), "jira") ? picked : [...picked, "jira"];
    this.syncPanel(space, panel, body, enabled);
    const panelButton = (id, label, icon) => {
      const on = panel && !panel.hidden && body?.active === id;
      return barButton(t, `panel-${id}`, on, icon, label)
        .on_click(() => {
          if (on) run("layout.hide", { id: "panel" });
          else { run("layout.select", { stack: "panel-body", child: id }); run("layout.show", { id: "panel" }); }
        });
    };
    return h_flex().size_full().min_w(0).px(10).pl(lightsPadding).gap(5).items_center().bg(t.chrome).border_b(1).border_color(t.border)
      .on_mouse_up("left", () => { this.pressed = null; })
      .on_scroll_wheel(e => { if (Math.abs(e.delta.y) >= 1) run(e.delta.y > 0 ? "tab.prev" : "tab.next"); })
      .child(h_flex().id("tab-scroll").flex_1().min_w(0).h_full().items_center().gap(3).overflow_x_scrollbar().children(shown.map(item)))
      .when(Boolean(findNode(layout(), "shepherdr")), el => { const shown = !findNode(layout(), "shepherdr").hidden;
        return el.child(div().id("shep-toggle").size(26).flex_none().flex().items_center().justify_center().cursor_pointer()
          .text_color(shown ? t.accent : t.dim).when(shown, e => e.bg(t.selected)).hover(st => st.bg(t.hover).text_color(t.foreground))
          .on_click(() => run("layout.toggle", { id: "shepherdr" }))
          .child(svg(`mods/px-tabs/${GS()}/shepherdr.svg`).size(15))); })
      .child(button("tab-new", svg(`mods/px-chrome/${GS()}/plus.svg`).size(14), () => openOverlay({ frame: "none", mod: "px-new-menu", kind: "popup", width: 250, height: 512 })))
      // Pair's panel (pairing-buddy), beside the focused terminal; the same as its Alt-p. Only where Pair installed its mod.
      // A second click closes the panel shown in this tab.
      .when(this.pairMod, el => { const tab = focused()?.tab, open = panes().find(p => p.mod === "pair" && p.tab === tab);
        return el.child(barButton(t, "panel-pair", Boolean(open), `mods/px-chrome/${GS()}/users.svg`, "Pair")
        .on_click(() => open ? run("pane.close", { pane: open.pane }) : run("pane.split", { direction: "right", mod: "pair" }))); })
      .when(enabled.includes("files"), el => el.child(panelButton("files", "Files", `mods/px-chrome/${GS()}/files.svg`)))
      .when(enabled.includes("changes"), el => el.child(panelButton("changes", "Changes", `mods/px-chrome/${GS()}/git-compare.svg`)))
      .when(Boolean(findNode(layout(), "pr")), el => el.child(panelButton("pr", "PR", `mods/px-chrome/${GS()}/git-pull-request.svg`)))
      .when(Boolean(findNode(layout(), "reviews")), el => el.child(panelButton("reviews", (shared("reviews.summary")?.requested ?? 0) > 0 ? `Reviews ${shared("reviews.summary").requested}` : "Reviews", `mods/px-chrome/${GS()}/check.svg`)))
      .when(Boolean(findNode(layout(), "gcp")), el => el.child(panelButton("gcp", "GCP", `mods/px-chrome/${GS()}/server.svg`)))
      .when(Boolean(findNode(layout(), "stacks")), el => el.child(panelButton("stacks", "Stacks", `mods/px-chrome/${GS()}/layers.svg`)))
      .when(Boolean(findNode(layout(), "docs")), el => el.child(panelButton("docs", "Docs", `mods/px-chrome/${GS()}/file-text.svg`)))
      .when(Boolean(findNode(layout(), "time")), el => el.child(panelButton("time", "Time", `mods/px-chrome/${GS()}/clock.svg`)))
      .when(enabled.includes("jira"), el => el.child(panelButton("jira", "Jira", "mods/px-jira/g/jira.svg")))
      // Shepherdr 3.0's panels, where this layout has them (its tasks and artifacts stacks)
      .when(Boolean(findNode(layout(), "tasks")), el => el.child(panelButton("tasks", "Tasks", `mods/px-chrome/${GS()}/circle-check.svg`)))
      .when(Boolean(findNode(layout(), "artifacts")), el => el.child(panelButton("artifacts", "Artifacts", `mods/px-chrome/${GS()}/layers.svg`)))
      .when(w?.buttons, el => el.child(button("minimize", w.platform === "windows" ? svg(`mods/px-tabs/${GS()}/win-min.svg`).size(16) : "−", () => run("window.minimize")))
        .child(button("maximize", w.platform === "windows" ? svg(`mods/px-tabs/${GS()}/${w.maximized ? "win-restore" : "win-max"}.svg`).size(16) : "□", () => run("window.maximize")))
        .child(button("close-window", w.platform === "windows" ? svg(`mods/px-tabs/${GS()}/win-close.svg`).size(16) : svg(`mods/px-pane/${GS()}/close.svg`).size(13), () => run("window.close"))));
  }
}
