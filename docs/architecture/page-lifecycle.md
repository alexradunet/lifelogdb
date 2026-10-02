# The life of a page

A page starts as a ghost when a link names a title that does not exist yet, or as a written page when
the owner creates it on purpose — a day page when the first thing is captured that day ([D5](../decisions/D05-pages-and-day-pages.md)). Either can
become the page of a person or a place ([D20](../decisions/D20-named-pages.md)) — except a day page (`pages_day_page_plain`).

```mermaid
%% diagram: page-life
stateDiagram-v2
    direction LR
    state "Ghost (empty)" as Ghost
    state "Written page" as Written
    state "Redirect stub" as Stub
    state "Named (person, place)" as Named
    [*] --> Ghost: a link names a title that does not exist yet
    [*] --> Written: created on purpose, with a day, or the day page on the day's first capture
    [*] --> Named: a person or a place is created
    Ghost --> Written: body saved, the day unchanged
    Ghost --> Named: promoted, entities.entity_type changes
    Written --> Named: promoted unless a day page, entities.entity_type changes
    Written --> Written: body edited or appended to, the title never changes
    Written --> Stub: renamed, its text and typed links moving to a new page, a redirect link added
    Ghost --> Stub: renamed, into a free title or one that exists, a redirect link added
```

Any row can be tombstoned (`entities.deleted_at`, [D11](../decisions/D11-tombstones.md)); a save whose link resolves a tombstoned title
revives that page instead of duplicating it ([save a body](../cookbook/save-a-body.md)). A ghost that nothing links to is listed by
`ghost_pages` after 30 days ([ghost pages](../cookbook/ghost-pages.md)) — the view only lists, tombstoning stays the owner's act.
