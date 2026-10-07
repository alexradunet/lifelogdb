# The day view

The day's page, other pages written that day, the places the owner was at, the habits active that
day with their state ([habits](habits.md)), and the other measurements (through `measurement_values`, so corrected readings never show).
`ORDER BY (at IS NOT NULL), at` puts undated items — the day page first — before the rest on purpose;
a bare `ORDER BY at` does it by accident.

```sql
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, e.body AS detail
    FROM entities e
   WHERE e.preferred_name_key = :day AND e.day = :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.updated_at > e.created_at THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM entities e JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE e.day = :day AND e.preferred_name_key <> :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'at', NULL, pl.title
    FROM entities d
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
    JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL
    JOIN entity_names pl ON pl.entity_id = e.id AND pl.name_key = e.preferred_name_key
   WHERE d.preferred_name_key = :day AND d.day = :day AND d.deleted_at IS NULL
  UNION ALL
  SELECT 'habit', NULL, m.title || ': ' ||
         CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = e.id AND v.day = :day AND v.session_id IS NULL)
           WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END
    FROM habit_periods h JOIN entities e ON e.id = h.metric_id AND e.deleted_at IS NULL
    JOIN entity_names m ON m.entity_id = e.id AND m.name_key = e.preferred_name_key
   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
  UNION ALL
  SELECT p.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id
    JOIN entities e ON e.id = m.id
    JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE me.day = :day AND me.session_id IS NULL
     AND NOT EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = me.metric_id
                        AND h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day)
)
ORDER BY (at IS NOT NULL), at;
```

A page shows on the day it was *written* (a link target the app created has no day, [D5](../decisions/D05-pages-and-day-pages.md)), flagged if
edited since.
