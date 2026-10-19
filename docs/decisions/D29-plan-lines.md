# D29 — A plan is a line of a page: the writer reads it, the file does not know it.

**Status:** accepted

- **Context.** The owner asked on 2026-10-19 for the planning axis ([issue 0087](../issues/0087-a-plan-written-as-words-cannot-be-read-as-a-plan.md),
  [RFC 0022](../rfcs/0022-plan-lines-and-period-pages.md)). The tasks of 2026-10-07 had cost two homes for one fact
  and were cut ([RFC 0019](../rfcs/0019-no-tasks.md)).
- **Decision.** A plan is a list item of a page body that starts with a box. The grammar, the repeats and the
  arithmetic of the next date are the page [plan lines](../contract/plan-lines.md), with vectors. The file holds no
  table, column or constraint for a plan: the body is the truth, and a line is derived from it at read time, as a
  wikilink is ([D19](D19-wikilink-save-contract.md)). Three actions of the writer, not of the file, change a line:
  `plan` adds one, `tick` closes one (and adds the next line of a repeat) or reopens one closed by mistake (and
  removes that next line), `carry` moves one to another page. Each is an edit of a body.
- **Alternatives.**
  - *A `tasks` table* (the earlier [D23](D23-no-tasks.md)): two homes for one fact, and the cost [RFC 0007](../rfcs/0007-personal-tasks-and-occurrences.md) showed.
  - *A derived index table*: a table in the frozen file that stays in its shape for good. Rejected.
  - *A generated column with a partial index* (`entities.has_box`, `entities_plans`): chosen, after `planned_day`
    took about 850 ms at lifetime scale. Any filter on `body` reads every body. SQLite keeps the index at every write,
    it is disposable, and the body is its one home. `planned_day` takes about 49 ms with it.
  - *The emoji grammar of Obsidian Tasks* ([R76](../research/references.md#r76)): the owner chose words, in the
    manner of todo.txt ([R77](../research/references.md#r77)), since a `key:value` token reads without a legend.
  - *An RFC 5545 RRULE* ([D15](D15-recurrence.md)): a broad parser for a small need. The repeats are a short list.
- **Trade accepted.** Nothing in the file refuses a malformed line. A line has no id: a tick names it by its number
  and its text, and an open edit of that page goes stale when a tick lands, as it does for a capture
  ([D12](D12-no-revision-tables.md)). The mitigation is the grammar with its vectors, which every writer reproduces.
  A rule about a line cannot be a CHECK, since the file reads the body as prose.
- **Sources.** [R76](../research/references.md#r76), [R77](../research/references.md#r77),
  [R78](../research/references.md#r78), [R59](../research/references.md#r59). Executed: the `plan-lines` suite
  reads the vectors of the contract page and reproduces them with the writer's parser.
