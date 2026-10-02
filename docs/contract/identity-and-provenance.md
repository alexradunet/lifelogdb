# Identity and provenance

- **Entity rows first, ids by `RETURNING`.** A domain row's `id` equals its `entities.id`. The app
  inserts the `entities` row with `INSERT … RETURNING id`, keeps the id in a variable and binds it
  in the same transaction ([capture](../cookbook/capture.md)). Never `last_insert_rowid()` across statements: any insert in
  between — a link, a ghost page, a measurement — moves it, and the next row silently points at the
  wrong entity (executed).
- **A person or a place is a page ([D20](../decisions/D20-named-pages.md)).** A person is one id with three rows: `entities`
  (`entity_type = 'person'`), `pages` (`entity_type = 'person'`, titled — the title is the handle that
  `[[wikilinks]]` write) and `people`. Insert them in that order in one transaction ([a person or a place](../cookbook/person-or-place.md)). A place is
  the first two only, with `entity_type = 'place'`: it has no columns of its own ([D16](../decisions/D16-places.md)). A ghost page an
  earlier `[[Name]]` created is *promoted* instead: `UPDATE entities SET entity_type = 'person'` (the
  foreign key cascades it to `pages.entity_type`), then insert the `people` row; a place needs nothing
  more. A day page is never promoted (`pages_day_page_plain`, [D20](../decisions/D20-named-pages.md)). The foreign keys refuse a person without a page and a person turned back into
  a page (executed). Title uniqueness already refuses a second `Sam`, so two people called Sam are told apart
  in the handle (`Sam (barber)`); `people.name` is the editable full name.
- **Provenance.** `source` on `entities`, `links`, `measurements` and `habit_periods` names the writer
  of the row — `ui`, `cli`, `api`, `agent:<name>`, `import:<name>` (lowercase `[a-z0-9_:.-]`, 1–64
  characters). Only the moment of writing knows it, so it is required at insert and never changes;
  with agents among the writers ([D3](../decisions/D03-integer-ids.md)) it is how a wrong row is traced to the writer that made it. An
  importer's `import_key` — on entities and on facts — is unique per `source`, so its name is also the
  deduplication namespace.
