# D26 — Categories: a category is a plain page, and a thing is filed in it by a `part-of` link.

**Status:** accepted

- **Context.** The first lab imports (2026-10) registered about a hundred metrics, and the list of
  metrics became one alphabetical run in which a blood marker, an allergen, a drink count and mood sat
  side by side. "Which metrics are biomarkers?" and "what do I take in?" had no answer in the data
  ([proposal 0001](../rfcs/0001-metric-categories.md)). A registry of categories with a pointer to a page stored every
  category's name twice ([proposal 0002](../rfcs/0002-categories-are-pages.md)); a category as a page with a metric's
  own column left two ways to categorise ([proposal 0003](../rfcs/0003-a-metric-is-a-page.md)). A day page filed in
  a category was in a category that listed no day ([proposal 0020](../rfcs/0020-a-day-page-is-never-filed.md)).
- **Decision.** A category is a plain page, and its name is the page title, as a place's is ([D16](D16-places.md)).
  Anything but a day page — a metric ([D27](D27-a-metric-is-a-page.md)), a plain page, a person, a place, a file — is
  filed in a category by a `part-of` link to the category's page, and a category belongs to another by the same link
  between their pages (`Ferritin` part-of `Iron` part-of `Biomarkers`). `part-of` is a registered kind
  ([D8](D08-entities-and-links.md)): its `link_kinds` row lets it start at any entity type and end at a `page`, and
  `links_endpoint_types` narrows both ends to a page that is not a day page. So the categories are in the one graph: what the
  owner writes about a category is its page's body, a `[[Lipids]]` in the journal is one of its backlinks, and a
  rename of the page moves the `part-of` links that end at it ([titles and wikilinks](../contract/titles-and-wikilinks.md)).
  The walk is [metrics by category](../cookbook/metrics-by-category.md).
- **A day page is neither a category nor filed in one** (`links_endpoint_types`, which refuses a `part-of` link
  that ends or starts at a day page). A day is the time axis: it is found by its date, and it meets the things of
  its day through the links it leaves — its wikilinks and its `at` links. A plain page whose title only contains a
  date (`Trip 2026-10-01`) is not a day page, and is filed as any page is. A link never changes its endpoints
  (`links_fixed`), and a day page never stops being one. A page becomes a day page only when it selects a date as
  its preferred key. A page with a name cannot do that (`entities_day_identity`, `entity_names_day_insert`); a page
  whose name row is not yet written (the foreign key of its key is deferred) can, and so `entities_day_identity`
  also refuses it while a `part-of` link starts or ends at it.
- **A habit is a metric** with 0/1 readings ([D24](D24-habits.md)), filed in a category like any other; a habit is
  not a category.
- **Alternatives.**
  - *A free-text `metrics.category`*: rejected — `biomarker` and `biomarkers` become two groups, and there
    are no subgroups.
  - *A registry of categories, a tree whose parents never change*: rejected — a table, two CHECKs and a
    trigger to keep a tree nothing else needs, and a name stored beside the title of the page written about it.
  - *A category as an entity type of its own*: rejected — a wider `entity_type` for nothing a plain page cannot
    hold.
  - *`#tags` for pages and a column for metrics*: rejected — two ways to say the same thing.
  - *A day page filed like any page, and shown with the things of its category*: rejected by the owner — the days
    stay apart from the categories.
  - *A writer that refuses to file a day page, with no trigger*: rejected — a rule about the data is a trigger,
    so every writer meets it ([building a writer](../guides/building-a-writer.md)).
- **Trade accepted.** Nothing keeps the `part-of` links a tree: anything may be in two categories and links may close
  a cycle ([D8](D08-entities-and-links.md)); a walk uses `UNION` and stops at a page it has seen,
  and the writer that draws the tree follows one parent. A `#tag` in a page's text remains a wikilink, not a filing.
  There are no seeded categories: they are the owner's pages.
- **Sources.** The `facts`, `links` and `renames` suites (executed).
