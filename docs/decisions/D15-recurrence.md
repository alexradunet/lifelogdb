# D15 — Anchored calendar recurrence with durable task outcomes

**Status:** accepted

- **Decision.** `tasks_recurrence` and the occurrence admission guards define a small typed calendar profile.
  The shared [planning algorithm and vectors](../contract/planning.md) give its arithmetic, bounded reads and clock
  resolution. Missing slots can be virtual; changed outcomes have stable persisted identities. A task's instruction
  is distinct from its occurrences ([D23](D23-no-tasks.md)).
- **Why.** The owner wants to retain recurring work and each occurrence's completion independently of an application.
  Calculating month/year candidates from the original anchor avoids permanent drift after short months. Original
  slot identity allows a rescheduled occurrence to coexist with the next one. Read expansion avoids filling the
  lifetime database with untouched future rows.
- **Alternatives.** *An RFC-5545 RRULE string*: it moves the meaning into a broad parser and obscures simple queries.
  *Only virtual occurrences*: it cannot retain completion, skips or rescheduling. *Eagerly generating every future
  row*: it creates unnecessary storage and reconciliation. *Editing a cadence in place*: it changes the meaning of
  retained slot keys. The fixed-cadence and shortening rules avoid a series-version framework.
- **Costs accepted.** A cadence change starts a new definition. Completion-relative recurrence, multiple weekdays,
  timed appointments and calendar synchronization are [deferred](../architecture/non-goals.md). Birthdays remain a
  query over `people.birth_day`; habit observations remain measurements ([D24](D24-habits.md)).
- **Sources.** [R51](../research/references.md#r51), [R52](../research/references.md#r52).
