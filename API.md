# The lifelog API, for client authors

This page is what a client (a phone app, a browser app, a script) needs to speak to `lifelog serve`: the entity every
answer is, every class of entity and the properties it carries, the errors, the pages, and how to reach a server. The
database behind it is specified on its own in [docs/](docs/README.md); this page is only the application's wire shape.
The test `TestAPIReferenceMatchesTheCode` in `internal/api` reads the property tables below and fails when the code
and this page disagree, so a property here is a property on the wire.

## Reaching a server

- **Local:** `lifelog serve` listens on `http://127.0.0.1:7777` for this machine only ([README](README.md#decisions)).
- **Other devices:** `lifelog serve --public --token-file PATH` listens on every interface; every request carries
  `Authorization: Bearer <token>`, or a browser logs in once at `/login`. `--allow-origin` names the browser origins
  allowed to call it; `--tls-cert`/`--tls-key` serve HTTPS. Without one of them the answer is 401 `token_required`.
- **Provenance:** `Lifelog-Source: <name>` names the writer on every row the request writes (`[a-z0-9_:.-]{1,64}`,
  e.g. `app:phone`). Without it a browser form writes as `ui` and anything else as `api`. A writer named `agent:…`
  or `import:…` is refused the owner-only actions: `register-metric`, `snapshot` and `integrity-check`.
- **Representation:** `Accept: application/vnd.siren+json` (or anything but `text/html`) gets the JSON below;
  `Accept: text/html` gets the same entity drawn as a page. A JSON client sends an action as
  `application/x-www-form-urlencoded`, `application/json` (one object; a non-string value is sent as its JSON text) or,
  for `add-file` and `set-picture`, `multipart/form-data`.
- **Writes answer with the resource they changed** (200, with `Location`), and `result` says what the write did. A
  browser form is answered 303 to that resource instead, or to the page its field `back` names when that is a path
  on this server.
- **Preview:** `POST /render` with the field `body` (or `text`) answers a page that shows the body as its page shows
  it once saved; it writes nothing, answers `text/html` only and is not a catalog action: it is the browser's Preview,
  a second button of a form that writes a body, which opens it in a new tab.

## The entity

Every answer is one [Siren](https://github.com/kevinswiber/siren) entity:

| field | what |
|---|---|
| `class` | the kind of resource: one of the classes below, sometimes with a second word (`day-page`, `deleted`, `dry-run`) |
| `title` | a human title |
| `properties` | the resource's own data, per class below |
| `entities` | embedded links: `{rel, href, title, class}` each, the things this resource lists or links to |
| `links` | `{rel, href, title}`: `self`, `index` (home), `prev`/`next` (a neighbour or the next page), and the rels named per class |
| `actions` | what may be done here: `{name, title, description, method, href, type, fields, owner, danger}`, each field `{name, type, title, value, required, options, in, rows, pages, accept}` with its current value filled in; `owner: true` marks the owner's alone and `danger: true` marks one that hides or retracts what is there, so a client can warn before it sends; `in: "path"` marks a field of a templated href (`GET /actions` only) and `rows` is how many lines a `textarea` is drawn; `pages: true` says the value is a page's title, so a client can offer the names of the pages; `accept` is what a `file` field takes (`audio/*`) |
| `result` | after a write: what it did (the result shapes below) |

`GET /actions` is the catalog: every action with a templated `href` (`/pages/{id}/body`). A client fills the path
fields and sends the rest as the form. An action is offered on a resource only where it is legal there, so a client
that follows `actions` needs no rules of its own. A `required` field that is missing or empty is refused (422
`invalid`, the message `missing field <name>`), and nothing is written. `search` is the one exception: with no `q`,
or a `q` of blanks only, it answers with no hits ([search](#search)).

**Types** in the tables: `string`, `integer`, `number`, `boolean`, `day` (a local day, `YYYY-MM-DD`), `instant`
(UTC, `2026-06-09T21:14:03.482Z`), `list`, `object`. A property marked *absent when empty* is left out of the JSON
when it has no value.

## Errors

An error is an entity of class `error` with `properties`:

| property | type | meaning |
|---|---|---|
| `status` | integer | the HTTP status |
| `code` | string | what went wrong, to branch on: `stale_version` (a save with an old `version`), `exists` (a title in use; the link `existing` names the page), `cross_origin` (a browser write from another origin), `token_required` (a public listener without a credential), `snapshot_refused` (the snapshot folder refused it), `restore_check_failed` (the snapshot is written but fails its restore check), or the status's general name: `bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `too_large`, `invalid` (422), `timeout`, `internal` |
| `message` | string | the text, for a person; a fault of the server (500 `internal`) says only `internal error`, and the server's log has the cause |

## Pages of a list

`/days`, `/browse` and `/search` take `limit` (1–500; defaults 60 for days, 50 for search, 200 for browse) and
`offset` (0 or more), carry both in their properties, and link `next` when a further row exists and `prev` past the
first page. Orders: days newest first; search by rank; browse by the kind of its rows ([browse](#browse)).

## The old addresses of the lists

A `GET` of a list that [browse](#browse) replaced answers `308 Permanent Redirect` to its Browse address, with its
`limit`, `offset` and `new` kept (docs/plans/117-one-browse-view.md):

| old address | answers 308 to |
|---|---|
| `/people` | `/browse?kind=person` |
| `/places` | `/browse?kind=place` |
| `/media`, `/media?kind=image` | `/browse?kind=media`, `/browse?kind=media&media=image` |
| `/files` | `/browse?kind=document` |
| `/ghosts` | `/browse?ghosts=1&kind=page` |
| `/categories` | `/browse` |

The `POST` routes of `/people`, `/places` and `/files` (`create-person`, `create-place`, `add-file`) do not change.

## Classes

### root

`GET /`: today at a glance. Links: `today`, `days`, `browse`, `categories` (`/browse`), `people`
(`/browse?kind=person`), `places` (`/browse?kind=place`), `media` (`/browse?kind=media`), `files`
(`/browse?kind=document`), `metrics`, `ghosts` (`/browse?ghosts=1&kind=page`), `actions`, `calendar` (this month),
`week` (this week), `year` (this year). Actions: `capture` (for today), `search`,
`find`, `open-day`,
`create-page`, `create-person`, `create-place`, `add-file`,
`record`, `register-metric` (owner), `snapshot` (owner).

| property | type | meaning |
|---|---|---|
| `today` | day | this machine's local day |
| `recent[]` | list | the 5 newest live day pages up to today, the newest first |
| `recent[].day` | day | the day |
| `recent[].page_id` | integer | the day page's id |
| `recent[].first_line` | string | the first line of its body that has text, as plain text: a heading's or a list item's marker and the brackets of a wikilink cut, 160 characters at most; absent when the body is empty |
| `recent[].mood` | list | the values of the day's current Mood readings, in the order they were recorded; absent when none |
| `on_this_day[]` | list | the live day pages of earlier years with the same month and day of the month, at most 10, the newest first; empty when none; 29 February matches 29 February only |
| `on_this_day[].day` | day | the day |
| `on_this_day[].page_id` | integer | the day page's id |
| `on_this_day[].first_line` | string | the first line of its body (as `recent[]` of the root); absent when the body is empty |
| `on_this_day[].mood` | list | the values of the day's current Mood readings; absent when none |

### actions

`GET /actions`: no properties; `actions` is the whole catalog with templated hrefs.

### days

`GET /days`: one page of the live day pages, newest first, each an embedded `item` link to its day.

| property | type | meaning |
|---|---|---|
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |

### browse

`GET /browse`, the catalog action `browse` (docs/plans/117-one-browse-view.md): one page of the things of one kind,
with the category tree beside them. The query, all optional:

- `kind`: `page`, `person`, `place`, `media`, `document`, `metric` or `all`; the default is `all` with `in`, else
  `page`.
- `in`: a title (any name of the page, in any case): only what is filed directly in that category.
- `unfiled`: `0` or `1`: only what is in no category. A deleted category is not one: what is filed only there is in
  no category.
- `ghosts`: `0` or `1`: only the rows of the `ghost_pages` view, a day page left out.
- `media`: `image`, `video` or `audio`: one media kind.
- `limit` and `offset`: as every list; 200 and 0 by default.

A combination with no meaning is refused (422): `all` without `in`; `in` with `unfiled`; `ghosts` with a kind other
than `page`, or with `in` or `unfiled`; `media` with a kind other than `media`. A title that names nothing is a 404. A title of a thing
that cannot be a category (a person, a place, a file, a metric or a day) is a 422, and of a deleted page a 409.
A page is a plain page: a day page is in no list. Media and documents are the files, split by the first part of their
type. Orders: pages, people, places and metrics by title, in any case (as the tree orders the categories); media and documents newest day first; `all` the categories
first, then by title; ghosts oldest first.

A category is a page that something is `part-of` (D26). Each row is an embedded `item` link (class: the row's
`type`), and each category of the tree an embedded `category` link to Browse in that category (it keeps a `kind` the
query names, but `all`). Links: `self` (with the filters), `index`, `metrics`, a `kind` link for each chip (class:
its kind; it keeps `in` and `unfiled`; `all` inside a category only, and there the first), `everything` and `unfiled` (the ends of the tree), `ghosts`, with `kind=media` also `pictures`,
`videos` and `recordings`, and `prev`/`next`. Actions: the New of the kind (`create-page`, `create-person`,
`create-place`, `add-file`, which keeps a file of any kind on the media list too; none for `metric`, `all` and the ghosts),
with `category` filled inside a category: each of them makes the thing and files it there in one write, and a
refusal of either step writes nothing; `file-pages` on every list but `all`; `tombstone-pages` on `page` (the ghosts
too), `media` and `document`.

| property | type | meaning |
|---|---|---|
| `kind` | string | the kind of the rows |
| `in` | object | the category; absent when the list is not in one |
| `in.id` | integer | its page id |
| `in.title` | string | its title |
| `in.path` | string | its path in the tree, or its title when nothing is filed in it |
| `unfiled` | boolean | the list is of what is in no category |
| `ghosts` | boolean | the list is of the ghost pages |
| `media` | string | the media kind read, or `""` |
| `counts` | object | the live things of each kind in the scope of the list (everything, the category, or no category) |
| `counts.page` | integer | the plain pages |
| `counts.person` | integer | the people |
| `counts.place` | integer | the places |
| `counts.media` | integer | the pictures, videos and recordings |
| `counts.document` | integer | the other files |
| `counts.metric` | integer | the metrics |
| `counts.all` | integer | inside a category only: the sum of the kinds |
| `counts.ghosts` | integer | the rows of `ghost_pages`, in any scope |
| `media_counts` | object | `kind=media` only: the live files of each media kind in the scope of the list, whatever `media` this page reads |
| `media_counts.image` | integer | the pictures |
| `media_counts.video` | integer | the videos |
| `media_counts.audio` | integer | the recordings |
| `categories[]` | list | every category page, by path, as [metrics](#metrics) `categories[]` |
| `categories[].id` | integer | the category's page id |
| `categories[].title` | string | its title |
| `categories[].path` | string | the titles from the top, joined by `/` |
| `categories[].count` | integer | how many live things are filed directly in it, a day page not counted; absent when none |
| `rows[]` | list | one row for each `item` link, in the same order |
| `rows[].id` | integer | the page's id |
| `rows[].title` | string | its title |
| `rows[].type` | string | its `entity_type` |
| `rows[].kind` | string | its kind: `page`, `person`, `place`, `media`, `document` or `metric` |
| `rows[].category` | boolean | a page that is a category; absent otherwise |
| `rows[].mentions` | integer | a person, a place, a file (not in `all`): the live pages that link to it, each counted once |
| `rows[].last_day` | string | a person, a place: the day of the newest live day page among them; absent when none |
| `rows[].picture` | string | a person, a file: the address of its picture; absent when none |
| `rows[].mime` | string | a file (not in `all`): the type of the original |
| `rows[].file_kind` | string | a file: the first part of its type, `image`, `video`, `audio`, `application`, `text`, … |
| `rows[].day` | string | a file (not in `all`): its day (the day given, a photo's EXIF day, else the day it was kept), else the day of the newest live day page that shows it; absent when it has neither |
| `rows[].unit` | string | a metric: its unit; absent for a count or a habit |
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |

### search

`GET /search?q=` (FTS5 syntax over titles and bodies). Hits are also embedded as `item` links. Action: `search`.
With no `q`, or a `q` of blanks only (spaces, tabs, a no-break space or another Unicode space), the answer has the
action `search` and no properties: a browser draws it as the page to search from.

| property | type | meaning |
|---|---|---|
| `q` | string | the query |
| `titles[]` | list | the live pages whose name key starts with the title key of `q` (as [titles and wikilinks](docs/contract/titles-and-wikilinks.md) folds a title), at most 8, in key order |
| `titles[].name` | string | the name that starts with `q`: the page's title or one of its aliases |
| `titles[].title` | string | the page's preferred title |
| `titles[].page_id` | integer | the page |
| `titles[].entity_type` | string | its type: `page`, `person`, `place`, `file`, … |
| `hits[]` | list | the hits of this page, best first |
| `hits[].id` | integer | the page's id |
| `hits[].title` | string | its title |
| `hits[].snippet` | string | the matching text, matches in `⟦`…`⟧` (U+27E6, U+27E7: never brackets, which a body's text uses) |
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |

### day

`GET /days/{day}` (`/days/today` redirects; `GET /days/go?day=`, the action `open-day`, redirects to the day it
names, or to today when `day` is absent, empty or the word `today`, and a value that is not a day is 422). Links:
`prev`, `next` (the neighbouring days; 0000-01-01 has no `prev` and 9999-12-31 no `next`), `month` (its calendar),
`page` (the day page, once it exists), `metrics`. Embedded: `reading` links to each measurement. Actions: `capture`,
`record`, `open-day`, `save-body`
of the day page once it exists (its body and `version` filled in). The write `capture`
answers with this entity and a `result` of the sync shape; its field `mood` has the `options` 1 to 5. `POST /days/today/capture` captures on the local day.

| property | type | meaning |
|---|---|---|
| `day` | day | the day |
| `page_id` | integer | the id of the day page; absent when none exists yet |
| `view[]` | list | the day view of the cookbook, one row each, in reading order |
| `view[].what` | string | `day page` (its body), `page` or `page (edited)` (a page written that day), `at` (a place the day was at), or a metric's title (a reading) |
| `view[].at` | instant | when, for a page or a reading; absent otherwise |
| `view[].detail` | string | the body, the page title, the place, `Metric: done`/`not done`/`not recorded`/`invalid`, or `value unit` |
| `readings[]` | list | the day's readings, each a reading (below) with its id, to correct one |
| `mood` | list | the values of the day's current Mood readings, in the order they were recorded (the Mood metric found by its name key); empty when none |
| `trends[]` | list | one for each metric with a current reading on the day, in the order of `readings` |
| `trends[].metric` | string | the metric's name |
| `trends[].unit` | string | its unit (`''` for a count or a habit, a range such as `1-5` for a scale) |
| `trends[].readings[]` | list | its current readings in the 14 days through the day, oldest first (the series of the cookbook) |
| `trends[].readings[].day` | day |  |
| `trends[].readings[].value` | number |  |
| `on_this_day[]` | list | the live day pages of earlier years with the same month and day of the month, at most 10, the newest first; empty when none; 29 February matches 29 February only |
| `on_this_day[].day` | day | the day |
| `on_this_day[].page_id` | integer | the day page's id |
| `on_this_day[].first_line` | string | the first line of its body (as `recent[]` of the root); absent when the body is empty |
| `on_this_day[].mood` | list | the values of the day's current Mood readings; absent when none |
| `today` | day | this machine's local day |

The **reading** shape (`readings[]` here and in a series; a measurement's own properties):

| property | type | meaning |
|---|---|---|
| `readings[].id` | integer | the measurement's id |
| `readings[].metric` | string | the metric's name |
| `readings[].day` | day | its local day |
| `readings[].taken_at` | instant | when it was taken; absent when only the day is known |
| `readings[].tz` | string | the IANA zone it was taken in; absent when unknown |
| `readings[].import_key` | string | the key an import gave it; absent otherwise |
| `readings[].value` | number | the value |
| `readings[].captured_with_id` | integer | the page of what captured it (a device, an app); absent otherwise |

### month

`GET /months/{YYYY-MM}` (`/months/current` redirects to this month; a value that is not a month is 422): the calendar
of a month. Links: `prev`, `next` (the months before and after; 0000-01 has no `prev` and 9999-12 no `next`), `days`
(the list of day pages). Embedded: a `day` link to each day of the month. Actions: `open-day`. Each day holds what
names it ([plan 101](docs/plans/101-the-calendar-holds-every-dated-thing.md)).

| property | type | meaning |
|---|---|---|
| `month` | string | the month, `YYYY-MM` |
| `today` | day | this machine's local day |
| `days[]` | list | each day of the month, in order |
| `days[].day` | day | the day |
| `days[].page` | boolean | a live day page exists for it |
| `days[].first_line` | string | the first line of that page's body that has text, as plain text (as `recent[]` of the root); absent when none |
| `days[].mood` | list | the values of the day's current Mood readings, in the order they were recorded; absent when none |
| `days[].items[]` | list | every other thing that names the day, in this order: the births and deaths, the places and the pages; absent when none |
| `days[].items[].kind` | string | `birthday` or `death` (a live person's birth or death day, and its day in each later year; 29 February is on 28 February in a common year); `place` (a live place that the live day page links with `at`); `page` or `file` (a live page that is not a day page and has the day, by title, at most 20 a day) |
| `days[].items[].title` | string | the title of the page |
| `days[].items[].href` | string | the item's own resource: the page |
| `days[].items[].years` | integer | a birthday's or a death's: the whole years since the day the person's row holds; absent on that day |
| `days[].more_pages` | integer | the pages and files of the day after the first 20 in `items[]`; absent when none |
| `days[].readings` | integer | the current readings on the day, of every metric but the Mood (`mood` holds those); absent when none |

### week

`GET /weeks/{YYYY-Www}` (`/weeks/current` redirects to this week; a value that is not a week, or a week number the
year does not have, is 422): a look back over an ISO 8601 week, Monday first. Links: `prev`, `next` (the weeks before
and after; 0000-W01 has no `prev` and 9999-W52 no `next`), `month` (the month of the week's Thursday), `year` (the ISO
year; absent in the year 0000, which has no page), `index`. Embedded: a `day` link to each day of the week. Actions:
`open-day`.

| property | type | meaning |
|---|---|---|
| `week` | string | the week, `YYYY-Www` (the ISO year) |
| `from` | day | the Monday |
| `through` | day | the Sunday; 9999-12-31, a Friday, in 9999-W52 |
| `today` | day | this machine's local day |
| `days[]` | list | the days from `from` through `through`, in order (as `days[]` of the month): seven, and five in 9999-W52 |
| `days[].day` | day | the day |
| `days[].page` | boolean | a live day page exists for it |
| `days[].first_line` | string | the first line of that page's body that has text, as plain text; absent when none |
| `days[].mood` | list | the values of the day's current Mood readings, in the order they were recorded; absent when none |
| `days[].items[]` | list | every other thing that names the day, in this order: the births and deaths, the places and the pages; absent when none |
| `days[].items[].kind` | string | `birthday` or `death` (a live person's birth or death day, and its day in each later year; 29 February is on 28 February in a common year); `place` (a live place that the live day page links with `at`); `page` or `file` (a live page that is not a day page and has the day, by title, at most 20 a day) |
| `days[].items[].title` | string | the title of the page |
| `days[].items[].href` | string | the item's own resource: the page |
| `days[].items[].years` | integer | a birthday's or a death's: the whole years since the day the person's row holds; absent on that day |
| `days[].more_pages` | integer | the pages and files of the day after the first 20 in `items[]`; absent when none |
| `days[].readings` | integer | the current readings on the day, of every metric but the Mood (`mood` holds those); absent when none |
| `metrics[]` | list | each metric with a current reading in the week, by name |
| `metrics[].metric` | string | the metric's name |
| `metrics[].unit` | string | its unit; empty for a metric with none |
| `metrics[].readings` | integer | how many such readings the week holds |
| `metrics[].mean` | number | their mean, rounded to 2 decimals |
| `metrics[].previous_mean` | number | the mean of the week before, in the same way; absent when that week had no reading |

### year

`GET /years/{YYYY}` (`/years/current` redirects to this year; a value that is not a year, 0001 to 9999, is 422): the
twelve months of a year. Links: `prev`, `next` (the years before and after; 0001 has no `prev` and 9999 no `next`),
`index`. Embedded: a `month` link to each month. Actions: `open-day`.

| property | type | meaning |
|---|---|---|
| `year` | string | the year, `YYYY` |
| `today` | day | this machine's local day |
| `written` | integer | the live day pages of the year |
| `readings` | integer | the current readings of every metric, on the days of the year |
| `months[]` | list | the twelve months, in order |
| `months[].month` | string | the month, `YYYY-MM` |
| `months[].written` | integer | the live day pages of the month |
| `months[].days[]` | list | each day of the month, in order |
| `months[].days[].day` | day | the day |
| `months[].days[].page` | boolean | a live day page exists for it |

### page, person, place, file

`GET /pages/{id}`; `GET /pages?title=` redirects to it. The class is the page's type; a day page adds `day-page`, a
tombstoned page adds `deleted`. Links: `day` (a day page's day view), `preview` (a file's picture). Embedded: every outgoing link under its kind, every incoming one under `backlink` and its kind, with the class
`category` added to a `part-of` backlink from a category page. Actions by
what is legal: `save-body` (with the current `body` and `version` filled; an empty `body` is refused, so a save
cannot empty a page), `promote`, `rename`, `link` and `unlink` (with the kinds this type may start), [`file-pages`](#filed) (a page that may
be `part-of`, not a day page: `ids` is this page, and `category` offers the category titles; a new title is made),
`create-page` with `category` filled (a live category page: something is `part-of` it; the new page is made and
filed in it in one write), `set-picture` (a person), `tombstone`, or `revive` alone on a deleted page. In a browser, the page of a
live metric answers 303 to its series, the metric's one page, which carries this page's text, links and actions. The
writes answer with this entity; `create-page` and `save-body` carry a `result` of the sync shape, `add-file` the kept
shape. `set-picture` (`POST /pages/{id}/picture`) puts `![[title]]` first in the body, unless that file is the page's
picture already (the first live file with a preview that the body embeds). It takes a new photo as `original` (and
`preview` when the photo is not a JPEG, PNG or GIF), kept as a file with no day and titled
`<page> <today>.jpg` (` 2`, ` 3`… when taken) unless `title` names it; its `result` is the kept shape. Or it takes
the `title` of a kept file with a preview, and its `result` is the sync shape. A photo with no picture, or a title
that is not a kept picture, is refused (422).

| property | type | meaning |
|---|---|---|
| `id` | integer | the page's id, permanent |
| `entity_type` | string | `page`, `person`, `place`, `metric` or `file` |
| `title` | string | its preferred title; a day page's is its day |
| `day` | day | the page's day: a day page's own, or the day a note or file belongs to; absent otherwise |
| `body` | string | the body, CommonMark with `[[wikilinks]]` and `#tags` |
| `created_at` | instant | when it was created |
| `version` | string | the version token a `save-body` must send back |
| `deleted_at` | instant | when it was tombstoned; absent while it lives |
| `is_day_page` | boolean | it is a journal day page |
| `person` | object | a person's details; absent otherwise |
| `person.name` | string | the full name |
| `person.birth_day` | day | born; absent when unknown |
| `person.death_day` | day | died; absent when unknown |
| `file` | object | a file's identity; absent otherwise |
| `file.sha256` | string | the SHA-256 of the original, hex |
| `file.mime` | string | its media type |
| `file.preview` | boolean | a picture is kept (`GET /pages/{id}/preview`) |
| `links[]` | list | the typed links this page starts |
| `links[].kind` | string | the link kind (`wikilink`, `at`, `part-of`) |
| `links[].id` | integer | the other page's id |
| `links[].title` | string | its title |
| `links[].entity_type` | string | its type |
| `links[].note` | string | the link's note; absent when none |
| `backlinks[]` | list | the links that end here, the same shape |
| `backlinks[].kind` | string | |
| `backlinks[].id` | integer | |
| `backlinks[].title` | string | |
| `backlinks[].entity_type` | string | |
| `backlinks[].note` | string | |

### measurement

`GET /measurements/{id}`: one reading, current or not. Links: `day`, `series`, `supersedes` (what it corrects),
`superseded-by` (what corrected it), `captured-with`. Actions on the current reading: `correct`, with a new value,
and `retract`: to remove a reading, retract it. The properties are the reading's (above) and:

| property | type | meaning |
|---|---|---|
| `id` | integer | |
| `metric` | string | |
| `day` | day | |
| `taken_at` | instant | |
| `tz` | string | |
| `import_key` | string | |
| `value` | number | |
| `captured_with_id` | integer | |
| `current` | boolean | this is the reading that stands (not corrected, not retracted) |
| `supersedes_id` | integer | the reading it corrects; absent for a first reading |
| `superseded_by` | integer | the reading that corrects it; absent while it stands |
| `retraction` | boolean | it retracts the chain: its value is void |

### series

`GET /metrics/{name}?from=&to=&all_history=`: the one page of a metric. Its
readings over `(from, to]`, or with no `from` every reading through `to` (`all_history=1` says the same; `to` is today
by default). An alias names the metric too. Links: `self` (`/metrics/{name}` when asked with no query),
`all-readings`, `up` (metrics), `page` (the metric's page). Embedded: `reading` links. Actions: `record`
(this metric). A live metric is a page
(D27), so its page's actions are here too: `save-body`, `rename`, `link`, `unlink`,
[`file-pages`](#filed) (this metric) and `tombstone`.

| property | type | meaning |
|---|---|---|
| `metric` | string | the name (an alias asked for gives the metric's name) |
| `from` | day | the exclusive lower bound; empty in all-history mode |
| `to` | day | the inclusive upper bound |
| `readings[]` | list | the readings (the reading shape), oldest first |
| `readings[].id` | integer | |
| `readings[].metric` | string | |
| `readings[].day` | day | |
| `readings[].taken_at` | instant | |
| `readings[].tz` | string | |
| `readings[].import_key` | string | |
| `readings[].value` | number | |
| `readings[].captured_with_id` | integer | |
| `categories` | list | the paths of the categories it is filed in (`biomarkers/iron`) |
| `id` | integer | the metric's page id; absent when the name is no live metric |
| `unit` | string | its unit, `''` for a count or a habit, a range (`1-5`) for a scale; absent likewise |
| `body` | string | its page's text, CommonMark; absent likewise |
| `links[]` | list | the typed links its page starts (the page's shape); absent likewise |
| `links[].kind` | string | |
| `links[].id` | integer | |
| `links[].title` | string | |
| `links[].entity_type` | string | |
| `links[].note` | string | |
| `backlinks[]` | list | the links that end at its page (the page's shape); absent likewise |
| `backlinks[].kind` | string | |
| `backlinks[].id` | integer | |
| `backlinks[].title` | string | |
| `backlinks[].entity_type` | string | |
| `backlinks[].note` | string | |
| `all_history` | boolean | no `from` was named: every admitted day through `to` was selected |

### metrics

`GET /metrics`: the registry. Embedded: `item` links to each series. Actions: `record` with the metrics as options,
[`file-pages`](#filed) and `register-metric` (owner), each with the category titles as the options of `category`. A browser draws each metric as a card: its latest reading and the
line of its recent readings ([plan 112](docs/plans/112-metrics-as-cards.md)). `register-metric` takes `category` (a
page title, made when missing): one write makes the metric and files it. A refusal of either step writes nothing. Its answer is the metric's [series](#series), with a `result` sentence
([plan 113](docs/plans/113-new-metric-on-metrics.md)).

| property | type | meaning |
|---|---|---|
| `metrics[]` | list | every live metric, by name |
| `metrics[].id` | integer | its page's id |
| `metrics[].name` | string | its name (the page's title) |
| `metrics[].unit` | string | the unit, `''` for a count or a habit, a range (`1-5`) for a scale |
| `metrics[].note` | string | the page's text; absent when empty |
| `metrics[].categories` | list | the category paths it is filed in; absent when none |
| `metrics[].recent[]` | list | its newest 30 current readings, oldest first: the last is its latest |
| `metrics[].recent[].id` | integer | the measurement's id |
| `metrics[].recent[].day` | day | its day |
| `metrics[].recent[].value` | number | its value |
| `groups[]` | list | the same metrics in sections: each category in tree order, Not filed |
| `groups[].title` | string | the section |
| `groups[].path` | string | the category's path; absent for Not filed |
| `groups[].page` | integer | the category's page id; absent likewise |
| `groups[].depth` | integer | the category's depth in the tree |
| `groups[].metrics[]` | list | the metrics in it (the metric shape) |
| `groups[].metrics[].id` | integer | |
| `groups[].metrics[].name` | string | |
| `groups[].metrics[].unit` | string | |
| `groups[].metrics[].note` | string | |
| `groups[].metrics[].categories` | list | |
| `categories[]` | list | every category page, by path: what `file-pages` files in |
| `categories[].id` | integer | the category's page id |
| `categories[].title` | string | its title |
| `categories[].path` | string | the titles from the top, joined by `/` |
| `categories[].count` | integer | how many live things are filed directly in it, a day page not counted; absent when none |
| `today` | day | the server's local day |

### integrity

`GET /integrity`: the four integrity checks of the contract, and this writer's own. It is an owner-only action
(`integrity-check`). The full-text check holds the write lock while it runs, so other writes wait.

| property | type | meaning |
|---|---|---|
| `ok` | boolean | everything passed |
| `integrity_check` | list | SQLite's `PRAGMA integrity_check` lines |
| `foreign_key_violations` | integer | |
| `entities_without_domain_row` | list | ids |
| `invalid_typed_links` | list | ids |
| `invalid_measurement_corrections` | list | ids |
| `invalid_measurement_chains` | list | ids |
| `invalid_journal_names` | list | ids |
| `full_text_index_ok` | boolean | |
| `full_text_error` | string | absent when the index is fine |

### result

The class of an answer that is not a resource: the `record` of a reading whose `import_key` was seen before
(`message` alone), and `add-file` with `dry_run=1` (class `result dry-run`, with the kept shape in `result`):

| property | type | meaning |
|---|---|---|
| `title` | string | the title the file would take |
| `sha256` | string | the hash of the original |
| `mime` | string | its type |
| `picture` | boolean | a picture was sent |

### snapshot

`POST /snapshots` (the owner's action `snapshot`, refused to an `agent:*` writer): a dated copy of the database into
the server's snapshot folder, then its restore check. The same two values are in `result`; `self` is `/`, so a
browser form is answered home with them as feedback. A refusal (no folder, a folder inside a git work tree, a name in
use) is 422 `snapshot_refused`. A copy that fails its restore check is kept and answered 500 `restore_check_failed`.
A timeout is 504 `timeout`, and any other failure is 500 `internal`.

| property | type | meaning |
|---|---|---|
| `snapshot` | string | the file written, on the server's machine |
| `restore_check` | object | the integrity result of the copy (the integrity shape); `ok` says whether it is one to restore |
| `restore_check.ok` | boolean | |
| `restore_check.integrity_check` | list | |
| `restore_check.foreign_key_violations` | integer | |
| `restore_check.entities_without_domain_row` | list | |
| `restore_check.invalid_typed_links` | list | |
| `restore_check.invalid_measurement_corrections` | list | |
| `restore_check.invalid_measurement_chains` | list | |
| `restore_check.invalid_journal_names` | list | |
| `restore_check.full_text_index_ok` | boolean | |
| `restore_check.full_text_error` | string | |

### tombstoned

`POST /pages/tombstone` (`tombstone-pages`): the one field `ids` names
the pages, separated by spaces or commas, or sent once per id as a browser form does; 500 at most. Every
id is tombstoned in one write, or none is: an id no row has is 404, and a row tombstoned already is left as it is. The
answer embeds an `item` link to each; `result` says the count in words, which a browser form shows on the list it
returns to. `self` is `/`.

| property | type | meaning |
|---|---|---|
| `ids` | list | the ids named, each once, in the order sent |
| `count` | integer | how many |

### filed

`POST /pages/file` (`file-pages`): the field `ids` names the pages as `tombstone-pages` does; `category` is the title
of the category page, made as an empty page when no page has it; `move` 1 removes every other `part-of` link of those
pages first. Each page gets a `part-of` link to the category, in one write ([D26](docs/decisions/D26-metric-categories.md)).
An id no page has is 404 and a deleted page or category is 409: either writes nothing. A day page or a person as the
category is 422 (`links_endpoint_types`). The answer embeds an `item` link to each page and a `category` link;
`result` says what was done in words, which a browser form shows on the list it returns to.

| property | type | meaning |
|---|---|---|
| `category` | object | the category page |
| `category.id` | integer | its id |
| `category.title` | string | its title |
| `category.path` | string | empty here |
| `category.count` | integer | absent here |
| `created` | boolean | the category page was made by this action |
| `ids` | list | the ids named, each once, in the order sent |
| `count` | integer | how many |
| `added` | integer | the part-of links added (a page filed there already adds none) |
| `moved` | integer | the other part-of links removed by `move` |

### login

`GET /login` on a public listener: the one page served without a credential. `POST /login` with `token` sets the
cookie and redirects home; `POST /logout` clears it.

| property | type | meaning |
|---|---|---|
| `failed` | boolean | the last attempt was refused |
| `logged_in` | boolean | this browser already carries the cookie |

## Result shapes

### result of capture, create-page, save-body

What the save contract did with the body's `[[wikilinks]]` and `#tags`:

| property | type | meaning |
|---|---|---|
| `linked` | list | the titles linked |
| `added` | list | the titles in `linked` that the page did not link before this save; absent when none |
| `created` | list | the titles whose page was created; absent when none |
| `revived` | list | the tombstoned pages revived by a link; absent when none |
| `skipped` | list | the `[[names]]` that are not valid titles; absent when none |

### result of add-file

| property | type | meaning |
|---|---|---|
| `stored_day` | day | the day the existing page already had; absent otherwise |
| `id` | integer | the file page's id |
| `existing` | boolean | the same original was kept before |
| `deleted` | boolean | its page is tombstoned |
| `preview_added` | boolean | a missing picture was written |
| `promoted` | boolean | a plain page of that title became the file |
| `sync` | object | the sync shape of its body |
| `sync.linked` | list | |
| `sync.added` | list | |
| `sync.created` | list | |
| `sync.revived` | list | |
| `sync.skipped` | list | |
| `day_sync` | object | the sync shape of its day page, when it was appended there |
| `day_sync.linked` | list | |
| `day_sync.added` | list | |
| `day_sync.created` | list | |
| `day_sync.revived` | list | |
| `day_sync.skipped` | list | |
| `day` | day | its own day: the `day` sent, else the EXIF date of a photo; absent when it has neither, or when its page is deleted |
| `embedded` | boolean | `![[title]]` was appended to its day page |
| `filed` | object | what filing it in `category` did, as [filed](#filed) says it; absent when no `category` was sent or its page is deleted |
| `filed.category` | object | the category page; in a dry run that would make it, its `id` is 0 |
| `filed.category.id` | integer | its id |
| `filed.category.title` | string | its title |
| `filed.category.path` | string | empty here |
| `filed.category.count` | integer | absent here |
| `filed.created` | boolean | the category page was made by this action |
| `filed.added` | integer | 1 when the part-of link is new, 0 when the file was in the category already |
| `filed.moved` | integer | always 0: `add-file` removes no link |
