# 033 — Photos from any source: keep the few chosen for a day, their day and place from the photo itself

- **Date:** 2026-10-04, at commit `0063eec`.
- **Priority:** P1. **Effort:** M (two phases).
- **Status:** TODO.
- **Answers:** issue 0010 ([its record](../issues/0010-a-photos-place-has-no-home.md)), for photos kept a few at a time
  from any library. Replaces plan 032, rejected: the owner keeps the few photos chosen for a day, not a library, and
  from any source, not a Google export (`git log -- docs/plans`).

## Why

`life.db` is not a photo archive: it keeps "the most representative photos of a day", one to a few, chosen by the owner
in whatever holds the photos — a phone, a backup folder, Google Photos, Apple Photos. Only those are kept, and only they
give a place. Every source carries the same thing inside the photo itself: its EXIF date and position. Plan 031 already
reads them (JPEG, HEIC) and links the day. What is missing:

- keeping the few chosen in one go, and seeing what would happen before anything is written;
- naming at once the places of the photos near none;
- and no source-specific code: the Google-export inventory of plan 032 goes.

## The owner's decisions (2026-10-04)

| subject | decision |
|---|---|
| which photos | the **few the owner chooses for a day**, in any tool; only they are kept, only they give a place |
| sources | **any**: the writer reads the photo file (its EXIF; a HEIC's container); nothing reads an export's own files |
| not | a photo archive: a library's photos, or their places, taken wholesale |
| HEIC | **no decoder added**: a HEIC gives its day and place from its EXIF; its picture needs a JPEG exported from the photo tool, or `--preview` |

## Phase A — the docs

1. [Non-goals](../architecture/non-goals.md): a row *A photo archive* — a library's photos or their places taken
   wholesale; `life.db` keeps the few photos chosen for a day ([D9](../decisions/D09-binary-files.md),
   [D21](../decisions/D21-location-history.md)), the library stays in the photo tool; reopen when a question needs the
   places of photos not kept.
2. [D9](../decisions/D09-binary-files.md) context: picked a few a day, not a folder at a time;
   [D21](../decisions/D21-location-history.md) context: the photos the owner keeps, from any library.
3. [Importing with a model](../guides/importing.md), *Files*: photos — choose a few for a day in any tool, export or copy
   the originals with their metadata (location included), keep them; the writer reads the day and position from the
   photo itself; a photo without them keeps no day and no place; a HEIC's picture needs a JPEG; a dry run first.
4. Indexes: plan 032 rejected, this plan listed; issue 0010 resolved when this plan is done.

## Phase B — the writer

1. **`lifelog file` keeps several**: any number of paths; a folder stands for its media files (not its sub-folders),
   sorted. Each is the add-file action as today (plan 031), one transaction per photo.
2. **`--dry-run`**: the same batch in transactions that are rolled back (`core.Store.DryRun`), and the same report. It
   is also what the Google inventory told: how many photos carry a date and a position — for any source.
3. **The report**: kept, kept already; per day, the photos kept and the place linked; the photos near no place,
   grouped (within 500 m of a group's first photo), each group with its count, its days, a map link and one file to
   name it with — `lifelog file <file> --at PLACE [--radius M]` — after which the batch again links the rest (a
   re-run writes nothing else). `--at` with several photos names the place for each.
4. **Remove** `internal/takeout` and `lifelog import photos inventory`, and their README lines.
5. **Tests**, synthetic only (`phototest`): a folder kept, a dry run writing nothing, a re-run writing nothing, the
   near-no-place groups, one photo named then the batch linking its group, a HEIC kept without a picture but with its
   day and place.

## Done when

`go generate ./... && go vet ./... && go test ./...` is green; the built binary keeps a synthetic folder on a throwaway
database as the report says.

## Privacy

Synthetic photos only in tests. The writer makes no network request; a map link is the owner's to open.
