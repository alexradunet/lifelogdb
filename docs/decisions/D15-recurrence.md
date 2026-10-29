# D15 — No recurrence table: a repeat lives in the owner's task manager.

**Status:** accepted

- **Decision.** No table holds a cadence, an anchor, an expander or an occurrence, and no virtual occurrence exists.
  A repeat is the business of the app the owner plans in ([D29](D29-plan-lines.md)). A birthday is a query over
  `people.birth_day`. A daily habit is a metric, and its days are measurements ([D24](D24-habits.md)).
- **Why.** A stored rule of a repeat needs an expander and occurrence rows, which are a second home for a plan
  ([RFC 0025](../rfcs/0025-no-plan-lines-and-connected-apps.md), [RFC 0019](../rfcs/0019-no-tasks.md)).
- **Alternatives.** *Anchored calendar recurrence with durable outcomes*: rejected, it needs the tasks table. *An RFC-5545
  RRULE string*: rejected, a broad parser for a small need. *A virtual occurrence for each future date*: rejected,
  it needs an expander and a second home for the date.
- **Sources.** [R51](../research/references.md#r51), [R52](../research/references.md#r52).
