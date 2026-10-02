# Everything inside a place (containment)

`located-in` (place → place: Tokyo → Kanto → Japan) is one-way and transitive. Walk it down with a
recursive CTE — `UNION`, not `UNION ALL`, so a mistaken cycle ends instead of looping (executed) —
then join what hangs off those places. "My days in Japan in 2019": the day pages with an `at` link to
a place inside it ([where was I](where-was-i.md)):

```sql
WITH RECURSIVE inside(id) AS (
  SELECT :place_id
  UNION
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id
    JOIN entities ep ON ep.id = l.from_id AND ep.deleted_at IS NULL
   WHERE l.kind = 'located-in'
)
SELECT d.day, pl.title AS place
  FROM inside
  JOIN links l    ON l.to_id = inside.id AND l.kind = 'at'
  JOIN pages d    ON d.id = l.from_id AND d.title = d.day
  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL
  JOIN pages pl   ON pl.id = inside.id
  JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL
 WHERE d.day BETWEEN :from_day AND :to_day
 ORDER BY d.day, pl.title;
```

A day that names two places inside Japan is listed once per place.
