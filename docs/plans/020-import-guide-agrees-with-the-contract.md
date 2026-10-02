# Plan 020: The import guide agrees with the imports contract and says what happens to a day that already exists

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/guides/importing.md docs/contract/imports.md tests/schema/imports.py tests/schema/mutants.py`
> Plans 017 and 018 edit other lines of `importing.md` (the `person` row; the workspace paragraph) — expected. Compare
> the lines quoted below; STOP on a mismatch in those.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (it specifies new writer behaviour for overlapping days; the owner chose it — see "Decisions")
- **Depends on**: 017, 018 (same file, earlier lines); 019 (frontmatter wording relies on the contract's scan rule)
- **Category**: docs
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

`docs/guides/importing.md` is the process any writer of `life.db` follows to import notes with a (local) model;
`docs/contract/imports.md` is the contract it must keep. The next milestone of the project is the **first import into
the canonical `life.db`** (the freeze). Read against each other, they disagree or are silent in ways that will stop
that run or make two writers behave differently:

1. The guide says to make "a **fresh** trial database"; the contract says the first run of an importer goes on a
   **copy** of `life.db` (`VACUUM INTO`). A fresh trial never sees the day pages and pages the owner already captured,
   so the real run (which must match the trial's counts) meets collisions the trial did not.
2. A daily note whose day page already exists has no defined outcome: the vault plan tells the model to fix "a title
   `life.db` already holds" by editing titles — impossible for a day page (its title must equal its day).
3. Prose can only come in through the vault path, but the guide never says so; a journal export has no route.
4. A `page` write titled with a day (`2031-04-12`) is unspecified (`pages_day_page` requires `day = title`).
5. A reading's key uses "its order in the file" — the source file or the facts file? Editing the facts file must never
   change a key.
6. "Frontmatter… its tags are not read as tags" contradicts the save contract, which scans all CommonMark text: a
   frontmatter `"[[Note]]"` or `"#x"` *is* read (verified with the reference implementation).
7. `imports.md` copies the live file with a read-write `sqlite3` shell, and does not say the batch runs on the writer's
   connection (the shell has `foreign_keys` off, so step 4's "a dangling foreign key still raises" would not hold).

## Decisions (made by the owner on 2026-10-02 — do not reopen)

- **D-a. A day that already exists: append, recorded.** An imported daily note whose day page already exists is
  appended to that day page (after a blank line, as capture appends), through the save contract; the writer records
  the note's path against the page in `plan.json`, so a re-run or a replay finds the record and appends nothing again.
- **D-b. Prose only through the vault path.** Facts files write rows, never page bodies. A journal or diary export is
  first converted to a folder of one Markdown file per day named `YYYY-MM-DD.md` (a vault) and imported as one.

## Current state

`docs/guides/importing.md` (403 lines). Excerpts to change:
- `:263-264` (step 1 of the procedure): "**1. Set up.** Make the workspace beside the source and a fresh trial database in it; run the *integrity check* (green before the import)."
- `:150-157` the kinds table; the `page` row: "| `page` | `title` | a plain, empty page (a topic the file names) |"
- `:159-163`: "- **Keys are derived, never written.** The writer derives each new row's `import_key` from the source path and the write: the kind and title for a person, place or page; for a reading, the day and whatever tells apart two readings of one metric on one day in one file (its `taken_at`, else its order in the file). …"
- `:326-334` ("An Obsidian vault"):
  "- **The plan lists every problem** for the model to fix by editing only titles and days: a title the [titles and wikilinks] predicate refuses, two notes with one title, a title `life.db` already holds (when unsure, ask).
   - **All pages are created first**, then each note's text is saved in its own transaction through the save contract ([save a body]), so a link between notes lands on the note. The note's path is its `import_key`; an unchanged body is left alone, so a second run writes nothing, also after a note's page is promoted to a person or a place."
- `:340-341`: "- **Frontmatter stays in the text as written**: its tags are not read as tags and its aliases make no redirect stubs. A nested tag reads as its first segment ([titles and wikilinks])."
- `:346-347` ("Trial, then the real run"): "Everything runs on a trial database first ([imports] step 1)."
- `:359-` "What an implementation must get right" — a bullet list of requirements.
- The guide's first section ("What this is", `:3-8`).

`docs/contract/imports.md`:
- `:6-8`: "1. **Trial run first.** … Do the first run of any new importer on a *copy*: `sqlite3 life.db "VACUUM INTO '/tmp/trial.db'"`."
- `:9-10`: "2. **Load the rows into a scratch database, never into `life.db`** (`sqlite3 scratch.db ".import --csv weights.csv staging"`), then insert in one `BEGIN IMMEDIATE` transaction per batch:"
- `:65-66` (step 4): "…a dangling foreign key still raises and the **whole batch rolls back**."

Facts verified on 2026-10-02 (SQLite 3.53.4): `VACUUM INTO` works on a read-only connection — both
`sqlite3.connect('file:life.db?mode=ro', uri=True).execute("VACUUM INTO 'copy.db'")` and
`sqlite3 -readonly life.db "VACUUM INTO 'copy.db'"`. `AGENTS.md` requires such a claim to be **executed by a suite**
before the docs state it; Step 1 adds that.

Conventions: `importing.md` is writer-neutral (no language, no repo paths beyond `docs/`), restates no rule (it links
the contract and cookbook), uses short declarative sentences. `tests/schema/imports.py` executes `imports.md` (it reads
the doc's SQL block); helpers from `tests/lib/kit.py` (`S.K`, `fresh`, `tryx`, `doc_page`).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Imports suite | `tests/.venv/Scripts/python.exe tests/schema/imports.py` | `imports: N/N met expectations` |
| Docs suite | `tests/.venv/Scripts/python.exe tests/schema/document.py` | all met |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |

## Scope

**In scope**: `docs/guides/importing.md`, `docs/contract/imports.md` (steps 1, 2, 4 wording), `tests/schema/imports.py`,
`tests/schema/mutants.py` (one mutant), `tests/README.md` (mutant count), `docs/plans/README.md`.

**Out of scope**: `docs/schema/schema.sql`; new facts kinds; any writer code (there is none in this repo); the
cookbook (the append is capture's existing shape — link it, do not copy SQL into the guide).

## Git workflow

Branch `advisor/020-import-guide`. Message e.g. `importing: the trial is a copy of life.db, a captured day takes the note appended once, prose only through a vault`; body: suites changed, `Ran tests/run_all.py: 20/20 suites passed.`

## Steps

### Step 1: `imports.md` — copy read-only, write on the writer's connection (and execute it)

- Step 1 command → `sqlite3 -readonly life.db "VACUUM INTO '/tmp/trial.db'"`, followed by "— a read-only connection may make the copy (executed)."
- Step 2, after "then insert in one `BEGIN IMMEDIATE` transaction per batch" add: ", on the writer's own connection set up as [connection setup](connections.md) requires (the `sqlite3` shell sets none of it: `foreign_keys` is off there)".
- In `tests/schema/imports.py` add: build a file DB from the DDL in the suite's temp dir, open it with
  `sqlite3.connect(f'file:{path}?mode=ro', uri=True)`, run `VACUUM INTO '<other path>'`, assert the copy has the same
  `lifelog_meta` row count; label `contract/imports step 1: a read-only connection makes the copy (VACUUM INTO)`.
  Also assert `'sqlite3 -readonly life.db "VACUUM INTO' in doc_page('contract/imports.md')` (label `…the documented command opens the file read-only`).
- Mutant (owner `imports`): `mutate('sqlite3 -readonly life.db "VACUUM INTO', 'sqlite3 life.db "VACUUM INTO')`.

**Verify**: `imports.py` → all met; the mutant `caught`.

### Step 2: The trial is a copy (guide)

- `:263-264` → "**1. Set up.** Make the workspace beside the source, and in it the trial database: a copy of the real
  `life.db` when one exists ([imports](../contract/imports.md) step 1), a new one from the schema only when none does yet. Run
  the *integrity check* (green before the import)."
- `:346-347` → "Everything runs on a trial database first — a copy of the real one, so the trial meets every page the owner
  already has ([imports](../contract/imports.md) step 1)."

**Verify**: `grep -n "fresh trial" docs/guides/importing.md` → no output.

### Step 3: A captured day takes the note, appended once (D-a)

Replace the "The plan lists every problem" bullet and extend the "All pages are created first" bullet:
- "**The plan lists every problem** for the model to fix by editing only titles and days: a title the [titles and
  wikilinks](../contract/titles-and-wikilinks.md) predicate refuses, two notes with one title, a title `life.db` already holds for a note
  that is not a daily note (when unsure, ask). A **daily note whose day page already exists** is not a problem: the
  plan marks it `append`."
- In "All pages are created first": after "…so a link between notes lands on the note." insert "A note marked `append`
  creates no page: its text is appended to the existing day page after a blank line, as [capture](../cookbook/capture.md) appends,
  through the save contract, and the writer records the note's path against that page in `plan.json`." Change "The
  note's path is its `import_key`" to "The note's path is its `import_key` (an appended note has none: its record in
  `plan.json` stands in)" and end the bullet "…so a second run writes nothing — an appended note is found by its record
  and appended again never — also after a note's page is promoted to a person or a place."
- Add to "What an implementation must get right": "- A daily note appended to an existing day page is appended once: a
  re-run or a *replay* finds its record and writes nothing; the record is written by the writer, in the same
  transaction as the append."

**Verify**: `grep -n "append" docs/guides/importing.md` shows the three places.

### Step 4: Prose only through a vault (D-b), and a day-titled page

- In "What this is", after the first paragraph, add: "Page text comes in only through a vault ("An Obsidian vault"
  below): facts files write rows, never a page's body. A journal or diary export is first converted to a folder of one
  Markdown file per day, named `YYYY-MM-DD.md`, and imported as a vault."
- Kinds table, `page` row → "| `page` | `title` | a plain, empty page (a topic the file names); a title that is a day makes
  that day's page, its `day` its title ([D5](../decisions/D05-pages-and-day-pages.md)) |".

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

### Step 5: A reading's key does not move when the facts file is edited

`:159-163`: replace "(its `taken_at`, else its order in the file)" with "(its `taken_at`; else its place, counted from 1,
among that metric's readings of that day in the **source** file, by where its quote first appears — so editing the
facts file never changes a key)".

### Step 6: Frontmatter is scanned like the rest of the text

`:340-341` → "- **Frontmatter stays in the text as written** and is scanned like the rest of the body ([titles and
wikilinks](../contract/titles-and-wikilinks.md)): a `tags: [health]` list has no `#`, so it makes no tag, while a `"[[Note]]"` or `"#tag"`
written inside it does; its aliases make no redirect stubs. A nested tag reads as its first segment (`#work/project` is `work`)."

