# Plan 029: The real run gets exactly what the trial has, or says loudly that it did not

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/importer/replay.go internal/importer/inspect.go internal/importer/importer_test.go internal/core/write.go internal/core/imports.go internal/core/habits.go internal/api/habits.go internal/api/import.go cmd/lifelog/main.go`
> Plans 026–028 do not touch these files except `cmd/lifelog/main.go` (027: the `serve` case only). On any other
> mismatch with the excerpts below, STOP.

## Status

- **Priority**: P1 — must land **before the first real import** (the real `life.db` cannot have rows deleted, D11)
- **Effort**: M
- **Risk**: MED (touches the replay path; the existing replay test guards it)
- **Depends on**: none; run **before plan 030** (both edit `internal/importer/importer_test.go`)
- **Category**: bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

An import runs first on a trial copy (`trial.db`), then *replay* writes the same decisions into the real
`life.db`, and *status* against the real file is supposed to show the trial's counts and no mismatches
(`docs/guides/importing.md`, "Trial, then the real run"). Each defect below was confirmed by the importer audit
(executed on throwaway workspaces at `9ac130f`) and re-read by the advisor:

1. **A second correction is lost.** Correcting a reading 48 → 47 and then 47 → 46 on the trial records only the
   first in `corrections.json`: the second corrects the *correction row*, which has no `import_key`, so it is not
   recorded. The real file gets 47, the trial has 46, and the replay reports **no differences** (it compares only
   counts).
2. **An agent's correction is replayed as the owner's.** A correction sent by `agent:*` is recorded and replayed with
   source `cli`. The guide says the single-row corrections are the owner's, outside the model's tools, and the README
   says a replay carries only the facts and the notes.
3. **`--db` is silently replaced by the trial** when it equals `$LIFELOG_DB` — so the README's own setup
   (`export LIFELOG_DB=…`) makes "status against the real file" check the trial: a false green on the real run.
4. **Re-registering metrics reopens a stopped habit**, and fails ("periods of one habit never overlap") once a
   later period exists — which aborts a replay, since the replay registers metrics first.
5. **A red result exits 0**: `import setup` with failed integrity checks, and `import replay` with differences or
   failed integrity checks, both return success to a script or a model.
6. **A replay writes before it knows it can finish**: the target is initialised before any gate is checked, the
   trial's vault plan is applied without being re-validated against the target, and an error part-way loses the
   record of what was already committed.

## Current state

- `internal/api/habits.go:12-24`:
  ```go
  // correctTo corrects or retracts a reading; with an import workspace, a correction of an imported reading is
  // also written to the workspace, so a replay makes it again (docs/guides/importing.md).
  func (h *server) correctTo(r *http.Request, src string, id int64, value *float64) (*Entity, error) {
  	fix, key, err := h.s.Correct(r.Context(), src, id, value)
  	if err != nil {
  		return nil, err
  	}
  	if h.ws != nil && core.IsImport(key.Source) && key.Key != "" {
  		if err := h.ws.RecordCorrection(key); err != nil {
  			return nil, err
  		}
  	}
  	return h.measurementEntity(r.Context(), fix)
  }
  ```
- `internal/core/write.go:614-636`:
  ```go
  // Correct also reports the corrected row's sender key, so an import workspace can carry the correction to a replay.
  func (s *Store) Correct(ctx context.Context, source string, wrong int64, value *float64) (id int64, key CorrectedKey, err error) {
  	err = s.Do(ctx, source, func(t *Tx) (e error) {
  		if key.Source, key.Key, key.Metric, e = t.MeasurementKey(wrong); e != nil {
  			return e
  		}
  		id, e = t.Correct(wrong, value)
  		return
  	})
  	...
  	key.Value = value
  	return
  }

  // CorrectedKey names a corrected reading by its sender's key, and the value it now has (nil = retracted).
  type CorrectedKey struct {
  	Source string   `json:"source"`
  	Key    string   `json:"import_key"`
  	Metric string   `json:"metric"`
  	Value  *float64 `json:"value"`
  }
  ```
  `Tx.Correct` (`write.go:518-534`) inserts the correction with the caller's source and **no** `import_key`.
- `internal/importer/replay.go:118-149` — `replayCorrections` runs **all** corrections in one `ts.Do(ctx, "cli", …)`;
  for each it finds the keyed row (`t.MeasurementByKey(c.Source, c.Metric, c.Key)`), follows the chain
  (`t.CurrentOf(id)`), skips when the value is already so, else `t.Correct(last, c.Value)`.
- `internal/importer/replay.go:31-47` — `Replay` initialises the target (`db.Init`) right after the "target is the
  trial" check, before reading any gate; `:49-61` applies the trial's plan with `Appended` reset but never calls
  `validatePlan` on the target; `:62` ignores `Gate`'s error; `:112-114` `compare(res.Trial, res.Counts)` compares
  counts only.
- `internal/importer/vault.go:88-138` — `validatePlan(ctx, s, p)` recomputes each note's `Action` and `Problems`
  against a store. `ApplyVault` (`vault.go:189-210`) refuses a plan whose notes have problems.
- `cmd/lifelog/main.go:138-147`:
  ```go
  if o.workspace != "" {
  	...
  	if o.db == "" || os.Getenv("LIFELOG_DB") == o.db {
  		o.db = ws.TrialDB() // an import works on its trial database unless --db names another
  	}
  }
  ```
  `parse` (`main.go:73-108`) starts with `o := opts{db: os.Getenv("LIFELOG_DB"), …}` and sets `o.db` from `--db`.
- `cmd/lifelog/main.go:372-389` — `import setup`: `if !res.OK { return printJSON(res) }` (exit 0).
  `main.go:425-430` — `import replay`: `return doAction(o, c, "replay", map[string]string{"to": o.to})`; `doAction`
  returns only the HTTP error, so a 200 replay with differences exits 0.
- `internal/importer/inspect.go:51-52` (inside `RegisterMetrics`): `if m.Since != "" { if err := t.StartHabit(m.Name, m.Since, m.Until); … }`.
- `internal/core/habits.go` — `Tx.StartHabit(metric, start, end string)` inserts the period with
  `ON CONFLICT(metric_id, start_day) DO NOTHING`, then runs
  `UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND start_day = ? AND end_day IS NOT ?` with
  `nullIfEmpty(end)` — so `end == ""` sets `end_day` back to NULL. The documented rule
  (`docs/cookbook/habits.md:44-48`): "A re-sent period is idempotent … carrying the `end_day` it was sent with … a
  closed period re-sent without its `end_day` is an open one that overlaps any later period". The API's
  `start-habit` keeps that behaviour; **only the importer** must stop clearing an end the owner set.
- The schema's view `measurement_values` (`docs/schema/schema.sql:209-214`) is "rows nothing has corrected, minus
  retractions"; `measurements(supersedes_id)` has a unique partial index (`schema.sql:190`).
- `core.IntegrityResult` serialises its verdict as `"ok"` (`internal/core/integrity.go:9-16`).
- The test to extend: `TestReadingsKeysAndReplay`, `internal/importer/importer_test.go:270-347` (fixture `setup(t)`,
  `f.approveRules`, `f.w.MakeLedger()`, `f.metrics(t)`, `f.facts(t, file, …)`, `f.w.Apply`, `f.s.Correct` +
  `f.w.RecordCorrection`, `f.w.Replay(ctx, f.s, target)`). **Do not depend on the exact reading key string**
  (`…|2031-03-01|1`): plan 030 changes how the last segment is derived. Find a reading's id by value instead, e.g.
  `SELECT id FROM measurement_values WHERE value = 48 AND day = '2031-03-01'` on `f.s.DB.R`.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Importer + core + api | `go test ./internal/importer ./internal/core ./internal/api` | ok |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/core/write.go` (`Store.Correct`, `CorrectedKey`), `internal/core/imports.go` (new reads:
`RootOf`, `ImportedValues`), `internal/core/habits.go` (new `Tx.EnsureHabitPeriod`), `internal/api/habits.go`
(`correctTo`), `internal/api/import.go` (`replay` handler only), `internal/importer/replay.go`,
`internal/importer/inspect.go` (`RegisterMetrics` only), `internal/importer/importer_test.go`,
`internal/core/core_test.go` / `internal/core/habits_test.go` (new tests), `cmd/lifelog/main.go` (`parse`, the
`--workspace` block, `import setup`, `import replay`).

