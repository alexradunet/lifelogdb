# 151 — No places: the type `place` and the link kind `at` go; a place is a plain page

> Dated implementation record. Tests use temporary folders and synthetic data. The owner's live `life.db` is never
> opened by a test.

## Status

- **Date / baseline:** 2026-10-29, `577fde7` (master) plus the commit of [issue 0091](../issues/0091-places-cost-more-than-they-give.md)
  and [RFC 0024](../rfcs/0024-no-places.md).
- **Priority:** P2. **Effort:** M. **Risk:** MEDIUM: a cut of one entity type, one link kind and three actions
  across the schema, the contract, the writer, the browser and the suites; the live file is converted by a one-off
  program before the new binary runs (Part 4, local).
- **Status:** PLANNED.
- **Resolves:** [issue 0091](../issues/0091-places-cost-more-than-they-give.md) through
  [RFC 0024](../rfcs/0024-no-places.md), which rewrites [D16](../decisions/D16-places.md) and
  [D21](../decisions/D21-location-history.md) in place.

## The owner's words (2026-10-29)

Of the Where box of a day ("No place on this day. I was at a place"): "I have this, which I don't really use it that
much, I am wondering if we should scrap the places." Shown the counts of the live file (thirty places, twenty-one
linked by nothing, five linked from a day page): "cut them, write the issue and the proposal", then "go ahead".

## What is true at the baseline

- `entities_entity_type` admits five types; `place` has no typed row (a person has `people`, a metric `metrics`, a
  file `files`). `link_kinds` has three rows: `wikilink`, `at` (`page` → `place`, from a day page only, held by the
  `at` clause of `links_endpoint_types`) and `part-of`. The sentence of `entities_promote_from_page` says it holds the
  type of a place, which no foreign key holds ([issue 0081](../issues/0081-only-the-writer-keeps-a-place-a-place.md)).
- The writer offers `create-place` (`POST /places`), `link` and `unlink` (`POST /pages/{id}/links`, `/unlink`, whose
  only kind is `at`: a wikilink comes from the body, a `part-of` from `file-pages`), and `promote` with `to` in
  `person, place`. Browse has a Places list with its count and a `place` kind; `search` and `random` take `place`;
  a day's card carries place chips; the day view draws Where; the New menu offers Place; the calendar's dated rows
  carry the places of a day.
- The docs: D16, D21, D8 (five types, three kinds), D20 (a person or a place), the recipes where-was-i,
  person-or-place, everything-about and day-view, the `at` check of the integrity checks, row 20 of the 2075 table,
  the link map and the ER diagrams, the surfaces table of the README, API.md, the site.
- The suites: `named` (34 lines name a place), `links`, `journal`; 293 mutants, of which these name a place or `at`:
  `links` "an at link may start at a plain page", "at accepts any endpoint", "an unknown endpoint id passes as a place
  with foreign_keys=OFF"; `named` "a place may be promoted", "entities_promote_from_page refuses an update that keeps
  the type"; `journal` "cookbook/where-was-i lists the places of a tombstoned day", "cookbook/where-was-i says I was at
  a tombstoned place", "an at link may start at a period page"; `diagrams` "the link map invents an edge".

## The decisions

| question | answer |
|---|---|
| what a place becomes | a plain page: same id, title, aliases, text and backlinks ([RFC 0024](../rfcs/0024-no-places.md), option C) |
| where "I was at" goes | the words of the day page, `I was at [[Lakeside]]`; the days at a place are the backlinks of its page ([days that name](../cookbook/days-that-name.md)) |
| `link` and `unlink` | removed whole: `at` was the only kind they took. `Tx.link` and `Tx.unlink` stay for the wikilink sync and `file-pages` |
| `promote` | keeps `person` alone: the field `to` goes, the action is "Make it a person", `name`, `birth_day` and `death_day` stay optional |
| `entities_promote_from_page` | stays, with its comment rewritten: the foreign key of a typed row refuses the same write, but the trigger's sentence names the rule |
| D16 | rewritten in place as "No places: a place is a plain page, and where a day was is the words of the day page"; its number stays, since `schema.sql` and the docs cite it |
| D21 | keeps "no location"; the answer to "where was I" is the day's words, not an `at` link |
| the Browse list | no Places list and no `place` kind; the thirty pages are listed with the pages |
| the icon `places` | removed from `internal/pixelart` with its drawing; the sprite is regenerated as `tools/pixelart` says |
| the live file | Part 4, run locally by the owner with a one-off program, after the code is merged and before the new binary is installed |

