# D21 — Location: a place has a point and a radius; a photo's place becomes an `at` link when it is kept; no track.

**Status:** accepted

- **Context.** The owner wants the place a photo was taken to say where its day was, as the `at` links of the day
  pages do ([D16](D16-places.md)), for the few photos they keep of a day, from any library — a phone, a backup,
  Google Photos, Apple Photos ([issue 0010](../issues/README.md), [proposal 0005](../rfcs/0005-a-place-has-a-point.md)). A position can be linked
  only if something knows which place it is in, and the owner does not want movements kept.
- **Decision.** A place may have one `places` row, keyed by its page (`places(id, entity_type)` references
  `pages(id, entity_type)`): its point (`places_lat`, `places_lon`, never 0°, 0°: `places_not_null_island`), the
  radius a position must fall in (`places_radius`) and `link_days` (`places_link_days`). When a photo is kept, its
  position is matched to the live places whose circle holds it, the smallest circle first, then the nearest — Café
  Lume before Lisbon; "where was I" reaches Lisbon by `located-in` ([inside a place](../cookbook/inside-a-place.md)).
  A match with `link_days = 1` is an `at` link from the photo's day page; `link_days = 0` marks a place recognised and
  never linked (home, work: they would mark nearly every day). A position no circle holds writes nothing and is
  asked about: the owner names the place, which takes that position as its point if it has none
  ([the place of a photo](../cookbook/place-of-a-photo.md)). The photo's position itself is never stored: the link is
  what is kept. A point is an attribute of the place, fixed by `UPDATE`, never deleted (`places_no_delete`).
- **Distance without math functions.** `sin` and `cos` exist only in builds with `SQLITE_ENABLE_MATH_FUNCTIONS`
  [R72](../research/references.md#r72), so the match uses `+ - *`: the writer binds the metres per degree of longitude at the position's
  latitude (`111320 · cos(lat)`) and compares the squared equirectangular distance with the squared radius. Within a
  city it is within 0.5 % of the great-circle distance, so only a position within 1 % of a radius can fall either
  side (executed, against a haversine oracle). **Known limit:** across the ±180° meridian two nearby points are 360°
  apart, so a circle there is not matched across it; GeoJSON cuts geometry at the meridian for the same reason
  [R71](../research/references.md#r71).
- **What stays out: a track.** No `positions` table, no fix per row: the owner does not want minute-by-minute
  tracking, and a day's `at` links answer "where was I that day". The design is kept for the day it returns —
  additive: `positions`, one GPS fix per row (`taken_at`, the local `day` and `tz`, WGS84 `lat`/`lon`, an optional
  `accuracy_m`, `source`, `import_key`), append-only with no correction row, refusing 0°, 0°; a fix matched to a place
  at query time by the same distance. Reopen when a question needs the moment: "where was I at 15:00 on that day?".
- **Alternatives.**
  - *The places' coordinates outside `life.db`*, in a writer's file: rejected — what the matching knows would be lost
    with that file and differ between writers; a place's point is part of the place.
  - *City names from an offline gazetteer, no point*: rejected as the rule — city-level only, and a page per city
    found; a gazetteer stays a hint when the owner names a place.
  - *One radius for every place*, nearest point within it (the earlier design): rejected — a café and a city cannot
    share a radius.
  - *An online reverse geocoder*: rejected — it would send the owner's positions to a third party.
  - *Latitude and longitude as two metrics*, *each fix an entity*: rejected for the track's day — nothing pairs two
    rows of one fix, and nothing links to a fix.
- **Trade accepted.** `life.db` holds the points of the owner's places, home included: as private as the rest of the
  file ([threat model](../contract/threat-model.md)). A photo matched before its place had a point is not matched again
  by itself; keeping it again, or the import's next run, links it. A point is the owner's, never moved by a photo.
- **Sources.** [R71](../research/references.md#r71)[R72](../research/references.md#r72). Executed: the `places` suite (the ranges, NaN, infinity and 0°, 0° refused; the
  match against a haversine oracle; the antimeridian limit; `link_days = 0` matched and not linked).
