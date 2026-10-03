# Mood over time

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN pages m ON m.id = me.metric_id AND m.title_key = 'mood'
 ORDER BY me.day;
```
