# The place of a photo: give a place its point, match a position, link the day (D21)

When a photo is kept ([keep a file](keep-a-file.md)), its metadata may give the day it was taken, `:taken_day` (the
camera's local date: `lifelog_meta.days`), and a position `:lat`, `:lon` (WGS84 degrees; 0°, 0° is none). The position
itself is never written: it is matched to a place, and the day is linked ([D21](../decisions/D21-location-history.md)).

**Give a place its point** — the owner's answer when a position matched no place. The place they name (an existing
place, a plain page promoted, or a new one: [a person or a place](person-or-place.md)) takes the position as its point if
it has none, with the radius they give; `:link_days` is 0 for a place to recognise and never link (home, work). A photo
never moves a point: the owner fixes one with `UPDATE places SET lat = …, lon = …, radius_m = … WHERE id = :place_id`.

```sql
INSERT INTO places(id, lat, lon, radius_m, link_days) VALUES (:place_id, :lat, :lon, :radius_m, :link_days)
ON CONFLICT(id) DO NOTHING;
```

**Match the position**: the live place whose circle holds it, the smallest circle first, then the nearest. The app binds
`:m_per_deg_lon` = 111320 · cos(`:lat` in radians), so the query needs no math function; `d2` is the squared distance in
metres. No row: the position is near no place — nothing is written, and the writer reports it for the owner to name.

```sql
SELECT id, title, link_days, d2
  FROM (SELECT pl.id, pg.title, pl.link_days, pl.radius_m,
               ((pl.lat - :lat) * 111320.0) * ((pl.lat - :lat) * 111320.0)
             + ((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2
          FROM places pl
          JOIN pages pg   ON pg.id = pl.id
          JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL)
 WHERE d2 <= radius_m * radius_m
 ORDER BY radius_m, d2
 LIMIT 1;
```

**Link the day and show the photo in it**, in one transaction — the keep's own when the photo is new. `:place_id` is the
place matched; when its `link_days` is 0 the `at` link is skipped. `:file_title` is the photo's page title; when the photo
has no picture the embed is skipped.

```sql
BEGIN IMMEDIATE;
-- the day page of :taken_day: found, the app keeps its id as :photo_day_id and skips the two INSERTs; found tombstoned,
-- it revives it (cookbook/capture.md)
SELECT p.id, e.deleted_at FROM pages p JOIN entities e ON e.id = p.id WHERE p.title_key = :taken_day;
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
RETURNING id;   -- the app keeps it as :photo_day_id
INSERT INTO pages(id, title, title_key, day) VALUES (:photo_day_id, :taken_day, :taken_day, :taken_day);
-- where the day was, once
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:photo_day_id, :place_id, 'at', strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
ON CONFLICT(from_id, to_id, kind) DO NOTHING;
-- the photo in its day, once: after a blank line, unless the page shows it already
UPDATE pages SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END || '![[' || :file_title || ']]'
 WHERE id = :photo_day_id AND instr(body, '![[' || :file_title || ']]') = 0;
-- the body names the photo: the link sync of cookbook/save-a-body.md runs here for :photo_day_id
COMMIT;
```

Ten photos of one day at one place make one `at` link (its unique key) and ten embeds; a photo kept again adds nothing.
A place with `link_days = 0` is matched — so its photos are never asked about — and links no day. A photo matched before
its place had a point is linked when it is kept again, or by an import's next run.
