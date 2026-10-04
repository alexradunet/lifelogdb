# 0004 — A file is a page: its text in the body, a small picture kept, the original outside

- **Date:** 2026-10-04
- **Status:** accepted
- **Answers:** 0009, a file the owner keeps has no home (resolved; `git log -- docs/issues`)

## Problem

The owner ingests files whose content belongs in the life log — recordings, PDFs, scans, photos, a few videos — and
wants (the owner's decisions of 2026-10-04):

- **everything that can be text, as text**: a recording's transcript, a PDF's or a scan's text, made by a local model
  and read by the owner; the source is deleted once its text is kept;
- **a small picture kept for good** where the picture is the point (a photo, a video's frame), the original left
  where it is (a photo library) — never stored in `life.db` and never managed by the writer;
- **files the owner picks**, one at a time or a folder at a time — not a whole photo library;
- **one page per file**, whichever writer sends it: never the same file twice.

## Options

- **A. Keep D9 deferred.** Text sources become plain pages with `import_key` = the file's hash; pictures stay out.
  No schema change, but the key deduplicates per `source` only, a transcript appended to a day page has no key, and
  no picture can be kept.
- **B. D9's kept design: originals in a content-addressed `media/` folder** and
  `attachments(entity_id, sha256, ext, mime, size)`. The owner does not want the originals managed: `life.db` stops
  being the one thing to keep ([D1](../decisions/D01-single-sqlite-file.md)), a snapshot no longer holds everything
  ([D25](../decisions/D25-snapshots.md)), a fifth integrity check and a write order (file before row) are needed.
- **C. The originals as BLOBs in `life.db`.** A few hundred GB over a lifetime of photos: every snapshot copies it
  all and every restore check reads it; SQLite caps a value at 1 GB by default, which a video passes.
- **D. A file is a page** (entity type `file`) with a `files` row keyed by `pages(id, entity_type)`, as `people` and
  `metrics` are ([D20](../decisions/D20-named-pages.md), [D27](../decisions/D27-a-metric-is-a-page.md)):
  - `sha256` of the original, unique — one page per original whatever its `source`; looked up before a write;
  - `mime` of the original (`audio/mp4` says the body is a transcript);
  - `preview`, a JPEG of at most 1 MB, its long edge at most 1600 px, or NULL where the text is the point.

  The body is the file's text: a transcript, the text of a PDF, a caption. `![[2026-10-04 Lake.jpg]]` in a day page
  is a wikilink as it is today (executed: the writer's extraction already reads it), so "which days show this
  photo" is the file's backlinks and a caption that names `[[Bob Sample]]` links Bob. Search finds a transcript;
  tombstones, `source`, renames' refusals (a title is a handle) and the save contract apply unchanged. The original
  stays outside: `sha256` finds it again only while its bytes are unchanged.

  **Not additive after the freeze**: it widens two named CHECKs (`entities_entity_type`, `pages_entity_type`) and the
  endpoint types of `wikilink` and `redirect`, which `link_kinds_structure_fixed` refuses to change. So it is
  decided before the freeze.

## Recommendation

**D.** One home per file, the text searchable, the picture kept small, nothing outside the one file to manage. Size,
at about 1 000 pictures a year of ~250 KB: ~12 GB in fifty years, which a snapshot copies each time — the price of
keeping the pictures in the file. JPEG because the copy is for decades and every decoder reads it; AVIF and WebP are
smaller but younger. A low-resolution video clip and a picture of every page of a document are left out until a real
file needs one ([non-goals](../architecture/non-goals.md)).

## Validation

A `files` suite: the type and its composite key; `sha256` (64 lowercase hex, unique), `mime` (`type/subtype`,
lowercase), `preview` (a JPEG by its first bytes, at most 1 MB, `IS` not `=` — `substr` of an empty blob is NULL, so
`=` passes an empty preview: executed); `sha256` and `mime` fixed, a missing preview added later, never deleted, a
tombstone; an embed lands on a file page, a caption links out, `about`, `part-of` and `redirect` reach it; a ghost
page becomes a file; the new cookbook recipe run literally, twice. The orphan query counts a `files` row; the 2075
test asks where the pictures are; the identity suite counts files among the rows never deleted; a vector for
`![[…]]`. A mutant for each new rule.

## Outcome

Accepted: [D9](../decisions/D09-binary-files.md), rewritten in place. One refinement in the making: the writer makes
every preview itself, one sent to it included, so a preview is upright and keeps no metadata — a photo's GPS would be
the location history [D21](../decisions/D21-location-history.md) leaves out.
