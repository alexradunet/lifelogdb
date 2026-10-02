# Plan 005: Make SCHEMA.md fully writer-neutral — the app is being deleted

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 6058f24..HEAD -- SCHEMA.md`
> If SCHEMA.md changed since this plan was written, compare the
> "Current state" excerpts against the live file before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none (but it rewrites D14, which mentions `app/`; execute
  before or together with the app's deletion)
- **Category**: direction / consistency (the contract must stand alone)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

The owner is deleting `app/` (the Go writer). SCHEMA.md is the product and
promises to be implementable without it — but four spots lean on the app as
if it were part of the contract: D14 names `app/`, "one Go binary" and
`app/README.md`; §2.4 says "the app uses goldmark"; §2.4 says "The app is
stricter in one way" about rejecting unassigned code points (Cn) — leaving
the *normative* status of that rule ambiguous for the next writer, even
though its rationale (casefold stability of `title_key`) binds every
writer; and §2.4's "40 000 generated strings" claim cites "its predicate"
whose executor is actually the Python reference in `tests/` (which
survives). After this plan, a developer building a new writer in any
language reads the contract and nothing else — which is exactly the
document's own rule (AGENTS.md: "Language-neutral. The contract must be
implementable without reading `app/`").

This plan does **not** delete `app/` and does not edit `AGENTS.md`'s
app-facing sections (the owner rewrites those when the deletion happens).

## Current state

- `SCHEMA.md:170–172` (§2.4, first paragraph of the save contract's rules):

  ```
  A conformant CommonMark parser
  yields exactly this, so nothing is hand-parsed (the app uses goldmark; the reference in `tests/` uses
  markdown-it-py [R59], which loses a code span that follows an unclosed `[`, where the CommonMark
  reference implementation keeps it).
  ```

- `SCHEMA.md:192–196` (§2.4, the invalid-target rule):

  ```
  before inserting — its predicate agrees with the DDL's CHECKs on more than 40 000 generated
  strings — and creates each target inside its own `SAVEPOINT` (§6.13), so even a target the
  ```

  (the sentence begins "The app checks the title rules / before inserting —")

- `SCHEMA.md:251–253` (§2.4, the Titles paragraph):

  ```
  The app is stricter in one way: it also rejects code points Unicode has not assigned yet (category
  `Cn`), whose case fold a later Unicode version could define — which would silently change
  `title_key` (executed). Unicode promises a stable case fold only for assigned characters, and formally
  ```

  The `(executed)` marker is real and stays: the Cn difference is tested in
  `tests/schema/pages.py` (see `tests/wikilinks/title_fuzz.py`'s header:
  "schema/pages.py tests that difference on its own") — no app needed.

- `SCHEMA.md:1328–1330` (D14, second decision bullet):

  ```
  - **The writing application** is `app/`: one Go binary that is the CLI, the REST API and the MCP
    server, so the owner's UIs, AI agents and importers all write through it. Its own decisions are in
    `app/README.md`.
  ```

- `SCHEMA.md:3` (Status line) says the next step is "the capture path" —
  app-neutral already, fine.
- Generic uses of "the app" meaning "whatever the writing application is"
  (e.g. §6.1 "the app keeps it as `:page_id`") are **fine** — they mean "the
  writer", not the Go binary. Leave them.
- Conventions that bind this edit (AGENTS.md): *language-neutral, vectors
  in tests; a decision is rewritten in place, D-number stable; "current
  truth and nothing else".*

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed` |
| No dangling app refs | `grep -n "app/\|goldmark" SCHEMA.md` | no hits |

No Go commands: no DDL change; and the app is on its way out (if it still
exists, nothing in it depends on these prose spots).

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (the four spots above; nothing else)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- `AGENTS.md` — its app-facing rows and sections are the owner's to rewrite
  when the deletion happens.
- `tests/` — no suite checks these sentences; nothing to change. (The
  "(executed)" claims' homes are `tests/schema/pages.py` and
  `tests/wikilinks/`, both app-independent.)
- Deleting `app/` itself — a separate, owner-driven act; this plan only
  stops the document from depending on it.
- §2.4's vectors, the save contract's steps, D3/D19 — the rules are
  app-neutral already.

## Git workflow

