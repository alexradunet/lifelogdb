# Backlinks to a page (or to anything)

```sql
SELECT l.kind, e.entity_type, l.from_id, pg.title AS label
  FROM links l
  JOIN entities e  ON e.id = l.from_id AND e.deleted_at IS NULL
  JOIN pages    pg ON pg.id = l.from_id
 WHERE l.to_id = :page_id
   AND l.kind <> 'redirect';   -- a rename stub is not a mention of its replacement (contract/titles-and-wikilinks.md)
```

A person or a place is a page, so its title labels it. Symmetric kinds are mirrored ([D8](../decisions/D08-entities-and-links.md)), so
this one direction suffices for them.
