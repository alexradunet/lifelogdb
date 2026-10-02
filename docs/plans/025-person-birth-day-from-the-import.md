# Plan 025: A person's birth and death day from the import

## Status

- **Priority**: P1 (a real import of person notes left every `people.birth_day` NULL, and frontmatter-only notes stayed plain pages)
- **Effort**: S
- **Category**: bug
- **Planned and built**: 2026-10-02, on top of plan 024

## Why

[Issue 0003](../issues/0003-an-import-cannot-set-a-persons-birth-day.md): two walls on notes under `People/`.

1. The facts file's `person` write carried a title and a name only, so the import created or promoted every
   person with `birth_day` NULL. Creating the person first through the API made the vault plan refuse the
   title; nothing set the day afterwards.
2. A person write needs a quote that names the person's title. A note that is only frontmatter
   (`type: Person`, `birthday: 1980-03-29`) never writes its own title: the title is its file name. So it
   could not be promoted at all.

No schema change: `people.birth_day` and `death_day` exist, are nullable and may be updated.

## What was built

- **The facts format** ([importing](../guides/importing.md), "The facts file"): `person` takes `birth_day` and
  `death_day`, each optional. A new person is created with them; a promoted page or an existing person gets a day it
  lacks; a day it already holds is left alone (`existing`), and a different one is refused: the facts never change
  a day, a correction is the owner's (as for a reading's key). An existing person given a day it lacked reports a
  new status, `updated`. *status* counts it as a mismatch until applied, like `new`.
- **The evidence rule**: a day must be in its quote, `YYYY-MM-DD` as written (nothing is converted). A quote names
  a title when it holds it, an alias of it, or when the title is the **file's own title** — its title in the vault
  plan, else its file name without the extension — beside the existing rule that a daily note names its own day. A
  file name is the file's own evidence the way a `YYYY-MM-DD` file name already was for a day; the quote is still
  required and still checked against the file (frontmatter included). So the frontmatter-only note is promoted with
  the quote `birthday: 1980-03-29` and that day reaches `people.birth_day`.
- **Code**: `core.Tx.FillPersonDays` (fill NULLs, refuse a different day); `named` in the importer takes a fill
  step after create, revive or promote; `ownTitle` and the day checks in `checkStatic`.
- **Guide**: the `person` row, the `updated` status, what naming a title means (frontmatter is quotable, a file names
  its own title), the two new refusals, and a row of the model's table (a day written in another form is
  `kept_as_text`).

## Verification (2026-10-02)

- `go generate ./... && go vet ./... && go test ./...` — green.
- `TestPersonNoteWithOnlyFrontmatter`: the issue's note, planned and applied as a vault, is promoted with its
  `birth_day`; a second apply is `existing`; *status* shows no mismatches; a replay into a new file makes the same
  person with the same day, and a second replay writes nothing.
- `TestPersonDays`: a person created through the API without days is `updated` with both, then `existing`; a day not
  in the quote, a day not `YYYY-MM-DD`, a title another file's quote does not name, and a different day than the one
  held are refused, and the held day is unchanged.
- Broken on purpose, each fails a test: the own-title rule removed (both tests: "the quote does not name"), the fill
  step removed (no `birth_day` after apply and replay), a held day overwritten instead of refused.

## Open

- There is still no API action to set or correct a person's days outside an import: the owner's correction path
  for them is SQL for now. Add one when a correction is needed.
- The vault plan still refuses a note whose title the database already holds as a person (the issue's second
  order); with this change that order is no longer needed.
