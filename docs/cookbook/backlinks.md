# Backlinks to a page (or to anything)

```sql
SELECT l.kind, e.entity_type, l.from_id, n.title AS label
  FROM links l
  JOIN entities e ON e.id = l.from_id AND e.deleted_at IS NULL
  JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key
 WHERE l.to_id = :page_id;
```

Old names resolve to this same endpoint; backlinks need no redirect traversal. The current preferred
spelling labels each source. Symmetric kinds are mirrored ([D8](../decisions/D08-entities-and-links.md)),
so this one direction suffices for them.
