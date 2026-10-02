# Plan 038: The habit completion recipe reads only the days it counts

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- docs/cookbook/habits.md internal/core/habits.go tests/habits_test.go tests/mutants_test.go docs/issues/README.md`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (same results; the habits suite runs the recipe literally and pins the counts)
- **Depends on**: none (030 adds issue 0007 — this plan adds 0008; if numbers clash, take the next free one)
- **Category**: perf
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

Completion over a period (`docs/cookbook/habits.md`, the last query) joins the active days to a subquery that
aggregates **every** measurement of every metric (`SELECT metric_id, day, max(value) … FROM measurement_values GROUP BY
metric_id, day`). Its cost grows with the whole `measurements` table, not with the habits or the period. Measured at
`9ac130f` on a synthetic database with 1 M readings: **3.3–3.7 s per call** for a 30-day window; `lifelog habits` took
2.3–3.1 s. The writer runs the same SQL on every `/habits` view and after **every check-in** (`lifelog done X`, the MCP
`check_in`). A correlated per-day lookup — the form the same page already uses for "the habits of :day" — measured
**13 ms** for 30 days and 141 ms for 10 years, with identical counts. The recipe is part of the contract (`docs/`), so the
change goes through an issue (`docs/process.md`: an issue records "a question the data could not answer" or a bug in
the writing application; a small fix that changes no decision skips the proposal).

## Current state

- `docs/cookbook/habits.md` — the fourth statement of the ```sql block (lines ~22–39):
  ```sql
  WITH RECURSIVE days(day) AS (
    SELECT :from_day
    UNION ALL
    SELECT date(day, '+1 day') FROM days WHERE day < :to_day
  ),
  active AS (
    SELECT h.metric_id, d.day
      FROM days d JOIN habit_periods h ON h.start_day <= d.day AND coalesce(h.end_day, '9999-12-31') >= d.day
  )
  SELECT m.name, count(*) AS active_days,
         sum(s.value IS 1) AS done, sum(s.value IS 0) AS not_done, sum(s.value IS NULL) AS not_recorded
    FROM active a
    JOIN metrics m ON m.id = a.metric_id
    LEFT JOIN (SELECT metric_id, day, max(value) AS value FROM measurement_values GROUP BY metric_id, day) s
           ON s.metric_id = a.metric_id AND s.day = a.day
   GROUP BY m.id
   ORDER BY m.name;
  ```
  The third statement ("the habits of :day") already uses the correlated form:
  `CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day) …`.
- `internal/core/habits.go:~145-170` — `Store.Completion(ctx, from, to)` runs a copy of the same SQL with
  `sql.Named("from_day", from), sql.Named("to_day", to)`.
- `tests/habits_test.go:55-98` runs the block literally: it requires exactly 4 statements (`len(B) == 4`), and pins
  `comp == "vitamin_d|6|2|1|3; water_before_coffee|9|1|0|8"` for 2026-09-28..2026-10-11, plus "two check-ins on one day
  count once" and "a corrected check-in counts as corrected".
- `tests/mutants_test.go:138` anchors on the text `sum(s.value IS 0) AS not_done` — **keep that exact text** (alias `s`,
  column `value`).
