# A person or a place: create one, promote a ghost page (D20)

Step 0 is the resolve of [save a body](save-a-body.md). No row: create it (steps 1–3). A plain page (`entity_type = 'page'`,
e.g. a ghost an earlier `[[Bob Sample]]` made) that is not a day
page and not a redirect stub: promote it instead — a tombstoned one is revived by the promotion, as a save revives a
page it names ([save a body](save-a-body.md) step 2a; the UI says so). A stub (`is_stub`) or any other
row: the handle is taken; choose another (`Bob Sample (colleague)`). A place is the same with its own
type and no domain row: `entities` and `pages` only, and its promotion is the `UPDATE` alone ([D16](../decisions/D16-places.md)).

```sql
-- 0. does the handle exist already?  :handle_key = title_key(:handle_title), contract/titles-and-wikilinks.md
SELECT p.id, p.entity_type, e.deleted_at,
       EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect') AS is_stub
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.title_key = :handle_key;

-- create: entity, page, people row — one id
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('person', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :person_id
INSERT INTO pages(id, entity_type, title, title_key) VALUES (:person_id, 'person', :handle_title, :handle_key);
INSERT INTO people(id, name) VALUES (:person_id, 'Bob Sample');
COMMIT;

-- promote: the plain page :ghost_id becomes a person; its links stay (the id does not change)
BEGIN IMMEDIATE;
UPDATE entities SET entity_type = 'person', deleted_at = NULL   -- cascades to pages.entity_type; revives a tombstoned page
 WHERE id = :ghost_id AND entity_type = 'page'
   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect');
INSERT INTO people(id, name) VALUES (:ghost_id, 'Ana Example');
COMMIT;
```

The day pages that already name the person ([the days that name someone](days-that-name.md)) keep their links: the id did not change. A promotion
cannot go wrong quietly: a page that is already named, a redirect stub, or none at all, makes the `UPDATE` change no
row, so the `people` insert fails on its key; a day page makes the `UPDATE` itself fail
(`pages_day_page_plain`). Both executed; roll the transaction back. A place's promotion has no second
statement, so its writer checks that the `UPDATE` changed one row.
