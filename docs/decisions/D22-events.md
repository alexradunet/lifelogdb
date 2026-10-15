# D22 — Recorded life periods, not a generic event framework

**Status:** accepted

- **Decision.** Named recorded spans use `periods` attached to the named identity: jobs, study and trips whose
  boundaries answer questions ordinary journal prose cannot. [The boundary profile](../contract/period-boundaries.md)
  retains precision/qualifiers and distinguishes unknown from ongoing without invented dates or tolerances.
  `periods_order` refuses only definitely reversed finite spans. Period categories use existing `part-of` links,
  not another kind namespace or primary-kind field. Bodies, aliases and tombstones retain their ordinary meaning.
- **Why.** Comparing readings and writing during remembered life periods needs structured boundaries, with explicit
  possible/definite/incomparable answers. Overlapping records are valid; a query's ongoing horizon is not an observed
  ending or a historical snapshot. A named period is not a copy of each outing sentence in the journal.
- **No sessions.** A sleep, a workout or another repeated activity is not a row of its own. A number it gives is a
  reading on its reporting day, under a metric of its own when it must not add to a daily total (`workout-steps`
  beside `steps`, [D7](D07-measurements.md)). Anything else is prose and links on the day page. A stored session,
  with its own clock and a scope for readings, cost more than it gave: no import or owner wrote one.
- **No generic events.** No events/attendance framework. Explicit personal tasks
  ([D23](D23-no-tasks.md)) describe intentions; completing one does not create a recorded period, a reading or presence. Day pages record ordinary outings with prose
  and links ([D5](D05-pages-and-day-pages.md)); structured recorded periods do not imply attendance or future presence.
  A period's membership on a supplied calendar day is not proof of physical interval overlap or location.