```mermaid
flowchart LR
  D["a day page"] -->|"its words: I was at [[Lakeside]]"| W["a wikilink, extracted on save (D19)"]
  W --> L["Lakeside: a plain page"]
  L -->|"its backlinks"| D
  D -. "an at link: removed" .-> L
```

## Part 0 — The contract

The path of [the process](../process.md), in one commit. The issue and the proposal exist; this part is the decision
and the change of the DDL and the pages.

1. **`schema.sql`**: in `entities_entity_type`, `('page','person','metric','file')`; the comment of `entities` no
   longer names a place or says "a place has no typed row (D21)". In `link_kinds`, the `wikilink` row admits
   `page,person,metric,file` on both sides, the `at` row goes, and the comment's example of a comma list is
   `('person,metric')`. In `links`, the comment says "wiki backlinks and what is filed where", and the `at` sentence
   goes. In `links_endpoint_types`, the statement `RAISE(ABORT, 'an at link starts at a day page (D16)')` with its
   `WHERE NEW.kind = 'at'` goes, and the comment above it says two rules (a category is a plain page, a day page is
   never filed), not three; the comment of the period-page statements loses "an at link still starts at a day page
   only". The `part-of` statements stay, and the registry's own `to_types` check stays as it is. In
   `entities_promote_from_page`, the comment says that the foreign key of a typed row also refuses the write and the
   trigger keeps the sentence; the `RAISE` says "a person, a metric or a file keeps its type". The `schema` row of
   `lifelog_meta` says "the journal (one page per day), wiki, people, health metrics and the files...".
2. **D16** rewritten from its template: the decision, why (issue 0091: five of thirty used; a type with nothing behind
   it), alternatives (keep the kind for the difference between naming and being at: rejected, the owner's words say
   it), the cost accepted (a day that names a place it was not at reads the same as one it was at), sources
   (executed: `named`, `links`). **D21**: the `at` link is no longer the answer; the day's words are. **D8**: four
   linkable types, two kinds. **D20**: "A person is a page". The decision index rows of D16 and D21 change.
3. **Pages**: delete [where was I](../cookbook/where-was-i.md) and its row of the cookbook index; rewrite
   [a person or a place](../cookbook/person-or-place.md) and [everything about](../cookbook/everything-about.md)
   for a person (rename the files to `person.md` and keep `everything-about.md`; every link to them follows);
   [days that name](../cookbook/days-that-name.md) loses its `at` sentence; [the day view](../cookbook/day-view.md)
   loses its `at` block; [integrity checks](../contract/integrity-checks.md) loses the `at` row of its orphan check
   and the sentence that cites D16; the [threat model](../contract/threat-model.md) loses row 20 of the 2075 table,
   and rows 21 to 26 become 20 to 25, since the `document` suite wants the rows numbered 1 to n without a gap;
   [link rules](../architecture/link-rules.md), the [entity model](../architecture/entity-model.md), the
   [page lifecycle](../architecture/page-lifecycle.md), the [product concepts](../architecture/product-concepts.md),
   the location row of the [non-goals](../architecture/non-goals.md), [imports](../contract/imports.md),
   [titles and wikilinks](../contract/titles-and-wikilinks.md) and [building a writer](../guides/building-a-writer.md)
   say the new truth in the lines that name a place; the totals line of [the schema page](../schema/README.md)
   stays at 42 triggers (no trigger goes). The diagrams: the link map has two edges; `Named (person, metric, file)`.
