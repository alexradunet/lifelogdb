# D11 — Deletion: tombstones, never hard deletes.

**Status:** accepted

- **Decision.** [deletion and corrections](../contract/deletion-and-corrections.md); the `*_no_delete` triggers. Tombstoning and un-tombstoning bump
  `entities.updated_at` (`entities_touch`, watching `deleted_at` only, so it cannot re-fire itself
  under `recursive_triggers=ON`, executed).
- **Rationale.** In a biography database, *erasure is itself biographical*: in 20 years it should be
  possible to see what the 2027 version of the owner deleted, and when. Hard deletes also break the
  `links` graph. Storage cost is irrelevant at this scale.
- **Alternatives.** Hard delete + `ON DELETE CASCADE` (destroys evidence, cascades surprises);
  trash-with-expiry (a policy layer that can be added on top of tombstones later).
- **Facts.** `measurements` are not even tombstoned: they are corrected by inserting rows, as medical
  records are. The triggers guard against mistakes, not against a writer that drops
  them, so "tamper-evident" would overstate it.
- **Known asymmetry.** `links` rows are hard-deleted — no tombstone, no audit. Wikilink removal is
  *required* (the body is the truth, [D19](D19-wikilink-save-contract.md)). Authored links (`friend`, `family`, …) go the same way,
  weighed against a split policy and **rejected**: the *evidence* (the day pages that name
  people together) survives, and relationship links are a summary over it. Relationship removal is therefore invisible. The split
  policy stays additive later (a `deleted_at` column + partial unique index + a flag in `link_kinds`),
  and `sqlite-history` triggers [R48](../research/references.md#r48) are the documented retrofit.
