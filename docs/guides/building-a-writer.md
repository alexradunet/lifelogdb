# Building a writer

Any developer, in any language, may build an application that writes a `life.db`. Principle 3 (single writing
application, [goals](../architecture/goals-and-principles.md)) is a rule about *one file*: whatever application writes a
given `life.db` is the only thing that writes it, and every other tool opens it read-only. Another developer's writer
for their own `life.db` is fine if it follows the whole contract; two writers on one file are not.

What a writer needs from this repo:

Keep the contract and vectors matched to the file's schema version. The in-file comments and metadata preserve
a reading summary; they do not contain the complete Unicode/Markdown algorithms or time-zone rules. Version
compatibility follows [D13](../decisions/D13-migrations-and-freeze.md).

1. **[schema.sql](../schema/schema.sql)** as the init DDL, applied to a new file verbatim — `PRAGMA application_id`
   marks it as a Lifelog database.
2. **The [storage contract](../contract/README.md)** as the writer's obligations, chiefly
   [connection setup](../contract/connections.md) (pragmas read back, `BEGIN IMMEDIATE`, the version floor) and
   [titles and wikilinks](../contract/titles-and-wikilinks.md) (titles, `title_key`, the save contract).
3. **The [cookbook](../cookbook/README.md)** as the canonical reads and writes; `lifelog_meta` and the comments inside
   `.schema` as the in-file summary.
4. **The test vectors** printed in [titles and wikilinks](../contract/titles-and-wikilinks.md) as a conformance suite: the
   page is their only copy, and the suites in `tests/` read them from it. The [exact-time](../contract/exact-time.md)
   vectors preserve the meaning of recorded time evidence.
5. For imports, [imports](../contract/imports.md): a program loads rows itself; an agent works with the owner through the
   writer's operations.

Such an application changes nothing here. If it finds a rule that is ambiguous, untestable or missing, that is an
[issue](../issues/README.md): the fix goes into the docs (and their suites), not just into the application.

## Conventions every writer and every DDL change preserves

Their homes are in these docs; this list is the checklist, not the rule.

- **Time** (`lifelog_meta.instants` and `.days`, [D10](../decisions/D10-time-model.md)): UTC ISO-8601 instants and local-day TEXT columns with round-trip CHECKs
  (`date(x) IS x`, `strftime(...) IS x` — the `IS` matters).
- **Identity and provenance**: follow the `entities`, `entity_names`, typed-extension and `lifelog_meta.source`
  rules in [schema.sql](../schema/schema.sql), with the insert conventions in
  [a person or a place](../cookbook/person-or-place.md) and [imports](../contract/imports.md).
  Review owned names, direct typed foreign keys, stable identity and provenance together.
- **No deletes** (`lifelog_meta.deletes`, [D11](../decisions/D11-tombstones.md)): tombstones (BEFORE DELETE triggers);
  only `links` rows are deleted.
- **Append-only facts**: measurements. A reading is corrected with `supersedes_id` and retracted with a
  NULL value (D7).
- **Imports**: `ON CONFLICT … DO NOTHING`, never `OR IGNORE` / `OR REPLACE` ([imports](../contract/imports.md)).
- **CHECKs**: every one NAMED (`CONSTRAINT <table>_<rule> CHECK …`), using only functions the minimum
  SQLite has (`lifelog_meta.sqlite`) — no math functions, even where a build has them.
- **Links**: a closed, endpoint-typed `link_kinds` registry (D8).
- **Names and journal identity**: use [titles and wikilinks](../contract/titles-and-wikilinks.md), its vectors,
  and [D5](../decisions/D05-pages-and-day-pages.md). Check preferred-name changes, retained aliases and journal
  reservations against those rules. Spans and sessions follow [D22](../decisions/D22-events.md);
  habits follow [D24](../decisions/D24-habits.md).
- **Connections** ([connection setup](../contract/connections.md)): one writing application per file; per connection
  `PRAGMA foreign_keys=ON`, `recursive_triggers=ON`, `synchronous=FULL`, `trusted_schema=OFF` (the first three read back and refused
  if wrong); SQLite ≥ 3.51.3 for writers; every write transaction starts with `BEGIN IMMEDIATE`; the driver opens
  no transactions of its own. Readers open the file read-only (`mode=ro`, never `immutable=1`) and set
  `trusted_schema=OFF` too;
  exploration tools (Datasette) likewise, and nothing that edits rows is pointed at it.
- **The wikilink save contract**: implement [titles and wikilinks](../contract/titles-and-wikilinks.md) and
  [save a body](../cookbook/save-a-body.md), including reference resolution, target savepoints, set equality,
  invalid targets, tags and ordinary REDIRECT prose. Keep the contract's vectors as their sole specification.
- **Integrity**: the four [integrity checks](../contract/integrity-checks.md).
