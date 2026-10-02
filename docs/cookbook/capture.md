# Capture: append to the day page (the universal insert convention)

Every entity insert is two statements in one transaction: `entities` first, `RETURNING id`, then the
domain row with that id, which the app keeps in a variable (below `:page_id`) and binds wherever the
page is meant (the `entities` comment in [schema.sql](../schema/schema.sql)). Capture appends to today's page; the first capture of a day creates it.

```sql
BEGIN IMMEDIATE;
-- today's page, if the day has one (a day page's key is its title, contract/titles-and-wikilinks.md): found, the app keeps its id
-- as :page_id and skips the two INSERTs; found tombstoned, it revives it as cookbook/save-a-body.md step 2a does
SELECT p.id, e.deleted_at FROM pages p JOIN entities e ON e.id = p.id WHERE p.title_key = '2026-09-29';
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :page_id
INSERT INTO pages(id, title, title_key, day)
VALUES (:page_id, '2026-09-29', '2026-09-29', '2026-09-29');
-- the entry, after a blank line when the page already has text
UPDATE pages SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END
                        || 'Shipped the schema doc. Review pending. [[Lifelog]]'
 WHERE id = :page_id;
-- the body names [[Lifelog]]: the link sync of cookbook/save-a-body.md runs here, inside this same transaction
-- optional mood, attached to the page it belongs to (D6):
INSERT INTO measurements(metric_id, day, value, source, captured_with_id, created_at)
SELECT id, '2026-09-29', 4, 'ui', :page_id, strftime('%Y-%m-%dT%H:%M:%fZ','now')
  FROM metrics WHERE name = 'mood';      -- (a timed reading also sets taken_at and tz)
COMMIT;
```
