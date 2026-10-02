# D18 — Money: DEFERRED out of v1. The design is kept here for the day it returns.

**Status:** deferred

- **Decision.** No `currencies`, `holdings` or `balances`. The first real import kept the owner's
  money notes as text, and no question has needed them as rows yet; what the schema holds at
  the freeze stays for good ([D13](D13-migrations-and-freeze.md)), while these tables are additive later (they reference only
  `entities` and `pages`).
- **The deferred design.**
  - **Exact integers** in minor units of a holding's currency; a closed `currencies` registry whose
    `subunits` (immutable) gives an amount its meaning — any integer, so a 1/5 subunit like MRU works.
  - **`holdings`** — an entity and a page ([D20](D20-named-pages.md)), like a person: `side` (asset or liability) and
    `currency`, both immutable; anything with a balance or a value is a holding: a bank account, a
    pension, a house at your own estimate, a mortgage. Record your own share of joint items.
  - **`balances`** — append-only snapshots of a holding's value on a local day; the newest row per
    `(holding_id, day)` wins (recorded last, the highest `id`) and a NULL amount retracts, read through
    a `balance_values` view. Bitemporal like `measurements` [R68](../research/references.md#r68).
  - **Net worth is derived, never stored, and reported per currency**: amounts of different currencies
    are never added, and nothing converts one into another. The queries (net worth on a day, at every
    month-end, holdings that need updating) and their exact-integer oracle are in the git history of
    `tests/`.
- **Alternatives kept rejected for that day.** *Money as `measurements`*: `REAL` drifts (`0.1 + 0.2`),
  the unit lives on the metric and there is no per-day retraction. *Decimal as TEXT or `NUMERIC`*: TEXT
  sorts as strings, and `NUMERIC` stores a long decimal as REAL. *One signed column and no `side`*: a
  loan typed positive flips net worth silently. *Stored net-worth totals*: they lose the drivers. *A
  double-entry ledger, quantity × price, exchange rates*: each additive on top of balances ([non-goals](../architecture/non-goals.md)).
- **Reopen trigger.** The owner wants net worth over time in this database: the first statement,
  balance or snapshot to be stored as a number.
- **Sources.** [R53](../research/references.md#r53)[R54](../research/references.md#r54)[R55](../research/references.md#r55)[R56](../research/references.md#r56)[R57](../research/references.md#r57)[R68](../research/references.md#r68).
