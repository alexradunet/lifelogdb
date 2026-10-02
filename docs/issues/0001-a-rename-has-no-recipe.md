# 0001 — A rename has no recipe, and the contract leaves open what moves with it

- **Date:** 2026-10-02
- **Status:** resolved
- **Seen in:** the writer `app/` (plan 024), implementing renames against the docs

## What happened

The "Renames" paragraph of [titles and wikilinks](../contract/titles-and-wikilinks.md) says what a rename is — create
the new page, make the old one a one-line `#REDIRECT [[New Title]]` stub, add `links(kind='redirect', from=old, to=new)`
— but the [cookbook](../cookbook/README.md) has no recipe for it, so no suite runs it, and three choices are left to
each writer:

1. **The text.** Does the old page's body move to the new page? A writer must decide; `app/` moves it.
2. **The old page's typed links** (`about`, `related`, ...). They stay on the stub, or move to the new page; `app/`
   moves them (delete and re-insert), so the stub holds only its `redirect`.
3. **Renaming into a page that exists** (a typo ghost `[[Sm]]` into the person `Sam`). `app/` allows it only when
   the old page is empty, and otherwise refuses rather than merging two texts.

Two writers that choose differently write different rows for the same rename.

## Reproduce

On a fresh database: create a page `Sourdogh` with a body and a `related` link, rename it to `Sourdough`, and ask
which page holds the body and the `related` link. The docs do not say.

## Rules involved

[titles and wikilinks](../contract/titles-and-wikilinks.md) ("Renames"), [D5](../decisions/D05-pages-and-day-pages.md),
`pages_title_fixed`, `link_kinds` (`redirect`), [backlinks](../cookbook/backlinks.md) (one redirect hop).

## Resolution

The owner decided (2026-10-02), and the "Renames" paragraph of [titles and wikilinks](../contract/titles-and-wikilinks.md)
now says it: the text and the typed links the old page starts move to the page that holds the new title (a symmetric
kind's mirror with them), so the stub holds only its `#REDIRECT [[New Title]]` body and its `redirect` row; a rename
into a title that exists is allowed only for an empty old page, and otherwise refused, never merged. The recipe is
[rename a page](../cookbook/rename-a-page.md), run by the `renames` suite (`Sourdogh` → `Sourdough` with a body and a
`related` link, a typo ghost into an existing person, a page with text into a taken title refused), which also checks
that the writer's own rename writes the same rows. The writer changed in two places to match: an old page is empty
only when its body is `''` (whitespace was counted as empty), and the new page keeps the old page's `day`, NULL
included (it was given today's).
