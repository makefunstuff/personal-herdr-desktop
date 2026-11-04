import { View, div, image, svg } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import * as fs from "fs/promises";
import { appHome, theme, run, props, window, shared, setShared, updates, layout } from "herdr";
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";

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

// To the rail (on macOS wide enough for the traffic lights), or back to the width it had.
function toggleSidebar() {
  const width = find(layout(), "sidebar")?.size?.px;
  if (!narrow()) setShared("sidebar.width", width);
  // Mods edited before the rail went by width still read the flag.
  setShared("sidebar.collapsed", !narrow());
  run("layout.resize", { id: "sidebar", px: narrow() ? (shared("sidebar.width") ?? 268) : window()?.platform === "macos" ? 76 : 44 });
}

// Each themed layout's own alert sounds. Herdr keeps one sound setting for every layout, so the header
// (in every one of these layouts) writes the shown layout's pair into settings.json when it differs.
const SOUNDS_BY_LAYOUT = {
  pixel: ["sounds/done-8bit.wav", "sounds/blocked-8bit.wav"],
  win31: ["sounds/done-win31.wav", "sounds/blocked-win31.wav"],
  fortress: ["sounds/done-fortress.wav", "sounds/blocked-fortress.wav"],
  workbench: ["sounds/done-workbench.wav", "sounds/blocked-workbench.wav"],
};
async function syncSounds() {
  const pair = SOUNDS_BY_LAYOUT[shared("layout")]; if (!pair) return;
  const file = `${appHome()}/settings.json`;
  try {
    const s = JSON.parse(await fs.readFile(file, "utf8"));
    const cur = s.sounds ?? {};
    if (!(cur.done === pair[0] && cur.blocked === pair[1])) {
    s.sounds = { ...cur, enabled: cur.enabled ?? true, done: pair[0], blocked: pair[1] };
    await fs.writeFile(file, JSON.stringify(s, null, 2));
    }
  } catch (e) { console.log("sound sync:", e?.message); }
  // Program icons: each themed layout keeps its own list in programs-<layout>.json.
  try {
    const want = await fs.readFile(`${appHome()}/programs-${shared("layout")}.json`, "utf8");
    const have = await fs.readFile(`${appHome()}/programs.json`, "utf8").catch(() => "");
    if (want !== have) await fs.writeFile(`${appHome()}/programs.json`, want);
  } catch (e) { console.log("icon sync:", e?.message); }
}

export default class WorkspaceChrome extends View {
  init(_p, cx) { if (!props()?.footer) cx.spawn(async () => { await syncSounds(); }); }
  button(id, label, action) {
    const t = theme();
    return div().id(id).size(26).flex_none().flex().items_center().justify_center().rounded(0).text_color(t.muted).hover(s => s.bg(t.hover).text_color(t.foreground))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).on_click(action).child(label);
  }
  render() {
    const t = theme();
    const collapsed = narrow();
    if (props()?.footer) {
      const row = (id, icon, label, dim, action) => h_flex().id(id).h(28).px(8).gap(9).items_center().rounded(0)
        .text_color(dim ? t.dim : t.muted).hover(s => s.bg(t.hover).text_color(t.foreground)).cursor_pointer()
        .on_click(action).child(svg(`mods/px-chrome/${GS()}/${icon}.svg`).size(14))
        .when(!collapsed, el => el.child(label));
      return v_flex().size_full().bg(t.chrome).border_t(1).border_color(t.border).px(8).pt(4).pb(8).text_size(13)
        .child(row("settings", "settings", "Settings", false, () => run("app.settings"))
          .when(Boolean(updates().app.ready), el => el.child(div().size(6).rounded(0).bg(t.accent))))
        .child(row("machine", "plus", "Add machine", true, () => run("mod.open", { name: "px-machine-add" })));
    }
    return h_flex().size_full().bg(t.chrome).pl(collapsed ? 6 : 14).pr(10).gap(4).items_center().text_size(13.5)
      .when(window()?.platform === "macos" && !collapsed, el => el.pl(76))
      .when(!collapsed, el => el.child(h_flex().flex_1().gap(8).items_center().child(image(`mods/px-chrome/${({ workbench: "herdr-wb-chip", fortress: "herdr-fortress" })[shared("layout")] ?? "herdr2"}.svg`).size(20).rounded(0)).child(shared("layout") === "workbench"
        ? h_flex().gap(6).items_center().child(div().font_semibold().text_color(t.foreground).child("HERDR"))
            .child(div().px(4).border_1().border_color(t.accent).text_size(9.5).font_semibold().text_color(t.accent).child("WORKBENCH"))
        : div().font_semibold().text_color(t.foreground).child("Herdr"))))
      .when(!collapsed, el => el.child(this.button("search", svg(`mods/px-chrome/${GS()}/search.svg`).size(15), () => run("mod.open", { name: "px-picker" }))))
      // On the macOS rail the traffic lights fill the row; the agents list's » unfolds it.
      .when(!(collapsed && window()?.platform === "macos"), el => el.child(this.button("sidebar-toggle", svg(`mods/px-chrome/${GS()}/sidebar.svg`).size(15), toggleSidebar)));
  }
}
