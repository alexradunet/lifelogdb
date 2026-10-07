# D7 — Measurements: one FxLifeSheet-shaped table, one row per metric; append-only.

**Status:** accepted

- **Decision.** A metric is a page ([D27](D27-a-metric-is-a-page.md)), so its title keeps a series canonical ('Weight'
  and 'weight' resolve through one owned normalized name; rename retains the series id), and its unit never changes (`metrics_unit_fixed`). `measurements` holds one row per data point
  and retains **valid time and recorded time**, following the distinction in temporal databases
  [R68](../research/references.md#r68): `day`/`taken_at` describes the observation; `created_at` is the
  writer-supplied recording timestamp. The append-only chain preserves corrections, so a recorded-time
  cutoff can answer which version was recorded by a stated instant. This is not database-managed transaction
  time: equal timestamps, clock regressions and writes in the same transaction prevent exact commit-history
  reconstruction from `created_at`. The table is append-only (`measurements_no_update`, `measurements_no_delete`); a
  correction supersedes (`measurements_one_correction`, `measurements_supersede_metric` and `measurements_supersede_scope` — the one
  supersede invariant that could silently corrupt a series); a NULL value retracts
  (`measurements_first_has_value`); values are finite (`measurements_value_finite`: a `REAL` column
  stores `1e999` as infinity, and one such row poisons every average). SQLite turns a bound `NaN` into
  NULL before any CHECK sees it: as a first reading that is rejected, but as a correction it is a
  retraction the database cannot tell from an intended one (executed) — so the app never binds NaN.
  `measurement_values` selects the current non-retracted leaves; two independent readings on one day are both returned.
  Historical cutoff reads select leaves within the eligible append-only rows, not a timestamp-filtered current view
  ([a metric series](../cookbook/metric-series.md)). Lifecycle filters belong to the read's stated purpose.
  The unique index on `supersedes_id` doubles as the index the view's `NOT EXISTS` needs (executed:
  the plan uses it).
- **Session scope** is distinct from capture provenance ([scope](../contract/measurement-scope.md)); the
  append-only chain keeps its metric and association. Wrong-scope owner correction retracts and creates an
  independent root atomically. Existing imported correction intent does not authorize imported/keyed relocation.
  `measurements_session_live` runs after insertion so its admission check does not reject a skipped import-key retry
  after the associated session is tombstoned; a genuinely new value still fails atomically.
- **Habits** are 0/1 metrics with active periods ([D24](D24-habits.md)): their check-ins are ordinary rows here.
- **Categories** file metrics in pages nested by `part-of` links (Biomarkers, Lipids, Substances) ([D26](D26-metric-categories.md)).
- **`captured_with_id`** is provenance (the day page the reading was captured with), not "about
  this person": the owner is the only subject of measurements.
- **This is the most battle-tested part of the design.** FxLifeSheet's actual schema is a single
  `raw_data` table carrying 380k data points over 6+ years with zero schema drama [R9](../research/references.md#r9)[R42](../research/references.md#r42). Open Brane
  runs one append-only table with keyed idempotent writes at 942k rows [R43](../research/references.md#r43). We keep three of their
  devices: `import_key` idempotency, denormalized local `day`, `source` provenance.
- **Deliberate simplifications** ([non-goals](../architecture/non-goals.md)): `value REAL` only (no text-valued measurements — prose belongs
  in pages); no LOINC/UCUM/reference ranges [R8](../research/references.md#r8); no raw/normalized two-tier wearable mirror [R8](../research/references.md#r8); no
  multi-resolution rollups (~5 GB/lifetime of sensor data queries fine raw) [R45](../research/references.md#r45).
- **Sources.** [R8](../research/references.md#r8)[R9](../research/references.md#r9)[R42](../research/references.md#r42)[R43](../research/references.md#r43)[R45](../research/references.md#r45)[R68](../research/references.md#r68).
