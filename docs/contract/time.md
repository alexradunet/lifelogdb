# Time

- **Instants** (`*_at` columns): UTC, ISO-8601, millisecond precision,
  e.g. `2026-06-09T21:14:03.482Z`. Written by the app, never by SQLite defaults
  (SQLite's `CURRENT_TIMESTAMP` is second-precision and non-ISO; `strftime('%Y-%m-%dT%H:%M:%fZ','now')`
  is the in-DB form used by triggers) [R5](../research/references.md#r5)[R6](../research/references.md#r6)[R28](../research/references.md#r28). Milliseconds because `%f` always renders
  `SS.SSS`, which gives one fixed-width string that the round-trip CHECK can compare, that sorts
  chronologically as plain text, and that keeps rows written within the same second in order (a
  reading and its correction) (executed).
- **Local days** (`*_day` columns): the *local calendar date where the thing happened or
  was captured*, TEXT `YYYY-MM-DD`, written at insert time in the zone of the device that captured
  it — a phone's, never the clock or zone of a hub on a server ([D3](../decisions/D03-integer-ids.md)).
  **Never derived from the UTC instant at query time.** This survives timezone changes,
  DST, and travel: "the day I graduated" is a local-date fact, not an instant [R7](../research/references.md#r7)[R8](../research/references.md#r8)[R9](../research/references.md#r9).
- **Round-trip CHECKs.** Every day column is checked with `date(x) IS x`, every instant with
  `strftime('%Y-%m-%dT%H:%M:%fZ', x) IS x`. The `IS` matters: a CHECK passes when it evaluates
  to NULL, and `date()` returns NULL for malformed input, so `date(x) = x` silently *accepts*
  `2026-9-3` (executed).
- **Written versus happened.** `created_at` — on `entities`, `links` and `measurements` alike — is
  when the row was written to `life.db`, never back-dated, so it is an audit trail. When a thing *happened* is its own `day` / `*_at`. (An entry in a day page has no time
  of its own; a time worth keeping is written in its text: a known limit, [D5](../decisions/D05-pages-and-day-pages.md).)
- **Zone.** `measurements.tz` stores the IANA zone of the device that took a timed reading
  (`Europe/Berlin`; NULL = unknown), so its `taken_at` can be read as local time. Only capture time
  can supply it. Every other instant is a write time, read in UTC.