4. **Suites**: the `named` suite is about a person (its "a place needs no detail row", "two Sams" with a place, and
   the cookbook runs of person-or-place and everything-about change with the recipes); the `links` and `journal`
   suites lose their `at` cases; the mutants named above go (the count in `tests/README.md` follows); the `document`
   suite holds (every link resolves, the `lifelog_meta` rows and the stored texts name only what the file has: the
   `schema` row and the comments must not say "place" or "at"); the `diagrams` suite holds (the link map equals
   `link_kinds`). The 2075 test of `document` reads the threat model's table: keep its parser happy.

## Part 1 — The writer: the core and the API

1. `internal/core`: delete `CreatePlace` (both), `PlaceTitles`, the `place` branches of `Promote` (`typ` is then
   `person` alone: `promote to person` is the only valid value, and the action has no `to`), `Store.Link` and
   `Store.Unlink` and their handlers; `browseKinds` and `searchKinds` lose `place`; the calendar's dated rows and
   the stream's chips lose the place rows (`calendar.go`, `stream.go`: a card's chips are the people); the
   `at` clause of the orphan check in `integrity.go` goes with the contract's; the comments that say "a person, a
   place or a file" say "a person or a file". `Promotable`/`Plain` comments follow.
2. `internal/api`: the catalog loses `create-place`, `link` and `unlink`; `promote` loses `to`, and its Title is
   "Make it a person", its Hint "Make this page a person. Its text and links stay."; `serve.go` drops the three
   handlers; `pages.go` drops the place options of the day page (`PlaceTitles`), the `at` branch of the links
   filter, `createPlace` and the place of `offerPromote`; `searchKindTitles` loses Places; `days.go` drops
   `create-place` from the day's actions, the `places` link of the root, the `Places` chips (`cardChip` stays for
   people and birthdays) and the `unlink` action of a day; `browse.go` drops the `place` kind, `Place` of the counts
   and the place of `namedRows` (people only). Every `alertOf`, hint and sentence that names a place follows.
3. The MCP tools and the forms follow the catalog: nothing to write; the end-to-end test of `extensions/mcp` runs
   in its folder. The words of the agent's instructions (`extensions/mcp/mcp/tools.go`,
   `extensions/whatsapp/agent/instructions.go`) say "people and health readings".

## Part 2 — The browser

1. `day.html`: the Where section goes. `shell.go`: the New menu loses Place. `cards.go`, `search.go`, `page.go`:
   the `place` kind and its icon go; `page.go` drops `Places` of the day parts and its `at` case; `browse.go` (view)
   and `browse.html`: no Places chip, no place words of `Mentions`. `forms.go` and `hand_forms_test.go`: the × of a
   place goes with `unlink`; the × of a category stays.
2. `internal/pixelart/icons.go`: `places` leaves `IconNames` and its drawing; then `go run ./tools/pixelart` from
   the repository root writes the icon sheet again (a test of `internal/api/view` compares each sheet with its
   drawing), in the same commit, and `icons_test.go` follows. The site
   ([site/index.html](../../site/index.html)): "the people in them", "People" card, "each person" (it names only
   what the README names).
3. Screenshots of a day and of Browse before and after, with the synthetic review server
   (`tests/README.md`, the UI review tooling), to see that the day's margin closes up and Browse has no Places.

## Part 3 — The README and API.md

