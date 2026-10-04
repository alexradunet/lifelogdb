# D1 — Container: a single SQLite file.

**Status:** accepted

- **Decision.** SQLite, one file (`life.db`). A file the owner keeps is in it as its text and a small picture; its original is never stored ([D9](D09-binary-files.md)).
- **Alternatives.** Postgres/server DB (rejected: operational burden for single user,
  no longevity benefit); plain files only (see [D4](D04-database-is-canonical.md)); NoSQL embedded stores (rejected:
  weaker durability guarantees, no standard query language for future readers).
- **Rationale.** The US Library of Congress lists SQLite as a *Recommended Storage
  Format* for datasets — one of only four, alongside XML, JSON, CSV [R10](../research/references.md#r10)[R11](../research/references.md#r11). The file
  format has been backwards-compatible since 2004 and the developers commit to reading
  today's files "for decades into the future," planning through 2050 [R12](../research/references.md#r12)[R13](../research/references.md#r13). SQLite's
  application-file-format essay: "Data lives longer than code … an SQLite database remains
  readable long after all traces of the original application have been lost" [R1](../research/references.md#r1).
- **Consequence.** The remaining risk is meaning living in app code — which [D2](D02-typed-strict-tables.md), [D4](D04-database-is-canonical.md) and [D17](D17-contract-as-data.md) address.
