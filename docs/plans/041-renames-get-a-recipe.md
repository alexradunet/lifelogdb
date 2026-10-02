# Plan 041 (design): Issue 0001 gets its proposal — what a rename moves, and what happens to older stubs

> **Executor instructions**: This plan produces a **proposal (RFC) for the owner to decide**, not a schema or code
> change. Write the RFC exactly as scoped, update the two indexes, run the docs suites, and stop. The owner's decision
> is a separate step (a follow-up plan writes the decision, the cookbook recipe, the suites and the mutants). When done,
> set this plan's status row in `docs/plans/README.md` to `IN REVIEW (owner)`.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- docs/issues/0001-a-rename-has-no-recipe.md docs/contract/titles-and-wikilinks.md docs/rfcs internal/core/rename.go`
> If issue 0001 is already `proposed` or `resolved`, or `docs/rfcs/` already holds a rename proposal, STOP.

## Status

- **Priority**: P2 (direction; freeze gate item 1 needs every issue resolved)
- **Effort**: S for the RFC; M for the follow-up the owner's decision triggers
- **Risk**: LOW (text only)
- **Depends on**: 033 (it makes the writer repoint older stubs — the RFC describes that behaviour as one option); can be
  written before 033 lands, citing it as planned
- **Category**: direction
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

`docs/process.md` ("Before the freeze", item 1): every issue must be resolved or won't-fix before the freeze, and the
freeze is the first unreplayable write — the first real capture. Issue 0001 ("A rename has no recipe, and the contract
leaves open what moves with it") has been open since the writer was built: the contract says what a rename **is**
(`docs/contract/titles-and-wikilinks.md`, "Renames"), but not three things every writer must choose, so two writers write
different rows for the same rename. The audit found a fourth: renaming a page that is itself a redirect **target** leaves
a two-hop chain, and readers follow one hop, so mentions written under the oldest name are lost from backlinks and "the
days that name someone" (executed at `9ac130f`: X → Y → Z leaves Z with no backlinks from days that wrote `[[X]]`). The
writer has already chosen on all four (README "Renames"; plan 033 Step 4), so the cheapest path is a proposal that
states the choices and lets the owner accept or change them.

## Current state

- `docs/issues/0001-a-rename-has-no-recipe.md` — the three open choices: (1) does the old body move to the new page;
  (2) do the old page's typed links (`about`, `related`, …) stay on the stub or move; (3) renaming into a title that exists
  (a typo ghost into a person). Status `open`. Reproduce: `Sourdogh` with a body and a `related` link, renamed `Sourdough`.
- `docs/contract/titles-and-wikilinks.md:128-134` — "**Renames.** A title never changes (`pages_title_fixed`, D5). To fix
  one: create the new page, make the old page a one-line stub (`#REDIRECT [[New Title]]`) and add `links(kind='redirect',
  from=old, to=new)`. … A person's or a place's own title is its permanent handle and is not renamed … Consumers follow one
  hop — the days that name someone and backlinks count a stub's mentions as its replacement's …; the `redirect` row itself
  is never a backlink."
- `docs/decisions/D05-pages-and-day-pages.md:30` — "*Renames*: rejected — renaming silently repoints every `[[Old
  Title]]` in decades of prose, or …" (the reason titles are fixed).
- The writer's choices (README.md, "Renames move the text and the typed links"): the new page gets the body and the old
  page's typed links; the old page becomes the stub; into an existing title only when the old page is empty; otherwise
  refused. Code: `internal/core/rename.go`. Plan 033 Step 4 adds: older stubs that pointed at the renamed page are
  repointed to the new one (stub body and `redirect` row).
- There is no `docs/cookbook/rename.md`. The RFC index `docs/rfcs/README.md` has an empty table
  (`| id | title | status | issues | decision |`). Template: `docs/rfcs/template.md` (Problem, Options, Recommendation,
  Validation, Outcome). RFCs are numbered `NNNN`; this is **0001**.
- Links rows may be deleted (`lifelog_meta.deletes`, D11); `pages` rows may not (tombstones).

## Scope

**In scope**: `docs/rfcs/0001-a-rename-moves-the-text-and-its-typed-links.md` (create), `docs/rfcs/README.md` (one row),
`docs/issues/0001-a-rename-has-no-recipe.md` (status line `proposed` only), `docs/issues/README.md` (0001's status cell).

**Out of scope**: `schema.sql`, the contract page, the decisions, the cookbook, the suites, the Go code — all wait for the
owner's decision.

## Steps

### Step 1: Write the RFC

From the template, date 2026-10-02, status `draft`, answers `[0001](../issues/0001-a-rename-has-no-recipe.md)`. Under
**Problem**, restate the issue's three choices and add the fourth (a rename of a page that is a redirect target leaves a
chain; consumers follow one hop). Under **Options**, for each of the four choices give the options in a small table with
what each writes and its cost; none of them changes `schema.sql` (all are writer behaviour plus a recipe), so all are
additive-safe after the freeze:

1. *The text*: (a) moves to the new page, the stub holds only `#REDIRECT [[New]]` — the writer's choice; (b) stays on the
   old page above the redirect line — but a stub is "a one-line stub" in the contract, so (b) contradicts it.
