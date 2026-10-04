# 031 — A place has a point: a photo's place and day when it is kept

- **Date:** 2026-10-04, at commit `f5d84fd`.
- **Priority:** P1. **Effort:** L (three phases).
- **Status:** TODO.
- **Answers:** [issue 0010](../issues/0010-a-photos-place-has-no-home.md), [proposal 0005](../rfcs/0005-a-place-has-a-point.md)
  (option B). The Takeout step that uses it is [plan 032](032-takeout-photos.md).

## Why

A photo's position should say where its day was: an `at` link from the day it was taken to a place
([D16](../decisions/D16-places.md)). Today a place has no point, so no position can be matched, and a file's day is the
day it was kept. This plan gives a place an optional point and radius, makes keeping a photo read its day and position
from the file, link the place, and show the photo in its day's page; a position near no place is reported so the
owner can name it.

## The owner's decisions (2026-10-04)

| subject | decision |
|---|---|
| where the knowledge lives | a point and a radius on the place, **inside `life.db`**; never the photo's position, never a track |
| a position near no place | **asked**: the owner names the place (and its radius), and the photo's position becomes its point |
| the photo's day | the day it was **taken** (the camera's local clock), shown in that day's page by `![[…]]` |
| Home, Work | recognised (they have a point, so they are never asked about) and **not linked** |

## The design

**Schema** ([schema.sql](../schema/schema.sql), in place):

- New table `places`, after `people`: `id INTEGER PRIMARY KEY`, `entity_type TEXT NOT NULL DEFAULT 'place'`
  (`places_entity_type`), `lat REAL NOT NULL` (`places_lat`: −90…90), `lon REAL NOT NULL` (`places_lon`: −180…180),
  `radius_m INTEGER NOT NULL` (`places_radius`: 10…100 000), `link_days INTEGER NOT NULL DEFAULT 1`
  (`places_link_days`: 0 or 1), `places_not_null_island` (`lat <> 0 OR lon <> 0`), `FOREIGN KEY (id, entity_type)
  REFERENCES pages(id, entity_type)`. Its comment: where a place is, never where the owner was; optional; how a
  position is matched (smallest circle, then nearest; the bound metres per degree of longitude; the antimeridian
  limit); `link_days`; edited in place. Triggers `places_no_delete`, `places_touch`.
- `pages.day`'s comment: a file's day is the day it was made when the file says so. The `entities` and `pages`
  comments: a place *may* have a `places` row. The orphan query is unchanged: a place needs no row.

**Writer rules** (the recipe states them; no schema):

- A file whose metadata has a date takes it as its day (EXIF `DateTimeOriginal`, the camera's local clock); else the
  day it is kept. A file with a picture and a day of its own is appended once to that day's page as `![[title]]` (the
  day page created if it has none).
- A position (EXIF GPS, never 0°, 0°) is matched: a live place whose circle holds it, the smallest circle first, then
  the nearest. `link_days = 1`: an `at` link from the photo's day page. `link_days = 0`: nothing. No place: nothing is
  written, and the writer reports the position for the owner.
- Naming a place for a photo (`at`): the page of that title — a place, or a plain page promoted to one, or a new
  place — takes the photo's position as its point if it has none, with the radius given (default 250 m), and the
  day is linked. A place's point is never moved by a photo; the owner moves it (`locate`).
- The photo's position is never written to `life.db`.

## Phase A — the contract

1. **schema.sql** as above; `go generate`.
2. **Decisions:** rewrite [D21](../decisions/D21-location-history.md) in place, accepted: *a place has a point and a
   radius; a photo's place becomes an `at` link when it is kept; no track* — the `positions` design kept as what stays
   out, with its reopen trigger ("where was I at 15:00"). [D16](../decisions/D16-places.md): a place may have a point
   (the alternative line becomes the decision). [D9](../decisions/D09-binary-files.md): a file's day, the embed. The
   decision index.
