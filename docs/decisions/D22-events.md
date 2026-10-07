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
- **Sessions.** Independently identified lightweight `sessions` carry a kind reference into the named namespace,
  not a unique title or graph identity for every sleep/workout. [Time evidence](../contract/session-time.md) keeps
  unresolved local clocks honest, with independent reporting-day attribution. [Scoped readings](../contract/measurement-scope.md)
  prevent session summaries from becoming extra daily totals.
- **No generic events.** No events/attendance framework. Explicit personal tasks
  ([D23](D23-no-tasks.md)) describe intentions; completing one does not create recorded sessions or presence. Day pages record ordinary outings with prose
  and links ([D5](D05-pages-and-day-pages.md)); structured recorded periods do not imply attendance or future presence.
  A period's membership on a supplied calendar day is not proof of physical interval overlap or location.
