# Rename a page (select a preferred name, retain direct aliases)

The [name contract](../contract/titles-and-wikilinks.md) defines the title predicate and journal reservations.
Validate `:new_title` before beginning. `:old_id` is unchanged; `:new_key` is the normalized reference key.
Run the following in one transaction. Refusal rolls back without changing names, prose, facts or links.

```sql
BEGIN IMMEDIATE;
-- 0) require a live identity which is not its canonical journal day
SELECT entity_type, preferred_name_key, day IS preferred_name_key AS is_day_page, deleted_at
  FROM entities WHERE id = :old_id;
-- 1) a registered key is usable only when it already belongs to :old_id
SELECT entity_id FROM entity_names WHERE name_key = :new_key;
-- refuse another owner's key, including an empty ghost or tombstone; never merge
-- refuse a new calendar date, which is reserved for its journal identity
INSERT INTO entity_names(entity_id, title, name_key)
SELECT :old_id, :new_title, :new_key
 WHERE NOT EXISTS (SELECT 1 FROM entity_names WHERE name_key = :new_key);
-- case-only edits retain the same registry key and owner
UPDATE entity_names SET title = :new_title
 WHERE entity_id = :old_id AND name_key = :new_key AND title IS NOT :new_title;
UPDATE entities SET preferred_name_key = :new_key
 WHERE id = :old_id AND preferred_name_key IS NOT :new_key;
COMMIT;
```

No body or edge is rewritten. Old spellings resolve directly to the same id, including old embeds and
backlinks. Selecting an owned alias creates no additional row; selecting the current spelling is a no-op.
A person, place, metric or file uses the same operation and keeps its typed details, readings and provenance.
A dated ordinary note/file is not a journal identity and remains renameable. `#REDIRECT` is ordinary prose,
not a stub or a second identity; names require no redirect traversal.
