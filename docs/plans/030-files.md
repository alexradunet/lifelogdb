# 030 — Files: a file is a page, its text the body, a small picture kept, the original outside

- **Date:** 2026-10-04, at commit `85e938c`.
- **Priority:** P1. **Effort:** L (four phases).
- **Status:** TODO.
- **Answers:** [issue 0009](../issues/0009-a-file-has-no-home.md), [proposal 0004](../rfcs/0004-a-file-is-a-page.md) (option D).

## Why

The owner is about to ingest PDFs, recordings and photos ([0009](../issues/0009-a-file-has-no-home.md)). The schema has no
place for a picture, no way to say a page is the text of a file, and no way to keep one file from being ingested twice
by two writers. This plan makes [proposal 0004](../rfcs/0004-a-file-is-a-page.md) option D the schema, rewrites
[D9](../decisions/D09-binary-files.md) in place, and gives the writer the operation, the API action, the CLI command and
the browser view that use it. It must land **before the freeze**: it widens CHECKs and link-kind endpoint types that
cannot change after it.

## The owner's decisions (2026-10-04)

| subject | decision |
|---|---|
| scope | files the owner picks, one at a time or a folder at a time — not a whole photo library |
| text sources | a recording, a PDF, a scan: its **text** is the page body (made by a local model, read by the owner); the source is deleted once its text is kept |
| pictures | a photo keeps a **preview**: JPEG, long edge at most **1600 px**, at most 1 MB; the original stays outside (the photo library), never stored or managed by the writer |
| video | a poster frame as the preview, the speech transcript as the body; no clip |
| import once | the **SHA-256 of the original** is kept and unique: a file is never ingested twice, whatever writer sends it |

## The design

**Schema** ([schema.sql](../schema/schema.sql), edited in place):

- `entities_entity_type` and `pages_entity_type`: add `'file'`. The `entities` and `pages` comments name a file
  beside a person, a place and a metric.
- New table `files`, after `metrics`:
  - `id INTEGER PRIMARY KEY`, `entity_type TEXT NOT NULL DEFAULT 'file'` (`files_entity_type`: `= 'file'`);
  - `sha256 TEXT NOT NULL` (`files_sha256`: `length = 64`, only `0-9a-f`), and `CREATE UNIQUE INDEX files_sha256`;
  - `mime TEXT NOT NULL` (`files_mime`: lowercase `type/subtype`, at most 127 characters, characters `a-z0-9/.+-`,
    one `/`);
  - `preview BLOB` (`files_preview`: `preview IS NULL OR (substr(preview, 1, 3) IS x'FFD8FF' AND length(preview) <=
    1048576)` — **`IS`, not `=`**: `substr` of an empty blob is NULL, and `=` passes it; probed on 3.53.4);
  - `FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type)`;
  - its comment: a file is a page with the same id; title is its handle (`![[…]]` links it); body its text; the
    original is never stored in `life.db` and stays outside; look `sha256` up before a write; the preview's 1600 px
    rule (the writer scales it); a video keeps one frame; never deleted.
- Triggers: `files_original_fixed` (BEFORE UPDATE OF sha256, mime, WHEN either changes: RAISE), `files_no_delete`,
  `files_touch` (updated_at, with the other touch triggers). A NULL preview may be filled later; nothing else changes.
- `link_kinds`: `wikilink` from and to `'page,person,place,metric,file'`; `redirect` to
  `'page,person,place,metric,file'` (a rename into a taken title may point at a file page).
- `lifelog_meta.schema`: names the files the owner keeps.

**Writer rules** (no schema): a new file page is written on the day it is kept (`pages.day`, like any page written on
purpose). A title held by a plain page that is not a day page or a stub (a ghost an earlier `![[…]]` made) is promoted
to the file, as a person is: its text kept, the incoming text written only into an empty body, and refused when both
have text (two texts are never merged). A title held by anything else is refused. A file kept already: the page found,
nothing written, except that a missing preview is filled; a tombstoned one is left alone. A file page is never renamed
(its title is a handle) and never promoted.

## Phase A — the contract (docs and suites)

1. **schema.sql** as above; `go generate ./...` copies it to `internal/db`.
2. **Decisions:** rewrite [D9](../decisions/D09-binary-files.md) in place, accepted, from the template (context: 0009;
   decision; alternatives B and C of 0004 and a `media/` folder; trade: snapshot size, the hash finds only an unchanged
   original, a deleted source cannot be re-read). [D1](../decisions/D01-single-sqlite-file.md): the originals are not
   stored; a preview is. [D20](../decisions/D20-named-pages.md): a file is a named entity in the same way. The
   decision index row.
3. **Architecture:** principle 5's parenthesis ([goals](../architecture/goals-and-principles.md)); the
   [non-goals](../architecture/non-goals.md) row for binary files becomes two: *keeping or managing the originals* (reopen:
   an original with no other home) and *a video clip or a picture of every page* (reopen: a file a frame or one picture
   cannot stand for); [entity model](../architecture/entity-model.md) text and `er-core` (`pages ||--o| files`);
   [link map](../architecture/link-rules.md) node `page, person, place, metric, file`;
   [page lifecycle](../architecture/page-lifecycle.md) (a ghost can become a file); [product concepts](../architecture/product-concepts.md) row.
