# Where was I: the places of a day, the days at a place (D16)

`:day_page_id` is the day's page ([capture](capture.md) finds or creates it), `:place_id` the place's page. The app
refuses an `at` link from a page that is not a day page.

```sql
-- I was at Lakeside on 2026-07-31, in the evening
INSERT INTO links(from_id, to_id, kind, note, created_at, source)
VALUES (:day_page_id, :place_id, 'at', 'evening', strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
ON CONFLICT(from_id, to_id, kind) DO NOTHING;

-- where was I on :day?
SELECT pl.title, l.note
  FROM entities d
  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
  JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL
  JOIN entity_names pl ON pl.entity_id = e.id AND pl.name_key = e.preferred_name_key
 WHERE d.preferred_name_key = :day AND d.day = :day AND d.deleted_at IS NULL
 ORDER BY pl.title;

-- the days I was at a place, newest first (links_to serves the place)
SELECT d.day, l.note
  FROM links l
  JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL
 WHERE l.to_id = :place_id AND l.kind = 'at'
 ORDER BY d.day DESC;
```
