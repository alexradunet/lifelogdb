# Integrity checks

Four checks tell whether a file still obeys the schema. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import ([imports](imports.md)) or a
migration ([D13](../decisions/D13-migrations-and-freeze.md)), on every snapshot ([take a snapshot](../cookbook/take-a-snapshot.md)), and after any writer crashed. Every claim below was executed on the **live**
file, not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION SELECT id FROM people);   -- no rows
INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error
```

- **`integrity_check` — the file's structure.** It caught a zeroed table page, a file truncated by
  three pages, and an index entry that no longer matches its row (a flipped byte in a `title_key`
  inside `pages_title`).
- **`foreign_key_check` — what the first cannot see.** A writer that forgot `PRAGMA foreign_keys=ON`
  ([connection setup](connections.md) — per connection; `STRICT` does not enforce foreign keys) stored a reading of a metric that
  does not exist, and `integrity_check` said `ok`.
- **The orphan query — the one check no constraint can express.** An `entities` row with no domain
  row (a writer that died between its inserts, or a person with a page but no `people` row): both
  other checks are clean on it.
- **The FTS5 integrity-check — the index against its content.** `pages_fts` is an external-content
  index over `pages`; if the two drift apart, searches return wrong rows and `integrity_check` still
  says `ok`. The FTS5 command with rank `1` compares the index with `pages` and fails. The index is
  derived: `INSERT INTO pages_fts(pages_fts) VALUES('rebuild')` repairs it. Writing it is a write, so
  it runs on a writer connection.
- **What none of them sees: a changed value.** A flipped byte inside a body passed
  `integrity_check` — SQLite keeps no page checksums. The cheap guard is below the file: keep
  `life.db` on a filesystem that checksums data (btrfs and ZFS do by default; never `chattr +C` the
  file or its folder, which turns btrfs checksums off) and scrub it now and then. SQLite's own
  `cksumvfs` [R64](../research/references.md#r64) does the same per page inside the file, at the cost of an extension every writer
  must load; it is not used.
