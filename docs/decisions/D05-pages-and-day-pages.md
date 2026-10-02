# D5 — One `pages` table for all prose; the journal is a page per day; titles are permanent.

**Status:** accepted

- **Decision.** One text entity `pages`, every row titled, unique and linkable — an essay, a reference
  page, a tag, a person, and the journal.
  - The journal is **one day page per local day**, titled with the day (`2026-09-29`); its `day` is
    its title (`pages_day_page`), so `[[2026-09-29]]` reaches it and the day page of a day is one
    lookup by key. Capture appends to today's page and creates it on the first write ([capture](../cookbook/capture.md), [titles and wikilinks](../contract/titles-and-wikilinks.md)).
  - *Dated is a property, not a type:* the app sets `day` on a page the owner creates on purpose and
    leaves it NULL on a page it creates as a link target, so the day view ([the day view](../cookbook/day-view.md)) shows what was
    **written** that day, not what was **mentioned** — except a day page, whose title is its day.
  - **Tags are pages**: one graph, one syntax.
  - **Titles** are permanent (`pages_title_fixed`), unique by `title_key` ([titles and wikilinks](../contract/titles-and-wikilinks.md)) and safe as a file
    name everywhere (`pages_title_safe`).
- **Why a page per day.** The first real import, an Obsidian vault of one note per day, met untitled
  journal entries: no day could be linked, and every `[[2026-08-20]]` the vault wrote made a second,
  empty page beside that day's entries — two homes for one day. Untitled entries also needed a second
  kind of page with its own CHECKs, and an inbox column nobody used.
- **Alternatives.**
  - *Untitled memos, the day page a query over them (a Memos-style stream, which is also the inbox)*:
    rejected — the incident above; and a page needs no second state.
  - *A `journal` table beside `pages`*: rejected — a day would not be linkable (`[[…]]` reaches only
    pages), and prose would have two homes.
  - *A day page with a free title and a unique `day`*: rejected — `[[2026-09-29]]` could not find it
    without a second lookup rule, and two pages could claim one day by title and by column.
  - *Separate `note` and `wiki` kinds*: rejected — they would differ only in the day rule and share
    one title namespace; a `[[link]]` to a title that does not exist yet creates a page, and anything
    linked before it was written would keep whatever kind the link guessed.
  - *Renames*: rejected — renaming silently repoints every `[[Old Title]]` in decades of prose, or
    leaves ghosts if it doesn't; a redirect stub keeps both working ([titles and wikilinks](../contract/titles-and-wikilinks.md)).
  - *ASCII-only case-insensitive uniqueness (`COLLATE NOCASE`)*: rejected — `Café notes` and
    `CAFÉ NOTES` (and NFC vs NFD spellings) would be distinct rows. *ASCII-only titles*: rejected —
    a life log has `日本語` and `Zürich` in it.
  - *An ICU or app-registered collation*: rejected — a database whose index needs a collation only one
    program supplies can be read by anyone but not written or integrity-checked (`no such collation
    sequence`, executed).
  - *Id-named files, so that titles need no file-name rules*: rejected — the rules are the strict
    direction: loosening `pages_title_safe` after the freeze is one `DROP CONSTRAINT` + `ADD
    CONSTRAINT` ([D13](D13-migrations-and-freeze.md)), while tightening it later would meet titles that already break the new rule.
    *Reopen only if* a title you actually want is forbidden (`Re: plan`) often enough to hurt.
- **Costs accepted.** An entry in a day page has no time of its own: a time worth keeping is written
  in the text. There is no inbox. A title cannot be corrected in place: a new page and a stub. An
  empty page created on purpose that nothing links to shows in `ghost_pages`.
- **Sources.** Kaydet [R41](../research/references.md#r41); FxLifeSheet [R9](../research/references.md#r9)[R42](../research/references.md#r42); Windows reserved names [R58](../research/references.md#r58); Unicode security [R63](../research/references.md#r63).
