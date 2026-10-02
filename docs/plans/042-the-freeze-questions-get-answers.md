# Plan 042 (decision prep): Five questions the freeze will make permanent, each with the writer's current answer

> **Executor instructions**: This plan has two phases. **Phase A** needs no executor: it is the decision sheet below,
> for the owner. **Phase B** runs only after the owner has filled in the "Owner decisions" table at the end of this file
> (a dated record): apply exactly the edits listed for the chosen options, run the checks, and stop. If a row of the
> table is empty, skip that question and leave it open. When done, update the status row for this plan in
> `docs/plans/README.md`.
>
> **Drift check (run first, Phase B)**: `git diff --stat cad659b..HEAD -- docs/schema/schema.sql docs/contract/titles-and-wikilinks.md docs/contract/connections.md docs/architecture/non-goals.md internal/text/text.go tests/mutants_test.go`
> On a mismatch with a quoted line below, STOP.

## Status

- **Priority**: P2 (direction; `docs/process.md` "Before the freeze" items 1, 3 and 5)
- **Effort**: S per question (owner-bound)
- **Risk**: LOW
- **Depends on**: none; Q-C and Q-D are independent of plan 039 but touch the same vector table (run one after the other)
- **Category**: direction
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The freeze ([D13](../decisions/D13-migrations-and-freeze.md)) is the first write that cannot be replayed from an import —
in practice the first real capture into the canonical `life.db`. After it, the schema changes only additively and every
comment inside `schema.sql` and every vector of the contract is permanent text. Plan 021 left three questions open for the
owner (Q2, Q4, Q5), and two contract points were deferred "until a second writer exists" (the plans index, "Findings
considered and rejected"): the `Cn` rule's Unicode version and what "`#REDIRECT` (any case)" means. The deleted Python
reference used to settle both implicitly; now only the Go writer does, and the contract must say what any writer must do.
Each is cheap now and fixed forever after.

## Phase A — the decision sheet

### Q-A (plan 021 Q2): keep or cut `pages_fts_delete`

- Today: `docs/schema/schema.sql:120-122`:
  ```sql
  CREATE TRIGGER pages_fts_delete AFTER DELETE ON pages BEGIN
    INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
  END;
  ```
  It can never fire while `pages_no_delete` (`schema.sql:387`) refuses every `DELETE` on `pages` (D11). No suite expectation
  or mutant covers it (plan 016 recorded this).
- Options: **(1) cut it** — one less statement frozen forever; if a future owner drops `pages_no_delete`, the FTS index
  would go stale on a delete and the FTS integrity check would report it. **(2) keep it** — a guard for that future owner;
  costs nothing at runtime; stays untested.
- The writer: indifferent (it never deletes a page).
- Advisor's recommendation: **(1) cut** — the repo's rule is "anything still in the schema at the freeze stays for good, so
  cutting happens before it" (AGENTS.md), and the integrity check already covers the case.

### Q-B (plan 021 Q4): the export / snapshot / off-box copy non-goal

- Today: `docs/architecture/non-goals.md:20`, the row "Markdown export of the prose; nightly snapshots, restore and an off-box
  copy; a CSV dump; continuous replication", reopen when "**Before the freeze** (D13) at the latest". The row itself says:
  "Until one exists there is **no second copy** of `life.db`". `docs/process.md` item 3 requires the owner to decide it.
- Options: **(1) kept out** — the row's "Reopen when" becomes a dated owner decision and a new trigger (e.g. "The first
  time the owner wants a second copy, or a disk failure"); **(2) reopened** — an issue records it and a proposal follows.
- The writer: has no export or backup command (README).
- Advisor's recommendation: none — this is the owner's risk decision (the only copy of decades of notes and health data).
  Note that the threat model's first row says "nothing recovers it: no second copy of the file is kept".

### Q-C (plan 021 Q5): readers set `trusted_schema = OFF` too?

- Today: `docs/contract/connections.md` asks writers for `trusted_schema=OFF` (the schema "may call only side-effect-free
  functions"); readers "need no setup but must be read-only" (`connections.md:37`). The writer's own readers already set it
  (`internal/db/db.go:96`: `q.Add("_pragma", "trusted_schema(0)")`).
- Options: **(1) yes** — one sentence in connections.md: readers open with `mode=ro` **and** `PRAGMA trusted_schema = OFF`,
  so a file someone else wrote cannot make a reader run a function with side effects through a view or trigger; **(2) no**.
- Advisor's recommendation: **(1) yes** — it matches what the writer does and costs a reader one pragma.

### Q-D: the Unicode version of the `Cn` title rule

- Today: `docs/contract/titles-and-wikilinks.md:138-141`: "Every writer must be stricter than the DDL in one way: it also
  rejects code points Unicode has not assigned yet (category `Cn`) …" — no version named. The writer pins it:
  `internal/text/text.go:21-23`, `const AssignedVersion = "15.0.0"` ("pinned, not the newer tables Go ships, so the titles
  this writer accepts do not change with a Go upgrade").
- Options: **(1) name Unicode 15.0 in the contract** and add a vector: a title made of a code point assigned in a later
  version (for example one from Unicode 16.0) is not a link; **(2) "the writer's Unicode version"** — two writers may then
  disagree on which titles are valid.
- Advisor's recommendation: **(1)**. A later move to a newer version is then an explicit contract change.

### Q-E: what "`#REDIRECT` (any case)" means

- Today: `titles-and-wikilinks.md:35-36`: "A body that starts — after any whitespace — with `#REDIRECT` (any case), then at
  least one whitespace character …, then `[[`, is a rename stub". The writer (`text.go:83-91`, `IsStub`) compares the first
  9 **bytes** with `strings.EqualFold`, which in effect is ASCII case-insensitive: `#ReDiReCt [[A]]` is a stub, `#REDİRECT [[A]]`
  (dotted capital I, U+0130) is not and links `A` (executed by the docs audit at `9ac130f`).
- Options: **(1) ASCII letters in any case** — write that, and add two vectors (`#ReDiReCt [[A]]` → no links;
  `#REDİRECT [[A]]` → `A`); **(2) Unicode case-insensitive** — the writer must change, and "any case" needs a defined fold.
- Advisor's recommendation: **(1)** — it is what the writer does and what most languages do with an ASCII keyword.

## Phase B — the edits for each choice

Apply only the rows the owner decided. Every edit to `docs/` follows AGENTS.md: one home per rule, current truth only, no
app named.

- **Q-A (1) cut**: delete the three lines of `pages_fts_delete` from `schema.sql`; update the totals line in
  `docs/schema/README.md` (the `document` suite checks the totals — read its message); remove any mention in
  `docs/` pages (`grep -rn pages_fts_delete docs --include=*.md` — ignore `docs/plans/`). Run `go generate ./...` (the
  embedded copy). **Q-A (2) keep**: add to the trigger's body a comment line inside the `CREATE` statement:
  `-- unreachable while pages_no_delete stands: kept so a dropped guard cannot leave the index stale`.
- **Q-B (1)**: rewrite the row's "Reopen when" cell with the owner's dated decision and new trigger. **Q-B (2)**: create
  an issue from the template and set the row's cell to link it.
- **Q-C (1)**: in `connections.md`, after the `mode=ro` sentence, add: "Readers also set `PRAGMA trusted_schema = OFF`, as
  writers do." (no *executed* claim).
- **Q-D (1)**: in the `Cn` paragraph, write "… code points Unicode **15.0** has not assigned (category `Cn`) …"; add a
  vector row with a code point unassigned in 15.0 and assigned later (check: `unicode.Is(rangetable.Assigned("15.0.0"), r)`
  is false and the code point is in Unicode 16.0's `UnicodeData.txt`; e.g. the block "Symbols for Legacy Computing
  Supplement", U+1CC00..) written as `\uXXXX` per the table's convention (5-digit code points: check how the table writes
  them — if only 4-digit `\uXXXX` exists, pick a BMP code point assigned in 16.0, e.g. from "Garay" or "Kirat Rai" ranges
  — verify in UnicodeData 16.0), result `—`.
- **Q-E (1)**: change "`#REDIRECT` (any case)" to "`#REDIRECT` (its ASCII letters in any case)"; add the two vector rows.

After the edits: `go generate ./... && go vet ./... && go test ./...` → exit 0. A new vector the writer does not reproduce is
a writer bug: STOP and report it (do not change the vector to fit). If a cut statement is anchored by a mutant in
`tests/mutants_test.go`, remove that mutant and update the count in `tests/README.md` in the same commit, saying so.

Then append to `docs/plans/README.md`'s "Open questions for the owner" each answered question's answer, dated, as the index
does for Q3 and Q6.

## Done criteria (Phase B)

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] each decided question's edit is in place; undecided ones untouched
- [ ] the plans index records the answers; status row for 042 updated

## STOP conditions

- The owner's decision table is empty — Phase B does not start.
- A chosen option needs a schema change other than cutting `pages_fts_delete` — STOP: that needs an issue and a proposal.

## Owner decisions

| question | choice | date | note |
|---|---|---|---|
| Q-A `pages_fts_delete` | | | |
| Q-B export / snapshot non-goal | | | |
| Q-C readers `trusted_schema` | | | |
| Q-D `Cn` Unicode version | | | |
| Q-E `#REDIRECT` case | | | |
