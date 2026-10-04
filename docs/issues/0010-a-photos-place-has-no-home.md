# 0010 — A photo's place cannot become an `at` link: a place has no point, and a file's day is the day it was kept

- **Date:** 2026-10-04
- **Status:** proposed
- **Seen in:** the files the owner keeps ([D9](../decisions/D09-binary-files.md)) and the Google Takeout export the owner is
  about to import (Google Photos: every photo with a JSON sidecar holding its position)

## What happened

The owner wants the place a photo was taken to say where the day was: an `at` link from that day's page to the place
([D16](../decisions/D16-places.md)), as the Timeline import of plan 029 does for visits. Plan 029 found the photos fill
days Timeline missed, and parked them: "matching it needs place coordinates and code". Three things stand in the way:

1. **A position cannot be matched to a place.** A photo says `38.7139, -9.1394`; an `at` link needs the page `Lisbon`
   or `Lakeside`. A place has no point ([D21](../decisions/D21-location-history.md) deferred it), so nothing in
   `life.db` says which place a position is in, and every photo would have to be named by hand.
2. **A photo lands on the wrong day.** A file page's `day` is the day it was kept
   ([keep a file](../cookbook/keep-a-file.md)), so a photo of 2019 kept today shows in today's day view and says nothing
   about the day it was taken.
3. **Not every place should be linked.** A photo taken at home or at work, linked as `at`, would mark nearly every day;
   plan 029 leaves Home (and Work) out of its links, and the photos must do the same.

The owner's decisions (2026-10-04): a place gets a point inside `life.db` (one point per place, never the owner's
movements); a position near no known place is asked about, and the answer gives that place its point; a photo's page
carries the day it was taken and is shown in that day's page (`![[…]]`); Home and Work are recognised but not linked;
every photo of the export gives its place, and only the albums the owner names are kept as files.

## Reproduce

On a fresh database: a place has no column for a point, so `SELECT … FROM pages WHERE entity_type = 'place'` cannot be
ordered by distance from a position; a file kept today with a photo of 2019 has `pages.day` = today.

## Rules involved

[D21](../decisions/D21-location-history.md) (location history deferred, "photo locations" a reopen trigger),
[D16](../decisions/D16-places.md) (a place is its page alone), [D9](../decisions/D09-binary-files.md) and
[keep a file](../cookbook/keep-a-file.md) (a file's day), `lifelog_meta.days` (a day is the local date of the device
that captured it), [non-goals](../architecture/non-goals.md) (location history).

## Resolution

Proposed: [0005](../rfcs/0005-a-place-has-a-point.md).
