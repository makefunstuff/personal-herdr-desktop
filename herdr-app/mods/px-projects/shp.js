// Our projects manager's views over simple-herdr-projects (`shp`): grouping, row words, opening a
// task, running an shp command. The data is shared("shp") = { places: [{ name, connected, tasks,
// spaces, error, read, health }] }, which px-status's poller keeps (mods/px-status/poll.js).
// The grouping and row words follow shp's own board (mods/shp-board/board.js, MIT).

import { exec, shared, spaces, panes, run } from "herdr";

export const SECTIONS = [
  { key: "needs", label: "Needs you", color: "blocked", statuses: ["needs"] },
  { key: "working", label: "Working", color: "working", statuses: ["working", "merging"] },
  { key: "ready", label: "Ready", color: "done", statuses: ["ready"] },
  { key: "backlog", label: "Backlog", color: "muted", statuses: ["todo", "queued"] },
  { key: "done", label: "Done", color: "dim", statuses: ["done"] },
  { key: "other", label: "Other", color: "dim", statuses: [] },
];
const WORDS = { todo: "To do", queued: "Queued", merging: "Merging" };

export function group(tasks) {
  const sections = SECTIONS.map((s) => ({ ...s, tasks: [] }));
  const other = sections[sections.length - 1];
  for (const task of tasks) {
    const status = String(task.status ?? "todo");
    (sections.find((s) => s.statuses.includes(status)) ?? other).tasks.push(task);
  }
  return sections.filter((s) => s.tasks.length > 0);
}

// A row's right-hand words: a status its section doesn't say, how far, what's left.
export function detail(task) {
  const parts = [];
  const word = WORDS[task.status ?? "todo"];
  if (word) parts.push(word);
  if (task.progress != null) parts.push(`${task.progress}%`);
  if (task.left) parts.push(`${task.left} left`);
  if (task.pr) parts.push(`PR ${String(task.pr).replace(/^.*\/pull\//, "#")}`);
  return parts.join(" · ");
}

// A row's second line: the open question, else the worker's step.
export function subtitle(task) {
  const ask = openAsk(task);
  if (task.status === "needs" && ask?.text) return String(ask.text);
  return task.step ? String(task.step) : "";
}

export const openAsk = (task) => (task.asks ?? []).find((a) => !a.closed && !a.answered) ?? task.asks?.[0] ?? null;

// The task's Herdr space on its machine: the recorded workspace id wins (a remote one reads
// "machine/id"), else the space named "<ID> · <title>".
export function workspaceOf(task, all = spaces()) {
  const here = all.filter((s) => (s.machine ?? null) === (task.machine ?? null));
  const id = task.workspace;
  const byId = id ? here.find((s) => s.id === id || String(s.id).endsWith(`/${id}`)) : null;
  const tail = (path) => String(path ?? "").replace(/^~|^\/(Users|home)\/[^/]+/, "");
  return byId ?? here.find((s) => task.worktree && tail(s.folder) === tail(task.worktree))
    ?? here.find((s) => s.name === `${task.id} · ${task.title}`) ?? null;
}

// Open a task: its space, then its Chat tab (the worker).
export function openTask(task) {
  const space = workspaceOf(task);
  if (!space) return false;
  run("space.select", { id: space.id });
  const chat = (space.tabs ?? []).find((t) => t.name === "Chat");
  if (chat) run("tab.select", { id: chat.id });
  return true;
}

// Show the Monitor tab wherever it is open, else open it here.
export function openMonitor() {
  const mine = new Set(panes().filter((p) => p.mod === "px-projects").map((p) => p.tab));
  for (const s of spaces()) {
    const tab = (s.tabs ?? []).find((t) => mine.has(t.id) && t.name === "Monitor");
    if (tab) { if (!s.active) run("space.select", { id: s.id }); run("tab.select", { id: tab.id }); return; }
  }
  run("tab.new", { name: "Monitor", mod: "px-projects", props: { view: "monitor" } });
}

// Run an shp command on the task's machine; resolves to null, or the reason it refused.
export async function shp(machine, argv) {
  try {
    const r = await exec("shp", argv, machine ? { on: machine } : {});
    if (r.code === 0) return null;
    return lastLine(r.stderr) || lastLine(r.stdout) || `shp ${argv[0]} exited ${r.code}`;
  } catch (e) {
    return lastLine(e?.stderr || e?.message || e) || "shp can't be reached there";
  }
}

export const allTasks = () => (shared("shp")?.places ?? []).flatMap((p) => p.tasks);
const lastLine = (text) => String(text ?? "").trim().split("\n").pop();
