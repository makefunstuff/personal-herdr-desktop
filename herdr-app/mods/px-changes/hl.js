// A small per-line syntax highlighter for diff rows: keywords, strings, comments, numbers, types,
// function calls, by the file's extension. Line by line (a diff shows pieces of a file), with one
// carried state: inside a block comment or a multi-line string. Colours come from the theme.

const W = (s) => new Set(s.split(/\s+/).filter(Boolean));
const C_LIKE = "if else for while do switch case default break continue return goto try catch finally throw new delete typeof instanceof void in of";
const LANGS = {
  js: { kw: W(`${C_LIKE} var let const function class extends super this import export from as async await yield static get set with debugger`), lit: W("true false null undefined NaN Infinity"), line: ["//"], block: ["/*", "*/"], tpl: true },
  ts: { kw: W(`${C_LIKE} var let const function class extends super this import export from as async await yield static get set interface type enum implements namespace declare readonly private public protected abstract keyof infer is satisfies`), lit: W("true false null undefined"), line: ["//"], block: ["/*", "*/"], tpl: true },
  py: { kw: W("def class return if elif else for while break continue pass import from as with try except finally raise lambda yield global nonlocal assert del in is not and or async await match case"), lit: W("True False None self cls"), line: ["#"], triple: true },
  sh: { kw: W("if then else elif fi for while until do done case esac in function return local export readonly set unset shift exit break continue"), lit: W("true false"), line: ["#"], vars: true },
  go: { kw: W("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var"), lit: W("true false nil iota"), line: ["//"], block: ["/*", "*/"], tpl: true },
  rs: { kw: W("as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while"), lit: W("true false None Some Ok Err"), line: ["//"], block: ["/*", "*/"] },
  lua: { kw: W("and break do else elseif end for function goto if in local not or repeat return then until while"), lit: W("true false nil self"), line: ["--"], block: ["--[[", "]]"] },
  c: { kw: W(`${C_LIKE} auto char const double enum extern float int long register short signed sizeof static struct typedef union unsigned volatile inline bool class namespace template typename public private protected virtual override using nullptr`), lit: W("true false NULL nullptr"), line: ["//"], block: ["/*", "*/"], pre: true },
  java: { kw: W(`${C_LIKE} abstract class extends implements interface enum package import public private protected static final void int long double float boolean char byte short this super synchronized val var fun object when data sealed override`), lit: W("true false null"), line: ["//"], block: ["/*", "*/"] },
  rb: { kw: W("def end class module if elsif else unless while until for in do return yield begin rescue ensure raise then case when require self nil"), lit: W("true false nil"), line: ["#"] },
  tf: { kw: W("resource data variable output locals module provider terraform for in if each count"), lit: W("true false null"), line: ["#", "//"], block: ["/*", "*/"] },
  yaml: { kw: W(""), lit: W("true false null yes no on off"), line: ["#"], keys: true },
  toml: { kw: W(""), lit: W("true false"), line: ["#"], keys: true },
  sql: { kw: W("select from where and or not insert into values update set delete create table index view drop alter add column join left right inner outer on group by order having limit offset as distinct union all case when then else end primary key references grant revoke begin commit rollback with returning"), lit: W("null true false"), line: ["--"], block: ["/*", "*/"], ci: true },
  css: { kw: W("@media @import @keyframes @font-face !important"), lit: W(""), block: ["/*", "*/"] },
  json: { kw: W(""), lit: W("true false null"), keys: true },
};
const EXT = { js: "js", mjs: "js", cjs: "js", jsx: "js", ts: "ts", tsx: "ts", mts: "ts", py: "py", sh: "sh", bash: "sh", zsh: "sh", fish: "sh",
  go: "go", rs: "rs", lua: "lua", c: "c", h: "c", cc: "c", cpp: "c", hpp: "c", java: "java", kt: "java", kts: "java", scala: "java", swift: "java",
  rb: "rb", tf: "tf", hcl: "tf", tfvars: "tf", yml: "yaml", yaml: "yaml", toml: "toml", ini: "toml", conf: "toml", sql: "sql",
  css: "css", scss: "css", less: "css", json: "json", jsonc: "json" };

export function langOf(path) {
  const name = String(path ?? "").split("/").pop() ?? "";
  if (/^(Dockerfile|Makefile|\.bashrc|\.zshrc|\.profile)$/.test(name) || /\.env/.test(name)) return LANGS.sh;
  const ext = name.includes(".") ? name.split(".").pop().toLowerCase() : "";
  return LANGS[EXT[ext]] ?? null;
}

