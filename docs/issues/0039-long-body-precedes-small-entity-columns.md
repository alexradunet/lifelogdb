# 0039 — A long body sits in front of the small entity columns

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a schema review measuring the row layout of `entities` on a synthetic file (generated pages, no real
  data), SQLite 3.53.4

## What happened

`entities.body` was the third column, ahead of `id`, `entity_type`, the instants, `deleted_at` and the provenance
columns. SQLite writes a row's columns in declaration order, and a value that does not fit its page continues on
overflow pages; every column declared after a long body is stored behind them. Reading `deleted_at` or `entity_type`
of a page with a long body therefore walks the overflow chain first, although the body itself is never asked for.
Every active read filters on `deleted_at` and many on `entity_type`.

The review measured 20,000 synthetic pages with 13 KB bodies: a scan reading `deleted_at` and `entity_type` of every
row took 209 ms with the body third and 15 ms with the body last; 20,000 point lookups by `id` took 536 ms against
338 ms. A re-measurement of the same shape in the Go driver (best of five warm runs) gave 91 ms against 8 ms and
198 ms against 107 ms. The absolute times belong to the machine; the ratio is the finding.

The column order is part of the file: after the freeze a migration can only append a column
([D13](../decisions/D13-migrations-and-freeze.md)), so the order has to be right before it.

## Reproduce

On two fresh databases, one from the schema before this change and one from [schema.sql](../schema/schema.sql),
insert 20,000 pages (entity and owned name, same transaction) with a 13,000-byte body each. Time
`SELECT count(*) FROM entities WHERE deleted_at IS NULL AND entity_type = 'page'` and 20,000 prepared
`SELECT deleted_at, entity_type FROM entities WHERE id = ?`. Nothing in the file layout is asserted by a suite (it is
an accident of the SQLite build); the suite checks the declared order only.

## Rules involved

[D13](../decisions/D13-migrations-and-freeze.md) (column order is frozen with the file), the `entities` table of
[schema.sql](../schema/schema.sql), and the engineering priority of fast feedback in [AGENTS.md](../../AGENTS.md).

## Resolution

The columns of `entities` are `id, entity_type, preferred_name_key, day, created_at, updated_at, revision,
deleted_at, source, import_key, body`: the body, the one large text, is last, and the `CREATE` comment says why.
D13 states that the order is frozen and that a column added later follows the body. Nothing selected columns by
position (`SELECT *` appears only in before/after state comparisons). The `schema-safeguards` suite checks that
`body` is the last declared column; a mutant moves it back in front of `import_key`. No decision changes.