**Verify**: run the reference on the guide's example, from the repo root:
`PYTHONUTF8=1 tests/.venv/Scripts/python.exe -c "import sys; sys.path.insert(0,'tests/wikilinks'); from wikisave import targets; print(list(targets('---\nup: \"[[Bob Sample]]\"\ntags: [health, \"#x\"]\n---\nBody')[0].values()))"`
→ `['Bob Sample', 'x']`.

### Step 7: Counts and full run

Update the mutant count in `tests/README.md`. **Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`.

## Test plan

New expectations in `imports.py` (read-only copy executed; the documented command is read-only) and one mutant. The
guide's process has no executable writer in this repo; its new rules are requirements for a writer (listed under
"What an implementation must get right").

## Done criteria

- [ ] `grep -n "fresh trial" docs/guides/importing.md` → no output
- [ ] `grep -n "sqlite3 -readonly life.db" docs/contract/imports.md` → 1 match
- [ ] `grep -c "append" docs/guides/importing.md` ≥ 3
- [ ] `grep -n "its tags are not read as tags" docs/guides/importing.md` → no output
- [ ] `grep -n "order in the file" docs/guides/importing.md` → no output
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; mutants all caught
- [ ] `docs/plans/README.md` row 020 updated

## STOP conditions

- `VACUUM INTO` fails on a read-only connection on your SQLite (report the version; do not state the claim).
- A sentence you would write restates a rule whose home is a contract page instead of linking it.
- Plan 017's or 018's edits to `importing.md` are not present (run order broken) — finish those first.

## Maintenance notes

- The `append` record in `plan.json` is the one place an imported row is identified by something other than
  `import_key`. If a writer ever needs it in the database itself, that is an issue → proposal (it would be a schema change).
- A replay onto a real database that has gained new captured days since the trial will find new overlaps; *status*
  against the real database must report them before the replay writes (the trial-vs-real count check already stops it).
