# Mood over time

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'
 WHERE me.session_id IS NULL
 ORDER BY me.day;
```
