# D16 — Places: a page of type `place`; where the owner was is an `at` link from the day page.

**Status:** accepted

- **Decision.** A place is an entity and a page ([D20](D20-named-pages.md)) with `entity_type = 'place'`: its name is the
  page title, and what the owner knows about it is the page's text; it has no point ([D21](D21-location-history.md)).
  Where the owner was on a day is a link `at` from that day's page to the place (several
  places a day are several links; the link's `note` may say when) — so "where was I on 7 August" and
  "when was I at Lakeside" are one link query each ([where was I](../cookbook/where-was-i.md)). There is no `lives-in` kind and no `located-in`
  ([RFC 0017](../rfcs/0017-no-typed-links.md)): where the owner lived, and what is inside what, is prose.
- **Why `at` and not the day page's `[[Lakeside]]`.** A wikilink says the day *names* Lakeside — "we
  talked about going to Lakeside" and "I was at Lakeside" would be the same row. The owner asked to
  register the place they were at on the day itself, so it is a link kind of its own.
- **Alternatives.**
  - *A `visited` link (person → place)*: rejected — undated, and never used by a real import.
  - *A `place_id` column on pages*: rejected — a day has several places, and a column for day pages
    only would sit empty on every other page.
  - *A place as plain text in prose, not a page*: the place queries fail, and backfilling 10 years of
    free text is the painful path.
- **Costs accepted.** `link_kinds` can say "from a page", not "from a *day* page": `links_endpoint_types` refuses an
  `at` from any other page by `entities.is_journal` ([titles and wikilinks](../contract/titles-and-wikilinks.md) says how a day page is recognised).
- **Sources.** [R46](../research/references.md#r46).
