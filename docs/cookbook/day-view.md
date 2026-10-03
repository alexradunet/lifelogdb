# The day view

The day's page, other pages written that day, the places the owner was at, the habits active that
day with their state ([habits](habits.md)), and the other measurements (through `measurement_values`, so corrected readings never show).
`ORDER BY (at IS NOT NULL), at` puts undated items — the day page first — before the rest on purpose;
a bare `ORDER BY at` does it by accident.

```sql
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, p.body AS detail
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.title_key = :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.updated_at > e.created_at THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.day = :day AND p.title <> :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'at', NULL, pl.title
    FROM pages d
    JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
    JOIN pages pl   ON pl.id = l.to_id
    JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL
   WHERE d.title_key = :day
  UNION ALL
  SELECT 'habit', NULL, m.title || ': ' ||
         CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day)
           WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END
    FROM habit_periods h JOIN pages m ON m.id = h.metric_id
   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
  UNION ALL
  SELECT p.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id JOIN pages p ON p.id = m.id
   WHERE me.day = :day
     AND NOT EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = me.metric_id
                        AND h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day)
)
ORDER BY (at IS NOT NULL), at;
```

A page shows on the day it was *written* (a link target the app created has no day, [D5](../decisions/D05-pages-and-day-pages.md)), flagged if
edited since.
