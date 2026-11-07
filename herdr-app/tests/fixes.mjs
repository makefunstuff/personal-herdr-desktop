// Regression checks for the PHD-5 fixes, against the real mod source.
// Run: node --experimental-vm-modules herdr-app/tests/fixes.mjs
import vm from "node:vm";
import fs from "node:fs/promises";
import assert from "node:assert/strict";

const source = async (rel) => fs.readFile(new URL(`../mods/${rel}`, import.meta.url), "utf8");

// A mod is an ES module with imports that only Herdr provides. Load the real
// source in a VM with a stub for each import it names.
async function load(rel, stubs = {}, named = null) {
  const text = (await source(rel)) + (named ? `\nexport { ${named} };\n` : "");
  const context = vm.createContext({
    console, Date, Math, JSON, Object, Array, Set, Map, Promise, String, Number, Error, RegExp, setTimeout,
    localStorage: { getItem: () => null, setItem: () => {} },
  });
  const mod = new vm.SourceTextModule(text, { context, identifier: rel });
  await mod.link((spec) => {
    if (stubs[spec]) return typeof stubs[spec] === "function" ? stubs[spec](context) : stubs[spec];
    const names = new Set();
    for (const m of text.matchAll(/import\s*\{([^}]*)\}\s*from\s*["']([^"']+)["']/g)) {
      if (m[2] !== spec) continue;
      for (const n of m[1].split(",")) names.add(n.trim().split(/\s+as\s+/).pop());
    }
    return new vm.SyntheticModule([...names], function () {
      for (const n of names) this.setExport(n, n === "View" ? class {} : () => undefined);
    }, { identifier: spec, context });
  });
  await mod.evaluate();
  return named ? mod.namespace : mod.namespace.default;
}

function herdr(overrides = {}) {
  return (context) => {
    const names = ["View", "div", "v_flex", "h_flex", "InputState", "Input", "Scrollbar", "appHome", "theme",
      "shared", "setShared", "layout", "playSound", "notify", "run", "props", "closeOverlay", "spaces", "panes", "exec", "home",
      "machines", "openOverlay", "watchFolder", "window", "readTextFile", "hc"];
    return new vm.SyntheticModule(names, function () {
      const base = {
        View: class {}, theme: () => ({}), shared: () => undefined, setShared: () => null,
        layout: () => ({}), playSound: () => null, notify: () => null, run: () => null,
        props: () => ({}), closeOverlay: () => null, spaces: () => [], panes: () => [], exec: async () => ({ code: 0, stdout: "", stderr: "" }), home: () => "/home/test",
        machines: () => [], openOverlay: () => "o", watchFolder: async () => null, window: () => ({}), readTextFile: async () => null, hc: async () => ({ code: 0, stdout: "", stderr: "" }),
      };
      for (const n of names) this.setExport(n, n in overrides ? overrides[n] : (base[n] ?? (() => undefined)));
    }, { identifier: "herdr", context });
  };
}

const files = new Map();
const fsStub = (context) => new vm.SyntheticModule(["readFile", "writeFile", "mkdir", "readdir"], function () {
  this.setExport("readFile", async (p) => { if (!files.has(p)) throw new Error("ENOENT"); return files.get(p); });
  this.setExport("writeFile", async (p, text) => { files.set(p, text); });
  this.setExport("mkdir", async () => null);
  this.setExport("readdir", async () => []);
}, { identifier: "fs/promises", context });


// Desk: a note typed while looking at a past day goes into that day.
{
  const Desk = await load("desk/main.js", { herdr: herdr({ appHome: () => "/app" }), "fs/promises": fsStub });
  const pad = (n) => String(n).padStart(2, "0");
  const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const yesterday = new Date(); yesterday.setDate(yesterday.getDate() - 1);
  const today = new Date();
  const desk = new Desk();
  desk.day = ymd(yesterday);
  desk.input = { value: () => "a note", set_value: () => {} };
  desk.loadNotes = async () => {};
  await desk.addNote();
  assert.match(files.get(`/app/desk-notes/daily/${ymd(yesterday)}.md`) ?? "", /a note/);
  assert.equal(files.has(`/app/desk-notes/daily/${ymd(today)}.md`), false);
}

// Worktree: removing a repo's main checkout must not close its space.
{
  const calls = [];
  const run = (name, args) => calls.push([name, args]);
  const Worktree = await load("px-worktree/main.js", {
    herdr: herdr({ props: () => ({ action: "remove" }), run }),
    "gpui-base": (context) => new vm.SyntheticModule(["v_flex", "h_flex", "InputState", "Input", "Scrollbar"], function () {
      for (const n of ["v_flex", "h_flex", "Input", "Scrollbar"]) this.setExport(n, () => undefined);
      this.setExport("InputState", { new: () => ({ on: () => {}, focus: () => {} }) });
    }, { identifier: "gpui-base", context }),
  });
  const w = new Worktree();
  w.repo = { linked: false, top: "/repo", main: "/repo", name: "repo" };
  const why = await w.remove();
  assert.match(why, /main checkout/);
  assert.deepEqual(calls.filter(([n]) => n === "space.close"), [], "main checkout's space must stay open");
}

// Sidebar: a task in "needs" shows its open ask, not an earlier answered one.
{
  const task = { id: "T-1", title: "a task", status: "needs", machine: null, worktree: "/repo/wt",
    asks: [{ text: "old question", answered: true }, { text: "new question", open: true }] };
  const space = { id: "sp1", name: "T-1 · a task", folder: "/repo/wt", machine: null, tabs: [] };
  const ns = await load("px-spaces/main.js", {
    herdr: herdr({ shared: (k) => k === "shp" ? { places: [{ name: null, tasks: [task] }] } : undefined, spaces: () => [space] }),
  }, "shpTasks");
  const out = ns.shpTasks([space]);
  assert.equal(out[space.id].doing, "new question");
}

console.log("fixes: ok");
