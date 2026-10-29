# D16 — No places: a place is a plain page, and where a day was is the words of the day page.

**Status:** accepted

- **Context.** [Issue 0091](../issues/0091-places-cost-more-than-they-give.md): of thirty places in the owner's file,
  five are linked from a day page. A place has no typed row and no point ([D21](D21-location-history.md)), so the type
  holds nothing but a label and the link kind `at`.
- **Decision.** There is no entity type `place` and no link kind `at`. A place is a plain page ([D5](D05-pages-and-day-pages.md)):
  a page of a city, a lake or a shop has a title, aliases, text and backlinks like any page. Where the owner was on a
  day is the words of the day page, `I was at [[Lakeside]]`. The wikilink is extracted on save ([D19](D19-wikilink-save-contract.md)),
  so the days at a place are the backlinks of its page ([days that name](../cookbook/days-that-name.md)). The rule of
  the types is `entities_entity_type`; the rule of the kinds is the registry `link_kinds`.
- **Why.** One type and one kind existed for one difference: a day that names a place, and a day on which the owner
  was at the place. The file shows that the owner does not use the difference. The owner's words say it, and a
  wikilink holds the words.
- **Alternatives.**
  - *Keep the type and the kind `at`, for the difference between naming a place and being at it*: rejected. The
    difference is not used, and the words of the day say it.
  - *A `visited` link from a person to a place*: rejected. It has no date, and no import used it.
  - *A `place_id` column on pages*: rejected. A day has several places, and the column would be empty on every
    page that is not a day.
- **Trade accepted.** A day that names a place it was not at reads the same as a day on which the owner was there.
  The reader of the words tells them apart.
- **Sources.** Executed: the `named` and `links` suites.
