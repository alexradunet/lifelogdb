# 0009 — A file the owner keeps has no home: a picture has no place, a transcript cannot say what it came from

- **Date:** 2026-10-04
- **Status:** proposed
- **Seen in:** the vault import, and the ingest the owner is starting: PDFs (lab reports, scans), voice recordings
  and photos, collected from disk

## What happened

The vault import skipped every attachment (`**/*.png`, `**/*.pdf` — skip) and wrote each embed it met as a code span
(`![[photo.png]]` became `` `![[photo.png]]` ``), because files are deferred ([D9](../decisions/D09-binary-files.md)). The
owner removed the attachments from the vault and kept the notes; the files are now to be ingested on their own, and
the schema cannot hold what that needs:

1. **A picture has no place.** A photo the owner chose for a day, or the drawing in a scan, cannot be kept at all —
   not even small.
2. **A transcript cannot say what it came from.** The text of a recording or a PDF can only be a plain page; nothing
   tells a reader that it is a transcript of audio and not something typed, or which file it was.
3. **The same file can be ingested twice.** `entities.import_key` is unique per `source` only
   ([imports](../contract/imports.md) step 3): a recording sent once from a phone (`api`) and once by a bulk import
   (`import:audio`) makes two pages; and a transcript appended to a day page has no entity, so no key at all.

The owner wants every file's text in `life.db`, the source deleted once its text is kept, a small picture kept for
good where the picture matters (photos, a video's frame), and the originals left outside, never managed by the
writer.

## Reproduce

On a fresh database: there is no column that can hold a JPEG, no row that names the file a page was made from, and
`INSERT INTO entities(entity_type, created_at, updated_at, source, import_key)` with the same key under two sources
succeeds twice.

## Rules involved

[D9](../decisions/D09-binary-files.md) (files deferred), [D1](../decisions/D01-single-sqlite-file.md) (one file),
[non-goals](../architecture/non-goals.md) (the binary-files row), `lifelog_meta.source` (`import_key` per source),
[imports](../contract/imports.md) step 3, [importing with a model](../guides/importing.md) (attachments skipped).

## Resolution

Proposed: [0004](../rfcs/0004-a-file-is-a-page.md).
