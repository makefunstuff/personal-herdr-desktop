# herdr.doc/1

A Shepherdr artifact whose data file is a `herdr.doc/1` document is drawn natively in Herdr's Shepherdr panel
(in the theme's own style) and by the stock renderer page in Shepherdr's web app and on a phone. Write only the JSON;
the HTML is always the stock renderer:

    shepherdr artifact new  ~/.config/herdr-app/shepherdr-doc/render.html --title "SHE-2: … · Proof" --data doc.json
    shepherdr artifact update <id> --data doc.json          # data only: open views redraw without a reload

## Document

```json
{
  "format": "herdr.doc/1",
  "title": "SEMA-3: Automatic notes · Proof",
  "subtitle": "branch work/3-automatic-notes · 14:02",
  "status": "ready",
  "blocks": [ … ]
}
```

`status` (optional): `ready` (green), `work` (amber), `need` (red: waits on the human), `done`, `fail`, `info`.
Keep text short and plain: no markdown, no HTML. Every string is shown as written.

## Blocks

| type | fields | shows |
|---|---|---|
| `heading` | `text` | a section heading |
| `text` | `text` | a short paragraph (newlines kept) |
| `stats` | `items: [{label, value, tone?, note?}]` | big numbers in a row; `tone`: ok, warn, bad, info |
| `kv` | `items: [[key, value], …]` | key/value lines (branch, PR, commit, versions) |
| `list` | `items: [string]`, `style?`: bullet, number | a list |
| `checks` | `items: [{text, ok: true\|false\|null, note?}]` | pass / fail / not run lines (tests, proof, review) |
| `steps` | `items: [{text, state: done\|now\|todo\|skip}]` | a plan or progress through steps |
| `table` | `columns: [string]`, `rows: [[string\|number]]`, `align?: [left\|right]` | a table (at most ~50 rows) |
| `bars` | `items: [{label, value, tone?}]`, `unit?`, `max?` | a horizontal bar chart |
| `progress` | `label`, `value`, `max`, `tone?` | one progress bar |
| `code` | `text`, `lang?`, `file?` | code, monospaced |
| `diff` | `text` (unified diff), `file?` | a diff, + and − lines coloured |
| `callout` | `text`, `tone?`: info, ok, warn, bad | a highlighted note |
| `links` | `items: [{label, url}]` | links (open in the browser) |
| `divider` | — | a rule |

Unknown block types are skipped, so newer documents still open. Keep a document under ~200 blocks and 1 MB.

## Example

```json
{ "format": "herdr.doc/1", "title": "SEMA-5: Plain-sentence file search · Proof", "status": "ready",
  "blocks": [
    { "type": "stats", "items": [ {"label": "tests", "value": "128", "tone": "ok"}, {"label": "p50", "value": "48 ms"} ] },
    { "type": "checks", "items": [ {"text": "unit tests", "ok": true}, {"text": "bench vs main", "ok": true, "note": "-12%"} ] },
    { "type": "kv", "items": [ ["branch", "work/5-plain-sentence"], ["PR", "#41"] ] },
    { "type": "diff", "file": "lua/semantic-grep/files.lua", "text": "@@ -10,3 +10,4 @@\n-local q = args\n+local q = sentence(args)\n" }
  ] }
```
