# 0004 — An `about` link applied before the person's own note fails, and the replay aborts on it

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the import, on a vault whose daily notes name people who also have their own notes

## What happened

`apply-vault` creates every planned note as a **plain page**; a note becomes a person only when its own
facts file is applied. Nothing orders the two. If a day page's `about` link is applied before the person's
own file, the target exists — as a page — so the `link_kinds` endpoint check refuses it:

```
Journal/2031-04-11.md: write 1 (link): constraint failed: link endpoint type not allowed for this kind
(see link_kinds.from_types / to_types) (1811)
```

`resolve` in the importer says "not written yet" only when the page is *missing*, and
`Replay` retries only errors containing "not written yet" (`internal/importer/replay.go`). A page that
exists with the wrong type is a hard error: the whole replay stops.

In a real import this failed 90 daily-note files in the trial database and aborted the replay into the
target. The only way through was to reorder `ledger.md` so every person file comes before the daily
notes — a manual, undocumented step.

## Reproduce

A source folder with two files:

- `Journal/2031-04-11.md` — `Swam at Riverside Pool with Bobby.`
- `Contacts/Bob Sample.md` — `Bob Sample, a friend from school.`

1. `lifelog import setup --workspace <source>.lifelog --from <db>`, `lifelog do make-ledger`, approve rules
   with the alias `"Bobby" → "Bob Sample"`.
2. `lifelog do apply-vault` — both notes become plain pages.
3. `lifelog do write-facts file="Journal/2031-04-11.md" facts='{"file":"Journal/2031-04-11.md","writes":[{"link":{"from":"2031-04-11","to":"Bob Sample","kind":"about"},"quote":"Swam at Riverside Pool with Bobby"}]}'`
4. `lifelog do apply-facts file="Journal/2031-04-11.md"` → the 422 above. Applying
   `Contacts/Bob Sample.md` first makes it succeed.

## Rules involved

- The `about` kind's endpoints in `link_kinds` ([entities and links (D8)](../decisions/D08-entities-and-links.md), [schema.sql](../schema/schema.sql)).
- [Named pages (D20)](../decisions/D20-named-pages.md) — a person is a page promoted to a person.
- [Importing](../guides/importing.md) — "Trial, then the real run".

## Resolution

_(open)_ The replay loop could apply every person/place write in a first pass before any link, or treat
"endpoint type not allowed" as a retryable ordering error like "not written yet".
