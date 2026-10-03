# D26 — Categories: a category is a page, and anything is filed in it by a `part-of` link.

**Status:** accepted

- **Context.** The first lab imports (2026-10) registered about a hundred metrics, and the list of
  metrics became one alphabetical run in which a blood marker, an allergen, a drink count and mood sat
  side by side. "Which metrics are biomarkers?" and "what do I take in?" had no answer in the data
  ([proposal 0001](../rfcs/0001-metric-categories.md)). A registry of categories with a pointer to a page stored every
  category's name twice ([proposal 0002](../rfcs/0002-categories-are-pages.md)); a category as a page with a metric's
  own column left two ways to categorise ([proposal 0003](../rfcs/0003-a-metric-is-a-page.md)).
- **Decision.** A category is a plain page, and its name is the page title, as a place's is ([D16](D16-places.md)).
  Anything — a metric ([D27](D27-a-metric-is-a-page.md)), a page, a person, a place — is filed in a category by a
  `part-of` link to the category's page, and a category belongs to another by the same link between their pages
  (`Ferritin` part-of `Iron` part-of `Biomarkers`). `part-of` is a registered kind ([D8](D08-entities-and-links.md)) from
  any entity to a plain page, so the categories are in the one graph: what the owner writes about a category is its
  page's body, a `[[Lipids]]` in the journal is one of its backlinks, and a rename of the page moves the `part-of`
  links that end at it ([titles and wikilinks](../contract/titles-and-wikilinks.md)). The walk is
  [metrics by category](../cookbook/metrics-by-category.md).
- **Habits are not a category.** A metric is a habit while it has a period ([D24](D24-habits.md)); a
  category saying so would be a second home for that fact, and could disagree with it.
- **Alternatives.**
  - *A free-text `metrics.category`*: rejected — `biomarker` and `biomarkers` become two groups, and there
    are no subgroups.
  - *A registry of categories, a tree whose parents never change*: rejected — a table, two CHECKs and a
    trigger to keep a tree nothing else needs, and a name stored beside the title of the page written about it.
  - *A category as an entity type of its own*: rejected — a wider `entity_type` for nothing a plain page cannot
    hold.
  - *`#tags` for pages and a column for metrics*: rejected — two ways to say the same thing.
- **Trade accepted.** Nothing keeps the `part-of` links a tree: anything may be in two categories and links may close
  a cycle, as `located-in` may ([D8](D08-entities-and-links.md)); a walk uses `UNION` and stops at a page it has seen,
  and the writer that draws the tree follows one parent. A `#tag` in a page's text remains a wikilink, not a filing.
  There are no seeded categories: they are the owner's pages.
- **Sources.** The `facts` and `renames` suites (executed).