**Out of scope**: `docs/` (the guide already says what this plan implements); the API's `start-habit` semantics;
reading-key derivation (plan 030); full atomicity of a replay in one transaction (deferred — see Maintenance).

## Git workflow

Branch `advisor/029-replay-fidelity`; commit per step group is fine; final message e.g.
`import: the real run is the trial — chained and owner-only corrections, --db honoured, habits keep their end,
red results exit 1`, body + `Ran: …` + trailer.

## Steps

### Step 1: Failing tests first

In `internal/importer/importer_test.go` add `TestReplayCarriesWhatTheTrialHas` (reuse `setup`, `approveRules`,
`MakeLedger`, `metrics`, `facts` exactly as `TestReadingsKeysAndReplay` does, applying `Medical/Ferritin.md`):

a. Correct the 2031-03-01 reading 48 → 47 as `cli`, then correct **the correction** 47 → 46 as `cli` (the id of the
   correction is what `f.s.Correct` returns), recording each returned key with `f.w.RecordCorrection` **only through
   the same rule the API uses** — so call the API path instead: build `h := api.New(f.s, f.w)` and
   `client.InProcess(h, "cli")`, and post `correct` on `/measurements/{id}/correct` with `value`. (If importing
   `internal/api` from `internal/importer`'s tests makes an import cycle, put this test in
   `internal/api/import_test.go` instead, package `api_test`, building the fixture from `importer` exported calls.)
