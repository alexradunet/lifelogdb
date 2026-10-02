# D9 — Binary files: DEFERRED out of v1. The design is kept here for the day it returns.

**Status:** deferred

- **Decision.** No `attachments` table, no `media/` directory, no binary files at all. The design
  below is **perfectly additive later** (a future `attachments` table touches nothing else). Until
  then, a page that needs a file references it in prose.
- **The deferred design.** Binary files live in `media/`, named by SHA-256; `attachments` rows carry
  `(entity_id, sha256, ext, mime, size)`; path = `media/<sha256[0:2]>/<sha256><ext>`, derived, never
  stored. Dedup is automatic. No inline BLOBs: SQLite's own benchmarks put the break-even around
  100 KB [R2](../research/references.md#r2)[R3](../research/references.md#r3), and "To BLOB or Not To BLOB" agrees [R47](../research/references.md#r47). ark and Open Brane converged here
  [R46](../research/references.md#r46)[R43](../research/references.md#r43). No GC: orphans accumulate, and a one-query sweep exists for the day it matters.
- **Reopen trigger.** An actual attachment need appears (photos in day pages, scanned documents).
- **Sources.** [R2](../research/references.md#r2)[R3](../research/references.md#r3)[R43](../research/references.md#r43)[R46](../research/references.md#r46)[R47](../research/references.md#r47).
