# D20 — A person or a place is a page: one id, and `[[Name]]` reaches it directly.

**Status:** accepted

- **Decision.** A person or place is one named identity and body in `entities`, with its handle and direct aliases in `entity_names`. A person also has a `people` extension keyed directly by `(id, entity_type)`; a place has no extension ([D16](D16-places.md)). The preferred spelling is the handle; `people.name` is the editable full name, trimmed text of 1 to 240 bytes without NUL (`people_name`). Two people called Sam need distinct owned handles (`Sam (barber)`). Ordinary references resolve to that same id, so [everything about](../cookbook/everything-about.md) uses one pair of link queries.
- **Promotion.** Only a plain page is promoted. A plain ghost becomes a person by updating its entity type and inserting its `people` extension in one transaction ([a person or a place](../cookbook/person-or-place.md)); a place has no extension, so its promotion is the update alone. Existing prose, names and links stay. A person, a place, a metric or a file never changes its type (`entities_promote_from_page`). Direct extension FKs also refuse incompatible types, but a place has no typed row: the trigger alone keeps a place from becoming a person or a metric. `entities_endpoint_types` protects retained edges. `entities_day_page_plain` refuses promotion of a canonical journal date. These refusals are executed.
- **Ownership.** `people_identity_fixed` prevents moving details between same-type identities;
  the direct FK alone only checks that the new owner has a compatible type. A corrected full name remains
  an edit of its original person or place, with that identity's revision advanced.
- **A metric** and **a file** use the same identity with their own direct extensions ([D27](D27-a-metric-is-a-page.md), [D9](D09-binary-files.md)).
- **Why.** The owner writes `Today I met [[Bob Sample]]` and wants that day's page attached to the
  person.
  A wikilink can only land on a page, so the person must be one. Giving the person and the page the
  same id means the backlinks of the person *are* the backlinks of the page: no second id to resolve,
  no `page_id` pointer and its rules, no separate prose identity.
- **Alternatives.**
  - *A separate page entity pointed at by `entities.page_id`*: rejected — two ids for one person (the
    page `[[…]]` reaches and a typed link points at), a pointer column with a CHECK, a
    UNIQUE and two triggers, a third leg in every "everything about X" query, and a place name that
    had to be unique twice (its own `name` and its title).
  - *A `mention` link kind, page → person, resolved by matching names on save*: rejected — a page and
    a person with one name need a precedence rule, and a link would depend on the `people` table at
    save time, so a rename or a new person changes what a re-save produces.
  - *A `[[@Name]]` prefix for people*: rejected — the same resolution problem with a namespace in front.
- **Costs accepted.** Every person and place needs a unique handle, even one never mentioned;
  an importer makes one ([a person or a place](../cookbook/person-or-place.md)). A preferred handle can change while its old owned names keep working; changing `people.name` changes only the full name. A typo in a name makes a ghost page like any wikilink typo. A page
  names a person by wikilink, and [everything about](../cookbook/everything-about.md) reads both directions.
