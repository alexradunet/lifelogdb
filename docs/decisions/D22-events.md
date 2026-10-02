# D22 — Events: DEFERRED out of v1. The design is kept here for the day it returns.

**Status:** deferred

- **Decision.** No `events` table. What happened on a day is that day's page ([D5](D05-pages-and-day-pages.md)): its text says
  what, and its `[[links]]` say who and where — the days with `[[Ana]]` or at `[[Lakeside]]` are the
  day pages that link them ([the days that name someone](../cookbook/days-that-name.md)); a reading is still a measurement.
- **Why.** The first real import, an Obsidian vault of daily notes, had a model write an event for
  every outing a note told ("we went to Lakeside", a haircut, a visit): each one a second copy of a
  sentence of that day's page, an event and a place of the same name, and nothing a question needed
  that the day page and its links did not already answer. Nothing else needed events yet: no source
  that delivers dated spans or timed sessions has been imported.
- **The deferred design** — additive later (a new table, two link kinds): `events(id, entity_type,
  name, start_day, end_day, start_at, end_at, place_id, note)` hanging off `entities` like `people` off `pages`,
  with the round-trip CHECKs of [time](../contract/time.md) and `end ≥ start`; day-precise events first-class, instants
  optional; one place per event (`place_id`); `attended` (person → event); and an event's **kind** as
  an `is-a` link to the page naming it — [[Workout]], [[Sleep]] — never a column (free text splits a
  kind by spelling, a CHECK list closes a personal taxonomy, and a registry is a second namespace
  beside page titles). An importer would key its events like any entity ([import a row once](../cookbook/import-a-row-once.md)), and a moved event
  would update its row.
- **Alternatives.** *Keep the table and tell importers to write fewer events*: rejected — whatever
  is in the schema at the freeze stays for good ([D13](D13-migrations-and-freeze.md)), and the table had no real row a day page could
  not hold. *An event as a page*: rejected — an event's name is a label, not a unique handle, and
  would collide (`Dentist`).
- **Reopen trigger.** A source that delivers dated spans or timed sessions — a phone's sleep and
  exercise sessions, a calendar, a location export's visits — or a question the day pages cannot
  answer: "how many workouts this year?", "where did I live in 2015?".
