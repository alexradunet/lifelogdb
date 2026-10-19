# D15 — No recurrence table: a repeat is a phrase of a plan line, computed when it is ticked.

**Status:** accepted

- **Decision.** No table holds a cadence, an anchor, an expander or an occurrence, and no virtual occurrence exists.
  A repeat is the token `every:` of a plan line ([D29](D29-plan-lines.md)). When the owner ticks the line, the
  writer computes the next date and appends the next line. The small vocabulary of the repeats and the arithmetic
  are in the page [plan lines](../contract/plan-lines.md). A birthday is a query over `people.birth_day`. A
  daily habit is a metric, and its days are measurements ([D24](D24-habits.md)).
- **Why.** The anchored calendar recurrence of the earlier D15 came with the tasks, and left with them
  ([RFC 0019](../rfcs/0019-no-tasks.md)). A repeat that is computed once, when a line is ticked, needs no stored
  rule and no occurrence rows ([RFC 0022](../rfcs/0022-plan-lines-and-period-pages.md)).
- **Alternatives.** *Anchored calendar recurrence with durable outcomes* (the earlier D15): removed. *An RFC-5545
  RRULE string*: rejected, a broad parser for a small need. *A virtual occurrence for each future date*: rejected,
  it needs an expander and a second home for the date.
- **Sources.** [R51](../research/references.md#r51), [R52](../research/references.md#r52),
  [R76](../research/references.md#r76), [R78](../research/references.md#r78).
