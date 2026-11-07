// Regression checks for the PHD-5 fixes, against the real mod source.
// Run: node --experimental-vm-modules herdr-app/tests/fixes.mjs
import vm from "node:vm";
import fs from "node:fs/promises";
import assert from "node:assert/strict";

const source = async (rel) => fs.readFile(new URL(`../mods/${rel}`, import.meta.url), "utf8");

// A mod is an ES module with imports that only Herdr provides. Load the real
// source in a VM with a stub for each import it names.
async function load(rel, stubs = {}) {
  const text = await source(rel);
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
  return mod.namespace.default;
}

function herdr(overrides = {}) {
  return (context) => {
    const names = ["View", "div", "v_flex", "h_flex", "InputState", "Input", "Scrollbar", "appHome", "theme",
      "shared", "setShared", "layout", "playSound", "notify", "run", "props", "closeOverlay", "spaces", "panes", "exec", "home"];
    return new vm.SyntheticModule(names, function () {
      const base = {
        View: class {}, theme: () => ({}), shared: () => undefined, setShared: () => null,
        layout: () => ({}), playSound: () => null, notify: () => null, run: () => null,
        props: () => ({}), closeOverlay: () => null, spaces: () => [], panes: () => [], exec: async () => ({ code: 0, stdout: "", stderr: "" }), home: () => "/home/test",
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
  const desk = new Desk();
  desk.day = "2026-11-06";
  desk.input = { value: () => "a note", set_value: () => {} };
  desk.loadNotes = async () => {};
  await desk.addNote();
  assert.match(files.get("/app/desk-notes/daily/2026-11-06.md") ?? "", /a note/);
  assert.equal(files.has("/app/desk-notes/daily/2026-11-07.md"), false);
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
  w.busy = false;
  let removed = false;
  w.remove = async () => { removed = true; return ""; };
  await w.work({ notify: () => {} });
  assert.equal(removed, false, "main checkout must not be removed");
  assert.deepEqual(calls.filter(([n]) => n === "space.close"), [], "main checkout's space must stay open");
  assert.match(String(w.error), /main checkout/);
}

// SYSTEM: the first Linux sample is already Linux, even before a CPU delta.
{
  const exec = async () => ({ code: 0, stdout: "cpu 1 2 3 4 5 6 7 8\nMemTotal: 1000\nMemAvailable: 500\ndf /dev/sda1 100 50 50 50% /\n", stderr: "" });
  const SysMon = await load("px-sysmon/main.js", { herdr: herdr({ exec }) });
  const mon = new SysMon();
  mon.prev = null;
  let done;
  const finished = new Promise((ok) => { done = ok; });
  const cx = { spawn: async (fn) => { await fn(cx); done(); }, notify: () => {} };
  mon.read(cx);
  await finished;
  assert.equal(mon.s.linux, true);
  assert.equal(mon.s.cpu, undefined);
}

console.log("fixes: ok");
