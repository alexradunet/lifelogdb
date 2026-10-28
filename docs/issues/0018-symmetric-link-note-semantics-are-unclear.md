# 0018 — The two directions of a symmetric relationship can retain different notes

- **Date:** 2026-10-06
- **Status:** resolved
- **Seen in:** a direct DDL probe of an explicitly permitted link-note edit.

## What happened

Creating a `friend` link between two synthetic people produced mirrored rows. Updating one row's `note` left the
other unchanged. There were then two different notes for the relationship. This was observed with a temporary
probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows.

The current contract establishes symmetric endpoints but does not say whether notes are directional observations
or shared relationship text. The probe's desired equality is therefore a proposed interpretation, not proof of a
violation of an existing note-equality rule. This issue records an ambiguity to decide before freezing it.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql):

1. Through the writer create `Person A`, `Person B` and a `friend` link with note `Old note`.
2. In a correctly configured write transaction, update only A-to-B's note to `New note`.
3. Read both directions.

Observed: A-to-B has `New note`, B-to-A has `Old note`. Either behavior can be specified, but readers and writers
need the same answer about whether the notes describe one relationship or two perspectives.

## Rules involved

- `links_mirror_insert`, `links_mirror_delete` and `links_fixed` in [schema.sql](../schema/schema.sql).
- [D8](../decisions/D08-entities-and-links.md) and [link rules](../architecture/link-rules.md).

## Resolution

Resolved by [RFC 0017](../rfcs/0017-no-typed-links.md) and [D8](../decisions/D08-entities-and-links.md): the
relationship kinds and the symmetric mirror are gone. `link_kinds` holds `wikilink`, `at` and `part-of`, none of them
mirrored, so no relationship has two rows that can hold two notes.
[RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) is withdrawn.
