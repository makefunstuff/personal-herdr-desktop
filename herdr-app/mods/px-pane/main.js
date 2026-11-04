// Win 3.1 title bars for the local "pixel" layout merged in (from omarchy-herdr-win31).
// A pane's header, for layouts that frame their panes:
//   { "tab": "active", "pane": { "header": { "content": { "mod": "px-pane" } } } }
// Shows the program's icon (the app's program table), the agent's name or
// the terminal's title, the folder, and the agent's status; buttons split, zoom and close
// this pane. Props from the app: { pane, terminal }. Click: focus the pane.

import { View, div, svg, image } from "gpui-kit";
import { h_flex } from "gpui-base";
import { panes, props, run, theme, home, shared, terminals, exec, hc, notify } from "herdr";

// Shepherdr 3.0's Chat / Terminal switch (as its pane-chat header has it), for agent panes in these layouts:
// Chat puts the `chat` mod in the agent pane's spot and parks the agent's terminal (labelled herdr.chat=<chat pane>,
// so every Shepherdr mod sees which chat shows which agent); Terminal brings the terminal back and closes the chat.
const CHAT = "chat", CHAT_LABEL = "herdr.chat";
const chatty = (p) => Boolean(p?.terminal) && (["claude-code", "codex"].includes(p.app) || ["claude", "codex"].includes(p.program?.key));
const chatPanes = () => new Set(panes().filter((p) => p.mod === CHAT).map((p) => p.pane));
const chatOf = (terminal) => { const c = terminals().find((x) => x.address === terminal)?.labels?.[CHAT_LABEL]; return c && chatPanes().has(c) ? c : null; };
const agentOf = (chat) => terminals().find((x) => x.labels?.[CHAT_LABEL] === chat && x.state === "live")?.address ?? null;
async function app(name, args) {
  const r = await exec("herdr-app", ["do", name, ...Object.entries(args).flatMap(([k, v]) => [`--${k}`, typeof v === "string" ? v : JSON.stringify(v)])]);
  let out = {}; try { out = JSON.parse(r.stdout || "{}"); } catch {}
  if (r.code !== 0 || out.error) throw new Error(out.error || String(r.stderr ?? "").trim() || `${name} failed`);
  return out;
}
const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";

function icon(p, size, color) {
  if (!p?.icon) return null;
  return p.tint ? svg(p.icon).size(size).flex_none().text_color(color) : image(p.icon).w(size).h(size).flex_none();
}

function folder(cwd) {
  if (!cwd) return "";
  const h = home();
  return cwd === h ? "~" : cwd.startsWith(h + "/") ? "~" + cwd.slice(h.length) : cwd;
}

export default class Pane extends View {
  init() { this.hovered = false; }

  me() {
    return panes().find((p) => p.pane === props()?.pane);
  }

  button(t, id, icon, command, args) {
    return div()
      .id(id)
      .p(3)
      .rounded(0)
      .text_color(t.dim)
      .hover((s) => s.bg(t.hover).text_color(t.foreground))
      .on_click((_e, cx) => {
        cx.stop_propagation();
        run(command, args);
      })
      .child(svg(`mods/px-pane/${GS()}/${icon}.svg`).size(14));
  }

  // Win 3.1 bevel button: white top-left, gray bottom-right, black outline.
  box(id, glyph, onClick, tip) {
    return div().id(id).flex_none().w(20).h(20).border_1().border_color("#000000").bg("#c0c0c0")
      .on_click((_e, cx) => { cx.stop_propagation(); onClick(); })
      .child(div().size_full().flex().items_center().justify_center()
        .border_t(2).border_l(2).border_color("#ffffff")
        .child(div().size_full().flex().items_center().justify_center().border_b(2).border_r(2).border_color("#808080")
          .text_size(10).text_color("#000000").child(glyph)));
  }

