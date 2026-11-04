// Voice: the secretary of herdr-voice-computer (vcd) in Herdr. One copy floats in the layout (a
// compact pill that shows only while it listens, thinks, speaks or asks), keeps vcd's state for the
// px-tabs mic button (shared "voice": the state word and counts, never words), posts vcd's toasts and
// takes the keys (voice.talk, voice.confirm, voice.decline, voice.quiet, voice.overlay, voice.dictate: Option+L). Opened as a
// window (props().overlay), it shows the conversation, the inbox and the quiet switch.
// When vcd isn't running as this copy loads, it runs `vc start` once (vcd then runs as Herdr Voice.app,
// which macOS asks for the microphone). vcd listens on 127.0.0.1:8792 and wants the token in ~/.local/state/herdr-voice-computer/token.
import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, home, readTextFile, theme, shared, setShared, props, openOverlay, closeOverlay, notify, nextCommand, focused, panes, run } from "herdr";

const GS = () => ({ workbench: "gw", fortress: "gf" })[shared("layout")] ?? "g";
const URL = "http://127.0.0.1:8792";
const STATE_DIR = () => `${home()}/.local/state/herdr-voice-computer`;
const VC = () => `${home()}/.local/bin/vc`;
const mic = (size = 14) => svg(`mods/px-tabs/${GS()}/mic.svg`).size(size).flex_none();
const oneLine = (s, n = 160) => { const t = String(s ?? "").replace(/\s+/g, " ").trim(); return t.length > n ? t.slice(0, n - 1) + "…" : t; };
const clock = (ms) => { const d = new Date(ms); return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`; };

let token = null;
async function api(path, body) {
  if (!token) token = (await readTextFile(`${STATE_DIR()}/token`))?.trim();
  if (!token) throw new Error("vcd isn't running");
  const opts = { headers: { "x-vc-token": token } };
  if (body) Object.assign(opts, { method: "POST", body: JSON.stringify(body), headers: { "x-vc-token": token, "content-type": "application/json" } });
  const r = await fetch(URL + path, opts);
  if (r.status === 401) { token = null; throw new Error("vcd's token changed"); }
  return r.json();
}
const cmd = (c, arg) => api("/cmd", { cmd: c, arg: arg ?? "" }).catch(() => null);

// The state word the mic shows: off, ready, listening, thinking, speaking, confirm.
const word = (s) => !s ? "off" : s.card ? "confirm" : s.state;
const WORDS = { off: "Off", ready: "Ready", listening: "Listening", thinking: "Thinking", speaking: "Speaking", confirm: "Needs you" };
export const color = (t, w) => ({ off: t.dim, ready: t.muted, listening: t.accent, thinking: t.working, speaking: t.done, confirm: t.blocked })[w] ?? t.dim;

export default class Voice extends View {
  init(_props, cx) {
    this.st = null; this.ver = 0; this.err = null; this.toastSeq = null; this.started = false; this.win = null;
    this.window = Boolean(props()?.overlay);
    cx.spawn(async (cx) => {
      for (;;) {
        try {
          const s = await api(`/state?after=${this.ver}`);
          this.st = s; this.ver = s.version; this.err = null;
          this.started = true; // vcd ran: a later stop is the owner's, never undone here
          if (!this.window) { this.publish(s); this.toast(s); }
        } catch (e) {
          this.st = null; this.ver = 0; this.err = e.message;
          if (!this.window) {
            this.publish(null);
            if (!this.started) { this.started = true; await exec(VC(), ["start"]).catch(() => null); }
          }
          await new Promise((ok) => cx.timer.after(3000, ok));
        }
        cx.notify();
      }
    });
    if (!this.window) {
      cx.spawn(async (cx) => {
        for (;;) {
          const c = await nextCommand();
          // One key's failure never ends this loop (it takes every voice key).
          try {
          if (c === "dictate") await this.dictate();
          else if (c === "overlay") this.toggleWindow();
          else if (c === "quiet") await cmd("quiet", "toggle");
          else if (c === "talk" && !this.st) { this.started = true; await exec(VC(), ["start"]).catch(() => null); }
          else if (["talk", "confirm", "decline", "cancel"].includes(c)) await cmd(c);
          } catch (e) {
            notify({ title: "Voice", body: oneLine(`That key failed: ${e?.message ?? e}`, 200), level: "warning", timeout: 5000 });
          }
          cx.notify();
        }
      });
    }
  }

  // Dictation, one key (voice.dictate, Option+L) for any agent: press, speak, press again. In a chat the chat's own
  // dictation runs (shared "chat.dictatereq": the text lands in its box, with its wave). In a terminal
  // (an agent's TUI, a shell) vcd writes it down (LFM3-Audio, this Mac) and it's pasted at the cursor,
  // not sent; the clipboard is put back after.
  async dictate() {
    const f = focused();
    if (this.dictating) {
      const at = this.dictating;
      this.dictating = null;
      const r = await exec(VC(), ["dictate", "stop"]).catch((e) => ({ code: 1, stderr: String(e?.message ?? e) }));
      const text = (r.stdout ?? "").trim();
      if (r.code !== 0) { notify({ title: "Dictation", body: oneLine(r.stderr || "Couldn't write that down.", 200), level: "warning", timeout: 6000 }); return; }
      if (!text) { notify({ title: "Dictation", body: "Didn't catch anything.", timeout: 3000 }); return; }
      const old = await exec("pbpaste", []).then((x) => x.stdout).catch(() => null);
      await exec("sh", ["-c", 'printf %s "$1" | pbcopy', "x", text]);
      run("pane.focus", { pane: at.pane });
      run("terminal.paste");
      // The clipboard back once the paste has read it (Herdr's mods have no setTimeout: a shell waits).
      if (old != null) exec("sh", ["-c", 'sleep 0.8; printf %s "$1" | pbcopy', "x", old]).catch(() => {});
      return;
    }
    const p = f?.pane ? panes().find((x) => x.pane === f.pane) : null;
    if (p?.mod === "chat") { setShared("chat.dictatereq", { pane: f.pane, at: Date.now() }); return; }
    if (!f?.terminal) { notify({ title: "Dictation", body: "Focus an agent's chat or terminal, then press it again.", timeout: 4000 }); return; }
    const r = await exec(VC(), ["dictate", "start"]).catch((e) => ({ code: 1, stderr: String(e?.message ?? e) }));
    if (r.code !== 0) { notify({ title: "Dictation", body: oneLine(r.stderr || "The voice computer isn't running.", 200), level: "warning", timeout: 6000 }); return; }
    this.dictating = { pane: f.pane, terminal: f.terminal };
    notify({ title: "Dictating", body: "Speak, then press the dictation key again to paste it here.", timeout: 4000 });
  }

  // What the px-tabs mic shows: the state word and counts only (shared state is saved to disk).
  publish(s) {
    const v = s ? { state: word(s), quiet: s.replies_muted, muted: s.muted, inbox: s.inbox?.length ?? 0, reminders: s.reminders?.length ?? 0 } : { state: "off" };
    const j = JSON.stringify(v);
    if (j !== this.published) { this.published = j; setShared("voice", v); }
  }

  toast(s) {
    const seq = s.toast?.seq ?? 0;
    if (this.toastSeq === null) { this.toastSeq = seq; return; } // what came before this copy started
    if (seq > this.toastSeq) {
      this.toastSeq = seq;
      notify({ title: s.toast.title, body: s.toast.body, level: s.toast.level ?? "info", timeout: 8000, action: { name: "voice.overlay" } });
    }
  }

  toggleWindow() {
    if (this.win) { closeOverlay(this.win); this.win = null; return; }
    this.win = openOverlay({ mod: "voice", kind: "window", key: "voice", frame: "none", width: 440, props: { window: true } });
  }

  button(t, id, label, on, strong) {
    return h_flex().id(id).h(24).px(10).items_center().flex_none().cursor_pointer().text_size(12)
      .border_1().border_color(strong ? t.accent : t.border).bg(strong ? t.accent : t.raised)
      .text_color(strong ? t.accent_foreground : t.foreground).hover((s) => s.bg(t.hover).text_color(t.foreground))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((_e, cx) => cx.spawn(async (cx) => { await on(); cx.notify(); })).child(label);
  }

  // The pill: only while something happens.
  pill(t) {
    const s = this.st, w = word(s);
    if (!s || (w === "ready" && !s.overlay)) return div();
    const line = s.card ? s.card.ask || s.card.title : w === "listening" ? "Listening… press again to send"
      : w === "thinking" ? (s.heard ? `“${oneLine(s.heard, 70)}”` : "Thinking…") : (s.answer || s.note || s.heard);
    return h_flex().id("voice-pill").max_w(460).min_w(0).px(10).py(6).gap(8).items_center().text_size(12.5)
      .text_color(t.foreground).cursor_pointer().on_click(() => this.toggleWindow())
      .child(div().text_color(color(t, w)).child(mic(14)).when(w === "listening", (e) => e.loop("opacity", { from: 0.35, to: 1, period: 900 })))
      .when(w === "thinking", (e) => e.child(div().w(14).text_color(t.working).frames(["·  ", "·· ", "···", " ··", "  ·", "   "], 140)))
      .child(div().flex_1().min_w(0).truncate().child(oneLine(line, 120)))
      .when(Boolean(s.card), (e) => e.child(this.button(t, "pill-no", "No", () => cmd("decline"))));
  }

  // The window: the conversation, the inbox, the quiet switch.
  panel(t) {
    const s = this.st, w = word(s);
    const name = s?.name ?? "Herdr";
    const head = h_flex().w_full().h(34).px(10).gap(8).items_center().bg(t.chrome).border_b(1).border_color(t.border)
      .child(div().text_color(color(t, w)).child(mic(14)))
      .child(div().text_size(12.5).font_semibold().child(name))
      .child(div().flex_1().text_size(11.5).text_color(color(t, w)).child(WORDS[w] ?? w))
      .when(Boolean(s), (e) => e.child(this.button(t, "quiet", s.replies_muted ? "Quiet: on" : "Quiet: off", () => cmd("quiet", "toggle"), false)))
      .child(div().id("close").size(24).flex().items_center().justify_center().cursor_pointer().text_color(t.dim)
        .hover((x) => x.bg(t.hover).text_color(t.foreground)).on_mouse_down("left", (_e, cx) => cx.stop_propagation())
        .on_click(() => closeOverlay(props().overlay)).child("✕"));
    const body = v_flex().w_full().p(10).gap(8).text_size(12.5).text_color(t.foreground);
    if (!s) {
      body.child(div().text_color(t.dim).child(this.err ? `Off: ${this.err}` : "Connecting…"))
        .child(h_flex().child(this.button(t, "start", "Start the voice computer", () => exec(VC(), ["start"]), true)));
      return v_flex().id("voice-window").w(440).bg(t.popover).border_1().border_color(t.border).child(head).child(body);
    }
    if (w === "off" && s.why) body.child(div().text_color(t.blocked).child(s.why));
    if (s.card) {
      body.child(v_flex().w_full().gap(6).p(8).border_1().border_color(t.blocked).bg(t.surface)
        .child(div().font_semibold().child(s.card.title))
        .when(Boolean(s.card.text), (e) => e.child(div().text_color(t.muted).child(`“${oneLine(s.card.text, 300)}”`)))
        .child(h_flex().gap(6).child(this.button(t, "yes", `Yes (${s.confirm_key})`, () => cmd("confirm"), true)).child(this.button(t, "no", "No", () => cmd("decline")))));
    }
    if (s.heard || s.answer || s.note) {
      body.child(v_flex().gap(3)
        .when(Boolean(s.heard), (e) => e.child(div().text_color(t.muted).child(`You: ${oneLine(s.heard, 300)}`)))
        .when(Boolean(s.answer), (e) => e.child(div().child(`${s.from && s.from !== name ? s.from : name}: ${oneLine(s.answer, 400)}`)))
        .when(Boolean(s.note), (e) => e.child(div().text_color(t.dim).child(oneLine(s.note, 200)))));
    }
    const inbox = s.inbox ?? [], rem = s.reminders ?? [];
    body.child(div().pt(4).text_size(11).font_semibold().text_color(t.muted).child(`INBOX · ${inbox.length} waiting · ${rem.length} reminder${rem.length === 1 ? "" : "s"}`));
    if (!inbox.length && !rem.length) body.child(div().text_color(t.dim).child("Nothing waits on you."));
    inbox.slice(0, 6).forEach((it, i) => body.child(h_flex().id(`in-${i}`).gap(7).items_center()
      .child(div().size(7).flex_none().bg(t.blocked)).child(div().flex_1().min_w(0).truncate()
        .child(it.what ? `${it.who}: ${oneLine(it.what, 120)}` : `${it.who} is waiting`))));
    rem.forEach((r, i) => body.child(h_flex().id(`rem-${i}`).gap(7).items_center()
      .child(div().flex_none().text_color(t.dim).child(clock(r.due))).child(div().flex_1().min_w(0).truncate().child(r.text || "Reminder"))));
    // The whole conversation vcd keeps (it replays it to the brain, older days condensed in its summary),
    // newest first so the latest is in view without scrolling; the list scrolls back through the rest.
    const turns = [...(s.turns ?? [])].reverse();
    if (turns.length) {
      body.child(div().pt(4).text_size(11).font_semibold().text_color(t.muted).child(`CONVERSATION · ${turns.length} turn${turns.length === 1 ? "" : "s"}`));
      body.child(v_flex().id("voice-convo").max_h(380).overflow_y_scroll().gap(8).pr(4)
        .children(turns.map((x, i) => v_flex().id(`turn-${i}`).gap(2).pl(8).border_l(2).border_color(i === 0 ? t.accent : t.border)
          .when(Boolean(x.you), (e) => e.child(div().text_color(t.muted).child(`You: ${x.you}`)))
          .when(Boolean(x.computer), (e) => e.child(div().child(x.computer))))));
    }
    body.child(h_flex().pt(4).gap(6)
      .child(this.button(t, "talk", w === "listening" ? "Send" : `Talk (${s.talk_key})`, () => cmd("talk"), true))
      .when(w === "speaking" || w === "thinking", (e) => e.child(this.button(t, "stop", "Stop", () => cmd("cancel")))));
    return v_flex().id("voice-window").w(440).bg(t.popover).border_1().border_color(t.border).child(head).child(body);
  }

  render() {
    const t = theme();
    return this.window ? this.panel(t) : this.pill(t);
  }
}
