# Goals and design principles

**The goal:** one database that a single human writes to for ~50 years and that remains
readable and meaningful long after the current application is gone. The dominant risk is
not SQLite durability (settled — see [D1](../decisions/D01-single-sqlite-file.md)); it is *meaning living only in application code*,
and *canonical data being corrupted by uncontrolled writers*.

**Principles, in priority order:**

1. **Pareto / KISS.** Choose the 20% of mechanism that delivers 80% of the value. Every
   table and column must justify itself against "what if we just didn't have it."
   Complexity that was researched and *deliberately cut* is listed in [non-goals](non-goals.md) so it is not
   silently re-added. The freeze makes this urgent: after it, change is additive only
   (principle 4), so whatever is in the schema then stays.
2. **The schema is the documentation.** Real tables, real column names, real types, real
   constraints. A stranger in 2075 should understand the database from `.schema` output and
   `SELECT * FROM lifelog_meta` alone — so the rules each table needs are comments *inside* its
   `CREATE` statement, the only comments the file keeps, and `lifelog_meta` holds the few rules that
   span tables. (SQLite's own "application file format" essay makes exactly this argument — see [R1](../research/references.md#r1).)
3. **Single writing application.** One application (later with CLI/API/agent
   front-ends) owns all writes to the database. Many
   *processes* are fine — one *writer* owning the conventions. No other app is ever
   pointed at the canonical data with write access (see [D3](../decisions/D03-integer-ids.md)).
4. **Additive-only evolution after freeze.** After the freeze ([D13](../decisions/D13-migrations-and-freeze.md) says what it is), schema changes are
   additive ([D13](../decisions/D13-migrations-and-freeze.md) says what that allows), in numbered forward-only migrations.
   SQLite explicitly blesses additive change as its compatibility mechanism
   ("adding new tables or columns does not change the meaning of prior queries" [R1](../research/references.md#r1)).
5. **Derived data is disposable.** The FTS index and `title_key` can be dropped and rebuilt
   from canonical data at any time; nothing else is derived. `life.db` is irreplaceable.
   (Binary files are out of v1 entirely — see [D9](../decisions/D09-binary-files.md).)
6. **One home per concept, one home per rule.** A concept is stored once ([D6](../decisions/D06-mood-is-a-measurement.md): mood lives in
   `measurements` and nowhere else; a named entity's name is its page title, [D20](../decisions/D20-named-pages.md)), and a rule is
   written once ([the docs index](../README.md) says where each kind of rule lives). A fact that can be derived from another column is not
   stored beside it.
7. **Real use drives change.** A new constraint, trigger or convention needs a real incident behind
   it — a failed import, a bug in the writing application, a question the data could not answer —
   or it must replace something it makes redundant. Hypothetical writers are not incidents.
