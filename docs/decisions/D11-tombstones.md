# D11 — Deletion: tombstones, never hard deletes.

**Status:** accepted

- **Decision.** `lifelog_meta.deletes`; the `*_no_delete` triggers, which also hold on a connection that forgot
  `foreign_keys` (executed), and under `recursive_triggers=ON` stop `INSERT OR REPLACE` from deleting a row
  (executed, [connection setup](../contract/connections.md)). Junk captured by accident is tombstoned like
  everything else, and an import does not bring it back: a write of a facts file revives a tombstoned person or place
  only on the owner's decision in the import's workspace, and a name the owner rejects there is a tombstone that the
  import carries, on its trial and by each replay ([importing with a model](../guides/importing.md)). A
  tombstone is never earlier than its row's creation (`entities_deleted_after_created`, `sessions_deleted_after_created`,
  `tasks_deleted_after_created`, `task_occurrences_deleted_after_created`): a tombstone stamped by a clock that has
  stepped back behind `created_at` is refused. Tombstoning and un-tombstoning bump `entities.updated_at`
  (`entities_touch`, watching `deleted_at` and `entity_type`, so it cannot re-fire itself under
  `recursive_triggers=ON`, executed).
- **Rationale.** Retaining a tombstoned row preserves its contents and references for recovery and explicit
  historical reads. A currently tombstoned row says when its current deletion was recorded. Revival clears that
  tombstone, and deleting it again replaces the deletion time; no sequence of deletion and revival events is
  retained ([D12](D12-no-revision-tables.md)). Hard deletes also break the `links` graph. Storage cost is irrelevant at this scale.
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
