# D13 — Migrations: numbered plain SQL + `PRAGMA user_version`; freeze-and-migrate.

**Status:** accepted

- **Decision.** No ORM, no migration framework, no down-migrations. **Until the freeze there are no
  migrations:** [schema](../schema/README.md) is edited in place and test databases are recreated; `user_version` stays 1. After
  real data exists: numbered plain-SQL files, `db/migrations/0002_*.sql`, … applied in order, progress
  in `PRAGMA user_version` [R20](../research/references.md#r20)[R21](../research/references.md#r21)[R22](../research/references.md#r22); additive only (new tables, columns, indexes; a column rename
  is allowed and recorded in its migration). `PRAGMA application_id = 0x4C494645` ('LIFE') lets
  `file(1)` and future tools recognize the database [R1](../research/references.md#r1).
- **Every CHECK is named, so every rule can change without a rebuild.** Widening an enum (a new
  entity type, a new link endpoint), letting partial dates into `birth_day` or loosening the title rules is a
  two-statement transactional migration — `ALTER TABLE … DROP CONSTRAINT <name>; … ADD CONSTRAINT
  <name> CHECK (…)` (SQLite ≥ 3.53 [R55](../research/references.md#r55)) — **only because the CHECK has a name**: an unnamed CHECK
  cannot be dropped (`no such constraint`), and adding a looser second CHECK does not relax the first
  (both apply). `ADD CONSTRAINT` checks the existing rows, so tightening is as safe as loosening. All
  executed on a populated database, with integrity and foreign-key checks clean after. The alternative
  is SQLite's 12-step table rebuild, with FTS triggers, composite FKs and tombstone triggers to recreate.
- **Down-migrations** are rejected as a category. A migration runs on a *copy* first (`VACUUM INTO`,
  as for an importer, [imports](../contract/imports.md)) and the four checks of [integrity checks](../contract/integrity-checks.md) must pass on the copy before it touches
  `life.db`.
- **Sources.** [R1](../research/references.md#r1)[R20](../research/references.md#r20)[R21](../research/references.md#r21)[R22](../research/references.md#r22)[R55](../research/references.md#r55).
