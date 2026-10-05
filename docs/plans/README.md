# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last: plans up to 028, 030, 031 and 033 are done, 032 rejected (the owner keeps the few photos chosen for a day, from any
source), and live in git history
(`git log -- docs/plans`), so the next one is **048**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [029](029-google-takeout.md) | Google Takeout: places and daily health totals, with no schema change | P1 | L | Import safety below before the real apply/replay | Phase A IN REVIEW (owner); B/C BLOCKED (owner inventory and approvals) |
| [034](034-browser-write-origin-protection.md) | Reject cross-origin browser writes | P1 | S | — | IN REVIEW (owner) |
| [035](035-correction-value-validation.md) | Enforce mood and habit domains on corrections | P1 | S | — | IN REVIEW (owner) |
| [036](036-unique-find-actions.md) | Give page lookup and import lookup distinct actions | P1 | S | — | IN REVIEW (owner) |
| [037](037-import-correction-lineage.md) | Carry repeated imported corrections through replay | P1 | M | 035 | IN REVIEW (owner) |
| [038](038-reading-source-evidence.md) | Check complete reading values and source units | P1 | M | — | IN REVIEW (owner) |
| [039](039-metric-note-wikilinks.md) | Synchronize wikilinks in newly registered metric notes | P2 | S | — | IN REVIEW (owner) |
| [040](040-ad-hoc-query-isolation.md) | Keep ad-hoc SQL read-only and isolated from pooled readers | P1 | M | 045 | TODO |
| [041](041-durable-import-corrections.md) | Make database corrections and replay intent recoverable | P1 | L | 035, 037 | TODO |
| [042](042-bounded-heif-metadata.md) | Parse HEIF item locations correctly and within bounds | P1 | M | — | IN REVIEW (owner) |
| [043](043-canonical-reading-keys.md) | Use canonical metric identity in derived reading keys | P1 | L | 037, 038, 041, 044 | TODO |
| [044](044-source-filename-identity.md) | Resolve normalized source names to their physical filenames | P2 | M | — | IN REVIEW (owner) |
| [045](045-literal-sqlite-paths.md) | Open literal filenames through escaped SQLite URIs | P1 | S | — | IN REVIEW (owner) |
| [046](046-exclusive-database-init.md) | Reserve new database paths exclusively before initialization | P1 | S | 045 | IN REVIEW (owner) |
| [047](047-file-promotion-day.md) | Fill the missing day when an embed ghost becomes a file | P2 | S | — | IN REVIEW (owner) |

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). An executor sets IN REVIEW (owner); the owner accepts or rejects. A plan that is DONE or REJECTED is deleted; git is the log.

## Audit plans (2026-10-05)

Plans 034–047 cover all fourteen owner-selected findings from the read-only audit at `726ffab`, after photo plan 033 landed. No implementation was performed by the advisor. Verification at that baseline: `go vet -mod=readonly ./...` and `go test -mod=readonly ./...` passed. No real database, import source or workspace was inspected; no race/fuzz, vulnerability-advisory or sustained-performance audit was run.

### Execution order and dependencies

Numbering preserves the finding order; execution follows dependencies, not strictly ascending numbers:

1. **Small independent fixes:** 034, 035, 036, 039, 042, 044, 045, 047. Start 034–036 and 045 first. Independent does not mean safe simultaneous edits: 039/047 share core tests, and 042/047 share API file tests; serialize overlapping changes.
2. **After their prerequisites:** 037 after 035; 040 and 046 after 045. Run 038 before changing import identity.
3. **Recovery:** 041 after 035/037, with the explicit protocol review gate. This is not a two-write reorder masquerading as atomicity.
4. **Identity:** 043 after 037/038/041/044, with legacy-key/correction compatibility tests before any key change. It must not rewrite stored facts or silently merge historical roots.
5. **Owner's rebuild/import:** settle 037/038/041/043 and database path/creation safety (045/046) before the canonical rebuild or plan 029's real apply/replay. Plan 029's inventory/converter design can proceed separately, subject to the owner's current photo decisions; do not revive archive-wide photo inventory from its older dated brief.

Plans cite one baseline, so prerequisite edits are expected drift. Each executor must verify those changes, rebaseline its plan excerpts/interfaces, and stop on unrelated drift. Use synthetic temporary data only; no DDL or migration is authorized by these plans. If a schema or new contract decision is needed, stop for the [change process](../process.md).

### Considered and not planned

