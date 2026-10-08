# 0047 — Two daily notes of one day in a folder of notes cannot be merged

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import of a notes folder: three days each had two notes (one in the month's folder, one
  in a week's folder), named `2031-04-11.md` and `2031-04-11 2.md` or `2031-04-11-Thursday.md`

## What happened

Once the model set both notes' titles to the day, the plan reported "another note has the same title: change one" on
each pair, and *apply a vault plan* refused. The owner's answer was to merge them — both texts on the day's page —
which is exactly what the writer does for a daily note whose day page already exists in the database (the plan marks it
`append`, [a folder of notes](../guides/importing.md#a-folder-of-notes)). But two notes of one day in the same plan
have no such path: the only fixes are a made-up title for one of them (a second page for the same day, against
[D5](../decisions/D05-pages-and-day-pages.md)) or an import in two runs.

Expected: a second daily note of one day in the plan is appended to the first's page, like a daily note of a day the
database already holds; the first by path creates the page. Two notes sharing a title that is not a day remain a
problem for the model to fix.

## Reproduce

1. A source with `Journal/2031-04-11.md` and `Journal/Week-15/2031-04-11.md`; a workspace with the rules approved.
2. *plan a vault*: both notes carry "another note has the same title: change one"; *apply a vault plan* refuses.

## Rules involved

- [importing with a model](../guides/importing.md), "A folder of notes" (`append`)
- [D5](../decisions/D05-pages-and-day-pages.md) — one day page per local day
- [capture](../cookbook/capture.md) — how text is appended to a day page

## Resolution

A second daily note of one day in the plan is marked `append` and its text is part of the day page from its creation; the page is compared whole on every run, so a re-run drops nothing ([a folder of notes](../guides/importing.md#a-folder-of-notes)). Two notes sharing a title that is not a day remain a problem for the model.
