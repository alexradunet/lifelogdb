# Plan 006: Reference hygiene and wording nits — R63/R73, D12, D13, R71, the WAL backports

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

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (execute last of the SCHEMA.md prose plans if possible — it touches §8, D12, D13, §2.6)
- **Category**: docs
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

Five small defects, each verified mechanically during the audit:

1. **[R63] and [R73] are defined in §8 but cited nowhere in §1–§7** (a
   script over all `[Rnn]` markers found zero citations). §8's intro
   promises "What each source contributed to the decisions above" — two
   entries currently contribute to nothing. R63 (UTS #39, invisible
   characters) belongs with D5's title-safety decision; R73 (R\*Tree) is a
   leftover from a cut §7 row and should be dropped (reference numbers are
   identifiers, not a count — the gaps are intentional, so a hole where R73
   was is fine).
2. **D12 says "`created_at` (on every table)"** — false: only `entities`,
   `links` and `measurements` have it (`lifelog_meta.instants` already has
   the correct phrasing, "on every table that has it"). A cold implementer
   of D12 would mis-model the registries.
3. **D13 writes `PRAGMA application_id = 'LIFE'`** — the DDL (§3) sets the
   integer `0x4C494645`; the quotes could make a reader try the string form,
   which SQLite does not accept there.
4. **R71's entry says "(§3.1.9)"** — `§` everywhere else in this document
   means *this* document, and §3.1 does not exist; it is RFC 7946's section.
5. **§2.6's WAL sentence claims "every version from 3.7.0 to 3.51.2 has a
   WAL race"** — but R65 itself records two backports with the fix inside
   that range (3.44.6 and 3.50.7). The 3.51.3 floor stays (conservative and
   simple); the universal claim is false as written.

## Current state

- `SCHEMA.md:1137+` — D5's Sources line (end of the D5 decision):

  ```
  - **Sources.** Kaydet [R41]; FxLifeSheet [R9][R42]; Windows reserved names [R58].
  ```

- `SCHEMA.md:2228–2231` — the R63 entry (§8):

  ```
  - **[R63]** Unicode Technical Standard #39, *Unicode Security Mechanisms* —
    <https://www.unicode.org/reports/tr39/> (with UAX #31, *Identifiers*): default-ignorable and bidi
    characters are dropped or rejected before identifiers are compared, because they are invisible.
    → §2.4 (the invisible-character rule).
  ```

- `SCHEMA.md:2291–2293` — the R73 entry (§8, last entry of the Location
  block):

  ```
  - **[R73]** SQLite: *The SQLite R\*Tree Module* — <https://sqlite.org/rtree.html> A spatial index as a
    virtual table, present only in builds compiled with `SQLITE_ENABLE_RTREE`. → §7.
  ```

- `SCHEMA.md:1290–1291` — D12's decision text:

  ```
  - **Decision.** No revision or history tables. The temporal metadata is row-level: `created_at` (on every
    table), `updated_at`, the tombstone, `source`, and the append-only facts.
  ```

- `SCHEMA.md:1306` — D13:

  ```
  `PRAGMA application_id = 'LIFE'` lets `file(1)` and
  ```

- `SCHEMA.md:2287` — R71:

  ```
  antimeridian is cut in two (§3.1.9). → D21.
  ```

