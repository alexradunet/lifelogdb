# Prior-art survey

The seven real systems surveyed during research, and exactly what was taken from each:

| System | Scale / longevity | Shape | Taken into this design | Rejected from this design |
|---|---|---|---|---|
| FxLifeSheet [R9](references.md#r9)[R42](references.md#r42) | 380k points, 6+ yrs | One `raw_data` table; metric registry in config | measurements shape, `import_key` idempotency, denormalized time buckets (`day`), capture-friction philosophy | value-as-TEXT (we use REAL), Postgres, 8 separate time-bucket columns (one `day` suffices) |
| ark [R46](references.md#r46) | 700k items, 125 GB store + 9 GB SQLite | Content-addressed files; SQLite index; typed edges | hash-based file identity ([D9](../decisions/D09-binary-files.md)), `links` as the one graph, "everything about a person" query | managed original store ([D9](../decisions/D09-binary-files.md)), annotations layer, classification/quality subsystems |
| Open Brane [R43](references.md#r43) | 942k rows, 3 GB | One append-only 8-column table; no FKs | append-only spirit for measurements, keyed idempotent writes (as `ON CONFLICT … DO NOTHING`), originals outside the database ([D9](../decisions/D09-binary-files.md)) | payload_json column (violates [D2](../decisions/D02-typed-strict-tables.md)), no-FK design (violates [D8](../decisions/D08-entities-and-links.md)), `INSERT OR IGNORE` (it also swallows CHECK and NOT NULL violations, [imports](../contract/imports.md)) |
| health-mcp [R8](references.md#r8) | Years of use | Typed biomarker tables; two-tier wearables; forward-only migrations | UTC+local-day convention, forward-only numbered migrations, metric registry concept | LOINC/UCUM/ref-ranges, raw mirror tier (both deferred, [non-goals](../architecture/non-goals.md)) |
| Myome [R45](references.md#r45) | Design paper | TSDB + SQLite + object store | Scale calibration (~5 GB/lifetime → no rollups needed) | TSDB, FHIR machinery, multi-resolution storage |
| Kaydet [R41](references.md#r41) | 9 yrs daily entries | Plain text + SQLite index | Evidence that boring survives; hybrid text+DB instinct (resolved as [D4](../decisions/D04-database-is-canonical.md): the database is canonical) | Files-as-canonical (owner's writer-drift objection) |
| Logseq OG vs DB [R34](references.md#r34)–[R36](references.md#r36) | Product-scale split | Files-canonical vs SQLite-canonical | The decisive precedent for [D4](../decisions/D04-database-is-canonical.md): a team that hit live-editing limits chose DB-canonical | Block-level datom model, collaboration machinery |
