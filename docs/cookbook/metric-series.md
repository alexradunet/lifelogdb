# Metric series, corrections applied (weight, last 90 days)

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL
  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'
 WHERE me.session_id IS NULL AND me.day > date(:day, '-90 day') AND me.day <= :day
 ORDER BY me.day;
```

Two legitimate readings on one day are both returned; aggregate in the query if a daily value is wanted.

This default series is unassociated, not inherently daily totals. Use explicit [scope](../contract/measurement-scope.md) for session summaries or labeled all-scope historical reads.

For a **recorded-time cutoff**, read the append-only table instead of filtering today's leaf view. Among rows
whose `created_at <= :as_of`, retain the last eligible descendant of each correction chain and omit its NULL
retraction. All scopes are labeled here; current metric/session tombstones are shown, not used to erase history.

```sql
WITH RECURSIVE selected AS (
  SELECT me.* FROM measurements me
  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = :metric
  WHERE me.created_at <= :as_of
),
overridden(id) AS (
  SELECT supersedes_id FROM selected WHERE supersedes_id IS NOT NULL
  UNION
  SELECT me.supersedes_id FROM measurements me JOIN overridden o ON me.id = o.id
  WHERE me.supersedes_id IS NOT NULL
)
SELECT me.id, me.day, me.value, me.session_id,
       e.deleted_at AS metric_deleted_at, s.deleted_at AS session_deleted_at
FROM selected me JOIN entities e ON e.id = me.metric_id
LEFT JOIN sessions s ON s.id = me.session_id
WHERE me.value IS NOT NULL AND NOT EXISTS (SELECT 1 FROM overridden o WHERE o.id = me.id)
ORDER BY me.day, me.taken_at, me.id;
```

`:as_of` is a canonical UTC instant. This is a projection by the supplied recording timestamps, **not a
reconstruction of an earlier committed database**. Equal timestamps include all rows at that instant; correction
ancestry determines the winner. A clock regression can put a correction before its ancestor's timestamp. The
recursive step follows even ancestors above the cutoff, preventing two eligible rows from one chain from appearing
as independent readings. Names, metric/session lifecycle and other mutable metadata are their current values;
the cutoff applies only to measurement recording timestamps. See [D7](../decisions/D07-measurements.md).
