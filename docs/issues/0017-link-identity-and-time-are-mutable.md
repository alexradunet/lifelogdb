# 0017 — Link identity and creation time can change despite the stated immutability rule

- **Date:** 2026-10-06
- **Status:** resolved
- **Seen in:** a direct DDL probe of the documented link mutation boundary.

## What happened

Updating an existing link's `id` and `created_at` succeeded. The current `links_fixed` trigger protects its endpoints,
kind, source and import key but not these two fields, while D8 says links are immutable except for `note`.
This was observed with a temporary synthetic probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows.

This is a storage-contract mismatch. It is not a claim that the writer exposes an action to edit these fields.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), using the [required connection settings](../contract/connections.md):

1. Through the writer create `One`, `Two` and a `related` link between them.
2. In a write transaction, update one direction to an unused link ID and a different valid `created_at` instant.
3. Read the link back.

Observed: both fields change. Expected under the documented rule: the update is refused and neither field changes.

## Rules involved

- `links_fixed` in [schema.sql](../schema/schema.sql).
- The immutable-except-note decision in [D8](../decisions/D08-entities-and-links.md).

## Resolution

Resolved in `schema.sql`: the trigger `links_fixed` refuses a change of `id`, `from_id`, `to_id`, `kind`,
`created_at` and `source`; only `note` may change. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) is
withdrawn.
