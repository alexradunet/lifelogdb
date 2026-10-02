# D12 — Audit trail: no revision tables.

**Status:** accepted

- **Decision.** No revision or history tables. The temporal metadata is row-level: `created_at`
  (on every table that has it — entities, links, measurements), `updated_at`, the tombstone, `source`,
  and the append-only facts. A commit must survive power loss, so connections use
  `synchronous = FULL` ([connection setup](../contract/connections.md)) [R54](../research/references.md#r54).
- **Alternatives.** *Full revision snapshots per edit*: rejected — significant code for a history
  nobody has asked to query. *Trigger-based history tables* (`sqlite-history` [R48](../research/references.md#r48)): rejected **for
  now**; it retrofits onto the current schema with no redesign if a real need appears.
- **Cost accepted.** An `UPDATE` to a mutable row (a page body, a person) overwrites the old
  value, and nothing recovers it.
- **Sources.** [R48](../research/references.md#r48)[R54](../research/references.md#r54).
