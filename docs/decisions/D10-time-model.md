# D10 — Time model: UTC instants + denormalized local days, both TEXT.

**Status:** accepted

- **Decision.** [time](../contract/time.md). A timed reading's zone is a column because only capture time can supply it: a
  UTC instant alone cannot say whether `22:30Z` was 14:30, 22:30 or 07:30 the next morning. Every
  other instant is a write time, and a write time needs no zone. The DB checks the
  shape of a zone name, not that it is a real zone; readers convert with a tz database, which keeps
  renamed zones (`Europe/Kiev` → `Europe/Kyiv`) as links. The standard way to write an instant with
  its zone is RFC 9557 [R69](../research/references.md#r69): `2026-06-09T21:14:03.482Z[Europe/Berlin]` — exactly `*_at` plus `tz`.
  Provenance (`source`) is a column for the same reason as the zone ([identity and provenance](../contract/identity-and-provenance.md)).
- **Alternatives.**
  - *Instants only, local day computed at query time*: rejected — a timezone move or DST rule
    silently rewrites history. This is why FxLifeSheet needed a dedicated `tag_days` importer to
    reconstruct local dates [R7](../research/references.md#r7), and why health-mcp denormalizes the local date at write time [R8](../research/references.md#r8).
  - *Unix epoch / Julian day integers*: unreadable in the file and in ad-hoc queries (principle 2).
    SQLite's own docs list TEXT ISO-8601 as the canonical representation [R5](../research/references.md#r5)[R6](../research/references.md#r6).
  - *SQLite `CURRENT_TIMESTAMP` defaults*: rejected — second precision, non-ISO (executed) [R28](../research/references.md#r28).
- **Sources.** [R5](../research/references.md#r5)[R6](../research/references.md#r6)[R7](../research/references.md#r7)[R8](../research/references.md#r8)[R28](../research/references.md#r28)[R69](../research/references.md#r69).
