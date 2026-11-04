# herdr.doc/2

A document Herdr draws natively, in the shown layout's theme: investigation drafts, incident notes, runbooks,
tedious documentation. One JSON file, `~/.herdr/docs/<slug>.hdoc.json`. Agents write the JSON; Herdr renders it
(mods/px-doc) and re-renders as the file changes, so a draft fills in while you watch. `herdr-doc export` turns it into
Markdown for Confluence, GitHub or a ticket. herdr.doc/1 documents open too (every /1 block is a /2 block).

    herdr-doc new "Pod scheduling failures in shop-prod" --kind investigation   # prints the file's path
    herdr-doc open <file>                                                          # shows it in Herdr
    herdr-doc export <file> [--out file.md]                                        # Markdown
    herdr-doc guide                                                                # this file

## Document

```json
{
  "format": "herdr.doc/2",
  "title": "Pod scheduling failures in shop-prod",
  "subtitle": "on duty · 4 Nov 2026",
  "kind": "investigation",
  "status": "work",
  "tags": ["shop-prod", "kubernetes"],
  "updated": "2026-11-04T14:05:00Z",
  "blocks": [ … ]
}
```

- `kind` (optional): `investigation`, `incident`, `runbook`, `design`, `notes`, `report`. Only a label.
- `status` (optional): `work` (amber), `need` (red: waits on a person), `ready` (green), `done`, `fail`, `info`.
- Text fields are plain text unless the block says markdown (`md`, `callout`, `finding.detail`, `section.intro`).

## Blocks

| type | fields | shows |
|---|---|---|
| `heading` | `text`, `level?` (1–3) | a heading |
| `text` | `text` | a plain paragraph (newlines kept) |
| `md` | `text` | Markdown prose: lists, emphasis, links, inline code, tables |
| `section` | `title`, `blocks: [...]`, `collapsed?`, `intro?` (md) | a titled group that folds |
| `columns` | `columns: [[blocks...], [blocks...]]` (2–3) | blocks side by side |
| `callout` | `text` (md), `tone?`: info, ok, warn, bad, `title?` | a highlighted note |
| `quote` | `text`, `who?`, `where?` | a quote (a Slack message, a log line, a review comment) |
| `stats` | `items: [{label, value, tone?, note?}]` | big numbers in a row; tone: ok, warn, bad, info |
| `kv` | `items: [[key, value], ...]` | key/value lines |
| `list` | `items: [string]`, `style?`: bullet, number | a list |
| `todo` | `items: [{text, done?, who?}]` | a checklist |
| `checks` | `items: [{text, ok: true\|false\|null, note?}]` | pass / fail / not run lines |
| `steps` | `items: [{text, state: done\|now\|todo\|skip, note?}]` | a plan or progress |
| `timeline` | `items: [{time, text, tone?, who?}]` | an incident timeline, in order |
| `finding` | `title`, `severity`: low, medium, high, critical, `detail?` (md), `evidence?: [blocks]`, `fix?` | a finding with its evidence |
| `evidence` | `command`, `output`, `where?` (project, cluster, host) | a command and what it printed |
| `table` | `columns: [string]`, `rows: [[string\|number]]`, `align?: [left\|right]` | a table (≤ ~100 rows) |
| `bars` | `items: [{label, value, tone?}]`, `unit?`, `max?` | a horizontal bar chart |
| `progress` | `label`, `value`, `max`, `tone?` | one progress bar |
| `code` | `text`, `lang?`, `file?` | code, monospaced |
| `diff` | `text` (unified diff), `file?` | a diff, + and − coloured |
| `links` | `items: [{label, url}]` | links (open in the browser) |
| `image` | `path` (absolute, on this machine), `caption?`, `width?` | a picture (PNG, JPG, SVG) |
| `divider` | — | a rule |

Unknown block types are skipped, so newer documents still open. Keep a document under ~400 blocks and 2 MB.

## Writing one well

- Lead with what matters: a `callout` or `stats` with the answer, then the evidence.
- Investigations: `kv` (scope, when, who), `timeline`, one `finding` per cause with its `evidence`, `todo` for follow-ups.
- Evidence is real: paste the command you ran and its actual output, trimmed. Never invent output.
- Update the same file as you learn more (write the whole file; Herdr re-renders it). Set `status` and `updated`.
- No secrets, tokens or personal data in a document.

## Example

```json
{ "format": "herdr.doc/2", "title": "Pod scheduling failures in shop-prod", "kind": "investigation", "status": "work",
  "blocks": [
    { "type": "callout", "tone": "warn", "text": "**6 alerts** since 19:21: the `default` node pool is at max nodes." },
    { "type": "kv", "items": [["project", "shop-prod"], ["cluster", "shop-prod-main"], ["since", "2026-11-03 19:21 UTC"]] },
    { "type": "timeline", "items": [ {"time": "19:21", "text": "First FailedScheduling: Insufficient cpu", "tone": "bad"},
                                     {"time": "19:24", "text": "Autoscaler: max node group size reached"} ] },
    { "type": "finding", "title": "Autoscaler ceiling", "severity": "medium", "detail": "The pool's `maxNodeCount` is 15.",
      "evidence": [ { "type": "evidence", "command": "kubectl get nodes | wc -l", "output": "16", "where": "shop-prod" } ] },
    { "type": "todo", "items": [ {"text": "Raise maxNodeCount to 20", "who": "platform"} ] }
  ] }
```
