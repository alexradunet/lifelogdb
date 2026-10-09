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
   page is their only copy, and the suites in `tests/` read them from it. The [exact-time](../contract/exact-time.md),
   [period-boundary](../contract/period-boundaries.md) and [session-time](../contract/session-time.md) vectors preserve
   the different meanings of recorded time evidence. The [planning vectors](../contract/planning.md)
   similarly define calendar expansion and reminder clock resolution; task writes use that page's transaction and
   revision rules. Persisted outcomes supplement bounded virtual recurrence, without implying notification delivery.
5. For imports, [imports](../contract/imports.md): a program loads rows itself; an agent works with the owner through the
   writer's operations.

Such an application changes nothing here. If it finds a rule that is ambiguous, untestable or missing, that is an
[issue](../issues/README.md): the fix goes into the docs (and their suites), not just into the application.
