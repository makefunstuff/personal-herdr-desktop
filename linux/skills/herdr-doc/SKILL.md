---
name: herdr-doc
description: Write investigation drafts, incident notes, runbooks and documentation as herdr.doc/2 documents that Herdr Desktop renders natively (Docs panel). Use instead of HTML artifacts or Markdown files whenever the user asks for an investigation write-up, a draft, a report, notes or documentation, or when an on-duty investigation needs writing down.
---

# herdr-doc

Documents for the person to read in Herdr: one JSON file in `~/.herdr/docs/`, drawn natively in Herdr's theme by
the Docs panel (and in a pane), re-rendered as the file changes. Never write these into a repository, and don't make
HTML artifacts or Markdown files for this instead.

1. `herdr-doc guide` once: the format (block types and fields).
2. `herdr-doc new "<title>" --kind investigation|incident|runbook|design|notes|report`: prints the file's path.
3. Write the whole JSON to that path as you go (status `work`), updating `updated`. Lead with the answer (a `callout` or
   `stats`), then evidence: `timeline`, one `finding` per cause with real `evidence` (the command you ran and its actual
   output, trimmed; never invented), `todo` for follow-ups.
4. `herdr-doc check <file>` must say ok. Then set `status` (`ready`, or `need` when it waits on the person).
5. Tell the person the title and that it's in the Docs panel; `herdr-doc open <file>` shows it beside their pane.
   `herdr-doc export <file>` gives Markdown when they want to paste it somewhere.

No secrets, tokens or personal data in a document.
