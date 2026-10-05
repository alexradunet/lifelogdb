# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last: plans up to 028, 030, 031 and 033 are done, 032 rejected (the owner keeps the few photos chosen for a day, from any
source), and live in git history
(`git log -- docs/plans`), so the next one is **072**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [029](029-google-takeout.md) | Complete Takeout inventory; scope one approved family | P1 | M now; L later | Import safety before real apply/replay | Phase A IN REVIEW (owner); B/C BLOCKED (owner inventory, selection and approvals) |
| [034](034-browser-write-origin-protection.md) | Reject cross-origin browser writes | P1 | S | — | IN REVIEW (owner) |
| [035](035-correction-value-validation.md) | Enforce mood and habit domains on corrections | P1 | S | — | IN REVIEW (owner) |
| [036](036-unique-find-actions.md) | Give page lookup and import lookup distinct actions | P1 | S | — | IN REVIEW (owner) |
| [037](037-import-correction-lineage.md) | Carry repeated imported corrections through replay | P1 | M | 035 | IN REVIEW (owner) |
| [038](038-reading-source-evidence.md) | Close leading-decimal and cropped-unit evidence gaps | P1 | M | — | IN REVIEW (owner) |
| [039](039-metric-note-wikilinks.md) | Synchronize wikilinks in newly registered metric notes | P2 | S | — | IN REVIEW (owner) |
| [040](040-ad-hoc-query-isolation.md) | Keep ad-hoc SQL read-only and isolated from pooled readers | P1 | M | 045 | IN REVIEW (owner) |
| [041](041-durable-import-corrections.md) | Make database corrections and replay intent recoverable | P1 | L | 035, 037 | IN REVIEW (owner) |
| [042](042-bounded-heif-metadata.md) | Parse HEIF item locations correctly and within bounds | P1 | M | — | IN REVIEW (owner) |
| [043](043-canonical-reading-keys.md) | Use canonical metric identity in derived reading keys | P1 | L | 037, 038, 041, 044 | IN REVIEW (owner) |
| [044](044-source-filename-identity.md) | Preserve source filename whitespace and Unicode identity | P1 | S | — | IN REVIEW (owner) |
| [045](045-literal-sqlite-paths.md) | Open literal filenames through escaped SQLite URIs | P1 | S | — | IN REVIEW (owner) |
| [046](046-exclusive-database-init.md) | Reserve new database paths exclusively before initialization | P1 | S | 045 | IN REVIEW (owner) |
| [047](047-file-promotion-day.md) | Align the promoted-file day recipe and executable tests | P2 | S | — | IN REVIEW (owner) |
| [048](048-facts-json-boundary.md) | Preserve facts on malformed trailing JSON | P1 | S | — | IN REVIEW (owner) |
| [049](049-source-path-confinement.md) | Confine physical source reads | P1 | M | 044 | IN REVIEW (owner) |
| [050](050-mcp-numeric-contract.md) | Preserve MCP numeric enums and integer IDs | P1 | S | — | IN REVIEW (owner) |
| [051](051-applied-vault-plan-identity.md) | Refuse applied vault-plan identity changes | P1 | M | — | IN REVIEW (owner) |
| [052](052-snapshot-physical-destination.md) | Check resolved snapshot destination ancestry | P1 | S | — | IN REVIEW (owner) |
| [053](053-exif-hemisphere-validation.md) | Validate EXIF hemisphere references | P1 | S | 042 behavior retained | IN REVIEW (owner) |
| [054](054-vault-append-boundaries.md) | Recognize complete trailing vault append blocks | P2 | S | Serialize after 051 | IN REVIEW (owner) |
| [055](055-truthful-photo-embeds.md) | Make automatic photo embeds truthful | P2 | M | 065; serialize with 047/053 | IN REVIEW (owner) |
| [056](056-correction-status-truth.md) | Align correction status with checked replay | P2 | M | 038; 041/043 behavior retained | IN REVIEW (owner) |
| [057](057-client-cancellation.md) | Propagate CLI/MCP request and upload cancellation | P2 | M | 050 | IN REVIEW (owner) |
| [058](058-browser-mutation-feedback.md) | Preserve browser mutation feedback | P2 | M | — | IN REVIEW (owner) |
| [059](059-habit-domain-and-visibility.md) | Enforce capture habit domains and live habit views | P2 | M | 035 behavior retained | IN REVIEW (owner) |
| [060](060-canonical-metric-actions.md) | Use canonical metric identity for actions | P2 | S | — | IN REVIEW (owner) |
| [061](061-redirect-stub-write-guards.md) | Guard redirect-stub mutations | P2 | M | — | IN REVIEW (owner) |
| [062](062-commonmark-safe-vault-rewrite.md) | Preserve CommonMark code during vault rewriting | P2 | M | Serialize after 051/054 | IN REVIEW (owner) |
| [063](063-http-authority-boundary.md) | Verify and constrain network HTTP authority | P2 | M | 034; explicit authority policy | IN REVIEW (owner) |
| [064](064-bounded-request-buffering.md) | Bound JSON and multipart metadata buffering | P2 | M | — | IN REVIEW (owner) |
| [065](065-markdown-link-precedence.md) | Preserve ordinary Markdown link precedence | P2 | M | — | IN REVIEW (owner) |
| [066](066-reading-root-scan-cost.md) | Batch reading-root lookup and day bucketing | P2 | M | 043; run after 038/056 | IN REVIEW (owner) |
| [067](067-deterministic-writer-and-mutant-tests.md) | Make writer/mutant evidence deterministic | P2 | M/L | Serialize mutant-registry edits | IN REVIEW (owner) |
| [068](068-import-setup-exit-status.md) | Fail setup when trial integrity is not clean | P2 | S | — | IN REVIEW (owner) |
| [069](069-rehearsal-preflight-reporting.md) | Collect per-file rehearsal identity failures | P3 | M | 043; run after 056/066 | IN REVIEW (owner) |
| [070](070-trial-capture-pilot.md) | Owner-run disposable capture pilot | P3 | S brief | Capture fixes and owner choice | BLOCKED (owner pilot/path/client approval) |
| [071](071-audit-followups.md) | Integrate the 22 residual audit follow-ups | P1/P2 | L aggregate | Existing import/surface behavior retained | IN REVIEW (owner) |

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). An executor sets IN REVIEW (owner); the owner accepts or rejects. A plan that is DONE or REJECTED is deleted; git is the log.

