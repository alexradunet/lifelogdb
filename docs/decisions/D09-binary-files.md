# D9 — Files: a file the owner keeps is a page; its text is the body, a small JPEG its picture, and the original stays outside.

**Status:** accepted

- **Context.** The owner ingests recordings, PDFs, scans, photos and a few videos, picked one at a time or a folder at
  a time, and wants everything that can be text kept as text, the source deleted once its text is kept, a small
  picture kept for good where the picture matters, the originals left where they are, and no file kept twice
  ([issue 0009](../issues/README.md), [proposal 0004](../rfcs/0004-a-file-is-a-page.md)). The vault import had skipped
  every attachment.
- **Decision.** A file is an entity of type `file` with a page and a `files` row, one id, as a person is
  ([D20](D20-named-pages.md)): `files(id, entity_type)` references `pages(id, entity_type)`. The page title is the
  file's handle, and `![[2026-10-04 Lake.jpg]]` in a day page is a wikilink to it like any other
  ([titles and wikilinks](../contract/titles-and-wikilinks.md)); the page body is the file's text — a transcript, the
  text of a PDF or a scan, a caption — so search finds it and the people a caption names are its links. `files` holds
  what is the file's own:
  - `sha256`, the hash of the original, unique (`files_sha256`): a writer looks it up before it writes, so a file is
    kept once whatever writer sends it — which `entities.import_key`, unique per `source`, cannot promise;
  - `mime`, the original's type (`files_mime`): `audio/mp4` tells a reader the body is a transcript;
  - `preview`, the picture kept for good (`files_preview`): a JPEG of at most 1 MB whose long edge the writer scales
    to at most 1600 px, made by the writer with no metadata — a photo's GPS would be the location history
    [D21](D21-location-history.md) leaves out — or NULL where the text is the point. A video keeps one frame.

  The hash and the type never change (`files_original_fixed`); a missing preview may be added later. A file is never
  deleted (`files_no_delete`); one kept by mistake is tombstoned ([D11](D11-tombstones.md)). The original is never
  stored in `life.db` and never managed by its writer. A plain page of the file's title — a ghost an earlier embed
  made — becomes the file by the promotion a person takes, its text kept ([keep a file](../cookbook/keep-a-file.md)).
- **A file's day.** A file's page carries the day the file was made when the file says so — a photo's EXIF date, the
  camera's local clock (`lifelog_meta.days`) — else the day it is kept. A file with a picture and a day of its own is
  shown in that day's page by `![[title]]`, appended once; a photo's position links that day to a place
  ([D21](D21-location-history.md), [the place of a photo](../cookbook/place-of-a-photo.md)).
- **Alternatives.**
  - *Keep files deferred, a transcript a plain page keyed by the hash*: rejected — the key deduplicates per `source`
    only, a transcript appended to a day page has no key, and no picture can be kept.
  - *The originals in a content-addressed `media/` folder, `attachments(entity_id, sha256, ext, mime, size)`*
    [R46](../research/references.md#r46)[R43](../research/references.md#r43): rejected — the owner does not want the originals managed; `life.db` would stop
    being the one thing to keep ([D1](D01-single-sqlite-file.md)), a snapshot would not hold everything
    ([D25](D25-snapshots.md)), and a fifth integrity check and a write order (the file before its row) would follow.
  - *The originals as BLOBs*: rejected — hundreds of GB over a lifetime of photos, copied by every snapshot and read by
    every restore check; SQLite caps a value at 1 GB by default, which a video passes.
  - *A row hanging off any entity* (D9's earlier `attachments(entity_id, …)`): rejected — a file would have no title,
    no body and no backlinks: its text would have nowhere to live, and the prose could not name it.
  - *AVIF or WebP previews*: smaller, but younger formats; the copy is for decades, and JPEG is the one every decoder
    reads.
- **Trade accepted.** The file grows by its pictures: about 250 KB a preview, ~12 GB in fifty years at a thousand a
  year, copied by every snapshot. A preview is past the ~100 KB where SQLite's own benchmark finds files faster
  [R2](../research/references.md#r2)[R3](../research/references.md#r3)[R47](../research/references.md#r47) — milliseconds a picture, for one file to keep. The hash finds an original only while its
  bytes are unchanged: a re-encoded or re-shared copy is another file. A deleted source cannot be read again, so an
  error in its text is found only while it lives: the owner reads the text before deleting the source. A file's
  title is a handle, never renamed. The change widens named CHECKs and two link kinds' endpoint types, which
  `link_kinds_structure_fixed` refuses after the freeze, so it was made before it.
- **Sources.** [R2](../research/references.md#r2)[R3](../research/references.md#r3)[R43](../research/references.md#r43)[R46](../research/references.md#r46)[R47](../research/references.md#r47). Executed: the `files` suite (an empty preview passes `=` because
  `substr` of an empty blob is NULL, and `IS` refuses it); the writer's extraction reads `![[…]]` as a wikilink (the
  vectors of [titles and wikilinks](../contract/titles-and-wikilinks.md)).
