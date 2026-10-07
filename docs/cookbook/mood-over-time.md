# Mood over time

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL
  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'
 WHERE me.session_id IS NULL
 ORDER BY me.day;
```