## Residual audit handoff (2026-10-05, based on `e1d7959`)

[071](071-audit-followups.md) records the 22 owner-authorized follow-ups, their implementation/test map,
measured synthetic performance and evidence limits. Nine preserved component patches were assembled, followed by
serialized CLI work and final integration review. All delivery remains unstaged on the isolated integration branch;
no commit, push or merge was requested or performed. The failed initial workflow/recovery and harness-staged
component artifact snapshots are recorded honestly in 071; integration's index is empty.

The candidate is **IN REVIEW (owner), not DONE**. Existing historical briefs/statuses remain unchanged.
029 B/C and 070 remain owner-gated. No real data, canonical replay/rebuild, freeze or migrations were performed.
Schema/modules and plan-063 authority behavior are preserved. Final validation evidence is in the integration
artifact; owner acceptance and any publication or real-data operation are separate decisions.

## Implementation handoff (2026-10-05, based on `0460379`)

All **26 actionable briefs are implemented, integrated and IN REVIEW (owner)**. This includes only Phase A
of 029. **063 implements the owner's approved local-only authority policy**, with synthetic IPv4/IPv6 tests and
parent review; no browser exploit claim is implied. The owner-gated 029 B/C and 070 remain blocked; the owner's
intended later database recreation authorizes no current database access or rebuild.

Implementation used isolated GPT-6.1 Sol low-thinking workers with parent source/protocol review and integration.
Review follow-ups fixed an escaped-existing-photo no-op refusal and unknown multipart-name retention, added complete
status state snapshots and serialized rehearsal diagnostics, and repaired weak/stopped mutant witnesses. Approved
narrow test-scope expansions are recorded in the affected briefs; no contract assertion or mutant credit was loosened.

