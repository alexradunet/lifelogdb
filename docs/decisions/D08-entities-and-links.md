# D8 — One `entities` supertype + one polymorphic `links` graph; a closed kind registry; symmetry in-DB.

**Status:** accepted

- **Decision.** The six linkable types (page, person, place, metric, file, period; `entities_entity_type`) share one ID space through `entities`. A domain row takes its id
  from the `entities` insert by `RETURNING id`: `last_insert_rowid()` across statements is moved by any insert in
  between — a link, a ghost page, a measurement — and the next row silently points at the wrong entity (executed). All relationships live
  in one `links(from_id, to_id, kind)` table with real foreign keys (`UNIQUE(from_id, to_id, kind)`
  allows several kinds between one pair, never a duplicate edge). `links.kind` references the closed
  registry `link_kinds`, whose structure is fixed at registration (`link_kinds_structure_fixed`);
  `links_endpoint_types` checks the kind and both endpoint types on every insert, including mirror rows
  — also on a connection with `foreign_keys=OFF`, executed in autocommit. `entities_endpoint_types`
  refuses type changes that would invalidate retained incoming or outgoing edges;
  [semantic integrity](../contract/integrity-checks.md) checks endpoints independently (executed). Symmetric kinds are mirrored
  by trigger on insert *and* delete, so a half-edge cannot exist whatever the writer, and both mirror
  triggers terminate under `recursive_triggers=ON` (executed). `links_mirror_note` gives symmetric
  relationships one shared note, with a change guard that terminates recursively; directional notes stay independent (executed). Links are immutable except `note`
  (`links_fixed`). Cycles (e.g. `located-in`) are not prevented; [inside a place](../cookbook/inside-a-place.md) walks it with `UNION`. Widening a
  kind's endpoint types is a deliberate migration: drop `link_kinds_structure_fixed`, update the row,
  recreate the trigger, in one transaction (executed).
- **Alternatives.**
  - *No supertype; discriminator pairs* (`from_kind TEXT, from_id INT`): rejected — no foreign keys,
    so edges can dangle silently forever.
  - *Per-relationship tables* (`friendships`, `attendance`, …): rejected — N tables and N code paths
    for one concept, and "everything about X" becomes a union over an open-ended set.
  - *A CHECK-list on `links.kind`*: rejected — relationship taxonomy is personal and grows
    ('godmother', 'college-roommate'). Structural enums (`entities.entity_type`) ARE
    constrained: **constrain structure, leave taxonomy open — but never implicit.**
  - *Free-text kinds auto-registered on first use*: rejected — a typo (`Friend`) would register a
    permanent kind.
  - *Symmetry as discipline or as app double-writes*: rejected — every graph query must remember the
    OR, or any future writer (API, CLI, agent) can silently create a half-edge.
  - *A `pending_links` table for unresolved wikilinks*: rejected — a second source of truth; the body
    is the record of an unresolved mention ([D19](D19-wikilink-save-contract.md)).
- **Rationale.** The graph is where a life database earns its keep: ark's "killer feature" is its
  SQLite edge tables answering "everything about my son" in one query [R46](../research/references.md#r46).
- **Sources.** [R46](../research/references.md#r46)[R43](../research/references.md#r43).
