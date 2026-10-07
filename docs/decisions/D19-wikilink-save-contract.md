# D19 — The wikilink save contract: one transaction, links follow the body, a bad target never blocks a save.

**Status:** accepted

- **Decision.** [titles and wikilinks](../contract/titles-and-wikilinks.md) and [save a body](../cookbook/save-a-body.md). The DDL does not implement it: the contract lives in the writing
  application, and the DDL checks what it can — endpoint types, filename-safe unique titles.
- **Why.** Without it: the auto-created page for `[[Health/Diet]]` is rejected by the title CHECK; a
  writer that swallows the error and commits leaves an orphan `entities` row, and one that does not
  loses the page's text. An "upsert" of links leaves a link behind after the body dropped it. Expanding tags into body markup would rewrite the owner’s prose.
- **Alternatives.**
  - *Make the database skip a bad target* (a trigger that swallows the page insert, or a title CHECK
    loose enough for any `[[text]]`): a CHECK cannot skip a row, and a title the filesystem cannot hold
    is what the CHECK exists to stop ([D5](D05-pages-and-day-pages.md)).
  - *A regular expression over the raw body*: rejected — it cannot tell code, URLs and raw HTML from
    prose without re-implementing a CommonMark parser.
  - *Expand `#health` into `[[health]]` in the body*: rejected — it rewrites the owner's text.
  - *Obsidian-style `[[Page#Heading]]` / `^block` targets*: rejected — `#` is legal in a title
    (`[[C#]]`); `|alias` is kept because `|` can never be in a title.
  - *Redirect-text suppression*: rejected — names resolve directly through ownership, and a `#REDIRECT` line is ordinary prose, not a second save mode.
- **Costs accepted.** The database does not check that `links` matches the bodies. That drift is
  detectable and repairable: a rebuild from the bodies gives the same links as 400 incremental random
  edits (executed). A skipped target is remembered only in the body's own text.
- **Sources.** [R58](../research/references.md#r58)[R59](../research/references.md#r59).