- One commit on `master`, repo message style, e.g.
  `SCHEMA.md: the contract stands without the app — D14 neutral, the Cn title rule is every writer's`.
  Say what you ran (`tests/run_all.py`, 20/20).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: D14 becomes writer-neutral

Replace `SCHEMA.md:1328–1330` with:

```
- **The writing application** is one stack the owner controls — one codebase, in whatever language —
  that carries the insert conventions (§2.2, §6.1) and exposes them as a CLI, a REST API and an
  agent surface, so the owner's UIs, AI agents and importers all write through it (principle 3).
  Its own engineering decisions live outside this document: it implements the schema, it never
  defines it.
```

(D14's number is stable; the decision — one writing application with
CLI/API/agent faces — is unchanged; only the Go/`app/` identification is
gone.)

**Verify**: `grep -n "app/README\|Go binary\|MCP" SCHEMA.md` → no hits.

### Step 2: §2.4 drops goldmark

In `SCHEMA.md:170–172`, replace the parenthetical
`(the app uses goldmark; the reference in \`tests/\` uses\nmarkdown-it-py [R59], which loses …)`
with:

```
(the reference implementation in
`tests/` uses markdown-it-py [R59], which loses a code span that follows an unclosed `[`, where the CommonMark
reference implementation keeps it)
```

i.e. the sentence becomes: "A conformant CommonMark parser yields exactly
this, so nothing is hand-parsed (the reference implementation in `tests/`
uses markdown-it-py [R59], … keeps it)."

**Verify**: `grep -c "goldmark" SCHEMA.md` → `0`

### Step 3: The title predicate is the reference's

In `SCHEMA.md:192–194`, change

```
The app checks the title rules
before inserting — its predicate agrees with the DDL's CHECKs on more than 40 000 generated
strings —
```

to

```
A writer checks the title rules
before inserting — the reference predicate in `tests/` agrees with the DDL's CHECKs on more than 40 000
generated strings —
```

**Verify**: `grep -n "reference predicate" SCHEMA.md` → one hit, §2.4.

### Step 4: The Cn rule becomes every writer's obligation

In `SCHEMA.md:251–252`, change

```
The app is stricter in one way: it also rejects code points Unicode has not assigned yet (category
`Cn`),
```

to

```
A writer is stricter in one way, and every writer must be: it also rejects code points Unicode has
not assigned yet (category `Cn`),
```

The rest of the sentence (rationale, `(executed)`, the [R74] citations, the
known limit) stays as is.

**Verify**: `grep -n "every writer must be" SCHEMA.md` → one hit, §2.4.

### Step 5: Run the suites

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`. Then `grep -n "app/\|goldmark" SCHEMA.md` → no hits
(the Status line's "capture path" and generic "the app" phrases are
allowed; only path references and the parser brand must be gone).

## Test plan

- No new tests: these are prose neutrality fixes; no suite reads the four
  sentences (verified during the audit: `title_fuzz.py` and `pages.py`
  execute the Cn claim without the app; the vectors suites are untouched).
- The regression guard is Step 5's grep: the contract must not regain app
  references.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -n "app/\|goldmark" SCHEMA.md` → no hits
- [ ] `grep -c "every writer must be" SCHEMA.md` → `1`
- [ ] `grep -c "(executed)" SCHEMA.md` is unchanged from before this plan
      (no executed marker was lost)
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git diff SCHEMA.md` shows changes only in the four spots
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts don't match the live file (drift since `6058f24`).
- You find more *normative* app dependencies in §1–§7 (a rule stated in
  terms of what "the app" does, where a third-party writer could not know
  what to do) — report them; do not rewrite rules beyond the four spots
  without instruction.
- Any suite fails — especially `document.py` ("the rules live in the file")
  objecting to the new phrasing; report the exact expectation.

## Maintenance notes

- When `app/` is actually deleted, `AGENTS.md` needs its own pass (the
  table rows for `app/`, the build-order text, the A-numbers) — that is the
  owner's edit, not this plan's.
- A future writer in another language should be pointed at §3 + §2 + §6 +
  `tests/wikilinks/vectors.py` (AGENTS.md's "Building another application"
  section already says this); the Cn rule is now part of that obligation.
- Reviewer should confirm D14 still cites principle 3 and that no
  "superseded" narrative crept in (decisions are rewritten in place).