b. Correct the 2031-04-01 reading 52 → 99 through a client with source `agent:lm`.
c. Replay into a new target. Expect: the target's current value for 2031-03-01 is **46**; for 2031-04-01 it is **52**
   (the agent's correction is not replayed); `res.Differences` names the 2031-04-01 reading (trial 99, target 52);
   no row in the target has `source = 'cli'` for that metric's 2031-04-01 chain.

And `TestRegisterMetricsKeepsTheOwnersStop`: with a metric approved as a habit `since 2031-01-01` and no `until`,
run `RegisterMetrics`, then `f.s.StopHabit(ctx, "cli", <metric>, "2031-06-30")` (use the core call the API's
`stop-habit` uses), then `f.s.StartHabit(…, "2032-01-01", "")` for a later period, then `RegisterMetrics` again →
no error, and the periods are `2031-01-01..2031-06-30` and `2032-01-01..` (read them with `f.s.Periods(ctx, name)`).

**Verify**: `go test ./internal/importer -run 'TestReplayCarriesWhatTheTrialHas|TestRegisterMetricsKeepsTheOwnersStop'`
(or the api package) → FAIL on the current code.

### Step 2: Corrections name the imported root, and only the owner's are recorded

In `internal/core/imports.go` add:

```go
// RootOf is the first reading of a correction chain (the row whose supersedes_id is NULL).
func (t *Tx) RootOf(id int64) (int64, error)
```

with a recursive CTE walking `supersedes_id` upward:
`WITH RECURSIVE up(id, sup) AS (SELECT id, supersedes_id FROM measurements WHERE id = ? UNION ALL SELECT m.id, m.supersedes_id FROM measurements m JOIN up ON m.id = up.sup) SELECT id FROM up WHERE sup IS NULL`.

In `Store.Correct`, read the key of the **root**: `root, e := t.RootOf(wrong)`, then
`t.MeasurementKey(root)`. Add a field to `CorrectedKey`: `By string \`json:"by,omitempty"\`` (the corrector's
source), and set `key.By = source`.

In `internal/api/habits.go`, `correctTo`: record only when `!strings.HasPrefix(src, "agent:")`, i.e.
`if h.ws != nil && core.IsImport(key.Source) && key.Key != "" && !strings.HasPrefix(src, "agent:")`.

In `internal/importer/replay.go`, `replayCorrections`: run each correction in its own `ts.Do(ctx, by, …)` where
`by := c.By; if by == "" { by = "cli" }` (old `corrections.json` files have no `by`). Keep the "already so" skip.

**Verify**: Step 1's correction test → the 46/52 values pass (the Differences part still fails until Step 3).

### Step 3: The replay compares values, not only counts

In `internal/core/imports.go` add:

```go
// ImportedValues is the current value of every imported reading, by "<source>|<metric>|<import_key>" of its first
// row (nil = retracted): what a replay must reproduce.
func (s *Store) ImportedValues(ctx context.Context) (map[string]*float64, error)
```

Query (on `s.DB.R`): for each root `r` (`r.import_key IS NOT NULL AND r.supersedes_id IS NULL AND r.source LIKE 'import:%'`),
the chain end `c` is the row reached from `r` through `supersedes_id` that nothing supersedes. Use a recursive CTE
from roots down (`chain(root, id, value)`), keeping rows with `NOT EXISTS (SELECT 1 FROM measurements y WHERE
y.supersedes_id = chain.id)`; join `metrics` for the name.

In `Replay`, after the counts: `tv, err := trial.ImportedValues(ctx)`; `gv, err := ts.ImportedValues(ctx)`; for each
key of `tv` whose value differs from `gv[key]` (nil vs non-nil, or unequal numbers), append
`fmt.Sprintf("reading %s: trial %s, target %s", key, show(tv[key]), show(gv[key]))` to `res.Differences`, at most 20
lines, then `"… and N more readings differ"`. (`show` prints `retracted` for nil.)

**Verify**: Step 1's correction test passes fully.

### Step 4: Metrics registration never clears the owner's end

In `internal/core/habits.go` add `func (t *Tx) EnsureHabitPeriod(metric, start string) error`: the same checks
as `StartHabit` (valid day, metric exists, readings only 0/1), then **only** the
`INSERT … ON CONFLICT(metric_id, start_day) DO NOTHING` with `end_day` NULL — no UPDATE.

In `internal/importer/inspect.go`, `RegisterMetrics`: when `m.Until == ""` call `t.EnsureHabitPeriod(m.Name, m.Since)`;
when `m.Until != ""` keep `t.StartHabit(m.Name, m.Since, m.Until)`.

Add a core test in `internal/core/habits_test.go`: a period with an end, then `EnsureHabitPeriod` of the same start →
the end stays.

**Verify**: `go test ./internal/core ./internal/importer -run 'Habit|RegisterMetrics'` → PASS.

### Step 5: `--db` is honoured

In `cmd/lifelog/main.go` add `dbFlag bool` to `opts`; in `parse`, set `o.dbFlag = true` when the `--db` flag is
consumed (give `--db` its own branch before the generic `vals` lookup, or check `name == "--db"` inside it). In the
`--workspace` block replace the condition with `if !o.dbFlag { o.db = ws.TrialDB() }` and update its comment:
`// an import works on its trial database unless --db names another (LIFELOG_DB is not --db)`.

**Verify**: `go build ./cmd/lifelog` → exit 0. Manual check, recorded in the commit body: with `LIFELOG_DB` set to
a real file, `lifelog import status --workspace W --db <same file>` prints that file's path as `database`, not
`trial.db`.

### Step 6: Red results exit 1; a replay checks before it writes

- `import setup` (`main.go`): when `!res.OK`, `printJSON(res)` then
  `return errors.New("the integrity checks failed on trial.db: stop and report (docs/contract/integrity-checks.md)")`.
- `import replay` (`main.go`): replace the `doAction` call with: find the `replay` action in `c.Catalog()`,
  `e, err := c.Do(a, …)`, `if perr := show(o)(e, err); perr != nil { return perr }`, then read `e.Properties` as
  `map[string]any`: if `differences` is a non-empty `[]any`, or `integrity` is missing or its `ok` is not `true`,
  return `errors.New("the real run differs from the trial or failed its integrity checks: stop and report")`.
- `Replay` (`replay.go`): before `db.Init`, refuse unless `w.Gate("rules.md")` returns `"approved"` (propagate its
  error) and `w.Ledger()` reports the ledger exists. After opening the target and before `applyPlan`, run
  `w.validatePlan(ctx, ts, fresh)` and refuse if any note has problems
  (`refuse("the plan does not fit the target (%s: %s)", n.Path, n.Problems[0])`). Stop ignoring the error of
  `w.Gate("metrics.md")`.
- When `Replay` returns an error after something was committed, wrap it so the caller learns what was written:
  `fmt.Errorf("%w — written before the failure: vault %d pages, %d files, metrics %d", err, …)` (use the counts
  in `res`). Keep `refuse` errors' status (wrap with `%w`).

Add to the replay test: a workspace whose `rules.md` is not approved → `Replay` returns an error and the target
file does **not** exist afterwards.

**Verify**: `go test ./internal/importer` → ok.

### Step 7: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0. `TestReadingsKeysAndReplay` still passes
unchanged (if it fails only because it now sees a value difference it did not expect, STOP and report).

## Test plan

- `TestReplayCarriesWhatTheTrialHas` (chained correction replayed, agent correction not replayed and reported).
- `TestRegisterMetricsKeepsTheOwnersStop` and the core `EnsureHabitPeriod` test.
- Replay refuses before initialising when rules are not approved.
- Existing `TestReadingsKeysAndReplay` unchanged and green.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n 'Getenv("LIFELOG_DB") == o.db' cmd/lifelog/main.go` → no match
- [ ] `grep -n 'ts.Do(ctx, "cli"' internal/importer/replay.go` → no match
- [ ] `grep -n "if !res.OK {" -A2 cmd/lifelog/main.go` shows an error being returned
- [ ] new tests present and passing; `git status --short` lists only in-scope files; status row for 029 updated

## STOP conditions

- The real `life.db` (the owner's canonical file) already holds replayed rows: STOP before changing correction
  replay — ask the owner how existing `corrections.json` files should be treated.
- `TestReadingsKeysAndReplay` fails for a reason other than a newly reported value difference.
- Step 2's test cannot be built without an import cycle in either package — report the cycle.
- A replay needs to be refused for a reason this plan does not list — report instead of adding gates.

## Maintenance notes

- **Issues filed by the owner after this plan was written (`d65b14c`)**: [0004](../issues/0004-an-about-link-applied-before-the-persons-own-note-fails.md)
  (a link to a page that a later file promotes aborts the replay: only "not written yet" is retried) and
  [0005](../issues/0005-a-replay-stops-in-the-middle-and-leaves-the-target-half-written.md) (a failed replay leaves the
  target half-written, no dry run). Step 6 covers part of 0005 (gates checked before the target is touched; the error
  says what was written); a full dry run and 0004's ordering are **not** in this plan — they need their own plan. Do not
  mark either issue resolved here.
- **Deferred**: a replay is still several transactions. Rows are idempotent, so re-running finishes a half-done
  replay, but it cannot be abandoned cleanly. A full dry run would need the whole replay in one transaction; revisit
  if a real run ever fails part-way.
- **Deferred**: a habit stop the owner makes on the trial with `stop-habit` is not in `metrics.md`, so the replay does
  not carry it; the count comparison does not see an `end_day`. If that matters, record habit stops like corrections.
- A reviewer should check that `ImportedValues` uses the chain **end**, not `measurement_values` alone (a retracted
  end is absent from that view and must compare as `retracted`).