  // The Win 3.1 layout ("pixel" on this machine): navy title bar, control-menu box, bevel min/max boxes.
  win31(t, p, name, st, pane) {
    const where = folder(p.cwd);
    const title = (where ? `${name} - ${where}` : name).toUpperCase();
    const on = Boolean(p.focused);
    const fg = on ? "#ffffff" : "#000000";
    return h_flex()
      .id(`pane-head-${pane}`)
      .size_full()
      .gap(0)
      .items_center()
      .bg(on ? "#000080" : "#ffffff")
      .border_b(1)
      .border_color("#000000")
      .text_size(12)
      .on_hover((hovered, cx) => { this.hovered = hovered; cx.notify(); })
      .on_click((e) => { if (e.click_count >= 2) run("pane.zoom", { pane }); else run("pane.focus", { pane }); })
      // control-menu box: double-click closes, like Windows 3.1
      .child(div().id(`ph-c-${pane}`).flex_none().w(28).h_full().flex().items_center().justify_center()
        .bg("#c0c0c0").border_r(1).border_color("#000000")
        .on_click((e, cx) => { cx.stop_propagation(); if (e.click_count >= 2) run("pane.close", { pane }); else run("pane.focus", { pane }); })
        .child(div().w(14).h(4).bg("#ffffff").border_1().border_color("#000000")))
      .child(h_flex().flex_1().min_w(0).h_full().justify_center().items_center().gap(6).px(8)
        .child(div().truncate().font_weight(700).text_color(fg).child(title))
        .when(st === "working", el => el.child(svg("mods/px-chrome/g/spinner.svg").size(12).flex_none().text_color(on ? "#ffff00" : t.working).spin(900)))
        .when(st === "blocked", el => el.child(div().flex_none().px(6).h(18).flex().items_center().bg("#ff0000").text_size(10.5).font_weight(700).text_color("#ffffff").child("NEEDS YOU")))
        .when(st === "done", el => el.child(div().size(8).flex_none().bg(on ? "#00ff00" : t.done))))
      .when(this.hovered, el => el
        .child(this.button(t, `ph-r-${pane}`, "split-right", "pane.split", { direction: "right", pane }))
        .child(this.button(t, `ph-d-${pane}`, "split-down", "pane.split", { direction: "down", pane })))
      .child(h_flex().flex_none().gap(0).h_full().items_center().border_l(1).border_color("#000000")
        .child(this.box(`ph-m-${pane}`, "\u25BC", () => run("tab.settle", { id: p.tab }), "Minimize"))
        .child(this.box(`ph-z-${pane}`, p.zoomed ? "\u2666" : "\u25B2", () => run("pane.zoom", { pane }), p.zoomed ? "Restore" : "Maximize")));
  }

  toChat(p, cx) {
    const open = chatOf(p.terminal);
    if (open) { run("pane.focus", { pane: open }); return; }
    if (this.busy) return;
    this.busy = true;
    const terminal = p.terminal;
    cx.spawn(async (cx) => {
      try {
        const out = await app("pane.split", { direction: "right", pane: p.pane, mod: CHAT, props: { terminal } });
        if (!out.pane) throw new Error("Herdr didn't open the chat");
        await hc(["label", terminal, `${CHAT_LABEL}=${out.pane}`]);
        run("pane.focus", { pane: out.pane });
        await app("terminal.park", { terminal }); // last: this removes the pane this header is on
      } catch (e) { notify({ title: "Chat didn't open", body: String(e?.message ?? e), level: "error" }); }
      this.busy = false; cx.notify();
    });
  }

  toTerminal(chat, cx) {
    const terminal = agentOf(chat), own = terminal && panes().find((x) => x.terminal === terminal);
    if (!terminal || own) {
      run("pane.close", { pane: chat });
      if (terminal) hc(["label", terminal, `${CHAT_LABEL}=`]);
      if (own) run("pane.focus", { pane: own.pane });
      return;
    }
    if (this.busy) return;
    this.busy = true;
    cx.spawn(async (cx) => {
      try {
        await app("pane.focus", { pane: chat });
        const out = await app("terminal.keep", { terminal, place: "right" });
        await hc(["label", terminal, `${CHAT_LABEL}=`]);
        if (out.pane) run("pane.focus", { pane: out.pane });
        await app("pane.close", { pane: chat }); // last: closing the chat removes this header too
      } catch (e) { notify({ title: "Terminal didn't come back", body: String(e?.message ?? e), level: "error" }); }
      this.busy = false; cx.notify();
    });
  }

  // CHAT | TERM: shown on agent panes and on chat panes. Drawn like the status bar's DESK/MONITOR buttons:
  // a 1px border, the accent border and a small accent dot on the one shown.
  switcher(t, p) {
    const isChat = p.mod === CHAT;
    if (!isChat && !chatty(p)) return null;
    const cell = (id, label, on, fn) => h_flex().id(`${id}-${p.pane}`).h(20).px(7).gap(6).items_center().flex_none().rounded(0)
      .border_1().border_color(on ? t.accent : t.border).font_semibold().text_size(10.5)
      .text_color(on ? t.foreground : t.muted).when(!on, (s) => s.cursor_pointer().hover((s) => s.bg(t.hover).text_color(t.foreground)))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((_e, cx) => { cx.stop_propagation(); if (!on) fn(cx); })
      .child(label).when(on, (s) => s.child(div().size(5).bg(t.accent)));
    return h_flex().h_full().px(6).gap(4).items_center().flex_none()
      .child(cell("ph-chat", "CHAT", isChat, (cx) => this.toChat(p, cx)))
      .child(cell("ph-term", "TERM", !isChat, (cx) => this.toTerminal(p.pane, cx)));
  }

