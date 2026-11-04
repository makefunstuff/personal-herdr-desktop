// Reviews: the PR reviewer cockpit. Pull requests waiting for my review, assigned to me, and my own open ones, from one
// GitHub GraphQL search (gh, the person's login), every 3 minutes while in the layout. Each row: repo, number, title,
// author, age, size, checks, unresolved threads, and my last review. Click: the PR in the PR panel, in place (mods/px-pr:
// threads, files, reactions, replies). REVIEW: an agent in this space reads the diff and drafts review comments, posting nothing.
// A new review request posts a toast. Shares "reviews.summary" for the status bar.
import * as fs from "fs/promises";
import { View, div } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, theme, shared, setShared, run, spaces, focused, agents, hc, notify, playSound, home } from "herdr";

const EVERY = 180000;
const caps = (s) => (shared("layout") === "workbench" ? String(s).toUpperCase() : String(s));
const ago = (iso) => { const m = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60000)); return m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h` : `${Math.floor(m / 1440)}d`; };
const FRAG = `fragment pr on PullRequest {
  number title url isDraft createdAt updatedAt additions deletions changedFiles reviewDecision
  repository { nameWithOwner } author { login } headRefName
  comments { totalCount } reviewThreads(first: 100) { nodes { isResolved } }
  latestReviews(first: 10) { nodes { author { login } state submittedAt } }
  commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } }`;
const QUERY = `query {
  viewer { login }
  requested: search(query: "is:pr is:open archived:false review-requested:@me", type: ISSUE, first: 50) { nodes { ...pr } }
  assigned: search(query: "is:pr is:open archived:false assignee:@me -author:@me", type: ISSUE, first: 30) { nodes { ...pr } }
  reviewed: search(query: "is:pr is:open archived:false reviewed-by:@me -author:@me", type: ISSUE, first: 30) { nodes { ...pr } }
  mine: search(query: "is:pr is:open archived:false author:@me", type: ISSUE, first: 30) { nodes { ...pr } } }
