# 0037 — REPLACE changes a used link kind's fixed structure

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a schema review probing the link-kind registry with direct SQL on a fresh synthetic database.

## What happened

`link_kinds_structure_fixed` refuses an `UPDATE` of a kind's symmetric flag or endpoint types, but
`REPLACE INTO link_kinds` deletes the conflicting row and inserts a new one. Neither the update trigger nor the
final-state (`NO ACTION`) foreign key from `links.kind` noticed, so a used symmetric kind could be re-registered as
directional: deleting one side of a mirrored edge then left the other, and the symmetry audit no longer expected a
mirror. Replacing the endpoint types left edges whose types the kind no longer admits.

The review also found that the registry comment overstated how a misspelt endpoint-type token behaves: a list such
as `'person,persn'` still admits `person`; the unknown token admits nothing. That claim was wording, not a defect.

## Reproduce

With two people linked by `friend` (both directions stored):

```sql
BEGIN IMMEDIATE;
REPLACE INTO link_kinds(kind, symmetric, from_types, to_types) VALUES ('friend', 0, 'person', 'person');
COMMIT;
DELETE FROM links WHERE from_id = 2 AND to_id = 3 AND kind = 'friend';  -- the reverse edge remained
```

## Rules involved

[D8](../decisions/D08-entities-and-links.md), `link_kinds_structure_fixed`, the `links.kind` foreign key and
`links_mirror_delete` in [schema.sql](../schema/schema.sql). Conforming imports never use `OR REPLACE`
([imports](../contract/imports.md)), and no writer entry point replaces registry rows: the defense is against
accidental SQL.

## Resolution

`links.kind` is `ON DELETE RESTRICT`, which SQLite checks at the parent deletion, so a used kind can be neither
deleted nor replaced; an unused kind may still be, and note edits use `UPDATE`. The links suite covers replacement
of the symmetric flag, the endpoint types and an unchanged definition, the unchanged graph after refusal, the
mirrored deletion afterwards and the replacement of an unused kind, with a mutant. The registry comment now
describes token-by-token matching, with a mixed-list regression. Rejecting unknown, duplicate or empty endpoint
tokens at registration was not added: registering a kind is a rare owner action, and no registry mistake has
happened yet. No RFC: the change narrows administrative SQL only and changes no decision's direction.
