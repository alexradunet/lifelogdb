# D7 — Measurements: one FxLifeSheet-shaped table + tiny metric registry; append-only.

**Status:** accepted

- **Decision.** `metrics` keeps series canonical (`metrics_name`: lowercase snake_case, so 'Weight'
  cannot become a second series; `metrics_unit_fixed`). `measurements` holds one row per data point
  and is **bitemporal** [R68](../research/references.md#r68): `day`/`taken_at` is *valid time*, `created_at` and the append-only
  rows are *transaction time*, so "what did I believe my weight was on 1 March, as of 1 April" stays
  answerable. The table is append-only (`measurements_no_update`, `measurements_no_delete`); a
  correction supersedes (`measurements_one_correction`, `measurements_supersede_metric` — the one
  supersede invariant that could silently corrupt a series); a NULL value retracts
  (`measurements_first_has_value`); values are finite (`measurements_value_finite`: a `REAL` column
  stores `1e999` as infinity, and one such row poisons every average). SQLite turns a bound `NaN` into
  NULL before any CHECK sees it: as a first reading that is rejected, but as a correction it is a
  retraction the database cannot tell from an intended one (executed) — so the app never binds NaN.
  `measurement_values` is the one read rule; two independent readings on one day are both returned.
  The unique index on `supersedes_id` doubles as the index the view's `NOT EXISTS` needs (executed:
  the plan uses it).
- **Habits** are 0/1 metrics with active periods ([D24](D24-habits.md)): their check-ins are ordinary rows here.
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