4. **Contract:** [integrity checks](../contract/integrity-checks.md) — the orphan query adds `UNION SELECT id FROM
   files`; [titles and wikilinks](../contract/titles-and-wikilinks.md) — an *Embeds* bullet (`![[title]]` is a
   wikilink; showing the picture is a reader's business) and vectors `![[Lake.jpg]]` → `Lake.jpg`, `![[Lake.jpg|a
   lake]]` → `Lake.jpg`; [imports](../contract/imports.md) step 3 — a file is found by its `sha256`, whatever its
   source; [threat model](../contract/threat-model.md) — the asset includes the pictures, and 2075 question 24 *Where
   are the photos, the recordings and the scans?* → `files` | `never stored in life.db`, `sha256`, `JPEG`.
5. **Cookbook:** new recipe `keep-a-file` (index row after `person-or-place`): step 0 the lookup by `sha256`; fill a
   missing preview; create (entity, page, files row; the link sync of save-a-body for the body); promote a ghost;
   the pictures a day shows (the file pages its day page links).
6. **Guides:** [importing with a model](../guides/importing.md) — the attachment lines say a file is kept on its own,
   not with the notes (the code span stays: it keeps a vault from making a ghost of every attachment), and a section
   *Files: a recording, a PDF, a photo* (text by a local model, read by the owner; the preview, made by a writer that
   reads the format or by another tool for HEIC and a video frame; keep it; delete the source only after).
7. **Indexes and totals:** [schema/README](../schema/README.md) (10 tables, 27 triggers, a `files` row);
   [docs/README](../README.md) scope line; [cookbook index](../cookbook/README.md); AGENTS.md and README.md where they
   say attachments are deferred or list what a page can be.
8. **Suites:** a new `files` suite (`tests/files_test.go`, registered in `suites_test.go`, a row in `tests/README.md`)
   that executes every claim of the recipe and the DDL listed in 0004's validation; the kit's `domain()` makes a
   `files` row for type `file`; `identity` counts files among the rows never deleted; `integrity` builds one file so
   the orphan query must count it. Mutants (one per new rule): `files_sha256`, `files_mime`, `files_preview` with `=`,
   the preview size cap, the unique index, `files_original_fixed`, `files_no_delete`, `files_touch`, the FK to
   `pages(id, entity_type)`, `files_entity_type`, `wikilink` not reaching a file, the orphan query forgetting files,
   the recipe not looking `sha256` up first. Keep the mutant count in `tests/README.md` true.

**Done when:** `go generate ./... && go vet ./... && go test ./...` is green, mutants included; every page links and is
reachable (the `document` suite).

## Phase B — the writer's core

1. `internal/preview` (new): `Make(original []byte) ([]byte, error)` decodes JPEG, PNG or GIF (the standard library),
   applies a JPEG's EXIF orientation, scales the long edge to at most 1600 px (area average), encodes JPEG and lowers
   the quality until it is at most 1 MB; `Fit(preview []byte)` keeps a JPEG that already obeys the rules byte for byte
   and remakes anything else it can read. Synthetic test images only (generated in the tests).
2. `internal/core/files.go`: `FileIn{Title, SHA256, MIME, Body, Day, Preview}`; `Tx.AddFile` (the writer rules above,
   the recipe's SQL, the body through `syncWikilinks`); `Store.AddFile`; `Store.Preview(id)`, `Store.PreviewByTitle`;
   `MimeOf(name, head)` (a fixed extension table, then `http.DetectContentType`, lowercased, parameters dropped).
3. `Page` carries `file {sha256, mime, preview}` for a file page; the integrity check's orphan query adds `files`.
4. Tests in `internal/core`: create; the same hash again (another source too) finds the page and writes nothing; a
   missing preview filled, an existing one kept; a ghost promoted; a title held by a person refused; text on both
   sides refused; a tombstoned file left alone; a rename refused.

## Phase C — the surfaces

1. API: `POST /files` (`add-file`; fields `title`, `original` (file), `sha256`, `mime`, `preview` (file), `body`,
   `day`). Multipart: the original is streamed through SHA-256 and never stored; an image the writer can read is
   buffered (up to 64 MB) to make the preview. Url-encoded (an agent that hashed the file itself): `sha256` and `mime`
   required, no preview. `GET /files` lists file pages; `GET /pages/{id}/preview` and `GET /previews?title=` serve the
   JPEG. The page entity of a file has class `file` and a `preview` link. Root links *Files* and offers *add-file*.
2. HTML: a form with a file field is sent as `multipart/form-data`; a file page shows its preview, mime and hash;
   `![[Title]]` renders as the picture (linking the page), and as its alt text when there is none.
3. Client: `DoFiles` streams files as multipart, in-process and remote. MCP: file fields are left out of a tool's
   schema (an agent sends `sha256`, `mime` and the text).
4. CLI: `lifelog file PATH [--title T] [--text FILE] [--preview JPG] [--mime M] [--day D]` — the title defaults to the
   file's name.
5. Importer: the vault's code-span comment cites the new rule instead of "deferred (D9)"; behaviour unchanged.
6. Tests: an API test posting a synthetic JPEG (page, preview served, the same file again finds the page), an
   url-encoded add-file, the embed rendering; the CLI against a throwaway database.

## Phase D — the records close

RFC 0004 accepted (Outcome: D9); the issue resolved and deleted (`git log -- docs/issues`); this plan deleted when
DONE; the plans and issues indexes say what is next.

## Privacy

Tests and fixtures are synthetic: images generated in the tests, hashes of made-up bytes. No real file, recording or
photo is read by a hosted model or committed; `life.db` and its snapshots now hold pictures too, and stay out of git
as before.
