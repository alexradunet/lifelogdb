# D20 — A person or a place is a page: one id, and `[[Name]]` reaches it directly.

**Status:** accepted

- **Decision.** A named entity is one id: `entities` (its type) and a titled `pages` row whose
  `entity_type` is that type; a person also has a `people` row, whose composite FK references the
  `pages` row (`people` → `pages` → `entities`), and a place has nothing more ([D16](D16-places.md)). The page title is
  the entity's handle, and the unique `title_key` forces two people called Sam apart (`Sam (barber)`).
  A place has no name column (the title is the name); `people.name` is the editable full name. The save contract is
  untouched: `[[Bob Sample]]` is an ordinary wikilink, and it lands on the person's own id, so
  "everything about Bob" is one pair of link queries ([everything about](../cookbook/everything-about.md)).
- **Promotion.** A ghost page made by an earlier `[[Bob Sample]]` becomes the person by
  `UPDATE entities SET entity_type = 'person'` — the `ON UPDATE CASCADE` foreign key carries the new type to
  `pages.entity_type` — and one `people` insert (a place needs none). The foreign keys refuse a person
  without a page, and undoing a promotion (the `people` row's FK) (executed). A day page is never
  promoted: it is the journal of its day ([D5](D05-pages-and-day-pages.md)), and its `at` links need it to stay a page ([D16](D16-places.md)).
  `pages_day_page_plain` checks the cascaded type, so the `UPDATE` on `entities` is refused (executed).
- **A metric** is a named entity in the same way: a page and a `metrics` row, one id ([D27](D27-a-metric-is-a-page.md)); so is **a file**,
  with a `files` row ([D9](D09-binary-files.md)).
- **Why.** The owner writes `Today I met [[Bob Sample]]` and wants that day's page attached to the
  person.
  A wikilink can only land on a page, so the person must be one. Giving the person and the page the
  same id means the backlinks of the person *are* the backlinks of the page: no second id to resolve,
  no `page_id` pointer and its rules, one row fewer per named thing.
- **Alternatives.**
  - *A separate page entity pointed at by `entities.page_id`*: rejected — two ids for one person (the
    page `[[…]]` reaches and the person `about` points at), a pointer column with a CHECK, a
    UNIQUE and two triggers, a third leg in every "everything about X" query, and a place name that
    had to be unique twice (its own `name` and its title).
  - *A `mention` link kind, page → person, resolved by matching names on save*: rejected — a page and
    a person with one name need a precedence rule, and a link would depend on the `people` table at
    save time, so a rename or a new person changes what a re-save produces.
  - *A `[[@Name]]` prefix for people*: rejected — the same resolution problem with a namespace in front.
- **Costs accepted.** Every person and place needs a unique handle, even one never mentioned;
  an importer makes one ([a person or a place](../cookbook/person-or-place.md)). A handle is permanent like any title: a changed name is `people.name`,
  and `[[Old name]]` keeps working. A typo in a name makes a ghost page like any wikilink typo. A page
  names a person by wikilink *or* by `about`; the two are separate rows, and [everything about](../cookbook/everything-about.md) reads both.
