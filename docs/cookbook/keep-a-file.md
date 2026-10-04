# Keep a file: a recording, a PDF, a photo (D9)

A file the owner keeps is a page of type `file` with a `files` row, one id ([D9](../decisions/D09-binary-files.md)).
Before the write the writer has, from the original: `:sha256`, the SHA-256 of its bytes in lowercase hex; `:mime`, its
type in lowercase (`audio/mp4`, `image/heic`); and from the owner or a local model: `:body`, the file's text (a
transcript, the text of a PDF or a scan, a caption) and `:preview`, a JPEG of the picture whose long edge is at most
1600 px, or NULL. `:file_title` is the page's title (checked by the title predicate first) and `:file_key` its
`title_key`; `:day` is the local day the file was made when the file says so (a photo's day taken: [the place of a photo](place-of-a-photo.md)),
else the day it is kept. The original is then left where it is, or deleted once its text is
here; it is never written to the database.

**One transaction.** Steps 0 and 1 read what the branches are decided on; a refusal is `ROLLBACK`, and nothing is
written.

```sql
BEGIN IMMEDIATE;
-- 0) the original kept already? A row: the file is kept — use that page, write nothing else except a missing
--    preview (a tombstoned file is left alone), then COMMIT. A file is never kept twice, whatever its source.
SELECT f.id, f.preview IS NULL AS no_preview, e.deleted_at
  FROM files f JOIN entities e ON e.id = f.id
 WHERE f.sha256 = :sha256;
UPDATE files SET preview = :preview
 WHERE sha256 = :sha256 AND preview IS NULL AND :preview IS NOT NULL
   AND id IN (SELECT id FROM entities WHERE deleted_at IS NULL);

-- 1) the title: no row, it is free (1a); a plain page that is not a day page or a stub — a ghost an earlier
--    ![[...]] made — is promoted (1b); any other row (a person, a file, a day page, a stub): ROLLBACK, choose another
SELECT p.id, p.entity_type, p.day IS p.title AS is_day_page, p.body = '' AS is_empty,
       EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect') AS is_stub
  FROM pages p
 WHERE p.title_key = :file_key;

-- 1a) free: the entity and its page, with the file's text
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('file', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
RETURNING id;   -- the app keeps it as :file_id
INSERT INTO pages(id, entity_type, title, title_key, day, body)
VALUES (:file_id, 'file', :file_title, :file_key, :day, :body);

-- 1b) a plain page holds it: :file_id is that page. Its links stay (the id does not change); a tombstoned one is
--     revived. Its text is kept: :body fills only an empty body, and when both have text, ROLLBACK — two texts are
--     never merged
UPDATE entities SET entity_type = 'file', deleted_at = NULL   -- cascades to pages.entity_type
 WHERE id = :file_id AND entity_type = 'page'
   AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :file_id AND kind = 'redirect');
UPDATE pages SET body = :body WHERE id = :file_id AND entity_type = 'file' AND body = '';

-- 2) either way, the files row: the original's hash and type, the picture
INSERT INTO files(id, sha256, mime, preview) VALUES (:file_id, :sha256, :mime, :preview);
-- the body names its links: the link sync of cookbook/save-a-body.md runs here for :file_id
COMMIT;
```

Step 2 cannot go wrong quietly: a page that is not a file — a person, a day page, a stub the `UPDATE` of 1b left as it
was — makes the `files` insert fail on its composite key, and a hash kept already makes it fail on `files_sha256`;
roll the transaction back. `![[2026-09-29 Lake.jpg]]` written in a day page before the file was kept made the ghost that
1b promotes, so that day's link lands on the file. A file page is never renamed and never promoted: its title is its
handle ([titles and wikilinks](../contract/titles-and-wikilinks.md)).

The pictures a day shows: the file pages its day page links, an embed `![[…]]` being a wikilink. The days that show a
file are its [backlinks](backlinks.md).

```sql
SELECT f.id, p.title, f.mime, f.preview IS NOT NULL AS has_preview
  FROM pages d
  JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL
  JOIN links l     ON l.from_id = d.id AND l.kind = 'wikilink'
  JOIN files f     ON f.id = l.to_id
  JOIN pages p     ON p.id = f.id
  JOIN entities e  ON e.id = f.id AND e.deleted_at IS NULL
 WHERE d.title_key = :day
 ORDER BY p.title_key;
```
