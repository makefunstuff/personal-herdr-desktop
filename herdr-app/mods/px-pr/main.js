// PR: the pull request of the focused pane's branch, to read and act on its review.
// Shows the PR (state, review decision, checks), every review comment thread (file:line, the code it is on,
// the replies), every review with a body and every conversation comment. On each comment: reactions (posted to
// GitHub only when clicked), "Ask agent" (sends the comment with its file, line and code to the agent doing the
// PR's work, or Chores, asking what it means; no code change) and "GitHub" (opens it). Asks gh on the pane's machine while shown,
// every 60 s; a click on the header refreshes. Reply: into a review thread, or a PR comment; posted only on Post.
// Through gh: the person's own login and permissions.
import * as fs from "fs/promises";
import { View, div } from "gpui-kit";
import { v_flex, h_flex, TextView, TextareaState, Textarea } from "gpui-base";
import { spaces, panes, focused, exec, theme, settings, shared, setShared, agents, hc, props, home, appHome, run, window } from "herdr";
// The desktop's opener for links and files: open on macOS, xdg-open on Linux.
const OPENER = () => (window()?.platform === "macos" ? "open" : "xdg-open");

const EVERY = 60000;
const REACTIONS = [["+1", "👍"], ["-1", "👎"], ["heart", "❤️"], ["hooray", "🎉"], ["confused", "😕"], ["eyes", "👀"], ["rocket", "🚀"], ["laugh", "😄"]];
const caps = (s) => (shared("layout") === "workbench" ? String(s).toUpperCase() : String(s));
const alpha = (c, a) => `${String(c ?? "#000000").slice(0, 7)}${a}`;
const ago = (iso) => {
  const m = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60000));
  return m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h` : `${Math.floor(m / 1440)}d`;
};

function location() {
  // A pane of this mod can be pointed at a folder (props {folder}) or at one PR (props {repo, number}).
  if (props()?.repo && props()?.number) return { cwd: home(), on: null, repo: props().repo, number: props().number };
  // the side panel shows a PR picked in Reviews (shared "pr.target") until BACK
  const pick = !props()?.folder ? shared("pr.target") : null;
  if (pick?.repo && pick?.number) return { cwd: home(), on: null, repo: pick.repo, number: pick.number, picked: true };
  if (props()?.folder) return { cwd: props().folder, on: props().machine ?? null };
  const space = spaces().find((s) => s.active), f = focused();
  const pane = f?.pane ? panes().find((p) => p.pane === f.pane) : null;
  const on = f?.pane === null ? (f.terminal?.includes("/") ? f.terminal.split("/")[0] : null) : space?.machine ?? null;
  return { cwd: pane?.cwd || f?.cwd || space?.folder, on };
}
const key = () => JSON.stringify(location());

// gh without prompts or update checks, and never longer than 25 s (a hung call would keep the panel loading).
async function gh(args, cwd, on) {
  const r = await exec("env", ["GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_PAGER=cat", "NO_COLOR=1", "timeout", "25", "gh", ...args], { cwd, on: on ?? undefined });
  if (r.code === 124) throw new Error(`gh ${args.slice(0, 2).join(" ")} took longer than 25 s`);
  if (r.code !== 0) throw new Error(String(r.stderr || r.stdout || "gh failed").trim().split("\n").pop());
  return r.stdout;
}

// Review work goes to one agent of its own in the "Chores" space (~/Work if it exists, else home), never the agent you're working with:
// its live agent gets the request (hc send waits until it's free, so requests queue); no agent there yet: a new tab
// starts Claude Code with the request as its first message; no Chores space yet: it's made the same way.
const CHORES = "Chores";
const choresDir = async () => { try { await fs.readdir(`${home()}/Work`); return `${home()}/Work`; } catch { return home(); } };
async function toChores(text) {
  const sp = spaces().find((s) => s.name === CHORES && !s.machine);
  const ag = sp ? agents().find((a) => a.space === sp.id && !String(a.address ?? "").includes("/")) : null;
  if (ag) {
    const r = await hc(["send", ag.address, text]);
    if (r.code && r.code !== 0) throw new Error(String(r.stderr || r.stdout).trim());
    return `Sent to ${CHORES} (${ag.tab_name || ag.address}).`;
  }
  const args = sp ? ["do", "tab.new", "--space", sp.id, "--name", "reviews", "--folder", await choresDir(), "--argv", JSON.stringify(["claude"]), "--task", text]
    : ["do", "space.new", "--name", CHORES, "--folder", await choresDir(), "--tab", "reviews", "--argv", JSON.stringify(["claude"]), "--task", text];
  const r = await exec("herdr-app", args);
  if (r.code !== 0) throw new Error(String(r.stderr || r.stdout).trim().split("\n").pop());
  return sp ? `Started a ${CHORES} agent with it.` : `Made the ${CHORES} space; its agent is on it.`;
}

// A PR that belongs to the person's own work goes to the agent doing that work, not Chores:
// 1. the agent of a running task on the PR's branch; 2. of a task sharing its ticket (ABC-123…); 3. for the person's own PR
// with no such task, the coordinator of the repo's space. Anything else (other people's PRs) goes to Chores.
const TICKET_RE = /\b[A-Z][A-Z0-9]+-\d+\b/g;
const tickets = (...xs) => new Set(xs.flatMap((x) => String(x ?? "").toUpperCase().match(TICKET_RE) ?? []));
async function workAgent(pr) {
  const live = new Set(agents().filter((a) => !String(a.address ?? "").includes("/")).map((a) => a.address));
  const repoName = String(pr.repo ?? "").split("/").pop();
  const want = tickets(pr.title, pr.branch);
  let byTicket = null;
  let dir = []; try { dir = await fs.readdir(`${home()}/.herdr/tasks`); } catch {}
  for (const f of dir.filter((x) => x.endsWith(".json"))) {
    let list; try { list = JSON.parse(await fs.readFile(`${home()}/.herdr/tasks/${f}`, "utf8")); } catch { continue; }
    if (String(list.folder ?? "").split("/").pop() !== repoName) continue;
    for (const t of list.tasks ?? []) {
      if (["done", "dropped", "archived"].includes(t.state)) continue;
      const a = [...(t.agents ?? [])].reverse().find((x) => live.has(x.address));
      if (!a) continue;
      const who = { address: a.address, label: `task ${list.prefix ?? ""}${list.prefix ? "-" : "#"}${t.id} ${String(t.title ?? "").slice(0, 40)}` };
      if (pr.branch && t.branch === pr.branch) return who;
      if (!byTicket && want.size && [...tickets(t.title, t.branch)].some((k) => want.has(k))) byTicket = who;
    }
  }
  if (byTicket) return byTicket;
  if (pr.mine) {
    const sp = spaces().find((s) => !s.machine && (s.name === repoName || String(s.folder ?? "").split("/").pop() === repoName));
    const tabName = (id) => sp?.tabs?.find((t) => t.id === id)?.name;
    const ag = sp ? agents().filter((a) => a.space === sp.id && live.has(a.address)).sort((a, b) => (tabName(b.tab) === "coordinator") - (tabName(a.tab) === "coordinator"))[0] : null;
    if (ag) return { address: ag.address, label: `${sp.name} coordinator` };
  }
  return null;
}
async function toWork(pr, text) {
  const w = await workAgent(pr).catch(() => null);
  if (!w) return toChores(text);
  const r = await hc(["send", w.address, text]);
  if (r.code && r.code !== 0) throw new Error(String(r.stderr || r.stdout || "send failed").trim());
  return `Sent to ${w.label} (your work, not ${CHORES}).`;
}

export default class PullRequest extends View {
  init(_p, cx) {
    this.ctx = null; this.data = null; this.error = null; this.loading = false; this.note = null; this.open = {};
    this.replyTo = null; // { kind: "thread" | "pr", id, label }
    this.box = TextareaState.new({ placeholder: "Write a reply… (Post sends it to GitHub)" });
    this.refresh(cx);
    cx.timer.every(EVERY, (cx) => this.refresh(cx));
  }

  // One GraphQL call: the PR, its review threads (with comments, reactions, resolved), reviews and conversation.
  // A PR seen before shows at once from the cache while it refreshes; the latest click wins over a slow earlier one.
  refresh(cx) {
    const L = location(), ctx = key();
    if (ctx === this.ctx && this.loading) return;
    if (ctx !== this.ctx) { this.ctx = ctx; this.data = (this.cache ??= new Map()).get(ctx) ?? null; this.error = null; }
    if (!L.cwd) return;
    const gen = (this.gen = (this.gen ?? 0) + 1);
    this.loading = true; this.where = L.repo ? `${L.repo}#${L.number}` : L.cwd; this.step = "pull request";
    cx.spawn(async (cx) => {
      try {
        let owner, name, num;
        if (L.repo) { [owner, name] = L.repo.split("/"); num = Number(L.number); }
        else {
          const g = (args) => exec("git", ["-C", L.cwd, ...args], { on: L.on ?? undefined }).then((r) => (r.code === 0 ? String(r.stdout).trim() : null)).catch(() => null);
          const [url, branch] = await Promise.all([g(["remote", "get-url", "origin"]), g(["branch", "--show-current"])]);
          if (!url) throw new Error("not a git repository");
          const m = /github\.com[:/]([^/]+)\/(.+?)(?:\.git)?$/.exec(url);
          if (!m || !branch) throw new Error("no pull requests found");
          [owner, name] = [m[1], m[2]];
          const q = `query($o:String!,$n:String!,$b:String!){repository(owner:$o,name:$n){pullRequests(headRefName:$b,first:1,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{number}}}}`;
          const j = JSON.parse(await gh(["api", "graphql", "-f", `query=${q}`, "-f", `o=${owner}`, "-f", `n=${name}`, "-f", `b=${branch}`], L.cwd, L.on));
          num = j.data?.repository?.pullRequests?.nodes?.[0]?.number;
          if (!num) throw new Error("no pull requests found");
        }
        if (gen !== this.gen) return;
        const RG = "reactionGroups{content users{totalCount}}";
        const q = `query($o:String!,$n:String!,$num:Int!){viewer{login} repository(owner:$o,name:$n){pullRequest(number:$num){
          number title state isDraft url headRefName baseRefName headRefOid body reviewDecision additions deletions changedFiles author{login}
          commits(last:1){nodes{commit{statusCheckRollup{state contexts(first:100){nodes{__typename ... on CheckRun{name conclusion status detailsUrl} ... on StatusContext{context state targetUrl}}}}}}}
          reviewThreads(first:100){nodes{isResolved isOutdated path line originalLine diffSide comments(first:60){nodes{databaseId body createdAt url author{login} diffHunk ${RG}}}}}
          comments(first:100){nodes{databaseId body createdAt url author{login} ${RG}}}
          reviews(first:100){nodes{databaseId state body submittedAt url author{login}}}}}}`;
        const j = JSON.parse(await gh(["api", "graphql", "-f", `query=${q}`, "-f", `o=${owner}`, "-f", `n=${name}`, "-F", `num=${num}`], L.cwd, L.on));
        if (j.errors?.length) throw new Error(j.errors[0].message);
        if (gen !== this.gen) return;
        const P = j.data.repository.pullRequest;
        const KEYS = { THUMBS_UP: "+1", THUMBS_DOWN: "-1", LAUGH: "laugh", HOORAY: "hooray", CONFUSED: "confused", HEART: "heart", ROCKET: "rocket", EYES: "eyes" };
        const reactions = (gs) => Object.fromEntries((gs ?? []).map((g) => [KEYS[g.content] ?? g.content, g.users?.totalCount ?? 0]));
        const rollup = P.commits?.nodes?.[0]?.commit?.statusCheckRollup;
        const pr = { number: P.number, title: P.title, body: P.body, state: P.state, isDraft: P.isDraft, url: P.url, headRefName: P.headRefName, baseRefName: P.baseRefName,
          headRefOid: P.headRefOid, reviewDecision: P.reviewDecision, additions: P.additions, deletions: P.deletions, changedFiles: P.changedFiles, author: P.author,
          statusCheckRollup: (rollup?.contexts?.nodes ?? []).map((c) => ({ __typename: c.__typename, name: c.name ?? c.context, conclusion: c.conclusion, status: c.status, state: c.state, url: c.detailsUrl ?? c.targetUrl })) };
        const threads = (P.reviewThreads?.nodes ?? []).map((th) => {
          const cs = (th.comments?.nodes ?? []).map((c) => ({ id: c.databaseId, user: { login: c.author?.login ?? "ghost" }, body: c.body, created_at: c.createdAt, html_url: c.url,
            path: th.path, line: th.line, original_line: th.originalLine, side: th.diffSide, diff_hunk: c.diffHunk, reactions: reactions(c.reactionGroups) }));
          return cs.length ? { root: cs[0], replies: cs.slice(1), resolved: th.isResolved, outdated: th.isOutdated } : null;
        }).filter(Boolean).sort((a, b) => a.resolved - b.resolved || (a.root.path || "").localeCompare(b.root.path || "") || (a.root.line ?? a.root.original_line ?? 0) - (b.root.line ?? b.root.original_line ?? 0));
        const issue = (P.comments?.nodes ?? []).map((c) => ({ id: c.databaseId, user: { login: c.author?.login ?? "ghost" }, body: c.body, created_at: c.createdAt, html_url: c.url, reactions: reactions(c.reactionGroups) }));
        const reviews = (P.reviews?.nodes ?? []).filter((r) => (r.body || "").trim()).map((r) => ({ id: r.databaseId, user: { login: r.author?.login ?? "ghost" }, state: r.state, body: r.body, submitted_at: r.submittedAt, html_url: r.url }));
        const repo = `${owner}/${name}`, prKey = `${repo}#${pr.number}`;
        if (this.prKey !== prKey) { this.prKey = prKey; this.drafts = []; this.lineTo = null; }
        const files = this.data && this.data.repo === repo && this.data.pr?.number === pr.number && this.data.pr?.headRefOid === pr.headRefOid ? this.data.files : null;
        this.data = { pr, repo, me: j.data.viewer?.login ?? null, threads, issue, reviews, files, cwd: L.cwd, on: L.on };
        this.cache.set(ctx, this.data);
        this.error = null;
        if (this.view === "files" && !files) this.loadFiles(cx);
      } catch (e) {
        if (gen !== this.gen) return;
        const msg = String(e?.message ?? e);
        if (!this.data) this.error = /not a git/i.test(msg) ? "Not a git repository." : /no pull requests found|could not find|Could not resolve/i.test(msg) ? "This branch has no pull request." : msg;
        else this.note = `Refresh failed: ${msg}`;
      } finally { if (gen === this.gen) this.loading = false; cx.notify(); }
    });
  }

  // The FILES tab's diffs, fetched when it opens (and again after a push).
  loadFiles(cx) {
    const d = this.data; if (!d || this.filesLoading) return;
    this.filesLoading = true;
    cx.spawn(async (cx) => {
      try {
        const fl = await gh(["api", `repos/${d.repo}/pulls/${d.pr.number}/files?per_page=100`, "--paginate"], d.cwd, d.on);
        const files = JSON.parse(`[${fl.trim().replace(/\]\s*\[/g, ",").replace(/^\[|\]$/g, "")}]`);
        if (this.data === d) { d.files = files; }
      } catch (e) { this.note = `Files didn't load: ${e.message ?? e}`; }
      this.filesLoading = false; cx.notify();
    });
  }

  react(kind, comment, content, cx) {
    const d = this.data; if (!d) return;
    const path = kind === "review" ? `repos/${d.repo}/pulls/comments/${comment.id}/reactions` : `repos/${d.repo}/issues/comments/${comment.id}/reactions`;
    cx.spawn(async (cx) => {
      try { await gh(["api", "-X", "POST", path, "-f", `content=${content}`, "-H", "Accept: application/vnd.github+json"], d.cwd, d.on); this.note = null; }
      catch (e) { this.note = `Reaction not added: ${e.message ?? e}`; }
      this.refresh(cx); cx.notify();
    });
  }

  post(cx) {
    const d = this.data, to = this.replyTo, text = this.box.value().trim();
    if (to?.kind === "review") return this.submitReview(cx, to.event, text);
    if (!d || !to || !text || this.posting) return;
    this.posting = true; cx.notify();
    cx.spawn(async (cx) => {
      try {
        if (to.kind === "thread") await gh(["api", "-X", "POST", `repos/${d.repo}/pulls/${d.pr.number}/comments/${to.id}/replies`, "-f", `body=${text}`], d.cwd, d.on);
        else await gh(["pr", "comment", String(d.pr.number), "--repo", d.repo, "--body", text], d.cwd, d.on);
        this.box.set_value(""); this.replyTo = null; this.note = "Reply posted.";
      } catch (e) { this.note = `Reply not posted: ${e.message ?? e}`; }
      this.posting = false; this.refresh(cx); cx.notify();
    });
  }

  replyButton(t, id, to) {
    const on = this.replyTo && this.replyTo.kind === to.kind && this.replyTo.id === to.id;
    return div().id(id).h(20).px(7).flex().items_center().border_1().border_color(on ? t.accent : t.border).text_size(10.5)
      .text_color(on ? t.accent : t.dim).cursor_pointer().hover((s) => s.text_color(t.foreground))
      .on_click((_e, cx) => { this.replyTo = on ? null : to; cx.notify(); }).child(caps("Reply"));
  }

  ask(c, hunk, cx) {
    const d = this.data; if (!d) return;
    const where = c.path ? `${c.path}:${c.line ?? c.original_line ?? "?"}` : "the PR conversation";
    const code = hunk ? `\nThe code it is on:\n\`\`\`\n${hunk.split("\n").slice(-8).join("\n")}\n\`\`\`` : "";
    const text = `Question about a review comment on PR #${d.pr.number} (${d.pr.title}), ${where}, by @${c.user?.login}:\n\n${(c.body || "").split("\n").map((l) => `> ${l}`).join("\n")}\n${code}\n\nWhat does this comment mean, and what would addressing it involve? Explain only; don't change code or reply on GitHub yet.`;
    this.note = "Asking…"; cx.notify();
    cx.spawn(async (cx) => {
      try { this.note = await toWork({ repo: d.repo, branch: d.pr.headRefName, title: d.pr.title, mine: Boolean(d.me) && d.pr.author?.login === d.me }, text); }
      catch (e) { this.note = `Not sent: ${e.message ?? e}`; }
      cx.notify();
    });
  }

  // ---- the files view: diffs to read, lines to comment on (drafts), one review to submit (COMMENT only)
  rows(patch) {
    const out = []; let o = 0, n = 0;
    for (const l of String(patch ?? "").split("\n")) {
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(l);
      if (m) { o = +m[1]; n = +m[2]; out.push({ k: "@", text: l }); continue; }
      const k = l[0];
      if (k === "-") out.push({ k, old: o++, text: l.slice(1) });
      else if (k === "+") out.push({ k, line: n++, text: l.slice(1) });
      else if (k === " ") out.push({ k: " ", old: o++, line: n++, text: l.slice(1) });
      else out.push({ k: "", text: l });
    }
    return out;
  }

  addDraft() {
    const to = this.lineTo, body = this.box.value().trim();
    if (!to || !body) return;
    this.drafts = [...(this.drafts ?? []).filter((x) => !(x.path === to.path && x.line === to.line && x.side === to.side)), { ...to, body }];
    this.box.set_value(""); this.lineTo = null;
  }

  submitReview(cx, event = "COMMENT", body = "") {
    const d = this.data, drafts = this.drafts ?? [];
    if (!d || this.posting || (event === "COMMENT" && !drafts.length && !body)) return;
    if (event === "REQUEST_CHANGES" && !body && !drafts.length) { this.note = "Say what needs changing first."; cx.notify(); return; }
    this.posting = true; cx.notify();
    cx.spawn(async (cx) => {
      const file = `${appHome()}/data/px-pr-review-${Date.now()}.json`;
      try {
        const payload = { commit_id: d.pr.headRefOid, event, body, comments: drafts.map((x) => ({ path: x.path, line: x.line, side: x.side, body: x.body })) };
        await fs.writeFile(file, JSON.stringify(payload));
        await gh(["api", "-X", "POST", `repos/${d.repo}/pulls/${d.pr.number}/reviews`, "--input", file], d.cwd, d.on);
        const what = { APPROVE: "Approved", REQUEST_CHANGES: "Changes requested", COMMENT: "Review posted" }[event];
        this.note = `${what}${drafts.length ? ` · ${drafts.length} line comment${drafts.length > 1 ? "s" : ""}` : ""}.`; this.drafts = [];
        if (this.replyTo?.kind === "review") { this.replyTo = null; this.box.set_value(""); }
      } catch (e) { this.note = `Review not posted (drafts kept): ${e.message ?? e}`; }
      await exec("rm", ["-f", file]).catch(() => {});
      this.posting = false; this.refresh(cx); cx.notify();
    });
  }

  filesView(t, body, d, mono) {
    const drafts = this.drafts ?? [];
    const threadAt = new Map();
    for (const th of d.threads) { const ln = th.root.line ?? th.root.original_line; threadAt.set(`${th.root.path}|${ln}|${th.root.side ?? "RIGHT"}`, th.replies.length + 1); }
    body.child(h_flex().w_full().mt(10).gap(8).items_center().text_size(10.5).text_color(t.dim)
      .child(div().child(`${(d.files ?? []).length} files · +${d.pr.additions} −${d.pr.deletions} · click a line to comment`))
      .child(div().flex_1())
      .when(drafts.length > 0, (e) => e.child(this.link(t, "submit-review", this.posting ? "Posting…" : `Submit review · ${drafts.length}`, (cx) => this.submitReview(cx), true))));
    for (const f of d.files ?? []) {
      const open = this.fileOpen?.[f.filename] ?? (d.files ?? []).length <= 12;
      const parts = String(f.filename).split("/"), name = parts.pop();
      const diff = v_flex().w_full().bg(t.background).font_family(mono).text_size(10.5);
      this.rows(f.patch).slice(0, 1500).forEach((r, i) => {
        if (r.k === "@") { diff.child(div().w_full().px(6).h(16).bg(t.raised).text_color(t.dim).truncate().child(r.text)); return; }
        if (r.k === "") { diff.child(div().w_full().px(6).h(16).text_color(t.dim).truncate().child(r.text || " ")); return; }
        const side = r.k === "-" ? "LEFT" : "RIGHT", ln = r.k === "-" ? r.old : r.line;
        const key = `${f.filename}|${ln}|${side}`, threads = threadAt.get(key), draft = drafts.find((x) => x.path === f.filename && x.line === ln && x.side === side);
        const picked = this.lineTo && this.lineTo.path === f.filename && this.lineTo.line === ln && this.lineTo.side === side;
        diff.child(h_flex().id(`dl-${f.filename}-${i}`).w_full().h(16).items_center().cursor_pointer()
          .when(r.k === "+", (x) => x.bg(alpha(t.done ?? "#5fd38a", "14"))).when(r.k === "-", (x) => x.bg(alpha(t.blocked ?? "#ff5a4a", "14")))
          .when(Boolean(picked), (x) => x.bg(alpha(t.accent, "33"))).hover((x) => x.bg(t.hover))
          .on_click((_e, cx) => { this.lineTo = picked ? null : { path: f.filename, line: ln, side, label: `${f.filename}:${ln}${side === "LEFT" ? " (removed)" : ""}` };
            this.replyTo = null; this.box.set_value(draft?.body ?? ""); cx.notify(); })
          .child(div().w(34).flex_none().pr(4).text_right().text_color(t.dim).child(r.k !== "+" && r.old != null ? String(r.old) : ""))
          .child(div().w(34).flex_none().pr(4).text_right().text_color(t.dim).child(r.k !== "-" && r.line != null ? String(r.line) : ""))
          .child(div().w(12).flex_none().text_color(r.k === "+" ? t.done : r.k === "-" ? t.blocked : t.dim).child(r.k === "+" || r.k === "-" ? r.k : ""))
          .child(div().flex_1().min_w(0).truncate().text_color(t.foreground).child(r.text || " "))
          .when(Boolean(threads), (x) => x.child(div().px(4).mr(4).bg(t.raised).text_size(9.5).text_color(t.accent).child(`● ${threads}`))));
        if (draft) diff.child(h_flex().w_full().pl(80).pr(6).py(2).gap(6).bg(alpha(t.accent, "14")).border_l(3).border_color(t.accent)
          .child(div().text_size(9.5).font_semibold().text_color(t.accent).child(caps("draft")))
          .child(div().flex_1().min_w(0).text_size(11).text_color(t.foreground).child(draft.body))
          .child(div().id(`dd-${key}`).text_size(10).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.blocked))
            .on_click((_e, cx) => { this.drafts = drafts.filter((x) => x !== draft); cx.notify(); }).child("×")));
      });
      if (!f.patch) diff.child(div().px(6).py(4).text_color(t.dim).child("No text diff (binary, renamed or too large)"));
      body.child(v_flex().w_full().mt(6).border_1().border_color(t.border).bg(t.chrome)
        .child(h_flex().id(`fh-${f.filename}`).w_full().h(22).items_center().cursor_pointer()
          .on_click((_e, cx) => { this.fileOpen = { ...(this.fileOpen ?? {}), [f.filename]: !open }; cx.notify(); })
          .child(div().h_full().w(22).flex().items_center().justify_center().bg(t.raised).text_size(10).child(open ? "▾" : "▸"))
          .child(div().px(6).text_size(10.5).text_color(t.dim).child(parts.length ? parts.join("/") + "/" : ""))
          .child(div().flex_1().min_w(0).truncate().text_size(11).font_semibold().text_color(t.foreground).child(name))
          .child(div().px(6).text_size(10).text_color(t.done).child(`+${f.additions}`)).child(div().pr(6).text_size(10).text_color(t.blocked).child(`−${f.deletions}`)))
        .when(open, (e) => e.child(diff)));
    }
  }

  // ---- pieces (workbench module look: square cells, slate frames, amber for what acts or matters)
  // A small text button: dim, amber on hover; `solid` fills it amber (the one main action).
  // Opens the composer for a review of this kind; POST (named after it) submits.
  reviewBtn(t, event, label, colour, d) {
    const on = this.replyTo?.kind === "review" && this.replyTo.event === event;
    return div().id(`rv-${event}`).h(20).px(8).flex().items_center().flex_none().text_size(10).font_semibold().cursor_pointer().border_1().border_color(colour)
      .when(on, (e) => e.bg(colour).text_color(t.background)).when(!on, (e) => e.text_color(colour).hover((s) => s.bg(t.raised)))
      .on_click((_e, cx) => { this.lineTo = null; this.replyTo = on ? null : { kind: "review", event, title: label, label: `${d.repo}#${d.pr.number}` }; cx.notify(); })
      .child(caps(label));
  }

  link(t, id, label, fn, solid = false) {
    return div().id(id).h(18).px(6).flex().items_center().flex_none().text_size(10).font_semibold()
      .when(solid, (e) => e.bg(t.accent).text_color(t.accent_foreground))
      .when(!solid, (e) => e.text_color(t.dim).hover((s) => s.text_color(t.accent)))
      .cursor_pointer().on_click((_e, cx) => fn(cx)).child(caps(label));
  }

  // Reactions it already has (counts), and one + that opens the full row.
  reactions(t, kind, c) {
    const r = c.reactions ?? {}, id = `${kind}-${c.id}`, open = this.picking === id;
    const has = REACTIONS.filter(([k]) => (r[k] ?? 0) > 0);
    const chip = (k, e, n) => div().id(`re-${id}-${k}`).h(18).px(4).flex().items_center().gap(3).flex_none().border_1().border_color(n ? t.line ?? t.border : t.border)
      .text_size(10.5).text_color(t.foreground).cursor_pointer().hover((s) => s.border_color(t.accent))
      .on_click((_e, cx) => { this.picking = null; this.react(kind, c, k, cx); }).child(e).when(n > 0, (el) => el.child(div().text_color(t.dim).child(String(n))));
    return h_flex().gap(3).items_center().flex_wrap()
      .children(has.map(([k, e]) => chip(k, e, r[k])))
      .when(open, (el) => el.children(REACTIONS.filter(([k]) => !(r[k] > 0)).map(([k, e]) => chip(k, e, 0))))
      .child(div().id(`re-${id}-more`).h(18).w(18).flex().items_center().justify_center().flex_none().border_1().border_color(open ? t.accent : t.border)
        .text_size(11).text_color(open ? t.accent : t.dim).cursor_pointer().hover((s) => s.text_color(t.accent))
        .on_click((_e, cx) => { this.picking = open ? null : id; cx.notify(); }).child(open ? "×" : "+"));
  }

  // One comment: author, review state, age (opens it on GitHub) and its actions on the header line; the body; reactions it has.
  comment(t, kind, c, hunk, state = null) {
    const id = `${kind}-${c.id}`;
    const has = Object.values(c.reactions ?? {}).some((n) => n > 0) || this.picking === id;
    const tone = state === "CHANGES_REQUESTED" ? t.blocked : state === "APPROVED" ? t.done : t.muted;
    return v_flex().w_full().gap(4)
      .child(h_flex().w_full().gap(6).items_center().text_size(10.5)
        .child(div().font_semibold().text_color(t.foreground).child(c.user?.login ?? "?"))
        .when(Boolean(state), (el) => el.child(div().px(4).border_1().border_color(tone).text_size(9.5).font_semibold().text_color(tone)
          .child(caps(state === "CHANGES_REQUESTED" ? "requested changes" : state === "APPROVED" ? "approved" : "reviewed"))))
        .child(div().id(`gh-${id}`).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.accent))
          .on_click(() => exec(OPENER(), [c.html_url]).catch(() => {})).child(ago(c.created_at ?? c.submitted_at)))
        .child(div().flex_1())
        .when(kind !== "review-body" && !has, (el) => el.child(this.link(t, `re-${id}-add`, "React", (cx) => { this.picking = id; cx.notify(); })))
        .child(this.link(t, `ask-${id}`, "Ask", (cx) => this.ask(c, hunk, cx))))
      .when(Boolean((c.body || "").trim()), (el) => el.child(div().w_full().text_size(12).text_color(t.foreground).child(TextView.markdown(`md-${id}`, c.body).selectable(true))))
      .when(kind !== "review-body" && has, (el) => el.child(this.reactions(t, kind, c)));
  }

  // A review thread: where it is, the code it's on, the comments, and a reply line.
  thread(t, th, mono) {
    const r = th.root, hunk = r.diff_hunk || "", line = r.line ?? r.original_line;
    const reply = { kind: "thread", id: r.id, label: `${r.path}:${line ?? "?"}` };
    const replying = this.replyTo?.kind === "thread" && this.replyTo.id === r.id;
    const fold = th.resolved && !this.openThread?.[r.id];
    const name = String(r.path ?? "").split("/").pop();
    const head = h_flex().id(`th-${r.id}`).w_full().h(22).items_center().gap(6).border_b(fold ? 0 : 1).border_color(t.border).cursor_pointer()
      .on_click((_e, cx) => { if (th.resolved) { this.openThread = { ...(this.openThread ?? {}), [r.id]: fold }; cx.notify(); } })
      .child(div().h_full().px(6).flex().items_center().bg(th.resolved ? t.raised : t.accent).text_color(th.resolved ? t.dim : t.accent_foreground)
        .text_size(10).font_semibold().child(`L${line ?? "?"}`))
      .child(div().flex_1().min_w(0).truncate().text_size(10.5).text_color(th.resolved ? t.dim : t.foreground)
        .child(fold ? `${r.user?.login}: ${String(r.body || "").replace(/\s+/g, " ").slice(0, 120)}` : name))
      .when(th.outdated, (el) => el.child(div().text_size(9.5).text_color(t.dim).child(caps("outdated"))))
      .when(th.resolved, (el) => el.child(div().text_size(9.5).text_color(t.done).child(caps("resolved"))))
      .child(div().pr(6).text_size(10).text_color(t.dim).child(`${th.replies.length + 1}`));
    const card = v_flex().w_full().mt(6).border_1().border_color(th.resolved ? t.border : t.line ?? t.border).bg(t.chrome).child(head);
    if (fold) return card;
    return card
      .when(Boolean(hunk), (el) => el.child(this.code(t, hunk, mono)))
      .child(v_flex().w_full().px(10).py(8).gap(10)
        .child(this.comment(t, "review", r, hunk))
        .children(th.replies.map((c) => h_flex().w_full().gap(8).child(div().w(2).flex_none().h_full().bg(t.border))
          .child(div().flex_1().min_w(0).child(this.comment(t, "review", c, hunk))))))
      .child(div().id(`rp-${r.id}`).mx(10).mb(8).h(24).px(8).flex().items_center().border_1().border_color(replying ? t.accent : t.border).bg(t.background)
        .text_size(11).text_color(replying ? t.accent : t.dim).cursor_pointer().hover((s) => s.border_color(t.accent))
        .on_click((_e, cx) => { this.lineTo = null; this.replyTo = replying ? null : reply; cx.notify(); })
        .child(replying ? "Replying below…" : "Reply…"));
  }

  checkTone(t, c) {
    const v = String(c.conclusion ?? c.state ?? "").toUpperCase();
    if (["FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED"].includes(v)) return ["fail", t.blocked];
    if (c.__typename === "CheckRun" ? String(c.status).toUpperCase() !== "COMPLETED" : ["PENDING", "EXPECTED"].includes(v)) return ["run", t.working];
    if (["SKIPPED", "NEUTRAL"].includes(v)) return ["skip", t.dim];
    return ["ok", t.done];
  }

  checksView(t, body, d) {
    const order = { fail: 0, run: 1, ok: 2, skip: 3 };
    const cs = (d.pr.statusCheckRollup ?? []).map((c) => ({ c, k: this.checkTone(t, c) })).sort((a, b) => order[a.k[0]] - order[b.k[0]] || String(a.c.name).localeCompare(String(b.c.name)));
    if (!cs.length) { body.child(div().pt(12).text_size(12).text_color(t.dim).child("No checks on the latest commit.")); return; }
    body.child(v_flex().w_full().mt(10).border_1().border_color(t.border).bg(t.chrome).children(cs.map(({ c, k }, i) =>
      h_flex().id(`ck-${i}`).w_full().h(24).px(8).gap(8).items_center().border_b(1).border_color(t.border).cursor_pointer().hover((s) => s.bg(t.hover))
        .on_click(() => c.url && exec(OPENER(), [c.url]).catch(() => {}))
        .child(div().w(8).h(8).flex_none().bg(k[1]))
        .child(div().flex_1().min_w(0).truncate().text_size(11.5).text_color(k[0] === "skip" ? t.dim : t.foreground).child(c.name ?? "?"))
        .child(div().text_size(10).font_semibold().text_color(k[1]).child(caps(k[0] === "ok" ? "passed" : k[0] === "fail" ? String(c.conclusion ?? c.state).toLowerCase().replace(/_/g, " ") : k[0] === "run" ? "running" : "skipped"))))));
  }

  conversationView(t, body, d, mono) {
    // the description, folded when long
    const desc = String(d.pr.body ?? "").trim();
    if (desc) {
      const long = desc.length > 700 || desc.split("\n").length > 14, open = this.descOpen || !long;
      const text = open ? desc : desc.split("\n").slice(0, 14).join("\n").slice(0, 700) + " …";
      body.child(v_flex().w_full().mt(10).border_1().border_color(t.border).bg(t.chrome)
        .child(h_flex().w_full().h(22).items_center().border_b(1).border_color(t.border)
          .child(div().h_full().px(8).flex().items_center().bg(t.raised).text_size(10).font_semibold().child(caps("Description")))
          .child(div().px(8).text_size(10).text_color(t.dim).child(`@${d.pr.author?.login ?? "?"}`)).child(div().flex_1())
          .when(long, (el) => el.child(this.link(t, "desc-more", open ? "Less" : "More", (cx) => { this.descOpen = !open; cx.notify(); }))))
        .child(div().w_full().px(10).py(8).text_size(12).child(TextView.markdown(`md-desc-${open ? 1 : 0}`, text).selectable(true))));
    }
    // open threads by file, then resolved (folded to one line each)
    const open = d.threads.filter((x) => !x.resolved), done = d.threads.filter((x) => x.resolved);
    body.child(this.bar(t, "Open threads", open.length));
    if (!open.length) body.child(div().pt(6).text_size(11).text_color(t.dim).child(d.threads.length ? "All threads are resolved." : "No review threads."));
    let file = null;
    for (const th of open) {
      if (th.root.path !== file) {
        file = th.root.path;
        const parts = String(file).split("/"), name = parts.pop();
        body.child(h_flex().w_full().mt(10).items_center().text_size(10.5)
          .child(div().text_color(t.dim).child(parts.length ? parts.join("/") + "/" : "")).child(div().font_semibold().text_color(t.foreground).child(name)));
      }
      body.child(this.thread(t, th, mono));
    }
    if (done.length) {
      const show = this.showResolved;
      body.child(h_flex().id("resolved").w_full().cursor_pointer().on_click((_e, cx) => { this.showResolved = !show; cx.notify(); })
        .child(div().flex_1().child(this.bar(t, `Resolved threads ${show ? "▾" : "▸"}`, done.length))));
      if (show) for (const th of done) body.child(this.thread(t, th, mono));
    }
    // reviews and conversation comments, oldest first
    const items = [...d.reviews.map((rv) => ({ at: rv.submitted_at, rv })), ...d.issue.map((c) => ({ at: c.created_at, c }))].sort((a, b) => String(a.at).localeCompare(String(b.at)));
    body.child(h_flex().w_full().items_center().child(div().flex_1().child(this.bar(t, "Activity", items.length))));
    for (const it of items) body.child(v_flex().w_full().mt(6).px(10).py(8).border_1().border_color(t.border).bg(t.chrome)
      .child(it.rv ? this.comment(t, "review-body", { ...it.rv, created_at: it.rv.submitted_at, reactions: null }, "", it.rv.state) : this.comment(t, "issue", it.c, "")));
    const commenting = this.replyTo?.kind === "pr";
    body.child(div().id("rp-pr").mt(8).h(28).px(10).flex().items_center().border_1().border_color(commenting ? t.accent : t.border).bg(t.background)
      .text_size(11.5).text_color(commenting ? t.accent : t.dim).cursor_pointer().hover((s) => s.border_color(t.accent))
      .on_click((_e, cx) => { this.lineTo = null; this.replyTo = commenting ? null : { kind: "pr", id: d.pr.number, label: `PR #${d.pr.number}` }; cx.notify(); })
      .child(commenting ? "Writing below…" : "Leave a comment…"));
  }

  // Fixed above the scroll: the PR, its status, the review actions and the tabs.
  header(t, d, view) {
    const picked = location().picked;
    const top = h_flex().id("pr-head").h(38).flex_none().px(10).gap(8).items_center()
      .when(Boolean(picked), (el) => el.child(div().id("pr-back").h(20).px(6).flex().items_center().border_1().border_color(t.border)
        .text_size(10).font_semibold().text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.accent))
        .on_click((_e, cx) => { const from = shared("pr.target")?.from; setShared("pr.target", null); if (from === "reviews") run("layout.select", { stack: "panel-body", child: "reviews" }); cx.notify(); })
        .child("‹")))
      .when(!d, (el) => el.child(div().flex_1().font_semibold().child(caps("Pull request"))))
      .when(Boolean(d), (el) => el
        .child(div().id("pr-link").h(22).px(7).flex().items_center().flex_none().bg(t.accent).text_color(t.accent_foreground).text_size(11).font_semibold()
          .cursor_pointer().on_click(() => exec(OPENER(), [d.pr.url]).catch(() => {})).child(`#${d.pr.number}`))
        .child(div().flex_1().min_w(0).truncate().text_size(12.5).font_semibold().text_color(t.foreground).child(d.pr.title)))
      .child(div().id("pr-refresh").h(22).w(26).flex().items_center().justify_center().flex_none().border_1().border_color(t.border)
        .text_size(12).text_color(this.loading ? t.accent : t.dim).cursor_pointer().hover((s) => s.text_color(t.accent).border_color(t.accent))
        .on_click((_e, cx) => { this.refresh(cx); cx.notify(); }).child(this.loading ? "…" : "↻"));
    const box = v_flex().w_full().flex_none().border_b(1).border_color(t.border).bg(t.chrome).child(top);
    if (!d) return box;
    const checks = d.pr.statusCheckRollup ?? [];
    const kinds = checks.map((c) => this.checkTone(t, c)[0]);
    const bad = kinds.filter((k) => k === "fail").length, pending = kinds.filter((k) => k === "run").length;
    const cell = (id, label, col, fn) => div().id(id).h(18).px(6).flex().items_center().flex_none().border_1().border_color(col).text_size(10).font_semibold().text_color(col)
      .when(Boolean(fn), (e) => e.cursor_pointer().hover((s) => s.bg(t.raised)).on_click((_e, cx) => fn(cx))).child(label);
    const decision = String(d.pr.reviewDecision || "").replace(/_/g, " ");
    const state = d.pr.isDraft ? "DRAFT" : d.pr.state;
    box.child(h_flex().w_full().px(10).pb(6).gap(4).items_center()
      .child(cell("st", state, d.pr.state === "OPEN" && !d.pr.isDraft ? t.done : d.pr.state === "MERGED" ? t.accent : t.dim))
      .when(Boolean(decision), (el) => el.child(cell("dc", decision, /CHANGES/.test(decision) ? t.blocked : /APPROVED/.test(decision) ? t.done : t.working)))
      .when(checks.length > 0, (el) => el.child(cell("ck", bad ? `${bad} FAILING` : pending ? `${pending} RUNNING` : `CHECKS OK`, bad ? t.blocked : pending ? t.working : t.done,
        (cx) => { this.view = "checks"; cx.notify(); })))
      .child(div().flex_1().min_w(0).truncate().text_size(10).text_color(t.dim).text_right()
        .child(`@${d.pr.author?.login ?? "?"} · ${d.pr.headRefName} → ${d.pr.baseRefName}`))
      .child(div().flex_none().text_size(10).text_color(t.done).child(`+${d.pr.additions}`))
      .child(div().flex_none().text_size(10).text_color(t.blocked).child(`−${d.pr.deletions}`)));
    const drafts = (this.drafts ?? []).length;
    const tab = (id, label, n, tone) => div().id(`tab-${id}`).h(24).px(10).flex().items_center().gap(5).text_size(10).font_semibold().cursor_pointer()
      .border_b(2).border_color(view === id ? t.accent : t.chrome).text_color(view === id ? t.foreground : t.dim).hover((s) => s.text_color(t.foreground))
      .on_click((_e, cx) => { this.view = id; cx.notify(); })
      .child(caps(label)).when(n != null && n !== "", (e) => e.child(div().px(4).bg(tone ?? t.raised).text_color(tone ? t.background : t.muted).child(String(n))));
    const openThreads = d.threads.filter((x) => !x.resolved).length;
    box.child(h_flex().w_full().px(4).gap(0).items_end()
      .child(tab("conversation", "Conversation", openThreads || null, openThreads ? t.accent : null))
      .child(tab("files", "Files", d.pr.changedFiles ?? d.files?.length ?? ""))
      .child(tab("checks", "Checks", checks.length || null, bad ? t.blocked : null))
      .child(div().flex_1())
      .when(d.pr.state === "OPEN", (el) => el.child(h_flex().pb(3).pr(6).gap(4).items_center()
        .when(drafts > 0, (e) => e.child(div().px(4).text_size(10).text_color(t.accent).child(`${drafts} draft${drafts > 1 ? "s" : ""}`)))
        .child(this.reviewBtn(t, "COMMENT", drafts ? "Submit" : "Comment", t.muted, d))
        .child(this.reviewBtn(t, "REQUEST_CHANGES", "Changes", t.blocked, d))
        .child(this.reviewBtn(t, "APPROVE", "Approve", t.done, d)))));
    return box;
  }

  // A module bar: label block, count, rule.
  bar(t, label, n) {
    return h_flex().w_full().h(22).mt(10).flex_none().items_center().border_b(1).border_color(t.border)
      .child(div().h_full().px(8).flex().items_center().bg(t.raised).text_size(10).font_semibold().text_color(t.foreground).child(caps(label)))
      .child(div().px(8).text_size(10).text_color(t.dim).child(String(n)));
  }

  // The code a thread is on: its last lines, with the new-file line numbers in a gutter.
  code(t, hunk, mono) {
    const lines = hunk.split("\n"), head = /^@@ -\d+(?:,\d+)? \+(\d+)/.exec(lines[0] ?? "");
    let n = head ? Number(head[1]) : null;
    const rows = [];
    for (const l of lines.slice(head ? 1 : 0)) {
      const k = l[0], num = k === "-" ? "" : n;
      if (k !== "-" && n != null) n++;
      rows.push({ k, num, text: l.slice(1) });
    }
    return v_flex().w_full().bg(t.background).border_l(2).border_color(t.border).font_family(mono).text_size(10.5)
      .children(rows.slice(-5).map((r) => h_flex().w_full().h(16)
        .when(r.k === "+", (e) => e.bg(alpha(t.done ?? "#5fd38a", "14"))).when(r.k === "-", (e) => e.bg(alpha(t.blocked ?? "#ff5a4a", "14")))
        .child(div().w(36).flex_none().pr(6).text_right().text_color(t.dim).child(r.num == null ? "" : String(r.num)))
        .child(div().w(12).flex_none().text_color(r.k === "+" ? t.done : r.k === "-" ? t.blocked : t.dim).child(r.k === "+" || r.k === "-" ? r.k : ""))
        .child(div().flex_1().min_w(0).truncate().text_color(t.foreground).child(r.text || " "))));
  }

  render(cx) {
    if (key() !== this.ctx) this.refresh(cx);
    const t = theme(), d = this.data, mono = settings().font_family ?? "JetBrains Mono";
    const view = this.view ?? "conversation";
    const body = v_flex().id(`pr-body-${view}`).flex_1().min_h(0).overflow_y_scrollbar().px(10).pb(16);
    if (!d) body.child(v_flex().pt(12).gap(4).text_size(12)
      .child(div().text_color(this.error && !/no pull request|not a git/i.test(this.error) ? t.blocked : t.dim).child(this.error || (this.loading ? `Loading ${this.step ?? "pull request"}…` : "No folder")))
      .when(Boolean(this.where), (el) => el.child(div().text_size(10.5).text_color(t.dim).child(String(this.where).replace(/^\/home\/[^/]+/, "~")))));
    else if (view === "files" && !d.files) {
      if (!this.filesLoading) this.loadFiles(cx);
      body.child(div().pt(12).text_size(12).text_color(t.dim).child(`Loading ${d.pr.changedFiles ?? ""} files…`));
    } else if (view === "files") {
      try { this.filesView(t, body, d, mono); }
      catch (e) { body.child(div().pt(12).text_size(11).text_color(t.blocked).child(`Files view failed: ${e?.message ?? e} ${String(e?.stack ?? "").split("\n").slice(0, 3).join(" | ")}`)); }
    } else if (view === "checks") this.checksView(t, body, d);
    else this.conversationView(t, body, d, mono);
    return v_flex().id("pr").size_full().min_h(0).overflow_hidden().bg(t.surface).border_1().border_color(t.border).text_color(t.foreground).text_size(13)
      .child(this.header(t, d, view))
      .when(Boolean(this.note), (el) => el.child(h_flex().flex_none().px(12).py(4).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().flex_1().min_w(0).text_size(11).text_color(/^(Not |Review not|Reply not|Refresh failed|Files didn)/.test(this.note) ? t.blocked : t.accent).child(this.note))
        .child(this.link(t, "note-x", "×", (cx) => { this.note = null; cx.notify(); }))))
      .child(body)
      // a line comment for the review (files view): kept as a draft until SUBMIT REVIEW
      .when(Boolean(this.lineTo && d), (el) => el.child(v_flex().flex_none().border_t(1).border_color(t.accent)
        .child(h_flex().w_full().h(22).items_center()
          .child(div().h_full().px(8).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(10).font_semibold().child(caps("Comment")))
          .child(div().flex_1().min_w(0).px(8).truncate().text_size(10.5).text_color(t.dim).child(this.lineTo.label))
          .child(this.link(t, "lt-cancel", "Cancel", (cx) => { this.lineTo = null; cx.notify(); })))
        .child(div().id("lt-box").mx(10).mt(6).min_h(64).max_h(200).p(8).border_1().border_color(t.border).bg(t.background)
          .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).child(Textarea.new(this.box).w_full().text_size(12).text_color(t.foreground)))
        .child(h_flex().px(10).py(6).gap(8).items_center()
          .child(div().flex_1().text_size(10).text_color(t.dim).child("Kept as a draft; SUBMIT REVIEW posts all drafts as one review (comment only)."))
          .child(div().id("lt-add").h(22).px(12).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(10.5).font_semibold()
            .cursor_pointer().on_click((_e, cx) => { this.addDraft(); cx.notify(); }).child(caps("Add to review"))))))
      .when(Boolean(this.replyTo && d), (el) => el.child(v_flex().flex_none().border_t(1).border_color(t.accent)
        .child(h_flex().w_full().h(22).items_center()
          .child(div().h_full().px(8).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(10).font_semibold()
            .child(caps(this.replyTo.kind === "thread" ? "Reply" : this.replyTo.kind === "review" ? this.replyTo.title : "Comment")))
          .child(div().flex_1().min_w(0).px(8).truncate().text_size(10.5).text_color(t.dim).child(this.replyTo.label))
          .child(this.link(t, "rp-cancel", "Cancel", (cx) => { this.replyTo = null; cx.notify(); })))
        .child(div().id("rp-box").mx(10).mt(6).min_h(64).max_h(200).p(8).border_1().border_color(t.border).bg(t.background)
          .on_mouse_down("left", (_e, cx) => cx.stop_propagation()).child(Textarea.new(this.box).w_full().text_size(12).text_color(t.foreground)))
        .child(h_flex().px(10).py(6).gap(8).items_center()
          .child(div().flex_1().text_size(10).text_color(t.dim).child(this.replyTo.kind === "review"
            ? (this.replyTo.event === "APPROVE" ? "Summary optional. Line drafts are included." : "Summary of what to change. Line drafts are included.")
            : "Posted to GitHub as you, only when you press Post."))
          .child(div().id("rp-post").h(22).px(12).flex().items_center().bg(t.accent).text_color(t.accent_foreground).text_size(10.5).font_semibold()
            .cursor_pointer().on_click((_e, cx) => this.post(cx)).child(this.posting ? caps("Posting…") : caps(this.replyTo.kind === "review" ? (this.replyTo.title) : "Post"))))));
  }
}
