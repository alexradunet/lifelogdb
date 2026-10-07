# Import a row once: insert it, re-run it, update a changed one

**Who sets `import_key`:** every writer that may send the same row twice — an importer (re-run, or a
fresh export years later), a phone replaying its offline queue, an agent retrying after a timeout whose
first attempt did commit. It is the key the *sender* gives the row: the source's own id when there is
one ([imports](../contract/imports.md) step 3), else a UUID the client makes once and resends unchanged. A row typed on the hub itself
cannot arrive twice and has none. The same holds for measurements. A key is 1 to 512 bytes without NUL
(`entities_import_key`): an empty one is refused, and a longer source key is hashed by the sender.

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, preferred_name_key, body, created_at, updated_at, source, import_key)
VALUES ('page', 'sourdough', 'Feed the starter the night before.', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:vault', :import_key)
ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING
RETURNING id;   -- the app keeps it as :page_id; no row back = imported before: skip the next INSERT
INSERT INTO entity_names(entity_id, title, name_key)
VALUES (:page_id, 'Sourdough', 'sourdough');
COMMIT;

-- a later run finds the note changed: update the live page that has the key; a tombstoned one stays gone
BEGIN IMMEDIATE;
UPDATE entities
   SET body = 'Feed the starter the night before; 75% water.'
 WHERE id = (SELECT id FROM entities
              WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);
-- then, before COMMIT, the link sync of cookbook/save-a-body.md (steps 1-4) for this page's new body
COMMIT;
```

A run that inserts nothing the second time is the check of [imports](../contract/imports.md) step 5. `import_key` never changes
(`entities_provenance_fixed`), so the key found on the next run is the key written on the first.
A body an import writes or changes is a save like any other ([D19](../decisions/D19-wikilink-save-contract.md)): run the link sync of [save a body](save-a-body.md) in
the same transaction — create or resolve each target the body names, drop the links it no longer
names — so `links(kind='wikilink')` stays equal to the body.