The 25-plan batch passed `go generate ./...`, `go vet -mod=readonly ./...`,
`go test -mod=readonly -count=1 ./...`, `git diff --check`, 20 writer-suite runs and three runs of all **190 mutants**.
Photo/text/API and harness/named/identity/facts/dates integration checks passed too. It was committed and pushed as
`9c19b37` at the owner's request. The subsequent 063 change passed generation, vet, the full uncached suite,
package gates and twenty authority/serve/origin focus runs after parent integration and fixes for Unicode
localhost matching and HTTP's implicit port 80. Its brief records an unrelated random-feedback-token test flake;
final gates passed without changing that test. The owner requested publication of 063 after review;
publication does not mark the plan DONE or authorize any database operation.
Canonical/embedded schema and module files are unchanged. Pre-existing plan inputs and `lifelog.exe~` were preserved.
No real data, canonical replay, migrations or freeze work was performed.

Windows junction cases executed; ordinary symlink cases skipped individually when privileges were unavailable.
No race/fuzz, browser exploitation, power-loss or real-import validation is claimed. Snapshot/source and metadata
limits, numeric-unit vocabulary, synthetic performance measurements and other residual limits are in the briefs.
Owner acceptance, not an executor's status change, closes these plans.

## Deep-audit plans (2026-10-05, `0460379`)

The owner selected **all 25 findings**, including the additional rehearsal-reporting regression. This set adds
**23 briefs (048–070)** and revises **029, 038, 044 and 047** rather than duplicating their work. Finding 3 is split
between filename identity (044) and physical confinement (049). The two direction options are explicitly
owner-gated briefs: 029's one-family design and 070's disposable capture pilot. Planning is not implementation,
real-data access, converter approval or permission to freeze the canonical file.

The new findings are **source-confirmed, not newly runtime-reproduced**. Network authority exploitation in a real
browser is untested, and import performance is unmeasured; 063/066 require characterization before stronger claims.
The read-only audit baseline passed `go vet -mod=readonly ./...`, `go test -mod=readonly -count=1 ./...`,
`go test -mod=readonly -short -cover ./cmd/... ./internal/...`, `go mod verify`, the canonical/embedded schema comparison,
and `git diff --check`. No source fixes, builds, generation, real data access or commits were performed by the advisor.
The pre-existing untracked `lifelog.exe~` was not opened or changed.

Not audited dynamically: real imports/databases, race/fuzz behavior, browser exploitation, power loss, sustained
performance, vulnerability advisories or Mermaid rendering. Future test commands in the briefs are executor gates,
not claims that those new regressions already ran.

Plan-delivery verification: `go test -mod=readonly -count=1 ./tests -run '^TestSuites/document$'`,
`go vet -mod=readonly ./...`, `go test -mod=readonly -count=1 ./...` and `git diff --check` passed.
A read-only plan check confirmed all 25 mappings, all 23 new/four revised briefs, required sections/baselines,
index links and plan-only scope. New plan files were also checked for trailing whitespace. No generation was run;
only plan Markdown changed.

### Finding-to-plan map

| Finding | Selected work | Plan |
|---------|---------------|------|
| 1 | Malformed JSON must not replace replay facts | [048](048-facts-json-boundary.md) |
| 2 | Leading-decimal and cropped-unit evidence | [038](038-reading-source-evidence.md) |
| 3 | Exact filenames and physical source confinement | [044](044-source-filename-identity.md), [049](049-source-path-confinement.md) |
| 4 | MCP numeric enums and identifiers | [050](050-mcp-numeric-contract.md) |
| 5 | Applied vault-plan title/day divergence | [051](051-applied-vault-plan-identity.md) |
| 6 | Physical snapshot destination in Git | [052](052-snapshot-physical-destination.md) |
| 7 | EXIF GPS hemisphere validation | [053](053-exif-hemisphere-validation.md) |
| 8 | Vault append substring false deduplication | [054](054-vault-append-boundaries.md) |
| 9 | Unrepresentable and mis-deduplicated photo embeds | [055](055-truthful-photo-embeds.md) |
| 10 | Correction status/proof/replay agreement | [056](056-correction-status-truth.md) |
| 11 | CLI/MCP cancellation | [057](057-client-cancellation.md) |
| 12 | Browser mutation feedback | [058](058-browser-mutation-feedback.md) |
| 13 | Captured mood domains and tombstoned habits | [059](059-habit-domain-and-visibility.md) |
| 14 | Unicode metric action lookup | [060](060-canonical-metric-actions.md) |
| 15 | Redirect-stub write guards | [061](061-redirect-stub-write-guards.md) |
| 16 | Takeout inventory completeness | [029](029-google-takeout.md), Phase A |
| 17 | Canonical promoted-file date recipe | [047](047-file-promotion-day.md) |
| 18 | Vault code-span/fence preservation | [062](062-commonmark-safe-vault-rewrite.md) |
| 19 | HTTP authority policy and characterization | [063](063-http-authority-boundary.md) |
| 20 | JSON/multipart buffering bounds | [064](064-bounded-request-buffering.md) |
| 21 | Ordinary Markdown link precedence | [065](065-markdown-link-precedence.md) |
| 22 | Repeated reading-root scans | [066](066-reading-root-scan-cost.md) |
| 23 | Deterministic writer and relevant mutant evidence | [067](067-deterministic-writer-and-mutant-tests.md) |
| 24 | Setup integrity failure exit | [068](068-import-setup-exit-status.md) |
| 25 | Additional rehearsal preflight reporting regression | [069](069-rehearsal-preflight-reporting.md) |

