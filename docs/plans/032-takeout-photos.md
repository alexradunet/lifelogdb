# 032 — Takeout photos: every photo gives its day's place, the albums the owner names are kept as files

- **Date:** 2026-10-04, at commit `f5d84fd`.
- **Priority:** P1. **Effort:** L (four phases; the last is the owner's).
- **Status:** TODO.
- **Depends on:** plan 031, done (`git log -- docs/plans`): a place's point, the match, a file's day and embed
  ([D21](../decisions/D21-location-history.md), [the place of a photo](../cookbook/place-of-a-photo.md)).
- **Answers:** [issue 0010](../issues/0010-a-photos-place-has-no-home.md), for the source the owner imports first.
  Plan 029 parked the photos of the same export; this plan is that step.

## Why

A Google Photos export holds every photo the owner took, each with a JSON sidecar giving its position. Kept as files,
twenty thousand photos would be ~5 GB of previews; their places alone are the "where was I" of years of days, and cost
one `at` link a day each. So the step takes **the place from every photo** and keeps **as files only the folders
(albums) the owner names**.

## The owner's decisions (2026-10-04)

| subject | decision |
|---|---|
| which photos give a place | **every** photo with a position |
| which photos become file pages | only those in the **folders the owner ticks** (albums), with a preview and an embed in their day's page |
| a position near no known place | grouped into **clusters**, each **asked once** in a file the owner approves |
| Home, Work | named once (a point with `link_days = 0`): recognised, never linked |
| how | the hybrid of the import guide: deterministic code reads and plans; the local model may draft place titles from the hints; the owner approves; the writer writes; trial first, then replay |

## Privacy: a hard rule for whoever executes this plan

The export, its workspace, `trial.db` and `life.db` are private. **No hosted model reads any of them**, not a listing
or a count it was not handed (memory rules `no-hosted-access-to-real-data`, `import-local-llm-only`). The executing
agent writes code, docs and **synthetic** fixtures only; every fact about the real export comes from a command the
owner runs and output the owner pastes. The writer never makes a network request: the map links in `photos.md` are
opened by the owner, and the city list is a file the owner downloads.

## Phase A — the inventory (code only, the owner runs it)

`lifelog import photos inventory <folder>` reads the extracted `Google Photos` folder read-only and prints, never a
name, a date or a value: per folder, the count of media files by extension and of JSON files; how many media files
found a sidecar by each naming form (`name.json`, `name.supplemental-metadata.json`, the 51-character truncations,
`name(1).json` duplicates, `-edited` copies sharing the original's) and how many found none; which sidecar keys appear
and how often (`photoTakenTime`, `geoData`, `geoDataExif`, `description`, `people`); how many positions are 0°, 0°;
how many files carry an EXIF date, by format (JPEG, HEIC, video: none). Synthetic fixtures for every form. **Done
when** the owner has pasted the output and this plan's Phase B is corrected to what it shows.

## Phase B — plan, ask, apply (code)

In the import workspace of the export (`lifelog import setup --workspace "<…>/Google Photos.lifelog" --from life.db`):

1. **`plan-photos`** writes `photos.json` (the writer's, in the workspace only): every media file — its path, its
   SHA-256, its folder, its day (EXIF `DateTimeOriginal`, else the sidecar's `photoTakenTime` in the zone `photos.md`
   names), its position (the sidecar's `geoData`, else `geoDataExif`, else the EXIF GPS; 0°, 0° is none), its caption
   (the sidecar's `description`), and the place it matches in the trial database or the cluster it falls in. Positions
   no place holds are clustered (greedy, a cluster's members within 300 m of its centre). It drafts `photos.md`:
   - `zone:` (default this machine's), used only for a photo with no EXIF date;
   - `## Folders`: every folder with its count, unticked — the owner ticks the ones to keep as files;
   - `## Places`: one entry per cluster — its id, photo count, first and last day, centre, a radius drawn from its
     spread (at least 100 m), an OpenStreetMap link, the nearest city of `cities15000.txt` if the owner put GeoNames'
     list in the workspace — and an empty `place:` line, to be filled with a title, `Title (no links)`, or `skip`.
2. **`name-place`** fills one cluster's `place:` line and radius (the model drafts from the hints; the owner checks).
3. **`lifelog import approve photos`** — the owner's stamp, at a terminal, as for `rules.md`.
4. **`apply-photos`** (gate: `photos.md` approved), one transaction per day, idempotent:
   - each named cluster's place — an existing place, a plain page promoted to one, or a new place — gets the
     cluster's centre and radius if it has no point (an existing point is kept, the difference reported), and
     `link_days = 0` when marked `(no links)`;
   - each photo is matched again against the points now in the database (smallest circle, then nearest): an `at` link
     from its day page (created if missing) unless `link_days = 0`;
   - each photo of a ticked folder is kept as a file (plan 031): title `YYYY-MM-DD <file name>` (so `IMG_0001.jpg` of
     two years are two titles), its caption as its text, its preview when the writer reads the format, its embed in
     its day's page. The same photo in an album and a year folder is one file and one link (its hash).
   A second run writes nothing.
5. **Replay** carries the step after the facts files when `photos.md` is approved: the same approved files, matched
   against the target database; the export folder must still be there (previews are made from the originals).
6. **Status** reports the photos step: planned, clusters left to name, approved, applied, and the counts.

Tests on synthetic exports only: every sidecar form; a photo with no sidecar (EXIF only) and with neither (skipped,
counted); duplicates across folders; a cluster named, skipped, `(no links)`; an existing place with a point; a
title held by a person (refused, reported); the second run writing nothing; a replay onto a fresh copy equal to the
trial.

## Phase C — the docs

The [import guide](../guides/importing.md) gains *Photos from an export*: the inventory, the plan, naming clusters,
the folders to keep, approval, apply and replay — language-neutral (the operations, not this writer's commands).
README.md lists the commands.

## Phase D — the owner's run

The inventory, pasted; the plan on the trial database; the clusters named (Home and Work `(no links)`); the folders
ticked; approval; apply; the trial's day views read; then the replay onto `life.db`, after a snapshot.