const isId = (c) => /[A-Za-z0-9_$]/.test(c);

// One line -> [[text, kind]...], kind one of: kw lit str com num type fn key var pre null (plain).
// `state` is carried between lines: { block: true } inside a block comment, { str: '"""' } in one.
export function tokens(line, lang, state = {}) {
  const out = [];
  const push = (text, kind) => { if (!text) return; const last = out[out.length - 1]; if (last && last[1] === kind) last[0] += text; else out.push([text, kind]); };
  let i = 0;
  const n = line.length;
  if (state.block && lang.block) {
    const end = line.indexOf(lang.block[1]);
    if (end < 0) { push(line, "com"); return out; }
    push(line.slice(0, end + lang.block[1].length), "com"); i = end + lang.block[1].length; state.block = false;
  }
  if (state.str) {
    const end = line.indexOf(state.str);
    if (end < 0) { push(line, "str"); return out; }
    push(line.slice(0, end + state.str.length), "str"); i = end + state.str.length; state.str = null;
  }
  if (lang.pre && /^\s*#/.test(line)) { push(line, "pre"); return out; }
  if (lang.keys) {
    const m = /^(\s*-?\s*)("[^"]*"|[A-Za-z0-9_.\-/]+)(\s*[:=])/.exec(line.slice(i));
    if (m) { push(m[1], null); push(m[2], "key"); push(m[3], null); i += m[0].length; }
  }
  while (i < n) {
    const rest = line.slice(i), c = line[i];
    const lc = lang.line?.find((p) => rest.startsWith(p));
    if (lc && !(lc === "#" && lang.vars && line[i - 1] === "$")) { push(rest, "com"); break; }
    if (lang.block && rest.startsWith(lang.block[0])) {
      const end = rest.indexOf(lang.block[1], lang.block[0].length);
      if (end < 0) { push(rest, "com"); state.block = true; break; }
      push(rest.slice(0, end + lang.block[1].length), "com"); i += end + lang.block[1].length; continue;
    }
    if (lang.triple && (rest.startsWith('"""') || rest.startsWith("'''"))) {
      const q = rest.slice(0, 3), end = rest.indexOf(q, 3);
      if (end < 0) { push(rest, "str"); state.str = q; break; }
      push(rest.slice(0, end + 3), "str"); i += end + 3; continue;
    }
    if (c === '"' || c === "'" || (c === "`" && lang.tpl)) {
      let j = i + 1;
      while (j < n && line[j] !== c) j += line[j] === "\\" ? 2 : 1;
      if (j >= n && c === "`") { push(rest, "str"); state.str = "`"; break; }
      push(line.slice(i, j + 1), "str"); i = j + 1; continue;
    }
    if (lang.vars && c === "$") {
      const m = /^\$(\{[^}]*\}|[A-Za-z_][A-Za-z0-9_]*|[0-9@#?*!$-])/.exec(rest);
      if (m) { push(m[0], "var"); i += m[0].length; continue; }
    }
    if (/[0-9]/.test(c) && !isId(line[i - 1] ?? " ")) {
      const m = /^(0x[0-9a-fA-F_]+|[0-9][0-9_]*(\.[0-9_]+)?([eE][+-]?[0-9]+)?[a-zA-Z]*)/.exec(rest);
      push(m[0], "num"); i += m[0].length; continue;
    }
    if (isId(c) || c === "@" || c === "!") {
      const m = /^[@!]?[A-Za-z0-9_$\-]*[A-Za-z0-9_$]|^[@!]?[A-Za-z_$]/.exec(rest);
      const w = m ? m[0] : c;
      // a dash only joins a word in languages whose words have dashes (css, yaml keys)
      const word = lang === LANGS.css ? w : w.split("-")[0] || w;
      const key = lang.ci ? word.toLowerCase() : word;
      const after = line.slice(i + word.length).trimStart()[0];
      const kind = lang.kw.has(key) ? "kw" : lang.lit.has(key) ? "lit" : /^[A-Z][a-z0-9]/.test(word) && lang !== LANGS.sql ? "type" : after === "(" ? "fn" : null;
      push(word, kind); i += word.length; continue;
    }
    push(c, null); i++;
  }
  return out;
}

export const colours = (t) => ({ kw: t.ansi5, lit: t.ansi6, str: t.ansi2, com: t.dim, num: t.ansi3, type: t.ansi3, fn: t.ansi4, key: t.ansi4, var: t.ansi6, pre: t.ansi5 });
