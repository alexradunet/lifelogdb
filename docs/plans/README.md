# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last: plans up to 028 are done and live in git history
(`git log -- docs/plans`), so the next one is **029**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). A plan that is DONE or REJECTED is deleted; git is the log.

## What is left (2026-10-02)

### The owner decided (2026-10-02) — to carry into the docs

- **Renames** ([issue 0001](../issues/0001-a-rename-has-no-recipe.md)): the text and the old page's typed links move to
  the new page; the stub holds only its `redirect`. A rename into an existing title only when the old page is empty
  (a typo ghost); otherwise refused, never merged. A cookbook recipe and its suite, then the issue closes.
- **A new file after the freeze**: `schema.sql` stays the full current DDL; each migration also edits it, and a suite
  proves `schema.sql` equals the frozen DDL plus every migration (same `sqlite_master`). D13 and `AGENTS.md`.
- **`pages_fts_delete`** is cut before the freeze (it can never fire while `pages_no_delete` exists; the FTS
  integrity check reports a stale index).
- **Snapshots are reopened** (only snapshots: a dated `VACUUM INTO` copy and a restore check; the export, CSV dump and
  off-box copy stay out): an issue, then the non-goals row and the freeze checklist's item 3.
- **Readers set `trusted_schema=OFF`** too, required by [connection setup](../contract/connections.md).
- **The `Cn` rule names Unicode 15.0** in [titles and wikilinks](../contract/titles-and-wikilinks.md).
- **A tombstoned day page keeps showing its mood reading**: left as is; a reading is retracted by hand (D7).

### Next step

- **The first import into the canonical `life.db`** (the real run of the import guide), then the freeze checklist
  ([process](../process.md#before-the-freeze)).

### Known bugs and gaps in the writer

- **`version` collides within a millisecond**: two saves of one page in the same millisecond share
  `entities.updated_at`, so the stale-save 409 can miss; `TestSaveCarriesItsVersion` flaked once on this.
- **No action to set or correct a person's birth or death day** outside an import: the correction is SQL for now.
- **A replay's real run is not atomic**: a failure after a clean rehearsal (a full disk, another writer in between)
  keeps the writes made before it; every write is idempotent, so a re-run completes it.
- **The vault step stops at its first failing note**, in the rehearsal too: a vault failure is reported once, not
  per note.
- **The workspace is not locked** between a replay's rehearsal and its real run.
- **The vault plan refuses a note whose title the database already holds as a person** (no longer needed for
  birthdays, but still a wall for a person created by hand first).

### Deferred until a real case (reopen when it happens)

- An `UPDATE` of an id leaves `pages_fts` stale — no write path changes an id.
- The symmetric link mirror fails closed under an explicit `INSERT OR ABORT`/`OR FAIL`/`OR ROLLBACK` into `links`;
  the fix is `WHERE NOT EXISTS` in `links_mirror_insert`.
- `ghost_pages` lists an empty day page only a measurement's `captured_with_id` references (a mood-only capture),
  and never lists a ghost whose only referrer was tombstoned.
- `#REDIRECT` "any case" is ambiguous for Unicode case variants (`#REDİRECT`).
- The day view's "(edited)" flag shows on freshly written non-day pages (an insert then a body update).
- Habit edge cases: a completion range with `from > to` returns one row; a same-day stop and restart is refused.
- Updatable columns no writer updates: a link's `note`/`created_at`, `entities.created_at`; an embedded NUL byte
  passes the GLOB checks; `INSERT OR REPLACE` could rewrite a used metric's unit (writers never use it).
- The approval stamp and its `.approved/` copy are lines in files: anything that writes the workspace directly can
  forge them (the guide's honest limit).

### Housekeeping

- No CI: a GitHub workflow running `go generate ./... && go vet ./... && go test ./...`.
- `.agents/skills` and `.claude/skills` are two identical copies of the improve skill.
