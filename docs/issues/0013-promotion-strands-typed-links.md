# 0013 — Promotion can strand a typed link while integrity reports success

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** a synthetic writer audit of category promotion.

## What happened

A metric was filed under an ordinary category page with `part-of`. Promoting the category to a place succeeded,
leaving `part-of` pointing at a type the kind does not permit. The four integrity checks still reported success.
This was observed with a temporary synthetic probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows.

The transition is already acknowledged as a limitation in the non-goals. The audit establishes that ordinary
writer operations, not only deliberately bypassed constraints, can create it. Structural SQLite integrity is not
proof that the graph still satisfies the kind registry.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), through the writer:

1. Create the category page `Body category` and metric `Weight` with unit `kg`.
2. Add `Weight part-of Body category`.
3. Promote `Body category` to a place.
4. Inspect the category's type, the retained link and the integrity result.

Observed: promotion succeeds, the invalid edge remains and integrity is reported clean. Expected: refuse the
transition atomically unless all retained incoming and outgoing edges remain valid; do not silently delete them.

## Rules involved

- `links_endpoint_types` and the registry in [schema.sql](../schema/schema.sql).
- [D8](../decisions/D08-entities-and-links.md), [D20](../decisions/D20-named-pages.md) and [D26](../decisions/D26-metric-categories.md).
- [Integrity checks](../contract/integrity-checks.md) and the promotion limitation in [non-goals](../architecture/non-goals.md).

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes checking both endpoints on type changes and
adding a semantic integrity witness for invalid typed edges. A permanent regression and constraint mutant remain
to be written; no existing check has been changed or described as covering this case.