- **Authentication/encryption as a redesign:** the local unauthenticated application and plaintext database are accepted tradeoffs ([non-goals](../architecture/non-goals.md)); 034 addresses browser request authenticity without reopening them.
- **Migration runner:** explicitly forbidden before the [freeze](../decisions/D13-migrations-and-freeze.md).
- **Photo-library inventory and automatic HEIC preview decoding:** the owner keeps selected photos from any source ([D9](../decisions/D09-binary-files.md)); the current writer accepts a supplied JPEG preview for unsupported image/video formats. Plan 033 is complete. Plan 042 concerns metadata parser correctness, not a replacement photo product.
- **Millisecond version collisions, whole-replay atomicity, person-date editing, restore CLI and CI:** already recorded below; not duplicated as new audit findings. The optional direction suggestions were not selected as additional implementation scope.
- **Generic performance/architecture refactoring:** no measured bottleneck justified a separate plan. The concrete missing MCP coverage is included in 036.

## What is left (2026-10-04)

The owner's decisions of 2026-10-02 are carried out: renames (the contract, cookbook/rename-a-page), one full DDL
after the freeze (D13), `pages_fts_delete` cut, readers set `trusted_schema=OFF`, the `Cn` rule names Unicode 15.0,
snapshots (D25, cookbook/take-a-snapshot); a tombstoned day keeps its mood reading. Files are kept as pages: their text, a small picture, the original outside (D9, plan 030 done). A place has a point and a photo kept links
its day (D21, plan 031 done). The few photos
chosen for a day are kept together, from any library (plan 033 done). No open issue: the next is **0011**.

### Next step

- After the import-safety dependencies above are accepted, **rebuild the canonical `life.db` from today's `schema.sql`** (the owner, locally: a fresh file, then *replay*
  every workspace into it, [D13](../decisions/D13-migrations-and-freeze.md)). The file was made before
  `pages_fts_delete` was cut and before the `files` and `places` tables ([D9](../decisions/D09-binary-files.md), [D21](../decisions/D21-location-history.md)), and a canonical file is rebuilt, never migrated, until the freeze. A snapshot first
  ([D25](../decisions/D25-snapshots.md)).
- **The rest of the imports and the capture path** — the owner's files among them (`lifelog file`, the import guide's
  "Files") — then the freeze checklist
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
- **No `lifelog restore`**: restoring a snapshot is the manual procedure of the recipe.
- **The import's `trial.db` and the replay's rehearsal copy come out in rollback-journal mode** (`VACUUM INTO` does not
  keep WAL, executed by the snapshots suite); only the restore sets WAL back.

### For the freeze checklist

- Consider an item "a snapshot of the frozen file passes its restore check" ([D25](../decisions/D25-snapshots.md)).
- Run the optional diagram render once (`LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender`): the page
  lifecycle diagram gained a `Ghost --> Stub` transition and was not rendered.

### Deferred until a real case (reopen when it happens)

- An `UPDATE` of an id leaves `pages_fts` stale — no write path changes an id.
- The symmetric link mirror fails closed under an explicit `INSERT OR ABORT`/`OR FAIL`/`OR ROLLBACK` into `links`;
  the fix is `WHERE NOT EXISTS` in `links_mirror_insert`.
- `ghost_pages` lists an empty day page only a measurement's `captured_with_id` references (a mood-only capture),
  and never lists a ghost whose only referrer was tombstoned.
- `#REDIRECT` "any case" is ambiguous for Unicode case variants (`#REDİRECT`).
- Renaming a page that is itself the target of an older stub leaves a two-hop chain (A → Old → New); reads follow
  one hop, so mentions of A do not count for New. A typed link from another page into a renamed page, of a kind
  registered later, would stay on the stub (no kind registered today can do that).
- The day view's "(edited)" flag shows on freshly written non-day pages (an insert then a body update).
- Habit edge cases: a completion range with `from > to` returns one row; a same-day stop and restart is refused.
- Updatable columns no writer updates: a link's `note`/`created_at`, `entities.created_at`; an embedded NUL byte
  passes the GLOB checks; `INSERT OR REPLACE` could rewrite a used metric's unit (writers never use it).
- The approval stamp and its `.approved/` copy are lines in files: anything that writes the workspace directly can
  forge them (the guide's honest limit).

### Housekeeping

- No CI: a GitHub workflow running `go generate ./... && go vet ./... && go test ./...`.
- `.agents/skills` and `.claude/skills` are two identical copies of the improve skill.
