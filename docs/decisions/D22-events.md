# D22 — No recorded spans, sessions or events: the day page and plain pages hold them

**Status:** accepted

- **Decision.** The schema has no table for a span of life (a trip, a job, a home), for a session (a sleep, a
  workout) or for an event. A span is a plain page: its title names it, its body says when it was, and the day
  pages inside it link it with `[[Trip to Rome]]`, so its backlinks list those days. Where the owner was on a day
  is an `at` link from the day page to a place ([D16](D16-places.md)), and prose. A number an activity gives is
  a reading on its reporting day, under a metric of its own when it must not add to a daily total (`workout-steps`
  beside `steps`, [D7](D07-measurements.md)).
- **Why.** A stored span needs a boundary grammar (partial dates, qualifiers, an ongoing end) and answers that are
  definite, possible or incomparable for a day. A stored session needs its own clock and a scope for readings. Both
  cost more than they gave: the owner asked neither question, and the day page already says where they were.
- **No generic events.** No events or attendance framework. Explicit personal tasks ([D23](D23-no-tasks.md))
  describe intentions; completing one does not create a span, a reading or presence. Day pages record ordinary
  outings with prose and links ([D5](D05-pages-and-day-pages.md)).