- Issue convention: `docs/issues/template.md`; index `docs/issues/README.md` (a table row per issue).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Habits suite | `go test ./tests -run 'TestSuites/habits' -v` | `habits: N/N met expectations` |
| Its mutants | `go test ./tests -run 'TestMutants/.*habits' -v` | every mutant caught |
| Core | `go test ./internal/core` | ok |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `docs/issues/0008-habit-completion-reads-every-measurement.md` (create), `docs/issues/README.md` (one
row), `docs/cookbook/habits.md` (the completion statement only), `internal/core/habits.go` (`Completion`'s SQL only),
`internal/core/habits_test.go` (an equivalence test).

**Out of scope**: the other three statements; `schema.sql` (no index change is needed — check Step 3); `tests/` (no
suite change should be needed; if one is, STOP).

## Git workflow

Branch `advisor/038-habit-completion`; message e.g. `cookbook/habits: completion looks up each active day instead of
aggregating every reading (issue 0008)`, body with the before/after timing if you measured it + `Ran: …` + trailer.

## Steps

### Step 1: The issue

Create `docs/issues/0008-habit-completion-reads-every-measurement.md` from the template: date 2026-10-02; status
`resolved`; seen in "the writer's habits view and check-in, on a synthetic database of 1 000 000 readings"; what happened
(the completion query aggregates all of `measurement_values` before joining the period's days: 3.3–3.7 s for 30 days at
1 M readings; it runs after every check-in); reproduce (a fresh database, one habit, 1 M readings of other metrics spread
over 30 years, run the completion block for 30 days); rules involved (`cookbook/habits.md`, D24); resolution (plan 038:
the completion looks up each active day's value with the same correlated read as "the habits of :day"). Add the row to
`docs/issues/README.md`.

### Step 2: The recipe

Replace the final `SELECT … FROM active a … ORDER BY m.name;` of the completion statement with:

```sql
SELECT m.name, count(*) AS active_days,
       sum(s.value IS 1) AS done, sum(s.value IS 0) AS not_done, sum(s.value IS NULL) AS not_recorded
  FROM (SELECT a.metric_id,
               (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = a.metric_id AND v.day = a.day) AS value
          FROM active a) s
  JOIN metrics m ON m.id = s.metric_id
 GROUP BY m.id
 ORDER BY m.name;
```

Keep the `WITH RECURSIVE days … active AS (…)` part unchanged, and keep the prose under the block unchanged ("Two
check-ins on one day count once (the higher wins)" stays true: `max`).

**Verify**: `go test ./tests -run 'TestSuites/(habits|cookbook)' -v` → `N/N` for both; `go test ./tests -run
'TestMutants/.*habits' -v` → all caught (in particular "cookbook/habits counts a day not recorded as not done").

### Step 3: The writer runs the recipe's SQL

Copy the new statement into `Store.Completion` in `internal/core/habits.go`, with the same named parameters.
Check the plan uses an index: run `EXPLAIN QUERY PLAN` of the new statement on a fresh database in a test or a scratch
program and confirm the correlated subquery reads `measurements` through an index (`SEARCH … USING INDEX …`), not a
`SCAN`. Paste the plan lines into the commit body.

Add `TestCompletionMatchesTheOldQuery` to `internal/core/habits_test.go`: on a fresh store, two habits with periods
(one stopped and restarted), 200 random check-ins (fixed seed, values 0/1, some days twice, a few corrected and one
retracted through `Correct`), and readings of a non-habit metric on the same days; run the **old** SQL (kept verbatim in
the test as a constant) and `Completion` over three windows (inside, overlapping a gap, spanning everything) and require
identical rows.

**Verify**: `go test ./internal/core -run Completion -v` → PASS.

### Step 4: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

The habits suite (literal recipe, pinned counts) and its mutants; `TestCompletionMatchesTheOldQuery` (old vs new on
random data).

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "GROUP BY metric_id, day" docs/cookbook/habits.md internal/core/habits.go` → no match
- [ ] `grep -n "sum(s.value IS 0) AS not_done" docs/cookbook/habits.md` → one match
- [ ] issue 0008 exists and is indexed; status row for 038 updated

## STOP conditions

- The habits suite's pinned counts change — the new query is not equivalent; report the difference.
- `EXPLAIN QUERY PLAN` shows a full scan of `measurements` in the correlated subquery — report it (an index change is a
  schema change and needs its own issue and proposal).
- A suite or mutant in `tests/` must change for this — STOP and report why.

## Maintenance notes

- The cookbook and `internal/core/habits.go` hold the same SQL; change both together (the suite runs the cookbook's, the
  core test compares behaviour).