${FRAG}`;

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

export default class Reviews extends View {
  init(_p, cx) {
    this.data = null; this.error = null; this.loading = false; this.note = null; this.open = {}; this.seen = null;
    this.refresh(cx);
    cx.timer.every(EVERY, (cx) => this.refresh(cx));
  }

  refresh(cx) {
    if (this.loading) return;
    this.loading = true; cx.notify();
    cx.spawn(async (cx) => {
      try {
        const r = await exec("env", ["GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "timeout", "40", "gh", "api", "graphql", "-f", `query=${QUERY}`]);
        if (r.code !== 0) throw new Error(String(r.stderr || r.stdout || "gh failed").trim().split("\n").pop());
        const j = JSON.parse(String(r.stdout));
        if (j.errors?.length) throw new Error(j.errors[0].message);
        const me = j.data.viewer.login;
        const shape = (n) => {
          const mineR = (n.latestReviews?.nodes ?? []).find((x) => x.author?.login === me);
          const unresolved = (n.reviewThreads?.nodes ?? []).filter((x) => !x.isResolved).length;
          const checks = n.commits?.nodes?.[0]?.commit?.statusCheckRollup?.state ?? null;
          return { ...n, repo: n.repository.nameWithOwner, author: n.author?.login ?? "?", unresolved, checks, myReview: mineR?.state ?? null, myReviewAt: mineR?.submittedAt ?? null,
            reviewers: (n.latestReviews?.nodes ?? []).filter((x) => x.author?.login !== n.author?.login).map((x) => ({ who: x.author?.login, state: x.state })) };
        };
        const list = (k) => (j.data[k]?.nodes ?? []).filter((n) => n && n.number).map(shape);
        const requested = list("requested"), seenNums = new Set(requested.map((p) => p.url));
        const assigned = list("assigned").filter((p) => !seenNums.has(p.url));
        const reviewed = list("reviewed").filter((p) => !seenNums.has(p.url) && !assigned.some((a) => a.url === p.url));
        this.data = { me, requested, assigned, reviewed, mine: list("mine"), at: Date.now() };
        this.error = null;
        const urls = new Set(requested.map((p) => p.url));
        if (this.seen) {
          const fresh = requested.filter((p) => !this.seen.has(p.url));
          if (fresh.length) {
            playSound("done");
            notify({ title: "Review requested", body: fresh.length > 1 ? `${fresh[0].title} and ${fresh.length - 1} more` : `${fresh[0].repo.split("/")[1]} #${fresh[0].number}: ${fresh[0].title}`,
              timeout: 10000, action: { name: "layout.show", args: { id: "panel" } } });
          }
        }
        this.seen = urls;
        setShared("reviews.summary", { requested: requested.length, assigned: assigned.length, at: Date.now() });
      } catch (e) { this.error = String(e?.message ?? e); }
      finally { this.loading = false; cx.notify(); }
    });
  }

  agent() {
    const space = spaces().find((s) => s.active), tab = focused()?.tab;
    return agents().filter((a) => a.space === space?.id).sort((a, b) => (b.tab === tab) - (a.tab === tab) || a.tab_index - b.tab_index)[0] ?? null;
  }

  review(p, cx) {
    const text = `Help me review PR ${p.url} (${p.repo} #${p.number}: ${p.title}, by @${p.author}, +${p.additions} −${p.deletions} in ${p.changedFiles} files). ` +
      `Read it with gh (gh pr view ${p.number} --repo ${p.repo} --comments; gh pr diff ${p.number} --repo ${p.repo}). Give me: what it changes and why, the risky parts, ` +
      `and draft review comments as file:line + comment that I can post myself. Don't post anything on GitHub, don't approve or request changes, and don't check out or change code.`;
    this.note = `Sending ${p.repo.split("/")[1]} #${p.number}…`; cx.notify();
    cx.spawn(async (cx) => {
      try { this.note = `${await toWork({ repo: p.repo, branch: p.headRefName, title: p.title, mine: false }, text)} Reviewing ${p.repo.split("/")[1]} #${p.number}.`; }
      catch (e) { this.note = `Not sent: ${e.message ?? e}`; }
      cx.notify();
    });
  }

  // In place: the PR panel (same side panel) shows this PR, with a way back here.
  openPr(p) {
    setShared("pr.target", { repo: p.repo, number: p.number, from: "reviews", at: Date.now() });
    run("layout.select", { stack: "panel-body", child: "pr" });
    run("layout.show", { id: "panel" });
  }

  // ---- pieces
  link(t, id, label, fn, solid = false) {
    return div().id(id).h(18).px(6).flex().items_center().flex_none().text_size(10).font_semibold()
      .when(solid, (e) => e.bg(t.accent).text_color(t.accent_foreground)).when(!solid, (e) => e.text_color(t.dim).hover((s) => s.text_color(t.accent)))
      .cursor_pointer().on_mouse_down("left", (_e, cx) => cx.stop_propagation()).on_click((_e, cx) => { cx.stop_propagation(); fn(cx); }).child(caps(label));
  }
  bar(t, id, label, n, col) {
    const open = this.open[id] !== false;
    return h_flex().id(`bar-${id}`).w_full().h(22).mt(10).flex_none().items_center().border_b(1).border_color(t.border).cursor_pointer()
      .on_click((_e, cx) => { this.open[id] = !open; cx.notify(); })
      .child(div().h_full().px(8).flex().items_center().bg(t.raised).text_size(10).font_semibold().text_color(t.foreground).child(caps(label)))
      .child(div().px(8).text_size(10).font_semibold().text_color(col ?? t.dim).child(String(n)))
      .child(div().flex_1()).child(div().px(6).text_size(10).text_color(t.dim).child(open ? "▾" : "▸"));
  }

  row(t, p, kind) {
    const ck = p.checks === "SUCCESS" ? [t.done, "✓"] : p.checks === "FAILURE" || p.checks === "ERROR" ? [t.blocked, "✗"] : p.checks ? [t.working, "…"] : [t.dim, "–"];
    const size = p.additions + p.deletions, sizeCol = size > 800 ? t.blocked : size > 250 ? t.working : t.dim;
    const mine = p.myReview ? ({ APPROVED: ["APPROVED", t.done], CHANGES_REQUESTED: ["CHANGES", t.blocked], COMMENTED: ["COMMENTED", t.muted] }[p.myReview] ?? [p.myReview, t.dim]) : null;
    const decision = p.reviewDecision === "APPROVED" ? ["APPROVED", t.done] : p.reviewDecision === "CHANGES_REQUESTED" ? ["CHANGES REQ", t.blocked] : null;
    return v_flex().id(`pr-${kind}-${p.url}`).w_full().mt(4).border_1().border_color(t.border).bg(t.chrome).cursor_pointer().hover((s) => s.border_color(t.accent))
      .on_click(() => this.openPr(p))
      .child(h_flex().w_full().h(22).items_center().border_b(1).border_color(t.border)
        .child(div().h_full().px(6).flex().items_center().bg(t.raised).text_size(10).font_semibold().text_color(t.foreground).child(caps(p.repo.split("/")[1])))
        .child(div().px(6).text_size(11).font_semibold().text_color(t.accent).child(`#${p.number}`))
        .child(div().flex_1().min_w(0).truncate().text_size(12).text_color(t.foreground).child(p.title))
        .when(p.isDraft, (e) => e.child(div().mx(4).px(4).border_1().border_color(t.border).text_size(9.5).text_color(t.dim).child("DRAFT"))))
      .child(h_flex().w_full().px(6).py(3).gap(8).items_center().text_size(10.5).text_color(t.dim)
        .child(div().child(kind === "mine" ? "you" : `@${p.author}`))
        .child(div().child(`${ago(p.createdAt)} old`))
        .child(div().text_color(sizeCol).child(`+${p.additions} −${p.deletions}`))
        .child(div().text_color(ck[0]).font_semibold().child(`checks ${ck[1]}`))
        .when(p.unresolved > 0, (e) => e.child(div().text_color(t.working).child(`${p.unresolved} open threads`)))
        .when(Boolean(decision), (e) => e.child(div().text_color(decision[1]).font_semibold().child(decision[0])))
        .when(Boolean(mine) && kind !== "mine", (e) => e.child(div().text_color(mine[1]).child(`you: ${mine[0]}`)))
        .when(kind === "mine" && p.reviewers.length > 0, (e) => e.child(div().min_w(0).truncate().child(p.reviewers.map((r) => `${r.who} ${r.state === "APPROVED" ? "✓" : r.state === "CHANGES_REQUESTED" ? "✗" : "·"}`).join("  "))))
        .child(div().flex_1())
        .when(kind !== "mine", (e) => e.child(this.link(t, `rv-${p.url}`, "Review", (cx) => this.review(p, cx), true)))
        .child(this.link(t, `op-${p.url}`, "GitHub", () => exec("xdg-open", [p.url]).catch(() => {}))));
  }

  render(cx) {
    const t = theme(), d = this.data;
    const body = v_flex().id("reviews-body").flex_1().min_h(0).overflow_y_scrollbar().px(10).pb(12);
    if (!d) body.child(div().pt(12).text_size(12).text_color(this.error ? t.blocked : t.dim).child(this.error || "Asking GitHub…"));
    else {
      const sections = [["requested", "Waiting for my review", d.requested, d.requested.length ? t.accent : t.done],
        ["assigned", "Assigned to me", d.assigned, t.muted], ["reviewed", "I reviewed, still open", d.reviewed, t.muted], ["mine", "My pull requests", d.mine, t.muted]];
      for (const [id, label, list, col] of sections) {
        if (!list.length && id !== "requested") continue;
        body.child(this.bar(t, id, label, list.length, col));
        if (this.open[id] === false) continue;
        if (!list.length) body.child(div().mt(6).text_size(11).text_color(t.dim).child("Nothing waits for your review."));
        // oldest waiting first: what has waited longest is reviewed next
        for (const p of [...list].sort((a, b) => (id === "mine" ? (b.updatedAt > a.updatedAt ? 1 : -1) : (a.createdAt > b.createdAt ? 1 : -1)))) body.child(this.row(t, p, id));
      }
    }
    return v_flex().id("reviews").size_full().min_h(0).overflow_hidden().bg(t.surface).border_1().border_color(t.border).text_color(t.foreground).text_size(13)
      .child(h_flex().h(42).flex_none().px(14).gap(8).items_center().border_b(1).border_color(t.border)
        .child(div().font_semibold().child(caps("Reviews")))
        .when(Boolean(d), (e) => e.child(div().px(5).py(1).border_1().border_color(d.requested.length ? t.accent : t.border).text_size(10)
          .text_color(d.requested.length ? t.accent : t.muted).child(`${d.requested.length} waiting`)))
        .child(div().flex_1())
        .child(div().id("rv-refresh").text_size(10).text_color(t.dim).cursor_pointer().hover((s) => s.text_color(t.foreground)).on_click((_e, cx) => this.refresh(cx))
          .child(this.loading ? caps("refreshing…") : d ? caps(`${ago(new Date(d.at).toISOString())} ago · refresh`) : caps("refresh"))))
      .when(Boolean(this.note), (el) => el.child(div().px(12).py(4).text_size(11).text_color(t.accent).child(this.note)))
      .child(body);
  }
}
