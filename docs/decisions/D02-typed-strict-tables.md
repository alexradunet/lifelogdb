# D2 — Typed tables with real columns, `STRICT` mode; no JSON property bags, no EAV.

**Status:** accepted

- **Decision.** Per-domain typed tables; every column real, named, and typed; every table
  declared `STRICT` (SQLite ≥ 3.37): every value must be *losslessly* convertible to the
  declared column type or the statement fails — `'xyz'` into INTEGER is rejected, `12.0` into
  INTEGER is stored as `12` (executed). No JSON columns anywhere. No free-form key/value tables.
- **Alternatives.**
  - *Single `objects` table + JSON properties*: rejected. `json_extract()` re-parses text
    on every call for every row; indexed JSON requires generated-column scaffolding
    [R15](../research/references.md#r15)[R16](../research/references.md#r16); benchmarks of tagging strategies show `json_each()` scans far slower than
    plain indexed tables [R17](../research/references.md#r17). Practitioner reports converge on JSON columns becoming
    "an unmapped wasteland of inconsistent keys" within months [R18](../research/references.md#r18).
  - *EAV (entity-attribute-value)*: rejected — the classic documented anti-pattern once
    attributes are known: no type integrity, no referential integrity, torturous queries [R19](../research/references.md#r19).
  - *Non-STRICT tables*: rejected — STRICT is one keyword per table and moves validation
    from app code into the durable artifact itself (principle 2).
- **Long-tail fields** ("bike serial number") are added by additive migration, *not* by a
  JSON escape hatch: the migration forces an explicit decision that a field deserves to exist.
- **Sources.** [R1](../research/references.md#r1)[R14](../research/references.md#r14)–[R19](../research/references.md#r19).
