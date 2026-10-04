# Imports

**Imports** — the path for data that already exists elsewhere (a journal archive, a health export,
lab results). Every step was executed on 1 000 synthetic rows:

1. **Trial run first.** Rows are never deleted, so a bad import can only be retracted row by row
   (a NULL-value correction for a measurement, a tombstone for an entity). Do the
   first run of any new importer on a *copy*: `sqlite3 -readonly -cmd "PRAGMA trusted_schema=OFF" life.db "VACUUM INTO '/tmp/trial.db'"` — a read-only connection may make the copy (*executed*).
2. **Load the rows into a scratch database, never into `life.db`** (`sqlite3 scratch.db ".import
   --csv weights.csv staging"`), then insert in one `BEGIN IMMEDIATE` transaction per batch, on the writer's own connection set up as [connection setup](connections.md) requires (the `sqlite3` shell sets none of it: `foreign_keys` is off there):

```sql
ATTACH 'scratch.db' AS s;
BEGIN IMMEDIATE;
INSERT INTO measurements(metric_id, day, taken_at, tz, value, created_at, source, import_key)
SELECT (SELECT id FROM pages WHERE title_key = 'weight' AND entity_type = 'metric'), day, NULLIF(taken_at, ''), NULLIF(tz, ''), value,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:scale', id
  FROM s.staging WHERE true
ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
COMMIT;
DETACH s;
```

   Three traps, each executed. **`WHERE true`** is required: in `INSERT … SELECT … FROM … ON CONFLICT`
   SQLite reads the `ON` as a join's, and answers "a JOIN clause is required before ON" (or `near "DO":
   syntax error`). **The conflict target repeats the index's `WHERE`**: the unique
   index is partial, and without it SQLite answers "ON CONFLICT clause does not match any PRIMARY
   KEY or UNIQUE constraint". **Never `CAST(value AS REAL)`**: it turns `'abc'` and `''` into `0.0`
   and `'12.5kg'` into `12.5`, silently — a plain insert into the STRICT column converts `'12.5'` and
   rejects `'abc'` and `''`. A CSV empty field is `''`, not NULL, so optional columns go through
   `NULLIF(…, '')`; a `''` in `taken_at` fails its CHECK. A fourth, for links: **never `INSERT OR
   REPLACE` into `links`** — on a symmetric kind the replace and the two mirror triggers keep firing
   each other, and SQLite stops with `too many levels of trigger recursion` (executed). Use
   `ON CONFLICT(from_id, to_id, kind) DO NOTHING`.
3. **Identity and time.** `source` names the importer (`import:<name>`), `import_key` is the source's
   own id, `day` / `taken_at` / `tz` say when it happened, `created_at` is when you imported it — on
   every table that has it, `created_at` is the write time (`lifelog_meta.instants`). An imported page, person or place
   carries its key on `entities` ([import a row once](../cookbook/import-a-row-once.md)). **The key must come out the same on every run**: the source's own id
   (a Health Connect record's id; a note's path in its vault). A source without ids gets a key built from
   fields it never changes (a note's file name, the day of a reading) — and a change to one of them then
   looks like a new row. The key deduplicates within one `source` only: the same reading from two
   sources is two rows, for the app to match and the owner to retract one. A file is the exception: it is found by
   the hash of its original, `files.sha256`, whatever its source, and kept once ([keep a file](../cookbook/keep-a-file.md)).
4. **What a failure does.** `ON CONFLICT … DO NOTHING` skips only a duplicate key: a malformed
   day, an impossible value or a dangling foreign key still raises and the **whole batch rolls back**.
   Fix the data and run the batch again.
5. **Check afterwards:** the four checks of [integrity checks](integrity-checks.md), per-source counts (`SELECT source, count(*),
   min(day), max(day) FROM measurements GROUP BY source`), and **run the importer a second time — it
   must insert nothing.**
6. **Before the freeze**, import a real export once into the file that will become canonical (steps 1–5); while that
   file holds only replayable imports it can still be rebuilt ([D13](../decisions/D13-migrations-and-freeze.md)). The
   2026-10 trial — a real vault, imported into a copy — already taught [D5](../decisions/D05-pages-and-day-pages.md), [D22](../decisions/D22-events.md), [D23](../decisions/D23-no-tasks.md) and [D24](../decisions/D24-habits.md); an
   import into `life.db` itself is the one test this schema has never had.
