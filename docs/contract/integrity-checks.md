# Integrity checks

Four groups of SQL checks inspect the file structure, stored relationships, the rules that only a trigger holds, and the derived search index. Two more checks that SQL cannot make compare the name keys and the schema. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import ([imports](imports.md)) or a
migration ([D13](../decisions/D13-migrations-and-freeze.md)), on every snapshot ([take a snapshot](../cookbook/take-a-snapshot.md)), and after any writer crashed. Every claim below was executed on the **live**
file, not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities e WHERE NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key) OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id)) OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id)) OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id));   -- no rows
SELECT l.id FROM links l LEFT JOIN link_kinds k ON k.kind=l.kind LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0) OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0) ORDER BY l.id; -- no rows
SELECT m.id FROM measurements m LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id) ORDER BY m.id; -- no rows
WITH RECURSIVE rooted(id) AS (
 SELECT id FROM measurements WHERE supersedes_id IS NULL
 UNION
 SELECT m.id FROM measurements m JOIN rooted r ON m.supersedes_id=r.id
)
SELECT id FROM measurements EXCEPT SELECT id FROM rooted ORDER BY id; -- no rows
SELECT n.id FROM entity_names n JOIN entities e ON e.id=n.entity_id
 WHERE (length(n.name_key)=10 AND date(n.name_key) IS n.name_key
   AND (e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key))
 OR (e.is_journal
   AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id; -- no rows
SELECT l.id FROM links l LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
 WHERE (l.kind='at' AND NOT coalesce(f.is_journal,0))
 OR (l.kind='part-of' AND coalesce(t.is_journal,0))
 OR (l.kind='part-of' AND coalesce(f.is_journal,0)) ORDER BY l.id; -- no rows
SELECT m.id FROM measurements m JOIN metrics x ON x.id=m.metric_id
 WHERE m.value IS NOT NULL AND x.unit GLOB '[0-9]*-[0-9]*' AND x.unit NOT GLOB '*[^0-9-]*' AND x.unit NOT GLOB '*-*-*'
 AND CAST(substr(x.unit, 1, instr(x.unit, '-') - 1) AS REAL) < CAST(substr(x.unit, instr(x.unit, '-') + 1) AS REAL)
 AND (m.value <> round(m.value)
   OR m.value NOT BETWEEN CAST(substr(x.unit, 1, instr(x.unit, '-') - 1) AS REAL) AND CAST(substr(x.unit, instr(x.unit, '-') + 1) AS REAL))
 ORDER BY m.id; -- no rows
INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1);   -- no error
```

- **`integrity_check` — the file's structure.** It caught a zeroed table page, a file truncated by
  three pages, and an index entry that no longer matches its row (a flipped byte in a `name_key`
  inside the unique `entity_names.name_key` index).
- **`foreign_key_check` — what the first cannot see.** A writer that forgot `PRAGMA foreign_keys=ON`
  ([connection setup](connections.md) — per connection; `STRICT` does not enforce foreign keys) stored a reading of a metric that
  does not exist, and `integrity_check` said `ok`.
- **Semantic integrity — domain ownership and typed graph endpoints.** The orphan query finds a missing owned preferred name or a missing typed extension. A fully named plain page/place is complete without another prose row; a person, metric or file needs its extension. Missing name ownership also violates a foreign key; missing typed extensions do not. The typed-edge query checks both endpoints against
  the closed kind registry (`link_kinds`) only, even when a damaged file bypassed the insertion and type-change guards (executed).
  It does not check the three rules that the registry cannot say, which the next group holds.
  The remaining semantic queries detect a correction of a missing reading or of another metric,
  correction rows unreachable from an original reading (cycles or dangling chains), and invalid journal-name ownership
  (executed with deliberately bypassed guards restored before checking). The rooted-chain query visits each
  reachable reading once; it does not follow every row's full ancestry.
  Journal diagnostics report `entity_names.id`; other diagnostics report the inspected table's `id`.
- **Rules that only a trigger holds.** Two triggers hold rules that no CHECK can say, and each has a query that
  finds a row written while the trigger was missing. The link query covers the three rules of `links_endpoint_types`
  that `link_kinds` cannot say, since a day page and a plain page are both `page`: an `at` link starts at a day page,
  a `part-of` link never ends at a day page, and a `part-of` link never starts at one ([D16](../decisions/D16-places.md),
  [D26](../decisions/D26-metric-categories.md)). The scale query covers `measurements_scale_range`: a reading of a scale
  (a unit `lo-hi`) is a whole number inside the range. A retraction has no value and is never held. Both queries
  read every row, so they find a row written while the trigger was missing. Both report the `id` of the inspected
  table (executed with the trigger dropped before the bad row was written).
- **The name keys — what SQL cannot compute.** `entity_names.name_key` is the key of its `title`, computed by the
  algorithm of [titles and wikilinks](titles-and-wikilinks.md). SQLite has no such function, so a writer compares
  every stored key with the key it computes from the title, and lists the ids of the `entity_names` rows that differ.
  This check is not SQL, and the page holds no query for it. The file refuses a change of a key (`entity_names_fixed`),
  so a drift is reported and not repaired: a repair is a change of the schema, which follows the
  [process](../process.md). A writer whose Unicode tables moved finds here the titles whose key moved.
- **The schema — a file that no longer has the DDL.** The data checks do not see a missing trigger or a changed
  index: a file edited by hand or by a one-off program can lose a rule while every row stays valid. A writer builds a
  fresh file from [`schema.sql`](../schema/schema.sql) in a temporary folder and compares the rows of `sqlite_schema`
  (`type`, `name`, `tbl_name`, `sql`) of the two files, with every run of white space in `sql` folded to one space. It
  lists each difference by name: `missing` (the DDL has the object and the file has not), `extra` (the file has an
  object that the DDL has not) and `changed` (both have it, with another text). Two kinds of rows are left out on
  purpose: the statistics tables (`sqlite_stat1` and its kin), which `PRAGMA optimize` makes and the DDL never names;
  and the shadow tables of a virtual table (`entities_fts_data` and its kin) with their indexes, which belong to the
  FTS5 module, whose shape can differ between SQLite versions. The `CREATE VIRTUAL TABLE` row of the index is compared.
  The autoindexes of the writer's own tables are compared. A schema difference is a finding about the file and the
  DDL, not damage to the data, so it does not make the data checks fail. A writer does not make this check when it
  opens the file: a refusal there would lock the owner out of the data. It is a check that the owner asks for.
- **One view.** A writer runs all the checks above, the name keys included, in one read transaction. Another writer
  that commits meanwhile changes none of their results. The FTS5 check below needs a writer connection and runs after
  them.
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

These are data checks, not a proof that a file has every required trigger, index or metadata row. A replacement
writer must also recognize the supported schema and apply the [connection contract](connections.md). The SQL
cannot recompute the pinned Unicode name-key algorithm (the name-key check above is the writer's), extract CommonMark wikilinks, decode
pictures, reconstruct overwritten history or establish that a plausible value is what the owner recorded.
Those boundaries need the [writer contract](../guides/building-a-writer.md), retained source evidence where
available, and [snapshots](../decisions/D25-snapshots.md); a clean result never certifies completeness or truth.
