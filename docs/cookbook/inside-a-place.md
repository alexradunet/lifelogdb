# Everything inside a place (containment)

`located-in` (place → place: Tokyo → Kanto → Japan) is one-way and transitive. Walk it down with a
recursive CTE — `UNION`, not `UNION ALL`, so a mistaken cycle ends instead of looping (executed) —
then join what hangs off those places. "My days in Japan in 2019": the day pages with an `at` link to
a place inside it ([where was I](where-was-i.md)):

```sql
WITH RECURSIVE inside(id) AS (
  SELECT id FROM entities WHERE id = :place_id AND entity_type = 'place' AND deleted_at IS NULL
  UNION
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id
    JOIN entities ep ON ep.id = l.from_id AND ep.deleted_at IS NULL
   WHERE l.kind = 'located-in'
)
SELECT d.day, pl.title AS place
  FROM inside
  JOIN links l    ON l.to_id = inside.id AND l.kind = 'at'
  JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL
  JOIN entities epl ON epl.id = inside.id AND epl.deleted_at IS NULL
  JOIN entity_names pl ON pl.entity_id = epl.id AND pl.name_key = epl.preferred_name_key
 WHERE d.day BETWEEN :from_day AND :to_day
 ORDER BY d.day, pl.title;
```

A day that names two places inside Japan is listed once per place.
