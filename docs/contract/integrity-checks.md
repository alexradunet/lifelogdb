# Integrity checks

Four check groups inspect the file structure, stored relationships and derived search index. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import ([imports](imports.md)) or a
migration ([D13](../decisions/D13-migrations-and-freeze.md)), on every snapshot ([take a snapshot](../cookbook/take-a-snapshot.md)), and after any writer crashed. Every claim below was executed on the **live**
file, not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities e WHERE NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key) OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id)) OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id)) OR (e.entity_type='period' AND NOT EXISTS (SELECT 1 FROM periods p WHERE p.id=e.id)) OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id));   -- no rows
SELECT l.id FROM links l LEFT JOIN link_kinds k ON k.kind=l.kind LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0) OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0) ORDER BY l.id; -- no rows
SELECT s.id FROM sessions s LEFT JOIN entities e ON e.id=s.kind_id WHERE e.id IS NULL OR e.entity_type<>'page' OR e.is_journal ORDER BY s.id; -- no rows
SELECT m.id FROM measurements m LEFT JOIN sessions s ON s.id=m.session_id LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE (m.session_id IS NOT NULL AND s.id IS NULL) OR (m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id OR p.session_id IS NOT m.session_id)) ORDER BY m.id; -- no rows
SELECT t.id FROM tasks t LEFT JOIN entities e ON e.id=t.project_page_id WHERE t.project_page_id IS NOT NULL AND (e.id IS NULL OR e.entity_type<>'page' OR e.is_journal) ORDER BY t.id; -- no rows
SELECT t.id FROM tasks t WHERE t.repeat_unit IS NULL AND (SELECT count(*) FROM task_occurrences o WHERE o.task_id=t.id AND o.occurrence_key='once')<>1 ORDER BY t.id; -- no rows
SELECT o.id FROM task_occurrences o WHERE NOT EXISTS
(SELECT 1 FROM tasks t WHERE t.id=o.task_id AND
   ((t.repeat_unit IS NULL AND o.occurrence_key='once') OR
    (t.repeat_unit IS NOT NULL AND o.occurrence_key>=t.anchor_day
     AND CASE t.repeat_unit
       WHEN 'day' THEN CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%t.repeat_every=0
       WHEN 'week' THEN CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%7=0
         AND (CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)/7)%t.repeat_every=0
       WHEN 'month' THEN ((CAST(substr(o.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))*12
         + CAST(substr(o.occurrence_key,6,2) AS INTEGER)-CAST(substr(t.anchor_day,6,2) AS INTEGER))%t.repeat_every=0
       WHEN 'year' THEN (CAST(substr(o.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))%t.repeat_every=0
         AND substr(o.occurrence_key,6,2)=substr(t.anchor_day,6,2)
     END
     AND (t.repeat_unit IN ('day','week') OR CAST(substr(o.occurrence_key,9,2) AS INTEGER)=min(CAST(substr(t.anchor_day,9,2) AS INTEGER),
       CASE WHEN substr(o.occurrence_key,6,2)='02' THEN 28+
         (CAST(substr(o.occurrence_key,1,4) AS INTEGER)%4=0 AND
          (CAST(substr(o.occurrence_key,1,4) AS INTEGER)%100<>0 OR CAST(substr(o.occurrence_key,1,4) AS INTEGER)%400=0))
       WHEN substr(o.occurrence_key,6,2) IN ('04','06','09','11') THEN 30 ELSE 31 END))))) OR (o.state='open' AND EXISTS (SELECT 1 FROM tasks t WHERE t.id=o.task_id AND t.repeat_until_day IS NOT NULL AND o.occurrence_key>t.repeat_until_day)) ORDER BY o.id; -- no rows
SELECT l.id FROM links l JOIN link_kinds k ON k.kind=l.kind
 LEFT JOIN links r ON r.from_id=l.to_id AND r.to_id=l.from_id AND r.kind=l.kind
 WHERE k.symmetric=1 AND (r.id IS NULL OR r.note IS NOT l.note) ORDER BY l.id; -- no rows
WITH RECURSIVE rooted(id) AS (
 SELECT id FROM measurements WHERE supersedes_id IS NULL
 UNION
 SELECT m.id FROM measurements m JOIN rooted r ON m.supersedes_id=r.id
)
SELECT id FROM measurements EXCEPT SELECT id FROM rooted ORDER BY id; -- no rows
SELECT h.id FROM habit_periods h LEFT JOIN metrics m ON m.id=h.metric_id
 WHERE m.id IS NULL OR m.unit<>'' OR EXISTS
 (SELECT 1 FROM habit_periods p WHERE p.metric_id=h.metric_id AND p.id<>h.id
  AND p.start_day<=coalesce(h.end_day,'9999-12-31') AND coalesce(p.end_day,'9999-12-31')>=h.start_day)
 ORDER BY h.id; -- no rows
SELECT v.id FROM measurement_values v WHERE v.value NOT IN (0,1)
 AND EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id=v.metric_id) ORDER BY v.id; -- no rows
SELECT n.id FROM entity_names n JOIN entities e ON e.id=n.entity_id
 WHERE (length(n.name_key)=10 AND date(n.name_key) IS n.name_key
   AND (e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key))
 OR (e.is_journal
   AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id; -- no rows
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
  Planning queries independently check project type, one-off ownership and occurrence membership/end semantics
  ([planning](planning.md)). They retain tombstoned parents and completed historical slots beyond an end; those
  are valid history, not damage. The occurrence-membership query restates the expression of the
  `task_occurrences_admit` trigger, so it detects rows admitted while the trigger was bypassed, not an error in the
  expression itself; the independent check of that expression is the anchor-generated calendar oracle of the
  planning suites ([the suite guide](../../tests/README.md)). The remaining semantic queries detect missing symmetric reverse links or
  unequal shared notes, correction rows unreachable from an original reading (cycles or dangling chains),
  overlapping habit periods and periods on a metric with a unit (a scale, Mood included), current nonbinary habit values, and invalid journal-name ownership
  (executed with deliberately bypassed guards restored before checking). The rooted-chain query visits each
  reachable reading once; it does not follow every row's full ancestry. Habit range checks include every current
  reading of a metric with a habit period; corrected or retracted invalid values remain legitimate history.
  Journal diagnostics report `entity_names.id`; other diagnostics report the inspected table's `id`.
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
cannot independently recompute the pinned Unicode name-key algorithm, extract CommonMark wikilinks, decode
pictures, reconstruct overwritten history or establish that a plausible value is what the owner recorded.
Those boundaries need the [writer contract](../guides/building-a-writer.md), retained source evidence where
available, and [snapshots](../decisions/D25-snapshots.md); a clean result never certifies completeness or truth.