2. *Typed links*: (a) move to the new page (delete and re-insert; links rows may be deleted) — the writer's choice; (b) stay
   on the stub — every typed-link read would then need the one-hop rule too.
3. *Into an existing title*: (a) only when the old page is empty (a typo ghost), else refused — the writer's choice;
   (b) merge the two texts — rejected by D5's reasoning (silent edits of prose); (c) always refuse.
4. *A rename of a redirect target*: (a) repoint every stub that pointed at it (rewrite its one line and its `redirect`
   row) — plan 033; (b) refuse the rename while stubs point at the page; (c) allow chains and make readers follow them
   (changes three cookbook recipes and the contract's "one hop").

**Recommendation**: 1a, 2a, 3a, 4a — they are what the writer does, they keep the contract's "one-line stub" and "one
hop", and none touches the schema. **Validation** (for the follow-up): a cookbook recipe `cookbook/rename.md` with the
statements of a rename (create or reuse the target, rewrite the body to the stub, insert the `redirect` row, move typed
links, repoint older stubs), run literally by the `named` or `links` suite on the issue's `Sourdogh` example and on the
X → Y → Z chain; a mutant per rule (e.g. "a rename leaves typed links on the stub", "a rename leaves an older stub
pointing at the old page"); the contract's "Renames" paragraph links the recipe. **Outcome**: empty.

### Step 2: Indexes and the issue

Add the RFC's row to `docs/rfcs/README.md` (`draft`, issues `0001`, decision `—`). Set issue 0001's `**Status:**` to
`proposed` and its row in `docs/issues/README.md` likewise (status values allowed there: open | proposed | resolved |
won't fix).

### Step 3: Docs suites

**Verify**: `go test ./tests -run 'TestSuites/document' -v` → `N/N` (the RFC is reachable from `docs/README.md` through
the RFC index; every link resolves). If the document suite checks that an RFC links an issue that exists, it passes.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `docs/rfcs/0001-…md` exists with Problem, Options (four choices), Recommendation, Validation, empty Outcome
- [ ] issue 0001 and its index row say `proposed`; the RFC index lists 0001
- [ ] status row for 041 is `IN REVIEW (owner)`

## STOP conditions

- The `document` suite refuses an RFC page for a structural reason (a required line) — follow what it asks if it is a
  format rule; report if it asks for content.
- You find the writer's behaviour differs from the README's "Renames" description — report it; the RFC must describe what
  the writer actually does.

## Maintenance notes

- After the owner accepts: a follow-up plan writes the decision (a new D-number, or a rewrite of D5's rename bullet),
  `cookbook/rename.md`, the suite expectations and the mutants, and links the recipe from the contract's "Renames"
  paragraph; then issue 0001 is `resolved`.
- If the owner picks 4b or 4c, plan 033 Step 4 must be revisited in `internal/core/rename.go`.