3. **Architecture:** [non-goals](../architecture/non-goals.md) — the location row keeps the track and "where was I at
   15:00" only; [entity model](../architecture/entity-model.md) — `pages ||--o| places` in `er-core`;
   [product concepts](../architecture/product-concepts.md) — a photo's place.
4. **Contract:** [threat model](../contract/threat-model.md) — 2075 question 25 *Where is a place, and why does a day
   have one?* → `places` | `never where the owner was`, `radius`, `at link`; [integrity checks](../contract/integrity-checks.md) —
   the orphan query's text says a place's row is optional; [imports](../contract/imports.md) — a place's point comes
   from the owner's answer, never from a guess.
5. **Cookbook:** a recipe `place-of-a-photo` (after `keep-a-file`): give a place its point; the match (the query of
   D21's kept design, with the circle and the order above); the `at` link from the day page (created if missing); the
   embed appended once. [keep a file](../cookbook/keep-a-file.md): `:day` is the day the file was made when it says.
6. **Totals and indexes:** [schema/README](../schema/README.md) (11 tables, 29 triggers, a `places` row); the cookbook
   index; AGENTS.md (Identity: a place may have a `places` row).
7. **Suites:** a `places` suite: the keys and CHECKs (NaN and infinity refused, 0°, 0° refused, the ranges); edited in
   place, never deleted, `updated_at`; a place without a row still whole; the recipe run literally; its match against a
   haversine oracle on 400 random positions among 60 places of random radii (the smallest circle holding the
   position, then the nearest; within 1 % of a radius either answer), a tombstoned place never matched, the
   antimeridian limit; the `at` link written once, `link_days = 0` matched and not linked, the embed appended once. The
   writer's own keep writes the same rows. Mutants for each rule; the counts in `tests/README.md`.

## Phase B — the writer's core

1. `internal/photo` (new): `Read(head []byte) Meta` — from a JPEG (APP1) and a HEIC (the ISOBMFF `meta` box: the
   `Exif` item by `iinf` and `iloc`): `Taken` (the local date and time), `Lat`, `Lon`, `HasGPS` (0°, 0° is none),
   `Orientation`. `internal/preview` takes its orientation from it. Synthetic fixtures only: JPEGs with a hand-built
   APP1, a minimal HEIC box tree built in the test.
2. `core`: `Tx.Locate(placeID, lat, lon, radius, linkDays)`; `Tx.MatchPlace(lat, lon)` (the recipe's query);
   `FileIn` gains `Taken bool` (the day came from the file), `Lat`, `Lon`, `HasGPS`, `At`, `Radius`; `Kept` gains
   `At` (the place linked), `Unmatched` (the position, when no place holds it), `Embedded`. The kept-already branch
   runs the place and embed steps too (not on a tombstone).
3. `Page` of a place carries its `point`.
4. Tests: a photo with a date and a position in a known place (linked, embedded once); in no place (reported, nothing
   written); at home (`link_days = 0`, not linked); `At` naming a new place (its point set), an existing place with a
   point (kept), a person's title (refused); the same photo again (no second embed).

## Phase C — the surfaces

1. API: `add-file` gains `at` and `radius`; the original's first megabyte is held to read its metadata.
   A place page shows its point (and a map link) and offers `locate` (`lat`, `lon`, `radius_m`, `link_days`).
   The result of a keep says the place linked, or the position near no place and how to name it.
2. CLI: `lifelog file PATH --at TITLE [--radius M]`; the human output prints the unmatched position with an
   OpenStreetMap link (the owner's browser opens it; the writer makes no request).
3. MCP: `locate` and the new fields come from the catalog.
4. Tests: an API keep of a synthetic JPEG with GPS (linked, embedded), unmatched then named with `at`; `locate`; the
   CLI on a throwaway database.

## Privacy

Synthetic data only: positions made up in the tests, no real photo read by a hosted model. `life.db` now holds the
points of the owner's places (home included); it stays out of git, as do its snapshots.