  render(cx) {
    const t = theme();
    const p = this.me();
    if (!p) return div();
    const name = ((p.program?.agent || p.program?.shell) && p.program.name) || p.title || p.program?.name || "shell";
    const pic = icon(p.program, 14, t.dim);
    const st = p.status;
    const pane = p.pane;
    if (shared("layout") === "pixel") return this.win31(t, p, name, st, pane);
    if (shared("layout") === "workbench") {
      // A module tab: a solid label block (amber when focused), the folder in caps, the node id at the right.
      const on = Boolean(p.focused);
      const node = String(p.terminal ?? "").replace(/^.*\//, "").toUpperCase();
      return h_flex().id(`pane-head-${pane}`).size_full().gap(0).items_center().bg(t.chrome).border_b(2).border_color(on ? t.accent : t.border)
        .text_size(11).on_hover((hovered, cx) => { this.hovered = hovered; cx.notify(); }).on_click(() => run("pane.focus", { pane }))
        .child(h_flex().h_full().px(10).gap(7).items_center().flex_none().bg(on ? t.accent : t.raised)
          .text_color(on ? t.accent_foreground : t.foreground).font_semibold()
          .when(Boolean(pic), (el) => el.child(pic)).child(div().truncate().max_w(240).child(String(name).toUpperCase())))
        .child(div().flex_1().px(10).truncate().text_color(t.dim).child(String(folder(p.cwd) ?? "").toUpperCase()))
        .when(st === "working", el => el.child(div().flex_none().px(6).text_color(t.working).font_semibold().child("RUN")))
        .when(st === "blocked", el => el.child(div().flex_none().px(6).h_full().flex().items_center().bg(t.blocked).text_color(t.background).font_semibold().child("WAIT")))
        .when(st === "done", el => el.child(div().flex_none().px(6).text_color(t.done).font_semibold().child("DONE")))
        .when(this.hovered, el => el
          .child(this.button(t, `ph-r-${pane}`, "split-right", "pane.split", { direction: "right", pane }))
          .child(this.button(t, `ph-d-${pane}`, "split-down", "pane.split", { direction: "down", pane }))
          .child(this.button(t, `ph-z-${pane}`, p.zoomed ? "unzoom" : "zoom", "pane.zoom", { pane }))
          .child(this.button(t, `ph-x-${pane}`, "close", "pane.close", { pane })))
        .when(Boolean(this.switcher(t, p)), el => el.child(this.switcher(t, p)))
        .when(Boolean(node), el => el.child(div().flex_none().h_full().px(8).flex().items_center().border_l(2).border_color(t.border).text_color(t.dim).child(node)));
    }
    return h_flex()
      .id(`pane-head-${pane}`)
      .size_full()
      .px(10)
      .gap(8)
      .items_center()
      .bg(t.surface)
      .border_b(1)
      .border_color(t.border)
      .text_size(12)
      .on_hover((hovered, cx) => { this.hovered = hovered; cx.notify(); })
      .on_click(() => run("pane.focus", { pane }))
      .when(Boolean(pic), (el) => el.child(pic))
      .child(div().flex_none().truncate().max_w(260).font_medium().text_color(t.foreground).child(name))
      .child(div().flex_1().truncate().text_color(t.dim).child(folder(p.cwd)))
      .when(st === "working", el => el.child(svg(`mods/px-chrome/${GS()}/spinner.svg`).size(12).flex_none().text_color(t.working).spin(900)))
      .when(st === "blocked", el => el.child(div().flex_none().px(6).h(18).flex().items_center().rounded(0).bg(t.blocked + "26").text_size(10.5).font_semibold().text_color(t.blocked).child("Needs you")))
      .when(st === "done", el => el.child(div().size(7).flex_none().rounded(0).bg(t.done)))
      .when(this.hovered, el => el
        .child(this.button(t, `ph-r-${pane}`, "split-right", "pane.split", { direction: "right", pane }))
        .child(this.button(t, `ph-d-${pane}`, "split-down", "pane.split", { direction: "down", pane }))
        .child(this.button(t, `ph-z-${pane}`, p.zoomed ? "unzoom" : "zoom", "pane.zoom", { pane }))
        .child(this.button(t, `ph-x-${pane}`, "close", "pane.close", { pane })))
      .when(Boolean(this.switcher(t, p)), el => el.child(this.switcher(t, p)));
  }
}
