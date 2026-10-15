# D15 — No recurrence: a repeating duty is words in a page.

**Status:** accepted

- **Decision.** No table holds a cadence, an anchor or an occurrence: there is no recurrence profile. A
  repeating duty is words in a page ([D23](D23-no-tasks.md)); a birthday is a query over `people.birth_day`; a
  habit's days are measurements ([D24](D24-habits.md)).
- **Why.** The anchored calendar recurrence of the earlier D15 came with the tasks, and left with them
  ([RFC 0019](../rfcs/0019-no-tasks.md)): its arithmetic, its vectors and its integrity query served a question the
  owner does not ask the writer.
- **Alternatives.** *Anchored calendar recurrence with durable outcomes* (the earlier D15): removed. *An RFC-5545
  RRULE string*: rejected, a broad parser for a question nobody asks.
- **Sources.** [R51](../research/references.md#r51), [R52](../research/references.md#r52).