1. README: the surfaces table loses `create-place`, `link` and `unlink`; the `promote` row says "Make it a person";
   the sentences that name places (the product sentence, Browse, the day's margin, search, the kind chips) change;
   `TestTheTableOfTheSurfacesIsTheCatalog` holds. The heading "page, person, place, file" of API.md becomes
   "page, person, file", and every anchor `#page-person-place-file` follows; the `places` link, `cards[].places[]`,
   `counts.place`, the kinds of `browse`, `search`, `random` and `rows[].kind`, the `at` of `view[].what`, the
   `create-place`, `link` and `unlink` sections and `entity_type` go or change.
2. The plans index: this plan's row, status IN REVIEW (owner) when the parts are merged.

## Part 4 — The live file (local; not for a cloud session)

The owner's file holds thirty places and the `at` links of five. It follows by a one-off program in the pattern of
plan 143 (`~/.local/share/lifelog-oneoff/plan143/`, `OLD.sql NEW.sql LIFE.db [--dry-run]`): a fresh file from each
SQL in a temporary folder, the live file accepted only when equal to the fresh OLD file, one `BEGIN IMMEDIATE`
transaction, the result compared with the fresh NEW file, the integrity checks run, the dry run rolled back.

1. OLD.sql is plan 143's NEW.sql, or `schema.sql` at the last commit applied live if a later docs-only change
   landed (the dry run says). NEW.sql is `schema.sql` at the commit of Part 0.
2. In the transaction: (a) for each `at` link, when the day page's body does not already hold `[[Title]]` of the
   place (by `title_key`, through the writer's own extraction), append `I was at [[Title]]`, with the link's note
   after a comma when there is one, as a last paragraph, through `core.SaveBody` with the page's version, so the
   wikilink row and the revision follow the save contract (D19); (b) `DELETE FROM links WHERE kind = 'at'`, then
   `DELETE FROM link_kinds WHERE kind = 'at'` (allowed once unreferenced); (c) `DROP TRIGGER
   entities_promote_from_page`, `UPDATE entities SET entity_type = 'page' WHERE entity_type = 'place'`, then the
   trigger with NEW's text; (d) `UPDATE link_kinds SET from_types = 'page,person,metric,file', to_types = ...
   WHERE kind = 'wikilink'` with `link_kinds_structure_fixed` dropped and made again from NEW's text; (e) the
   `entities` and `links` table texts and the `schema` meta row by the writable_schema rewrite, as meta124 did;
   (f) compare with the fresh NEW file; (g) the integrity checks with the staged new binary.
3. The owner runs it with `!`: stop the service, `mkdir -p /home/cico/life/snapshots/2026-MM-DD-plan-151`, snapshot
   there, dry run, run, integrity, install the staged binary (`cp` to `lifelog.new`, `mv -f`), start, read the
   `serving` line of `journalctl -o short-iso`. The counts before and after (places, `at` rows, the pages touched)
   go in the plan's status line.

## Tests

- `tests/`: the suites of Part 0; `go test ./tests` green, mutants included.
- `internal/core`: a promote to anything but a person is refused by the writer with a 422 before SQL, and a person's
  promote still works; the calendar's dated rows and the stream's chips carry people only; the orphan check has no
  `at` row (`integrity_rules_test.go`); `people_places_test.go` of `internal/api` becomes `people_test.go`.
- `cmd/lifelog`: `surfaces_test.go` and `parity_test.go` lose the `link`/`unlink`/`create-place` cases and keep
  parity for `promote`; `boundary_test.go` holds.
- `extensions/mcp`: the end-to-end test lists no `create_place`, `link` or `unlink` tool.
- The baseline: `gofmt`, `go vet ./... && go test ./...` at the root and in each extension; `staticcheck ./...` and
  `govulncheck ./...`; `go test -count=1 -shuffle=on ./...`.

## Done when

1. `schema.sql` admits four entity types and two link kinds; D16, D21, D8 and D20 say the new truth; every page of
   `docs/` that named a place or an `at` link is rewritten or deleted; `go test ./tests` is green.
2. The catalog has no `create-place`, `link` or `unlink`; `promote` has no `to`; Browse, search and random take no
   `place`; a day draws no Where and no place chips; the sprite has no `places` icon; the README's table and API.md
   agree with the catalog; the site names no place.
3. The baseline and the static checks pass at the root and in each extension.
4. Part 4 ran on the live file: the counts are recorded here, the service serves the new binary, and the issue
   closes as resolved.
