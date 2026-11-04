import { View, div } from "gpui-kit";
import { v_flex, h_flex, InputState, Input, Scrollbar, v_virtual_list } from "gpui-base";
import { spaces, panes, focused, exec, readTextFile, watchFolder, theme, settings, agents, hc, shared, setShared } from "herdr";

// Review comments on diff lines, per repo root, in shared state (they survive reloads and show in every copy).
const COMMENTS = "changes.comments";
const allComments = () => shared(COMMENTS) ?? {};
const commentsFor = (root) => (root ? allComments()[root] ?? [] : []);
const setCommentsFor = (root, list) => { const all = { ...allComments() }; if (list.length) all[root] = list; else delete all[root]; setShared(COMMENTS, all); };
// A theme colour with alpha: themes may give #rrggbb or #rrggbbaa, so always rebuild it as #rrggbb + aa.
const alpha = (c, a) => `${String(c ?? "#000000").slice(0, 7)}${a}`;
const sameLine = (a, b) => a.path === b.path && a.line === b.line && a.side === b.side;

function location() {
  const space = spaces().find((s) => s.active), f = focused();
  const pane = f?.pane ? panes().find(p => p.pane === f.pane) : null;
  const on = f?.pane === null ? (f.terminal?.includes("/") ? f.terminal.split("/")[0] : null) : space?.machine ?? null;
  return { cwd: pane?.cwd || f?.cwd || space?.folder, on };
}
const key = () => JSON.stringify(location());
// Reads write nothing under .git (no index refresh or lock, no fsmonitor cookie):
// the folder watch would see it, and every refresh would start the next one.
const READ = ["--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "diff.autoRefreshIndex=false"];
const countLines = (text) => text ? text.split("\n").length - Number(text.endsWith("\n")) : 0;

// Numbers each diff line once, so a frame only draws the rows on screen.
function diffRows(lines) {
  let old = 0, next = 0;
  return lines.map((line) => {
    const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(line);
    if (match) { old = +match[1]; next = +match[2]; return { kind: "@", text: line }; }
    const kind = line[0];
    if (kind === "-") return { kind, number: old++, text: line.slice(1) || " " };
    if (kind === "+") return { kind, number: next++, text: line.slice(1) || " " };
    if (kind === " ") { old++; return { kind, number: next++, text: line.slice(1) || " " }; }
    return { kind: "", text: line };
  });
}

export default class Changes extends View {
  init(_props, cx) {
    this.files = []; this.rows = []; this.diffWidth = 0; this.message = ""; this.handle = cx.focus_handle();
    this.input = InputState.new({ placeholder: "Commit message" });
    this.input.on("change", (_e, cx) => {
      const hadMessage = Boolean(this.message.trim());
      this.message = this.input.value();
      if (hadMessage !== Boolean(this.message.trim())) cx.notify();
    });
    this.input.on("submit", (_e, cx) => this.commit(cx));
    this.input.on("focus", (_e, cx) => { this.typing = true; cx.notify(); });
    this.input.on("blur", (_e, cx) => { this.typing = false; cx.notify(); });
    // The comment editor for the picked diff line (this.target = {path, line, side, code}).
    this.target = null;
    this.comment = InputState.new({ placeholder: "Comment on this line… (Enter adds)" });
    this.comment.on("submit", (_e, cx) => { this.addComment(); cx.notify(); });
    this.comment.on("focus", (_e, cx) => { this.commenting = true; cx.notify(); });
    this.comment.on("blur", (_e, cx) => { this.commenting = false; cx.notify(); });
    this.refresh(cx);
    cx.timer.every(10000, (cx) => this.refresh(cx));
  }

  watch(root, cx) {
    if (this.on || this.watched === root) return;
    this.watched = root;
    cx.spawn(async (cx) => {
      try {
        while (this.watched === root) {
          await watchFolder(root);
          if (this.watched === root) this.refresh(cx);
        }
      } catch { this.watched = null; }
    });
  }

  refresh(cx) {
    if (this.loading || this.busy) return;
    const { cwd, on } = location(), context = key();
    if (context !== this.context) {
      this.context = context; this.root = null; this.files = []; this.open = null;
      this.setLines([]); this.message = ""; this.input.set_value(""); this.error = null; this.note = null; this.watched = null; this.closed = false; this.limited = false;
    }
    if (!cwd) return;
    this.loading = true;
    cx.spawn(async (cx) => {
      try {
        const top = await exec("git", ["rev-parse", "--show-toplevel"], { cwd, on });
        if (key() !== context) return;
        if (top.code !== 0) { this.root = null; this.files = []; this.setLines([]); this.open = null; this.error = "Not a git repository"; return; }
        const root = top.stdout.trim(), git = (args) => exec("git", [...READ, ...args], { cwd: root, on });
        const status = await git(["status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all"]);
        if (status.code !== 0) throw new Error(status.stderr);
        const head = await git(["rev-parse", "--verify", "HEAD"]);
        const stats = {};
        const results = head.code === 0 ? [await git(["diff", "HEAD", "--numstat", "-z", "--no-renames"])] : [await git(["diff", "--cached", "--numstat", "-z", "--no-renames"]), await git(["diff", "--numstat", "-z", "--no-renames"])];
        for (const result of results) {
          if (result.code !== 0) throw new Error(result.stderr);
          for (const entry of result.stdout.split("\0")) {
            const [a, d, ...parts] = entry.split("\t");
            if (!parts.length) continue;
            const path = parts.join("\t"), old = stats[path] ?? { add: 0, del: 0 };
            stats[path] = { add: old.add + (Number(a) || 0), del: old.del + (Number(d) || 0), binary: a === "-" };
          }
        }
        const files = [];
        const entries = status.stdout.split("\0").filter(Boolean);
        for (const entry of entries.slice(0, 1000)) {
          const xy = entry.slice(0, 2), path = entry.slice(3), fresh = xy === "??";
          const conflict = xy.includes("U") || xy === "AA" || xy === "DD";
          const s = conflict ? "U" : xy.includes("D") ? "D" : fresh || xy.includes("A") ? "A" : "M";
          const stat = stats[path] ?? { add: 0, del: 0 };
          files.push({ path, status: s, fresh, ...stat });
        }
        if (key() !== context) return;
        this.root = root; this.on = on; this.hasHead = head.code === 0; this.files = files; this.limited = entries.length > 1000; this.error = null;
        this.watch(root, cx);
        if (this.open && !files.some((f) => f.path === this.open)) { this.open = null; this.setLines([]); }
        // The first file's diff shows until you pick another.
        if (!this.open && !this.closed && files.length) this.open = files[0].path;
        if (this.open) {
          try { await this.load(this.open, context); }
          catch { if (key() === context) this.setLines(["Cannot preview this diff (too large or unreadable)"]); }
        }
      } catch (e) { if (key() === context) this.error = String(e); }
      finally { this.loading = false; cx.notify(); }
    });
  }

  async load(path, context) {
    const file = this.files.find((f) => f.path === path), root = this.root, on = this.on;
    if (!file) return;
    let lines;
    if (file.fresh || (!this.hasHead && file.status === "A")) {
      const text = await readTextFile(`${root}/${path}`, { on }).catch(() => null);
      if (text === null) lines = ["Cannot preview this file (binary, too large, or unreadable)"];
      else if (text.includes("\0")) lines = ["Binary file · no text diff"];
      else {
        file.add = countLines(text);
        lines = [`@@ -0,0 +1,${file.add} @@`, ...text.slice(0, 400000).replace(/\n$/, "").split("\n").slice(0, 1500).map(l => `+${l}`)];
      }
    } else {
      const r = await exec("git", [...READ, "diff", ...(this.hasHead ? ["HEAD"] : []), "--no-ext-diff", "--no-textconv", "--no-color", "--", path], { cwd: root, on });
      if (r.code !== 0) throw new Error(r.stderr);
      const all = r.stdout.replace(/\n$/, "").split("\n"), first = all.findIndex((l) => l.startsWith("@@"));
      lines = first < 0 ? [file.binary ? "Binary file changed" : "No text diff available"] : all.slice(first);
    }
    if (key() === context && this.open === path) this.setLines(lines.slice(0, 1500).map(line => line.length > 4000 ? line.slice(0, 4000) + "… (line truncated)" : line));
  }

  setLines(lines) {
    this.rows = diffRows(lines);
    this.diffWidth = Math.max(300, ...lines.map(line => line.length * 7 + 50));
  }

  pick(path, cx) {
    this.open = this.open === path ? null : path; this.setLines([]); this.note = null;
    this.closed = !this.open;
    const context = this.context;
    if (this.open) cx.spawn(async (cx) => {
      try { await this.load(path, context); }
      catch (e) { if (key() === context && this.open === path) this.note = `Cannot preview this diff (too large or unreadable): ${e}`; }
      cx.notify();
    });
    cx.notify();
  }

  pickLine(row, cx) {
    if (!this.open || row.number === undefined) return;
    const t = { path: this.open, line: row.number, side: row.kind === "-" ? "old" : "new", code: row.text.trim().slice(0, 160) };
    if (this.target && sameLine(this.target, t)) { this.target = null; cx.notify(); return; }
    this.target = t;
    const had = commentsFor(this.root).find((c) => sameLine(c, t));
    this.comment.set_value(had?.text ?? "");
    this.comment.focus?.();
    cx.notify();
  }

  pickFile(path, cx) {
    const t = { path, line: 0, side: "file", code: "" };
    if (this.target && sameLine(this.target, t)) { this.target = null; cx.notify(); return; }
    this.target = t;
    const had = commentsFor(this.root).find((c) => sameLine(c, t));
    this.comment.set_value(had?.text ?? "");
    cx.notify();
  }

  addComment() {
    const text = this.comment.value().trim(), t = this.target;
    if (!t || !this.root) return;
    const list = commentsFor(this.root).filter((c) => !sameLine(c, t));
    if (text) list.push({ ...t, text, at: Date.now() });
    list.sort((a, b) => a.path.localeCompare(b.path) || a.line - b.line);
    setCommentsFor(this.root, list);
    this.comment.set_value(""); this.target = null;
  }

  removeComment(c) { setCommentsFor(this.root, commentsFor(this.root).filter((x) => !sameLine(x, c))); }

  // The agents in this space, the focused tab's first: where the review goes.
  reviewers() {
    const space = spaces().find((s) => s.active), tab = focused()?.tab;
    return agents().filter((a) => a.space === space?.id).sort((a, b) => (b.tab === tab) - (a.tab === tab) || a.tab_index - b.tab_index);
  }

  sendComments(agent, cx) {
    const list = commentsFor(this.root);
    if (!list.length || !agent) return;
    const lines = list.map((c) => c.side === "file" ? `- ${c.path} (whole file)\n  ${c.text}`
      : `- ${c.path}:${c.line}${c.side === "old" ? " (removed line)" : ""}${c.code ? ` \`${c.code.replace(/`/g, "'")}\`` : ""}\n  ${c.text}`);
    const text = `Review comments on the uncommitted changes in ${this.root}:\n\n${lines.join("\n")}\n\nPlease address each one.`;
    const root = this.root;
    this.sending = true; this.note = null;
    cx.spawn(async (cx) => {
      try {
        const r = await hc(["send", agent.address, text]);
        if (r.code && r.code !== 0) throw new Error((r.stderr || r.stdout || "hc send failed").trim());
        setCommentsFor(root, []);
        this.note = `Sent ${list.length} comment${list.length === 1 ? "" : "s"} to ${agent.tab_name || agent.address}`;
        this.noteOk = true;
      } catch (e) { this.note = `Not sent: ${e.message ?? e}`; this.noteOk = false; }
      finally { this.sending = false; cx.notify(); }
    });
    cx.notify();
  }

  commit(cx) {
    if (!this.message.trim() || !this.root || this.busy || this.loading || this.context !== key()) return;
    const root = this.root, on = this.on, context = this.context, message = this.message.trim();
    this.busy = true; this.note = null;
    cx.spawn(async (cx) => {
      try {
        for (const args of [["add", "-A"], ["commit", "-m", message]]) {
          const r = await exec("git", args, { cwd: root, on });
          if (r.code !== 0) throw new Error((r.stderr || r.stdout).trim());
        }
        if (key() === context) { this.message = ""; this.input.set_value(""); }
      } catch (e) { if (key() === context) this.note = `Commit failed: ${e}`; }
      finally { this.busy = false; this.refresh(cx); cx.notify(); }
    });
    cx.notify();
  }

  // The diff with each comment as its own row right under the line it is about.
  shownRows() {
    const list = commentsFor(this.root).filter((c) => c.path === this.open);
    if (!list.length) return this.rows;
    const out = list.filter((c) => c.side === "file").map((c) => ({ kind: "comment", comment: c }));
    for (const row of this.rows) {
      out.push(row);
      if (row.number === undefined) continue;
      const side = row.kind === "-" ? "old" : "new";
      for (const c of list) if (c.line === row.number && c.side === side) out.push({ kind: "comment", comment: c });
    }
    return out;
  }

  diffRow(row, t, i) {
    const { kind, number, text } = row;
    if (kind === "comment") {
      const c = row.comment;
      return h_flex().id(`cmt-${i}`).w_full().h(20).flex_none().gap(8).px(8).items_center().bg(alpha(t.accent, "1f")).border_l(3).border_color(t.accent)
        .child(div().flex_none().text_color(t.accent).font_semibold().child("▸ REVIEW"))
        .child(div().flex_1().min_w(0).truncate().text_color(t.foreground).child(c.text))
        .child(div().id(`cmt-edit-${i}`).flex_none().px(4).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.foreground))
          .on_click((_e, cx) => { this.target = { path: c.path, line: c.line, side: c.side, code: c.code }; this.comment.set_value(c.text); cx.notify(); }).child("edit"))
        .child(div().id(`cmt-del-${i}`).flex_none().px(4).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.blocked))
          .on_click((_e, cx) => { this.removeComment(c); cx.notify(); }).child("×"));
    }
    if (kind === "@") return div().w_full().px(12).h(20).flex_none().truncate().bg(t.raised).text_color(t.dim).child(text);
    const side = kind === "-" ? "old" : "new";
    const picked = this.target && this.target.path === this.open && this.target.line === number && this.target.side === side;
    return h_flex().id(`dl-${i}`).w_full().h(20).flex_none().gap(10).when(kind === "+", (e) => e.bg(t.ansi2 + "18")).when(kind === "-", (e) => e.bg(t.ansi1 + "18"))
      .when(number !== undefined, (e) => e.cursor_pointer().hover((s) => s.bg(t.hover)).on_click((_e, cx) => this.pickLine(row, cx)))
      .when(Boolean(picked), (e) => e.bg(alpha(t.accent, "33")))
      .child(div().w(30).flex_none().text_right().text_color(picked ? t.accent : t.dim).child(number === undefined ? "" : String(number)))
      .child(div().flex_1().min_w(0).truncate().child(text));
  }

  render(cx) {
    if (key() !== this.context) this.refresh(cx);
    const t = theme(), add = this.files.reduce((n, f) => n + f.add, 0), del = this.files.reduce((n, f) => n + f.del, 0);
    const enabled = Boolean(this.message.trim() && this.root && !this.busy && !this.loading);
    const shown = this.shownRows(), comments = commentsFor(this.root), reviewers = comments.length ? this.reviewers() : [];
    const counts = {}; for (const c of comments) counts[c.path] = (counts[c.path] ?? 0) + 1;
    return v_flex().id("changes").size_full().min_h(0).rounded(0).overflow_hidden().bg(t.surface).border_1().border_color(t.border).text_color(t.foreground).text_size(13).track_focus(this.handle)
      .on_key_down((e, cx) => {
        if (e.key === "escape" && this.typing) { this.typing = false; this.handle.focus(); cx.stop_propagation(); cx.notify(); }
      })
      .child(h_flex().h(42).flex_none().px(14).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().font_semibold().child("Changes"))
        .child(div().px(5).py(1).rounded(0).border_1().border_color(t.border).text_size(10).text_color(t.muted).child(`${this.files.length} files`))
        .child(div().flex_1()).child(div().text_size(11).text_color(t.ansi2).child(`+${add}`)).child(div().text_size(11).text_color(t.ansi1).child(`−${del}`)))
      .child(v_flex().id("changes-list").flex_none().h(Math.min(200, Math.max(44, this.files.length * 28 + 12))).overflow_y_scrollbar().p(6).border_b(1).border_color(t.border).children(this.files.map((f) => {
        const parts = f.path.split("/"), name = parts.pop();
        return h_flex().id(`change-${f.path}`).h(28).flex_none().px(8).gap(8).items_center().rounded(0).cursor_pointer().when(this.open === f.path, (e) => e.bg(t.selected)).hover((s) => s.bg(t.hover)).on_click((_e, cx) => this.pick(f.path, cx))
          .child(div().w(14).text_size(11).text_color(f.status === "A" ? t.ansi2 : f.status === "D" ? t.ansi1 : f.status === "U" ? t.ansi1 : t.blocked).child(f.status))
          .child(h_flex().flex_1().min_w(0).child(div().text_color(t.dim).truncate().child(parts.length ? parts.join("/") + "/" : "")).child(div().max_w(260).min_w(0).truncate().child(name)))
          .when(f.add > 0, (e) => e.child(div().text_size(11).text_color(t.ansi2).child(`+${f.add}`))).when(f.del > 0, (e) => e.child(div().text_size(11).text_color(t.ansi1).child(`−${f.del}`)))
          .when(Boolean(counts[f.path]), (e) => e.child(div().flex_none().px(4).bg(t.accent).text_color(t.accent_foreground).text_size(10).font_semibold().child(`${counts[f.path]}`)))
          .child(div().id(`fc-${f.path}`).flex_none().px(5).h(20).flex().items_center().border_1().border_color(this.target?.path === f.path && this.target?.side === "file" ? t.accent : t.border)
            .text_size(10).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.accent).border_color(t.accent))
            .on_click((_e, cx) => { cx.stop_propagation(); this.pickFile(f.path, cx); }).child("comment"));
      })).when(!this.files.length, (e) => e.child(div().p(8).text_color(t.dim).child(this.error || (this.loading ? "Loading changes…" : this.root ? "No uncommitted changes" : "No folder")))))
      .when(this.limited, el => el.child(div().px(12).py(4).text_size(11).text_color(t.dim).child("Showing the first 1,000 changed files")))
      .child(div().flex_1().min_h(0).relative().font_family(settings().font_family ?? "JetBrains Mono").text_size(11)
        .child(div().id("changes-diff-x").size_full().overflow_x_scrollbar()
          // A plain scrolling list (not a virtual one): its lines are clickable, to comment on them. Diffs are capped at 1,500 lines.
          .child(v_flex().id("changes-diff").w_full().min_w(this.diffWidth).h_full().overflow_y_scrollbar()
            .children(shown.map((row, i) => this.diffRow(row, t, i)))))
        .when(!this.open && this.files.length > 0, (e) => e.child(div().absolute().top(6).px(12).text_color(t.dim).child("Select a file to view its changes"))))
      .when(Boolean(this.note), (e) => e.child(div().px(10).pb(8).text_size(11).text_color(this.noteOk ? t.done : t.ansi1).child(this.note)))
      // Comment editor for the picked line
      .when(Boolean(this.target), (e) => e.child(v_flex().flex_none().px(10).pt(8).gap(6).border_t(1).border_color(t.border)
        .child(h_flex().gap(6).text_size(11).text_color(t.dim)
          .child(div().text_color(t.accent).font_semibold().child("COMMENT"))
          .child(div().flex_1().min_w(0).truncate().child(this.target.side === "file" ? `${this.target.path} (whole file)` : `${this.target.path}:${this.target.line}${this.target.side === "old" ? " (removed)" : ""}`))
          .child(div().id("cmt-cancel").cursor_pointer().hover((s) => s.text_color(t.foreground)).on_click((_e, cx) => { this.target = null; cx.notify(); }).child("cancel")))
        .child(h_flex().gap(8)
          .child(div().id("comment-input").flex_1().min_w(0).h(30).px(10).flex().items_center().border_1().border_color(this.commenting ? t.accent : t.border).cursor_text()
            .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).child(Input.new(this.comment).h_full().text_size(12).text_color(t.foreground)))
          .child(div().id("comment-add").h(30).px(12).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(12).font_semibold().cursor_pointer()
            .on_click((_e, cx) => { this.addComment(); cx.notify(); }).child("Add")))))
      // The review so far: send it to an agent in this space, or clear it
      .when(comments.length > 0, (e) => e.child(h_flex().flex_none().px(10).pt(8).gap(6).items_center().flex_wrap().border_t(1).border_color(t.border).text_size(11)
        .child(div().text_color(t.accent).font_semibold().child(`${comments.length} COMMENT${comments.length === 1 ? "" : "S"}`))
        .child(div().flex_1())
        .when(!reviewers.length, (el) => el.child(div().text_color(t.dim).child("no agent in this space")))
        .children(reviewers.slice(0, 3).map((a) => div().id(`send-${a.address}`).h(24).px(8).flex().items_center().border_1().border_color(t.accent).text_color(t.accent)
          .cursor_pointer().hover((s) => s.bg(t.accent).text_color(t.accent_foreground)).on_click((_e, cx) => this.sendComments(a, cx))
          .child(this.sending ? "Sending…" : `Send to ${a.tab_name || a.program?.name || a.address}`)))
        .child(div().id("cmt-clear").h(24).px(8).flex().items_center().border_1().border_color(t.border).text_color(t.dim).cursor_pointer()
          .hover((s) => s.text_color(t.foreground)).on_click((_e, cx) => { setCommentsFor(this.root, []); cx.notify(); }).child("Clear"))))
      .child(h_flex().flex_none().p(10).gap(8).border_t(1).border_color(t.border)
        .child(div().id("commit-message").flex_1().min_w(0).h(32).px(10).flex().items_center().rounded(0).border_1().border_color(this.typing ? t.accent : t.border).cursor_text().on_mouse_down("left", (_e, cx) => cx.stop_propagation()).child(Input.new(this.input).h_full().text_size(12).text_color(t.foreground)))
        .child(div().id("commit").h(32).px(14).flex().items_center().rounded(0).bg(enabled ? t.solid : t.raised).text_color(enabled ? t.solid_foreground : t.dim).text_size(12).when(enabled, (e) => e.cursor_pointer()).on_click((_e, cx) => this.commit(cx)).child(this.busy ? "Committing…" : "Commit")));
  }
}
