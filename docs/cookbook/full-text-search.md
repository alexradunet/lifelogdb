# Full-text search

```sql
SELECT e.id, n.title,
       snippet(entities_fts, 2, '<b>', '</b>', '…', 24) AS ctx
  FROM entities_fts
  JOIN entities e ON e.id = entities_fts.rowid AND e.deleted_at IS NULL
  JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
 WHERE entities_fts MATCH :query
 ORDER BY rank;
```
