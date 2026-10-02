# D21 — Location history: DEFERRED out of v1. The design is kept here for the day it returns.

**Status:** deferred

- **Decision.** No GPS track. The owner does not want minute-by-minute tracking: where they were is
  the `at` links of the day pages ([D16](D16-places.md)). A place has no point, since nothing would be matched to it.
- **The deferred design** — additive later (two new tables, nothing else changes): `positions`, one
  GPS fix per row — `taken_at`, the local `day` and `tz`, WGS84 `lat`/`lon` [R71](../research/references.md#r71), an optional
  `accuracy_m`, `source` and `import_key` — append-only with no correction row (a fix is raw sensor
  output; a doubtful one is left out when read, by `accuracy_m`), refusing a fix at exactly 0°, 0° (how
  photo metadata says "no location"); and `places(id, lat, lon)`, a point per place, hanging off its
  page. A fix is matched to the nearest place at query time, never stored, with `+ - *` only (`sin` and
  `cos` exist only in builds with `SQLITE_ENABLE_MATH_FUNCTIONS` [R72](../research/references.md#r72)): the app binds the metres per
  degree of longitude at the fix's latitude and the query ranks places by the squared equirectangular
  distance. The query and its haversine oracle are in the git history of `tests/`.
- **Alternatives kept rejected for that day.** *Latitude and longitude as two metrics*: nothing pairs
  the two rows of one fix. *Each fix an entity*: hundreds a day, and nothing links to a fix.
- **Reopen trigger.** A location export the owner wants kept (Google Timeline, a phone's track, photo
  locations), or a question the day pages cannot answer: "where was I at 15:00 on that day?".
- **Sources.** [R71](../research/references.md#r71)[R72](../research/references.md#r72).
