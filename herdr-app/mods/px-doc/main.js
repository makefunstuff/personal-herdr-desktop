// Docs: herdr.doc/2 documents (SPEC.md beside this file) drawn natively, in the shown layout's theme.
// As a pane: pane.split {mod: "px-doc", props: {file}} shows that file. As a panel (no file): the drafts in
// ~/.herdr/docs, newest first, and the one picked. Both re-render when the file changes (an agent filling it in).
// Read only: agents write the JSON (herdr-doc). EXPORT writes Markdown beside it (herdr-doc export).
import * as fs from "fs/promises";
import { View, div, image } from "gpui-kit";
import { v_flex, h_flex, TextView } from "gpui-base";
import { theme, shared, home, props, run, exec, settings, watchFolder, notify, window } from "herdr";
// The desktop's opener for links and files: open on macOS, xdg-open on Linux.
const OPENER = () => (window()?.platform === "macos" ? "open" : "xdg-open");

const caps = (s) => (shared("layout") === "workbench" ? String(s).toUpperCase() : String(s));
const alpha = (c, a) => `${String(c ?? "#000000").slice(0, 7)}${a}`;
const DIR = () => `${home()}/.herdr/docs`;
const ago = (ms) => { const m = Math.max(0, Math.round((Date.now() - ms) / 60000)); return m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h` : `${Math.floor(m / 1440)}d`; };

export default class Doc extends View {
  init(_p, cx) {
    this.file = props()?.file ?? null; this.panel = !this.file;
    this.doc = null; this.error = null; this.list = []; this.fold = {}; this.note = null;
    cx.spawn(async (cx) => { await this.scan(); await this.load(); cx.notify(); });
    // re-render on change: watch the docs folder (and the file's own folder for a pane elsewhere)
    cx.spawn(async (cx) => {
      for (;;) {
        try { await watchFolder(this.file ? this.file.replace(/\/[^/]+$/, "") : DIR()); }
        catch { await new Promise((ok) => cx.timer.after(3000, ok)); }
        await this.scan(); await this.load(); cx.notify();
      }
    });
  }

  async scan() {
    if (!this.panel) return;
    try {
      // read only (a mod writes only under the Herdr home): herdr-doc makes the folder
      const names = (await fs.readdir(DIR()).catch(() => [])).filter((f) => f.endsWith(".hdoc.json"));
      const out = [];
      for (const n of names) {
        try {
          const p = `${DIR()}/${n}`, d = JSON.parse(await fs.readFile(p, "utf8"));
          let st = null;
          try { st = typeof fs.stat === "function" ? await fs.stat(p) : null; } catch { st = null; }
          out.push({ path: p, title: d.title || n, kind: d.kind, status: d.status, mtime: st?.mtimeMs ?? (Date.parse(d.updated || "") || 0) });
        } catch { out.push({ path: `${DIR()}/${n}`, title: n, status: "fail", mtime: 0, broken: true }); }
      }
      this.list = out.sort((a, b) => b.mtime - a.mtime);
      if (!this.picked && this.list[0]) this.picked = this.list[0].path;
      this.error = null;
    } catch (e) { this.error = String(e?.message ?? e); }
  }

  async load() {
    const f = this.file ?? this.picked;
    if (!f) { this.doc = null; return; }
    try { this.doc = JSON.parse(await fs.readFile(f, "utf8")); this.error = null; }
    catch (e) { this.error = `${f.replace(home(), "~")}: ${e.message ?? e}`; }
  }

  // ---- blocks
  tone(t, tone) { return { ok: t.done, good: t.done, warn: t.working, bad: t.blocked, info: t.accent }[tone] ?? t.muted; }
  sev(t, s) { return { critical: t.blocked, high: t.blocked, medium: t.working, low: t.muted }[s] ?? t.muted; }
  md(t, id, text) { return div().w_full().text_size(12.5).text_color(t.foreground).child(TextView.markdown(id, String(text ?? " ") || " ").selectable(true)); }
  plain(t, text, col) { return div().w_full().text_size(12.5).text_color(col ?? t.foreground).child(String(text ?? "")); }
  mono() { return settings().font_family ?? "JetBrains Mono"; }

  block(t, b, key) {
    const c = (x) => div().w_full().child(x);
    switch (b.type) {
      case "heading": {
        const lv = b.level ?? 2;
        return h_flex().w_full().mt(lv === 1 ? 6 : 10).gap(8).items_center()
          .child(div().h(lv === 1 ? 22 : 20).px(8).flex().items_center().bg(lv === 1 ? t.accent : t.raised).text_color(lv === 1 ? t.accent_foreground : t.foreground)
            .text_size(lv === 3 ? 11 : 12).font_semibold().child(caps(b.text)))
          .child(div().flex_1().h(1).bg(t.border));
      }
      case "text": return this.plain(t, b.text);
      case "md": return this.md(t, `md-${key}`, b.text);
      case "divider": return div().w_full().h(1).my(4).bg(t.border);
      case "callout": {
        const col = this.tone(t, b.tone ?? "info");
        return v_flex().w_full().p(8).gap(4).border_l(3).border_color(col).bg(alpha(col, "14"))
          .when(Boolean(b.title), (e) => e.child(div().text_size(10.5).font_semibold().text_color(col).child(caps(b.title))))
          .child(this.md(t, `co-${key}`, b.text));
      }
      case "quote": return v_flex().w_full().pl(10).py(2).gap(3).border_l(2).border_color(t.border)
        .child(this.plain(t, b.text, t.muted))
        .when(Boolean(b.who || b.where), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(`— ${[b.who, b.where].filter(Boolean).join(", ")}`)));
      case "stats": return h_flex().w_full().gap(6).flex_wrap().children((b.items ?? []).map((s) => {
        const col = this.tone(t, s.tone);
        return v_flex().min_w(90).flex_1().px(8).py(5).border_1().border_color(s.tone ? col : t.border).bg(t.chrome)
          .child(div().text_size(9.5).text_color(t.dim).child(caps(s.label)))
          .child(div().text_size(18).font_semibold().text_color(s.tone ? col : t.foreground).child(String(s.value)))
          .when(Boolean(s.note), (e) => e.child(div().text_size(10).text_color(t.dim).child(s.note)));
      }));
      case "kv": return v_flex().w_full().border_1().border_color(t.border).children((b.items ?? []).map(([k, v], i) =>
        h_flex().w_full().min_h(20).items_start().when(i > 0, (e) => e.border_t(1).border_color(t.border))
          .child(div().w(130).flex_none().px(6).py(2).bg(t.chrome).text_size(10.5).font_semibold().text_color(t.dim).child(caps(k)))
          .child(div().flex_1().min_w(0).px(6).py(2).text_size(12).text_color(t.foreground).child(String(v)))));
      case "list": return v_flex().w_full().gap(2).children((b.items ?? []).map((x, i) => h_flex().w_full().gap(6).items_start()
        .child(div().w(18).flex_none().text_size(11).text_color(t.accent).child(b.style === "number" ? `${i + 1}.` : "▪"))
        .child(div().flex_1().min_w(0).child(this.md(t, `li-${key}-${i}`, x)))));
      case "todo": return v_flex().w_full().gap(3).children((b.items ?? []).map((x) => h_flex().w_full().gap(8).items_center()
        .child(div().size(11).flex_none().border_1().border_color(x.done ? t.done : t.border).bg(x.done ? t.done : t.background))
        .child(div().flex_1().min_w(0).text_size(12).text_color(x.done ? t.dim : t.foreground).when(Boolean(x.done), (e) => e.line_through()).child(x.text))
        .when(Boolean(x.who), (e) => e.child(div().px(4).border_1().border_color(t.border).text_size(10).text_color(t.dim).child(x.who)))));
      case "checks": return v_flex().w_full().gap(3).children((b.items ?? []).map((x) => h_flex().w_full().gap(8).items_center()
        .child(div().w(34).flex_none().text_size(9.5).font_semibold().text_color(x.ok === true ? t.done : x.ok === false ? t.blocked : t.dim)
          .child(x.ok === true ? "PASS" : x.ok === false ? "FAIL" : "—"))
        .child(div().flex_1().min_w(0).text_size(12).child(x.text))
        .when(Boolean(x.note), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(x.note)))));
      case "steps": return v_flex().w_full().gap(3).children((b.items ?? []).map((x, i) => {
        const col = x.state === "done" ? t.done : x.state === "now" ? t.accent : x.state === "skip" ? t.dim : t.border;
        return h_flex().w_full().gap(8).items_center()
          .child(div().w(20).h(18).flex_none().flex().items_center().justify_center().border_1().border_color(col).bg(x.state === "now" ? t.accent : t.chrome)
            .text_size(10).font_semibold().text_color(x.state === "now" ? t.accent_foreground : col).child(String(i + 1).padStart(2, "0")))
          .child(div().flex_1().min_w(0).text_size(12).text_color(x.state === "skip" ? t.dim : t.foreground).when(x.state === "skip", (e) => e.line_through()).child(x.text))
          .when(Boolean(x.note), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(x.note)));
      }));
      case "timeline": return v_flex().w_full().children((b.items ?? []).map((x, i, all) => {
        const col = x.tone ? this.tone(t, x.tone) : t.accent;
        return h_flex().w_full().gap(8).items_start()
          .child(div().w(64).flex_none().pt(1).text_right().text_size(10.5).font_semibold().text_color(t.dim).child(String(x.time ?? "")))
          .child(v_flex().w(10).flex_none().items_center().h_full()
            .child(div().size(8).mt(3).flex_none().bg(col))
            .when(i < all.length - 1, (e) => e.child(div().w(2).flex_1().min_h(10).bg(t.border))))
          .child(v_flex().flex_1().min_w(0).pb(6)
            .child(div().text_size(12).text_color(t.foreground).child(x.text))
            .when(Boolean(x.who), (e) => e.child(div().text_size(10).text_color(t.dim).child(x.who))));
      }));
      case "finding": {
        const col = this.sev(t, b.severity);
        return v_flex().w_full().border_1().border_color(t.border).bg(t.chrome)
          .child(h_flex().w_full().h(22).items_center().border_b(1).border_color(t.border)
            .child(div().h_full().px(8).flex().items_center().bg(col).text_color(t.background).text_size(10).font_semibold().child(caps(b.severity ?? "finding")))
            .child(div().flex_1().min_w(0).px(8).truncate().text_size(12).font_semibold().text_color(t.foreground).child(b.title)))
          .child(v_flex().w_full().p(8).gap(6)
            .when(Boolean(b.detail), (e) => e.child(this.md(t, `fd-${key}`, b.detail)))
            .children((b.evidence ?? []).map((x, i) => this.block(t, x, `${key}-ev${i}`)).filter(Boolean))
            .when(Boolean(b.fix), (e) => e.child(h_flex().gap(6).items_start().child(div().text_size(10).font_semibold().text_color(t.done).child(caps("fix"))).child(this.plain(t, b.fix)))));
      }
      case "evidence": return v_flex().w_full().border_1().border_color(t.border)
        .child(h_flex().w_full().px(6).py(3).gap(6).bg(t.raised).font_family(this.mono()).text_size(10.5)
          .child(div().text_color(t.accent).child("$")).child(div().flex_1().min_w(0).text_color(t.foreground).child(b.command ?? ""))
          .when(Boolean(b.where), (e) => e.child(div().text_color(t.dim).child(b.where))))
        .child(v_flex().w_full().p(6).bg(t.background).font_family(this.mono()).text_size(10.5)
          .children(String(b.output ?? "").split("\n").slice(0, 80).map((l) => div().w_full().text_color(t.muted).child(l || " "))));
      case "code": return v_flex().w_full().border_1().border_color(t.border)
        .when(Boolean(b.file || b.lang), (e) => e.child(div().px(6).py(2).bg(t.raised).text_size(10).text_color(t.dim).child(b.file || b.lang)))
        .child(v_flex().w_full().p(6).bg(t.background).font_family(this.mono()).text_size(11)
          .children(String(b.text ?? "").split("\n").slice(0, 300).map((l) => div().w_full().text_color(t.foreground).child(l || " "))));
      case "diff": return v_flex().w_full().border_1().border_color(t.border)
        .when(Boolean(b.file), (e) => e.child(div().px(6).py(2).bg(t.raised).text_size(10).text_color(t.dim).child(b.file)))
        .child(v_flex().w_full().bg(t.background).font_family(this.mono()).text_size(11)
          .children(String(b.text ?? "").split("\n").slice(0, 300).map((l) => div().w_full().px(6)
            .when(l.startsWith("+"), (e) => e.bg(alpha(t.done, "18")).text_color(t.done))
            .when(l.startsWith("-"), (e) => e.bg(alpha(t.blocked, "18")).text_color(t.blocked))
            .when(l.startsWith("@@"), (e) => e.text_color(t.dim))
            .when(!/^[-+@]/.test(l), (e) => e.text_color(t.foreground)).child(l || " "))));
      case "table": {
        const cols = b.columns ?? [], al = b.align ?? [];
        const cell = (v, i, head) => div().flex_1().min_w(0).px(6).py(2).truncate().text_size(head ? 10 : 11.5).when(Boolean(head), (e) => e.font_semibold().text_color(t.dim))
          .when(!head, (e) => e.text_color(t.foreground)).when(al[i] === "right", (e) => e.text_right()).child(head ? caps(v) : String(v ?? ""));
        return v_flex().w_full().border_1().border_color(t.border)
          .child(h_flex().w_full().bg(t.raised).children(cols.map((c, i) => cell(c, i, true))))
          .children((b.rows ?? []).slice(0, 100).map((r, ri) => h_flex().w_full().border_t(1).border_color(t.border).when(ri % 2 === 1, (e) => e.bg(t.chrome))
            .children(r.map((v, i) => cell(v, i, false)))));
      }
      case "bars": {
        const max = b.max ?? Math.max(1, ...(b.items ?? []).map((x) => Number(x.value) || 0));
        return v_flex().w_full().gap(3).children((b.items ?? []).map((x) => h_flex().w_full().gap(8).items_center()
          .child(div().w(120).flex_none().truncate().text_size(11).text_color(t.muted).child(x.label))
          .child(h_flex().flex_1().h(10).bg(t.raised).child(div().h_full().w(`${Math.min(100, (Number(x.value) / max) * 100)}%`).bg(x.tone ? this.tone(t, x.tone) : t.accent)))
          .child(div().w(70).flex_none().text_right().text_size(11).text_color(t.foreground).child(`${x.value}${b.unit ? ` ${b.unit}` : ""}`))));
      }
      case "progress": {
        const pct = Math.max(0, Math.min(100, (Number(b.value) / (Number(b.max) || 1)) * 100)), col = b.tone ? this.tone(t, b.tone) : t.accent;
        return v_flex().w_full().gap(3)
          .child(h_flex().w_full().text_size(11).child(div().flex_1().text_color(t.muted).child(b.label)).child(div().text_color(t.foreground).child(`${b.value}/${b.max}`)))
          .child(h_flex().w_full().gap(1).children(Array.from({ length: 30 }, (_, i) => div().flex_1().h(6).bg(i < Math.round(pct / 100 * 30) ? col : t.raised))));
      }
      case "links": return h_flex().w_full().gap(6).flex_wrap().children((b.items ?? []).map((x, i) => div().id(`ln-${key}-${i}`).h(22).px(8).flex().items_center()
        .border_1().border_color(t.border).text_size(11).text_color(t.accent).cursor_pointer().hover((s) => s.border_color(t.accent))
        .on_click(() => exec(OPENER(), [x.url]).catch(() => {})).child(x.label || x.url)));
      case "image": return v_flex().w_full().gap(3)
        .child(image(b.path).w(b.width ?? 420))
        .when(Boolean(b.caption), (e) => e.child(div().text_size(10.5).text_color(t.dim).child(b.caption)));
      case "section": {
        const open = this.fold[key] ?? !b.collapsed;
        return v_flex().w_full().border_1().border_color(t.border)
          .child(h_flex().id(`sec-${key}`).w_full().h(24).items_center().bg(t.chrome).cursor_pointer().on_click((_e, cx) => { this.fold[key] = !open; cx.notify(); })
            .child(div().h_full().w(24).flex().items_center().justify_center().bg(t.raised).text_size(10).text_color(t.foreground).child(open ? "▾" : "▸"))
            .child(div().flex_1().min_w(0).px(8).truncate().text_size(12).font_semibold().text_color(t.foreground).child(caps(b.title ?? "")))
            .child(div().px(8).text_size(10).text_color(t.dim).child(`${(b.blocks ?? []).length}`)))
          .when(open, (e) => e.child(v_flex().w_full().p(10).gap(8)
            .when(Boolean(b.intro), (x) => x.child(this.md(t, `si-${key}`, b.intro)))
            .children((b.blocks ?? []).map((x, i) => this.block(t, x, `${key}-${i}`)).filter(Boolean))));
      }
      case "columns": return h_flex().w_full().gap(10).items_start().children((b.columns ?? []).slice(0, 3).map((col, ci) =>
        v_flex().flex_1().min_w(0).gap(8).children((col ?? []).map((x, i) => this.block(t, x, `${key}-c${ci}-${i}`)).filter(Boolean))));
      default: return null;
    }
  }

  docView(t, d) {
    const st = { work: t.working, need: t.blocked, ready: t.done, done: t.done, fail: t.blocked, info: t.accent }[d.status] ?? t.dim;
    return v_flex().w_full().gap(10)
      .child(v_flex().w_full().border_1().border_color(t.border).bg(t.chrome)
        .child(h_flex().w_full().min_h(28).items_center().border_b(1).border_color(t.border)
          .when(Boolean(d.kind), (e) => e.child(div().h_full().min_h(28).px(8).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(10.5).font_semibold().child(caps(d.kind))))
          .child(div().flex_1().min_w(0).px(10).py(4).text_size(15).font_semibold().text_color(t.foreground).child(d.title ?? "Untitled"))
          .when(Boolean(d.status), (e) => e.child(div().mx(8).h(18).px(6).flex().items_center().border_1().border_color(st).text_size(10).font_semibold().text_color(st).child(caps(d.status)))))
        .when(Boolean(d.subtitle || d.tags?.length || d.updated), (e) => e.child(h_flex().w_full().px(10).py(4).gap(8).items_center().text_size(10.5).text_color(t.dim)
          .when(Boolean(d.subtitle), (x) => x.child(div().child(d.subtitle)))
          .children((d.tags ?? []).map((g) => div().px(4).border_1().border_color(t.border).text_size(10).child(g)))
          .child(div().flex_1())
          .when(Boolean(d.updated), (x) => x.child(div().child(`updated ${ago(Date.parse(d.updated))} ago`))))))
      .children((d.blocks ?? []).map((b, i) => this.block(t, b, `b${i}`)).filter(Boolean));
  }

  exportMd(cx) {
    const f = this.file ?? this.picked; if (!f) return;
    cx.spawn(async (cx) => {
      const r = await exec("herdr-doc", ["export", f]).catch((e) => ({ code: 1, stderr: String(e) }));
      this.note = r.code === 0 ? `Markdown: ${String(r.stdout).trim().replace(home(), "~")}` : `Export failed: ${String(r.stderr || r.stdout).trim().split("\n").pop()}`;
      cx.notify();
    });
  }

  render(cx) {
    const t = theme(), d = this.doc;
    const tools = h_flex().gap(4)
      .when(Boolean(d), (e) => e.child(div().id("doc-md").h(20).px(7).flex().items_center().border_1().border_color(t.border).text_size(10).font_semibold()
        .text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.accent)).on_click((_e, cx) => this.exportMd(cx)).child(caps("export md"))))
      .when(this.panel && Boolean(this.picked), (e) => e.child(div().id("doc-pane").h(20).px(7).flex().items_center().border_1().border_color(t.border).text_size(10).font_semibold()
        .text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.accent))
        .on_click(() => run("pane.split", { direction: "right", mod: "px-doc", props: { file: this.picked } })).child(caps("open in pane"))));
    const head = h_flex().h(42).flex_none().px(14).gap(8).items_center().border_b(1).border_color(t.border)
      .child(div().font_semibold().child(caps(this.panel ? "Docs" : "Document")))
      .when(this.panel, (e) => e.child(div().px(5).py(1).border_1().border_color(t.border).text_size(10).text_color(t.muted).child(`${this.list.length} drafts`)))
      .child(div().flex_1()).child(tools);
    const body = v_flex().id("doc-body").flex_1().min_h(0).overflow_y_scrollbar().p(12);
    if (this.panel) {
      body.child(v_flex().w_full().gap(2).pb(10).mb(10).border_b(1).border_color(t.border)
        .when(!this.list.length, (e) => e.child(div().text_size(11.5).text_color(t.dim).child("No drafts yet. Ask an agent for one: it writes ~/.herdr/docs/<name>.hdoc.json with herdr-doc.")))
        .children(this.list.slice(0, 30).map((x) => {
          const on = x.path === this.picked, st = { work: t.working, need: t.blocked, ready: t.done, done: t.done, fail: t.blocked }[x.status] ?? t.border;
          return h_flex().id(`dl-${x.path}`).w_full().h(22).gap(6).items_center().px(4).cursor_pointer().when(on, (e) => e.bg(t.selected)).hover((s) => s.bg(t.hover))
            .on_click((_e, cx) => { this.picked = x.path; cx.spawn(async (cx) => { await this.load(); cx.notify(); }); })
            .child(div().size(7).flex_none().bg(st))
            .when(Boolean(x.kind), (e) => e.child(div().w(84).flex_none().truncate().text_size(9.5).font_semibold().text_color(t.dim).child(caps(x.kind))))
            .child(div().flex_1().min_w(0).truncate().text_size(11.5).text_color(on ? t.foreground : t.muted).child(x.title))
            .child(div().flex_none().text_size(10).text_color(t.dim).child(x.mtime ? ago(x.mtime) : ""));
        })));
    }
    if (this.error) body.child(div().text_size(11.5).text_color(t.blocked).child(this.error));
    else if (d) body.child(this.docView(t, d));
    return v_flex().id("doc").size_full().min_h(0).overflow_hidden().bg(t.surface).border_1().border_color(t.border).text_color(t.foreground).text_size(13)
      .child(head)
      .when(Boolean(this.note), (e) => e.child(div().px(12).py(4).text_size(11).text_color(t.accent).child(this.note)))
      .child(body);
  }
}
