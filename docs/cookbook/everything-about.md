# Everything about a person (the ark query)

A link has a direction, so query both. The pages that write `[[Bob Sample]]`, day pages included, link to the
person's own id ([D20](../decisions/D20-named-pages.md)), so they are in the first leg; what the person's page body
links to, and the categories it is filed in, are in the second.

```sql
SELECT l.kind, e.entity_type, l.from_id AS other_id, 'in' AS direction
  FROM links l JOIN entities e ON e.id = l.from_id
 WHERE l.to_id = :entity_id AND e.deleted_at IS NULL
UNION ALL
SELECT l.kind, e.entity_type, l.to_id, 'out'
  FROM links l JOIN entities e ON e.id = l.to_id
 WHERE l.from_id = :entity_id AND e.deleted_at IS NULL;
```

The legs are index-served by `links_to` and by the `UNIQUE(from_id, to_id, kind)` index.
