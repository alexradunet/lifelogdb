# D4 — Text ownership: the database is canonical.

**Status:** accepted

- **Context.** This was the hardest decision. *Files canonical*: any app, plugin, or future tool
  pointed at the folder becomes a legitimate writer of canonical data → dialect drift and
  corruption. Evidence: the Logseq↔Obsidian ecosystem needs dedicated conversion tools (journal
  filename formats, URL-encoded filenames, block-reference syntax, property formats, task
  statuses) [R29](../research/references.md#r29)[R30](../research/references.md#r30)[R31](../research/references.md#r31)[R32](../research/references.md#r32). *DB canonical*: the fear of meaning trapped in the app.
- **Decision.** `entities.body` in SQLite is the single source of truth for prose. No folder of
  files holds canonical text, and one writing application owns its writes ([D3](D03-integer-ids.md)). The ability to
  leave rests on the file format itself ([D1](D01-single-sqlite-file.md)), its in-file reading summary and the
  language-neutral writer contract ([D17](D17-contract-as-data.md)).
- **Why not files-canonical with discipline (linters + git as recovery net)?** Git is a
  *recovery* net, not a *guard*. The owner's own multi-app history (Obsidian, Logseq, Trilium,
  each leaving residue) is direct evidence that the discipline requirement fails in practice.
- **Why not files-canonical with a single writer?** It inherits every engineering complaint Logseq
  documented when they *split their product in two* over this exact axis: live editing rewrites
  whole files; renaming a page must rewrite every referencing file; files lack persistent IDs and
  timestamps [R34](../research/references.md#r34)[R35](../research/references.md#r35)[R36](../research/references.md#r36).
- **Costs accepted.** Prose is edited only through the writing application's interfaces.
- **Sources.** [R29](../research/references.md#r29)–[R32](../research/references.md#r32), [R34](../research/references.md#r34)–[R36](../research/references.md#r36).
