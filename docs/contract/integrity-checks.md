# Integrity checks

Four check groups tell whether a file still obeys the schema. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import ([imports](imports.md)) or a
migration ([D13](../decisions/D13-migrations-and-freeze.md)), on every snapshot ([take a snapshot](../cookbook/take-a-snapshot.md)), and after any writer crashed. Every claim below was executed on the **live**
file, not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities e WHERE NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key) OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id)) OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id)) OR (e.entity_type='period' AND NOT EXISTS (SELECT 1 FROM periods p WHERE p.id=e.id)) OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id));   -- no rows
SELECT l.id FROM links l LEFT JOIN link_kinds k ON k.kind=l.kind LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0) OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0) ORDER BY l.id; -- no rows
SELECT s.id FROM sessions s LEFT JOIN entities e ON e.id=s.kind_id WHERE e.id IS NULL OR e.entity_type<>'page' OR (length(e.preferred_name_key)=10 AND date(e.preferred_name_key) IS e.preferred_name_key) ORDER BY s.id; -- no rows
SELECT m.id FROM measurements m LEFT JOIN sessions s ON s.id=m.session_id LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE (m.session_id IS NOT NULL AND s.id IS NULL) OR (m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id OR p.session_id IS NOT m.session_id)) ORDER BY m.id; -- no rows
INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1);   -- no error
```

- **`integrity_check` — the file's structure.** It caught a zeroed table page, a file truncated by
  three pages, and an index entry that no longer matches its row (a flipped byte in a `name_key`
  inside the unique `entity_names.name_key` index).
- **`foreign_key_check` — what the first cannot see.** A writer that forgot `PRAGMA foreign_keys=ON`
  ([connection setup](connections.md) — per connection; `STRICT` does not enforce foreign keys) stored a reading of a metric that
  does not exist, and `integrity_check` said `ok`.
- **Semantic integrity — domain ownership and typed graph endpoints.** The orphan query finds a missing owned preferred name or a missing typed extension. A fully named plain page/place is complete without another prose row; a person, metric, file or period needs its extension. Missing name ownership also violates a foreign key; missing typed extensions do not. The typed-edge query independently checks both endpoints against
  the closed kind registry, even when a damaged file bypassed the insertion and type-change guards (executed).
- **The FTS5 integrity-check — the index against its content.** `entities_fts` is an external-content
  index over `entity_search_content`; if the two drift apart, searches return wrong rows and `integrity_check` still
  says `ok`. The FTS5 command with rank `1` compares the index with `entity_search_content` and fails. The index is
  derived: `INSERT INTO entities_fts(entities_fts) VALUES('rebuild')` repairs it. Writing it is a write, so
  it runs on a writer connection.
- **What structural integrity does not see: a changed value.** A flipped byte inside a body passed
  `integrity_check` — SQLite keeps no page checksums. This indexed-token change is detected by the FTS5 content comparison; structural success alone does not establish intact prose. The cheap guard is below the file: keep
  `life.db` on a filesystem that checksums data (btrfs and ZFS do by default; never `chattr +C` the
  file or its folder, which turns btrfs checksums off) and scrub it now and then. SQLite's own
  `cksumvfs` [R64](../research/references.md#r64) does the same per page inside the file, at the cost of an extension every writer
  must load; it is not used.