- `SCHEMA.md:334–336` — §2.6:

  ```
  executed) — none of these is stored in the file, and `PRAGMA foreign_keys` is a silent no-op inside
  a transaction (executed). It also refuses to run on a SQLite older than **3.51.3**: every version from
  3.7.0 to 3.51.2 has a WAL race in which a write that lands while two checkpoints overlap can be lost
  ```

  (R65's own §8 entry, `SCHEMA.md:2243+`, already records: "fixed in 3.51.3
  and 3.53.0; backports 3.44.6 and 3.50.7".)

- Note: `tests/schema/document.py` does not check §8 citations (verified),
  so no suite change is needed — but the suites must still pass, and the
  `instants` meta row (`SCHEMA.md:513`) already says "on every table that
  has it" and stays untouched.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed` |
| Citation completeness | `python - <<'EOF'` (the script below) | no output |

Citation check (run from the repo root):

```bash
python - <<'EOF'
import re
doc = open('SCHEMA.md', encoding='utf-8').read()
body, refs = doc.split('## 8. References', 1)
cited = set(int(n) for n in re.findall(r'\[R(\d+)\]', body))
defined = set(int(n) for n in re.findall(r'\*\*\[R(\d+)\]\*\*', refs))
print("cited but not defined:", sorted(cited - defined))
print("defined but never cited:", sorted(defined - cited))
EOF
```

Expected after this plan: both lists empty.

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (D5 Sources; R63 entry; R73 entry — deleted; D12; D13; R71; §2.6)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- `tests/` — no suite reads any of these sentences.
- The `lifelog_meta` `instants` row (`SCHEMA.md:513`) — already correct.
- R65's §8 entry — it already records the backports; only §2.6's sentence
  overclaims.
- Any other §8 entry; any other decision text.

## Git workflow

- One commit on `master`, repo message style, e.g.
  `SCHEMA.md: every reference is cited, D12/D13 say what the DDL does, the WAL backports break the universal claim`.
  Say what you ran (`tests/run_all.py`, 20/20).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Cite R63 from D5; delete R73

1. D5's Sources line becomes:

   ```
   - **Sources.** Kaydet [R41]; FxLifeSheet [R9][R42]; Windows reserved names [R58]; Unicode security [R63].
   ```

2. R63's entry: change its last line
   `→ §2.4 (the invisible-character rule).` to
   `→ D5, §2.4 (the invisible-character rule).`

3. Delete the whole R73 entry (the three lines quoted in "Current state").
   Leave the resulting gap in the reference numbers — §8's intro says the
   gaps are intentional.

**Verify**: the citation script above prints both lists empty.

### Step 2: D12 tells the truth about created_at

In `SCHEMA.md:1290–1291`, change
`` `created_at` (on every table), `updated_at` ``
to
`` `created_at` (on every table that has it — entities, links, measurements), `updated_at` ``

**Verify**: `grep -n "on every table that has it" SCHEMA.md` → two hits
(D12 and the `instants` meta row inside §3).

### Step 3: D13's application_id literal

In `SCHEMA.md:1306`, change

```
  `PRAGMA application_id = 'LIFE'` lets `file(1)` and
```

to

```
  `PRAGMA application_id = 0x4C494645` ('LIFE') lets `file(1)` and
```

**Verify**: `grep -c "application_id = 'LIFE'" SCHEMA.md` → `0`

### Step 4: R71 names RFC 7946

In `SCHEMA.md:2287`, change `(§3.1.9)` to `(RFC 7946 §3.1.9)`.

**Verify**: `grep -n "RFC 7946 §3.1.9" SCHEMA.md` → one hit.

### Step 5: The WAL sentence admits the backports

In `SCHEMA.md:334–336`, change

```
every version from
3.7.0 to 3.51.2 has a WAL race
```

to

```
every version from
3.7.0 to 3.51.2 — bar the backports 3.44.6 and 3.50.7 [R65] — has a WAL race
```

The floor stays 3.51.3 (the next sentence, "Migrations need 3.53 (D13)",
is untouched).

**Verify**: `grep -n "3.44.6" SCHEMA.md` → two hits (§2.6 and R65's entry).

### Step 6: Run the suites

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`, and the citation script prints two empty lists.

## Test plan

- No new suite expectations: `document.py` does not check §8 citations, and
  none of these sentences is executed. The verification is the citation
  script (a one-off check, inlined above) plus the existing suites.
- If the owner wants citation completeness machine-checked forever, that is
  a separate small change to `document.py` — deliberately not bundled here.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] The citation script prints both lists empty
- [ ] `grep -c "R73" SCHEMA.md` → `0`
- [ ] `grep -n "on every table that has it" SCHEMA.md` → 2 hits
- [ ] `grep -c "application_id = 'LIFE'" SCHEMA.md` → `0`
- [ ] `grep -n "RFC 7946 §3.1.9" SCHEMA.md` → 1 hit
- [ ] `grep -n "3.44.6" SCHEMA.md` → 2 hits
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git status` shows changes only in `SCHEMA.md` (+ `plans/README.md`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts don't match the live file (drift since `6058f24`).
- Deleting R73 leaves a dangling `[R73]` citation somewhere the script
  missed (e.g. inside a code span) — report the location.
- A suite fails — none of these sentences is suite-read, so a failure means
  something else changed; report it.

## Maintenance notes

- New references must be cited from a decision or section the day they are
  added; the citation script in "Commands" is the cheap guard to re-run.
- The 3.51.3 floor is an operational rule, not a claim about history —
  future SQLite versions that change the WAL story should update §2.6,
  `lifelog_meta.sqlite` and R65 together.