### Recommended execution order and dependency barriers

1. **Replay evidence and the first five findings:** 048 → 038 → 044 → 049 → 050 → 051. The strict new dependency
   here is 044 → 049; 050 is independently executable. Also take the small safety/parity fixes 052, 068 and 047 early.
2. **Finish import correctness before optimization:** 054 → 062 after 051 for shared vault code; 056 after 038 while
   retaining implemented 041/043 proofs. Then 066, then 069. Run all existing correction/legacy-key regressions at
   each barrier; HIGH-risk 056 needs protocol-focused owner review. No immutable fact/key repair is authorized.
3. **Photo correctness:** 053; 065 → 055, with 047 accepted/rebaselined before shared cookbook/test work. Filename-safe
   titles and wiki representability remain different: 055 may refuse an automatic embed, not widen the grammar.
4. **Everyday surfaces:** 057 after 050; 059 → 060 → 061, then 058/064 for shared handler/test files. 063 starts with
   authority-policy characterization; it must not infer a new remote/proxy deployment policy. Independent tasks
   can be scheduled separately, but do not run overlapping writers in one cwd/worktree.
5. **Validation:** 067 can start independently, but serialize its registry edits with 047/055/059 and rebaseline
   any new mutants. Its outcome must strengthen witnesses, never relax suites to turn them green.
6. **Direction gates:** 029 Phase A may proceed on synthetic fixtures. B/C stays blocked until corrected owner-run
   inventory, one selected populated family and approvals yield a separate converter brief. 070 stays blocked until
   its capture dependencies and owner-approved disposable path/client are ready. Neither brief authorizes a real
   canonical write, real export inspection, archive-wide photos or schema freeze.

Dependencies on 034/035/037/041/042/043/045/046 refer to behavior already implemented at this baseline, still awaiting
owner acceptance. Reopened 038/044/047 contain residual work only. Reviewed prerequisite changes are expected drift:
compare/rebaseline the brief before starting; stop on unrelated changes or a needed schema/contract decision.
Every executor uses synthetic data, reports exact checks run and ends at **IN REVIEW (owner)**, not self-accepted DONE.

### Considered and excluded from this set

- Local unauthenticated service, plaintext storage and externally retained originals are accepted tradeoffs; no
  authentication/encryption/sync product redesign is planned.
- Plans 034–047 are not repeated wholesale. Only observed residual defects reopen 038/044/047; correction diagnostics
  in 056 retain 041/043 rather than replacing their protocols.
- Existing deferrals remain: version timestamp collisions, whole-replay atomicity, workspace locking, person-date
  editing, restore CLI, absent CI, link-type revalidation and multi-hop redirect handling.
- 069 addresses reading-file preflight aggregation, **not** the separately recorded first-error vault-note limitation.
- No migration runner, speculative schema/index, whole-photo-library inventory, location track, tasks, events or finance.

## First audit plans (2026-10-05, `726ffab`)

This subsection records the earlier selection; use the deep-audit sequence above for current follow-up execution.
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
