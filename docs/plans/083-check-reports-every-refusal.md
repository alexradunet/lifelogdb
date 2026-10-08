# 083 — Check reports every refused write with a class; look-alikes by kind

> Dated implementation record, not permission to operate on real data. The owner runs every approval and replay.

## Status

- **Date / baseline:** 2026-10-08, `5906b64` (master).
- **Priority:** P1. **Effort:** M. **Risk:** LOW for the schema (untouched), LOW for the import contract (the
  file-level invariant is unchanged; *check* reports more).
- **Status:** IN PROGRESS.
- **Resolves:** issues [0050](../issues/0050-one-doubtful-write-refuses-a-whole-file.md),
  [0051](../issues/0051-look-alikes-compare-with-every-entity-type.md).

## The change

| what | where |
|---|---|
| **Every refused write, with a class.** *check facts* runs each write in a savepoint inside its rolled-back dry run and returns `refused: [{write, kind, what, quote, class, reason, candidates, waits}]` — every write the writer would refuse. Classes: `event`, `task`, `kind`, `quote`, `not_named`, `alias`, `number`, `day`, `invalid_title`, `link`, `metric`, `unit`, `value`, `look_alike`, `not_yet`, `taken`, `person_day`, `link_endpoint`. File-level refusals stay file-level (kept_as_text and waiting entries; reading identities). *apply facts* is unchanged in its invariant: whole or not at all, the first refusal's class on the error (`core.Error.Code`). | `internal/importer/apply.go`, `facts.go`, `internal/core/write.go` (`Tx.Savepoint`, used by `syncWikilinks` too), `core.go` |
| **Look-alikes by kind, all candidates.** A new person, place or page is compared with live persons, places and plain pages (titles, names, aliases); day pages, metrics, files and periods are never candidates. Every candidate is reported with its relation. | `apply.go` (`lookAlikes`) |
| **Status.** A done file whose check now refuses a write is a mismatch naming the write and its class. | `status.go` |
| **Docs.** The guide's "The checks" names the classes; the operations table says what *check facts* returns; "What an implementation must get right" states both rules. README's import notes. | `docs/guides/importing.md`, `README.md` |

## Done criteria

1. A file with several bad writes: *check* lists all of them with their classes and keeps the good ones in
   `writes`; *apply* refuses the file with the first class; the trial is unchanged.
2. A look-alike against a metric, a day page or a file is not a refusal; against a person, a place or a page it is,
   with every candidate listed.
3. Existing refusal tests rewritten to classes; the document suite green; baseline green on Windows.
4. The issues marked resolved and this plan DONE in the commit of the change.
