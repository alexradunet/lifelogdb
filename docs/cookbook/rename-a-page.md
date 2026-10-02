# Rename a page (a new page and a stub, D5)

What a rename moves and what it refuses is the "Renames" paragraph of [titles and wikilinks](../contract/titles-and-wikilinks.md);
this is its SQL, **one `BEGIN IMMEDIATE` transaction**. `:old_id` is the page renamed, `:new_title` the new title
(checked by the title predicate first) and `:new_key` its `title_key`; `:source` is the renaming writer. Steps 0 and 1
read what the contract's refusals are decided on; a refusal is `ROLLBACK`, and nothing is written.

```sql
BEGIN IMMEDIATE;
-- 0) the page renamed: live, plain, not a day page, not a stub — else ROLLBACK
SELECT p.entity_type, p.title_key, p.day IS p.title AS is_day_page, p.body = '' AS is_empty, e.deleted_at,
       EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect') AS is_stub
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.id = :old_id;

-- 1) the new title: no row, it is free (1a); a row, it is taken (1b)
SELECT p.id, e.deleted_at,
       EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect') AS is_stub
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.title_key = :new_key;

-- 1a) free: the new page, with the old page's text and day (a title that is a day has that day, pages_day_page)
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
RETURNING id;   -- the app keeps it as :new_id
INSERT INTO pages(id, title, title_key, day, body)
SELECT :new_id, :new_title, :new_key, CASE WHEN date(:new_title) IS :new_title THEN :new_title ELSE day END, body
  FROM pages WHERE id = :old_id;
-- the body names its links: the link sync of cookbook/save-a-body.md runs here for :new_id
-- 1b) taken: :new_id is the row found — only if the old page is empty (is_empty), the row is live and not a stub;
--     else ROLLBACK: two texts are never merged

-- 2) the old page becomes the stub, named with the replacement's own spelling. It changes a row only when the old
--    text is empty or now lives on :new_id: the writer checks that it changed one row, else ROLLBACK
UPDATE pages SET body = '#REDIRECT [[' || (SELECT title FROM pages WHERE id = :new_id) || ']]'
 WHERE id = :old_id AND body IN ('', (SELECT body FROM pages WHERE id = :new_id));

-- 3) the redirect
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:old_id, :new_id, 'redirect', strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source);

-- 4) the typed links the old page starts are made again from :new_id (a symmetric kind's mirror follows,
--    links_mirror_insert); one to :new_id itself is dropped, one :new_id has already is kept as it is
INSERT INTO links(from_id, to_id, kind, note, created_at, source)
SELECT :new_id, to_id, kind, note, strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source
  FROM links
 WHERE from_id = :old_id AND kind NOT IN ('wikilink', 'redirect') AND to_id <> :new_id
ON CONFLICT(from_id, to_id, kind) DO NOTHING;

-- 5) the stub keeps its redirect alone: its typed links go (their mirrors with them, links_mirror_delete), and so do
--    its wikilinks (a stub is not scanned: the link sync of its body finds no target)
DELETE FROM links WHERE from_id = :old_id AND kind <> 'redirect';
COMMIT;
```

`Sourdogh` with a body, a `related` link to `Baking` and an `about` link to `Japan`, renamed to `Sourdough`: the new
page holds the body, its wikilinks, `related` (and `Baking` its mirror) and `about`; `Sourdogh` holds
`#REDIRECT [[Sourdough]]` and its `redirect` row. A day that wrote `[[Sourdogh]]` keeps its text and its link, and
counts as a backlink of `Sourdough` ([backlinks](backlinks.md), one hop). A typo ghost `Sm`, empty, renamed to `sam`
when the person `Sam` exists: no new page, the stub reads `#REDIRECT [[Sam]]`. `Sourdough` renamed to `Baking`, which
exists: refused, whether or not `Baking` has text of its own.

Every typed link a plain page can start with the registered kinds (`about`, `related`) may start at any type, so the
move never meets `links_endpoint_types`; a kind the owner registers that the replacement's type may not start makes
step 4 fail, and the rename is rolled back.
