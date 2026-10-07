# Metric series, corrections applied (weight, last 90 days)

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'
 WHERE me.session_id IS NULL AND me.day > date(:day, '-90 day') AND me.day <= :day
 ORDER BY me.day;
```

Two legitimate readings on one day are both returned; aggregate in the query if a daily value is wanted.

This default series is unassociated, not inherently daily totals. Use explicit [scope](../contract/measurement-scope.md) for session summaries or labeled all-scope historical reads.
