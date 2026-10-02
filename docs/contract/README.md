# Storage contract

These conventions are part of the schema's meaning but cannot be expressed as a constraint. The
ones that span tables are also rows of `lifelog_meta` ([schema](../schema/README.md)), so the file carries them.

| page | what it holds |
|---|---|
| [Time](time.md) | instants, local days, the round-trip CHECKs, written versus happened, the zone |
| [Identity and provenance](identity-and-provenance.md) | entity rows first and ids by `RETURNING`, a person or a place is a page, `source` |
| [Deletion and corrections](deletion-and-corrections.md) | tombstones, append-only readings, imports never `OR IGNORE` |
| [Prose, wikilinks and titles](titles-and-wikilinks.md) | the save contract, the wikilink and `#tag` grammar and its vectors, renames, titles, `title_key`, day pages |
| [Integrity checks](integrity-checks.md) | the four checks that tell whether a file still obeys the schema |
| [Connection setup](connections.md) | the pragmas every writer sets and reads back, the version floor, `BEGIN IMMEDIATE`, read-only readers |
| [Threat model and the 2075 test](threat-model.md) | what is protected and from what; the questions the file alone must answer |
| [Imports](imports.md) | the import path for data that already exists elsewhere, and its traps |
