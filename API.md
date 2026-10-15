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
  is refused the owner-only actions.
- **Representation:** `Accept: application/vnd.siren+json` (or anything but `text/html`) gets the JSON below;
  `Accept: text/html` gets the same entity drawn as a page. A JSON client sends an action as
  `application/x-www-form-urlencoded`, `application/json` (one object; a non-string value is sent as its JSON text) or,
  for `add-file` and `set-picture`, `multipart/form-data`.
- **Writes answer with the resource they changed** (200, with `Location`), and `result` says what the write did. A
  browser form is answered 303 to that resource instead, or to the page its field `back` names when that is a path
  on this server.

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
that follows `actions` needs no rules of its own.

**Types** in the tables: `string`, `integer`, `number`, `boolean`, `day` (a local day, `YYYY-MM-DD`), `instant`
(UTC, `2026-06-09T21:14:03.482Z`), `list`, `object`. A property marked *absent when empty* is left out of the JSON
when it has no value.

## Errors

An error is an entity of class `error` with `properties`:

| property | type | meaning |
|---|---|---|
| `status` | integer | the HTTP status |
| `code` | string | what went wrong, to branch on: `stale_version` (a save with an old `version`), `exists` (a title in use; the link `existing` names the page), `cross_origin` (a browser write from another origin), `token_required` (a public listener without a credential), `snapshot_refused` (the snapshot folder refused it), `restore_check_failed` (the snapshot is written but fails its restore check), `no_model` (503: `ask` with no model configured), `model` (502: the model failed or did not answer), or the status's general name: `bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `too_large`, `invalid` (422), `timeout`, `internal` |
| `message` | string | the text, for a person |

## Pages of a list

`/days`, `/people`, `/places`, `/media`, `/files`, `/ghosts` and `/search` take `limit` (1–500; defaults 60 for days, 50 for
search, 200 otherwise) and `offset` (0 or more), carry both in their properties, and link `next` when a further row
exists and `prev` past the first page. Orders: days newest first; people and places by title; media and files newest day first;
ghosts oldest first; search by rank.

## Classes

### root

`GET /`: today at a glance. Links: `today`, `days`, `people`, `places`, `media`, `files`, `metrics`,
`habits`, `tasks`, `deadlines`, `ghosts`, `actions`, `calendar` (this month), `week` (this week), `year` (this year). Actions: `capture` (for today), `search`,
`find`, `open-day`, `deadlines`,
`create-task`, `create-page`, `create-person`, `create-place`, `add-file`,
`record`, `register-metric` (owner), `snapshot` (owner); then the `check-in` of each habit of today (today
filled in), and the outcome (see task) of each occurrence in `due`.

| property | type | meaning |
|---|---|---|
| `today` | day | this machine's local day |
| `due[]` | list | the open occurrences due in the 30 days before today, then the open ones due today, by due day (the occurrence shape, with `label`) |
| `due[].due_day` | day |  |
| `due[].state` | string |  |
| `due[].completed_at` | instant |  |
| `due[].reminder_mode` | string |  |
| `due[].reminder_at` | instant |  |
| `due[].id` | integer |  |
| `due[].task_id` | integer |  |
| `due[].key` | string |  |
| `due[].import_key` | string |  |
| `due[].source` | string |  |
| `due[].version` | string |  |
| `due[].created_at` | instant |  |
| `due[].updated_at` | instant |  |
| `due[].deleted_at` | instant |  |
| `due[].task_version` | string |  |
| `due[].task_deleted_at` | instant |  |
| `due[].project_title` | string |  |
| `due[].project_deleted_at` | instant |  |
| `due[].virtual` | boolean |  |
| `due[].label` | string | the task's label |
| `due[].reminder` | object |  |
| `due[].reminder.state` | string |  |
| `due[].reminder.at` | instant |  |
| `habits[]` | list | the habits active today, by name (the habit shape of habits) |
| `habits[].metric` | string | the habit's metric |
| `habits[].state` | string | `done`, `not done`, `not recorded` or `invalid` |
| `habits[].recent` | list | its state on each of the 14 days through today, oldest first: a state above, or `inactive` on a day no period of the habit holds |
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

`GET /actions`: no properties; `actions` is the whole catalog with templated hrefs. With an import workspace the
import actions are included and the link `import` leads to its status.

### days, people, places, ghosts

`GET /days`, `/people`, `/places`, `/ghosts`: one page of a list, each row an embedded `item` link to the page (with
`class` the page's type where it has one). `people` and `places` offer `create-person` and `create-place`.

| property | type | meaning |
|---|---|---|
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |
| `rows[]` | list | `people` and `places`: one row for each `item` link, in the same order |
| `rows[].id` | integer | the page's id |
| `rows[].title` | string | its title |
| `rows[].mentions` | integer | the live pages that link to it, of any kind, each counted once |
| `rows[].last_day` | string | the day of the newest live day page among them; absent when there is none |
| `rows[].picture` | string | `people` only: the href of the preview of the first live file with a preview that the person's body embeds; absent when there is none |

### categories

`GET /categories`: the category tree, then one page of the plain pages filed nowhere. A category is a page that
something is `part-of` (D26). Each category is an embedded `category` link to its page, and each unfiled page an
embedded `item` link.

| property | type | meaning |
|---|---|---|
| `categories[]` | list | every category page, by path, as [metrics](#metrics) `categories[]` |
| `categories[].id` | integer | the category's page id |
| `categories[].title` | string | its title |
| `categories[].path` | string | the titles from the top, joined by `/` |
| `categories[].count` | integer | how many live things are filed directly in it, the categories under it included; absent when none |
| `unfiled[]` | list | the live plain pages in no category that are not categories, by title: not a day page, and not a metric, a person, a place or a file |
| `unfiled[].kind` | string | `page` |
| `unfiled[].id` | integer | the page's id |
| `unfiled[].title` | string | its title |
| `unfiled[].entity_type` | string | `page` |
| `unfiled[].note` | string | absent |
| `limit` | integer | the page size of `unfiled`, 200 by default |
| `offset` | integer | the rows skipped |

### media, files

`GET /media`: one page of the live files whose type is `image/*`, `video/*` or `audio/*`, newest day first, each row
an embedded `item` link to the file's page (class `file`). `?kind=image`, `video` or `audio` reads one kind; any
other word is refused (422). Links: `pictures`, `videos`, `recordings` (the three kinds). Actions: `add-file`
(its `original` field carries `accept`, the three kinds) and `tombstone-pages`, the delete of several at once (its
answer is the class `tombstoned`), which a browser draws as a box on each row.

`GET /files`: the same page of every other live file (a PDF, a text), newest day first, with the link `media` and the
actions `add-file` and `tombstone-pages`. It carries no `kind` and no `counts`.

| property | type | meaning |
|---|---|---|
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |
| `kind` | string | `media`: the kind read: `image`, `video`, `audio`, or `""` for all three |
| `counts` | object | `media`: the live files of each kind, whatever kind this page reads |
| `counts.image` | integer | the live pictures |
| `counts.video` | integer | the live videos |
| `counts.audio` | integer | the live recordings |
| `rows[]` | list | one row for each `item` link, in the same order |
| `rows[].id` | integer | the page's id |
| `rows[].title` | string | its title |
| `rows[].kind` | string | the first part of its type: `image`, `video` or `audio` in `media`; `application`, `text`, … in `files` |
| `rows[].mime` | string | the type of the original |
| `rows[].picture` | string | the address of its picture (`GET /pages/{id}/preview`); absent when it keeps none |
| `rows[].day` | string | its day (the day given, a photo's EXIF day, else the day it was kept), else the day of the newest live day page that shows it; absent when it has neither |
| `rows[].mentions` | integer | the live pages that link to it, of any kind, each counted once |

### search

`GET /search?q=` (FTS5 syntax over titles and bodies). Hits are also embedded as `item` links. Action: `search`.

| property | type | meaning |
|---|---|---|
| `q` | string | the query |
| `titles[]` | list | the live pages whose name key starts with the title key of `q`, at most 8, in key order: the rows of `GET /titles` |
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

### titles

`GET /titles?prefix=&limit=`: the names of live pages whose name key starts with the title key of `prefix` (as
[titles and wikilinks](docs/contract/titles-and-wikilinks.md) folds a title), in key order: what an editor suggests
after `[[`. `limit` is 1–20, default 10; an empty prefix is 422. Embedded: an `item` link to the page of each name,
class its type. `POST /render` with the field `body` (or `text`) answers a page that shows the body as its page
shows it once saved; it writes nothing, answers `text/html` only and is not a catalog action: it is the browser's
Preview, a second button of a form that writes a body, which opens it in a new tab.

| property | type | meaning |
|---|---|---|
| `prefix` | string | the prefix, as sent |
| `limit` | integer | at most this many names |
| `titles[]` | list | the names, in key order |
| `titles[].name` | string | the name that starts with the prefix: the page's title or one of its aliases |
| `titles[].title` | string | the page's preferred title |
| `titles[].page_id` | integer | the page |
| `titles[].entity_type` | string | its type: `page`, `person`, `place`, `file`, … |

### day

`GET /days/{day}` (`/days/today` redirects; `GET /days/go?day=`, the action `open-day`, redirects to the day it
names, and a value that is not a day is 422). Links: `prev`, `next` (the neighbouring days), `month` (its calendar),
`page` (the day page, once it exists), `metrics`. Embedded: `reading` links to each measurement. Actions: `capture`,
`record`, `open-day`, `save-body`
of the day page once it exists (its body and `version` filled in), the `check-in` of each habit in `habits`, and the
outcome (see task) of each occurrence in `due` that is due 7 days after `today` or earlier. The write `capture`
answers with this entity and a `result` of the sync shape; its field `mood` has the `options` 1 to 5. `POST /days/today/capture` captures on the local day.

| property | type | meaning |
|---|---|---|
| `day` | day | the day |
| `page_id` | integer | the id of the day page; absent when none exists yet |
| `view[]` | list | the day view of the cookbook, one row each, in reading order |
| `view[].what` | string | `day page` (its body), `page` or `page (edited)` (a page written that day), `at` (a place the day was at), `habit` (a habit and its state), or a metric's title (a reading not part of a habit) |
| `view[].at` | instant | when, for a page or a reading; absent otherwise |
| `view[].detail` | string | the body, the page title, the place, `Metric: done`/`not done`/`not recorded`/`invalid`, or `value unit` |
| `readings[]` | list | the day's readings, each a reading (below) with its id, to correct one |
| `due[]` | list | the occurrences due on the day, in every state; on today, first the open ones due in the 30 days before it (the occurrence shape, with `label`) |
| `due[].due_day` | day |  |
| `due[].state` | string |  |
| `due[].completed_at` | instant |  |
| `due[].reminder_mode` | string |  |
| `due[].reminder_at` | instant |  |
| `due[].id` | integer |  |
| `due[].task_id` | integer |  |
| `due[].key` | string |  |
| `due[].import_key` | string |  |
| `due[].source` | string |  |
| `due[].version` | string |  |
| `due[].created_at` | instant |  |
| `due[].updated_at` | instant |  |
| `due[].deleted_at` | instant |  |
| `due[].task_version` | string |  |
| `due[].task_deleted_at` | instant |  |
| `due[].project_title` | string |  |
| `due[].project_deleted_at` | instant |  |
| `due[].virtual` | boolean |  |
| `due[].label` | string | the task's label |
| `due[].reminder` | object |  |
| `due[].reminder.state` | string |  |
| `due[].reminder.at` | instant |  |
| `mood` | list | the values of the day's current Mood readings, in the order they were recorded (the Mood metric found by its name key); empty when none |
| `habits[]` | list | the habits active on the day, by name |
| `habits[].metric` | string | the habit's metric |
| `habits[].state` | string | `done`, `not done`, `not recorded` or `invalid` |
| `habits[].recent` | list | its state on each of the 14 days through the day, oldest first: a state above, or `inactive` on a day no period of the habit holds |
| `trends[]` | list | one for each metric with a current reading on the day that is not a habit active on the day, in the order of `readings` |
| `trends[].metric` | string | the metric's name |
| `trends[].unit` | string | its unit (`''` for a habit, a range such as `1-5` for a scale) |
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
of a month. Links: `prev`, `next` (the months before and after), `days` (the list of day pages). Embedded: a `day`
link to each day of the month. Actions: `open-day`. Each day holds what names it
([plan 101](docs/plans/101-the-calendar-holds-every-dated-thing.md)).

| property | type | meaning |
|---|---|---|
| `month` | string | the month, `YYYY-MM` |
| `today` | day | this machine's local day |
| `days[]` | list | each day of the month, in order |
| `days[].day` | day | the day |
| `days[].page` | boolean | a live day page exists for it |
| `days[].first_line` | string | the first line of that page's body that has text, as plain text (as `recent[]` of the root); absent when none |
| `days[].mood` | list | the values of the day's current Mood readings, in the order they were recorded; absent when none |
| `days[].items[]` | list | every other thing that names the day, in this order: the occurrences due on it (open, then done, then skipped), the births and deaths, the places and the pages; absent when none |
| `days[].items[].kind` | string | `task` (an occurrence of a live task, in every state, a recurring task's days included); `birthday` or `death` (a live person's birth or death day, and its day in each later year; 29 February is on 28 February in a common year); `place` (a live place that the live day page links with `at`); `page` or `file` (a live page that is not a day page and has the day, by title, at most 20 a day) |
| `days[].items[].title` | string | a task's label; else the title of the page |
| `days[].items[].href` | string | the item's own resource: the occurrence or the page |
| `days[].items[].state` | string | a task's: `open`, `done` or `skipped` |
| `days[].items[].years` | integer | a birthday's or a death's: the whole years since the day the person's row holds; absent on that day |
| `days[].more_pages` | integer | the pages and files of the day after the first 20 in `items[]`; absent when none |
| `days[].readings` | integer | the current readings on the day, of every metric but the Mood (`mood` holds those); absent when none |

### week

`GET /weeks/{YYYY-Www}` (`/weeks/current` redirects to this week; a value that is not a week, or a week number the
year does not have, is 422): a look back over an ISO 8601 week, Monday first. Links: `prev`, `next` (the weeks before
and after), `month` (the month of the week's Thursday), `year` (the ISO year), `index`. Embedded: a `day` link to each
day of the week. Actions: `open-day`.

| property | type | meaning |
|---|---|---|
| `week` | string | the week, `YYYY-Www` (the ISO year) |
| `from` | day | the Monday |
| `through` | day | the Sunday |
| `today` | day | this machine's local day |
| `days[]` | list | the seven days, in order (as `days[]` of the month) |
| `days[].day` | day | the day |
| `days[].page` | boolean | a live day page exists for it |
| `days[].first_line` | string | the first line of that page's body that has text, as plain text; absent when none |
| `days[].mood` | list | the values of the day's current Mood readings, in the order they were recorded; absent when none |
| `days[].items[]` | list | every other thing that names the day, in this order: the occurrences due on it (open, then done, then skipped), the births and deaths, the places and the pages; absent when none |
| `days[].items[].kind` | string | `task` (an occurrence of a live task, in every state, a recurring task's days included); `birthday` or `death` (a live person's birth or death day, and its day in each later year; 29 February is on 28 February in a common year); `place` (a live place that the live day page links with `at`); `page` or `file` (a live page that is not a day page and has the day, by title, at most 20 a day) |
| `days[].items[].title` | string | a task's label; else the title of the page |
| `days[].items[].href` | string | the item's own resource: the occurrence or the page |
| `days[].items[].state` | string | a task's: `open`, `done` or `skipped` |
| `days[].items[].years` | integer | a birthday's or a death's: the whole years since the day the person's row holds; absent on that day |
| `days[].more_pages` | integer | the pages and files of the day after the first 20 in `items[]`; absent when none |
| `days[].readings` | integer | the current readings on the day, of every metric but the Mood (`mood` holds those); absent when none |
| `habits[]` | list | each habit active on a day of the week through today, by name, counted as `completion` of habits is: a week in progress counts through today, a week to come counts nothing |
| `habits[].metric` | string | the habit's metric |
| `habits[].done` | integer | the days of the week on which it was done |
| `habits[].active_days` | integer | the days of the week on which it was active |
| `metrics[]` | list | each metric, a habit excepted, with a current reading in the week, by name |
| `metrics[].metric` | string | the metric's name |
| `metrics[].unit` | string | its unit; empty for a metric with none |
| `metrics[].readings` | integer | how many such readings the week holds |
| `metrics[].mean` | number | their mean, rounded to 2 decimals |
| `metrics[].previous_mean` | number | the mean of the week before, in the same way; absent when that week had no reading |
| `occurrences[]` | list | the occurrences due in the week, in every state (the occurrence shape, with `label`) |
| `occurrences[].due_day` | day |  |
| `occurrences[].state` | string |  |
| `occurrences[].completed_at` | instant |  |
| `occurrences[].reminder_mode` | string |  |
| `occurrences[].reminder_at` | instant |  |
| `occurrences[].id` | integer |  |
| `occurrences[].task_id` | integer |  |
| `occurrences[].key` | string |  |
| `occurrences[].import_key` | string |  |
| `occurrences[].source` | string |  |
| `occurrences[].version` | string |  |
| `occurrences[].created_at` | instant |  |
| `occurrences[].updated_at` | instant |  |
| `occurrences[].deleted_at` | instant |  |
| `occurrences[].task_version` | string |  |
| `occurrences[].task_deleted_at` | instant |  |
| `occurrences[].project_title` | string |  |
| `occurrences[].project_deleted_at` | instant |  |
| `occurrences[].virtual` | boolean |  |
| `occurrences[].label` | string | the task's label |
| `occurrences[].reminder` | object |  |
| `occurrences[].reminder.state` | string |  |
| `occurrences[].reminder.at` | instant |  |

### year

`GET /years/{YYYY}` (`/years/current` redirects to this year; a value that is not a year, 0001 to 9999, is 422): the
twelve months of a year. Links: `prev`, `next` (the years before and after), `index`. Embedded: a `month` link to each
month. Actions: `open-day`.

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
what is legal: `save-body` (with the current `body` and `version` filled), `promote`, `rename`, `link` and `unlink` (with the kinds this type may start), [`file-pages`](#filed) (a page that may
be `part-of`, not a day page: `ids` is this page, and `category` offers the category titles; a new title is made),
`create-page` with `category` filled (a live category page: something is `part-of` it; the new page is made and
filed in it in one write), `set-picture` (a person), `tombstone`, or `revive` alone on a deleted page. In a browser, the page of a
live metric answers 303 to its series, the metric's one page, which carries this page's text, links and actions. The
writes answer with this entity; `create-page` and `save-body` carry a `result` of the sync shape, `add-file` the kept
shape. `set-picture` (`POST /pages/{id}/picture`) puts `![[title]]` first in the body, unless that file is the page's
picture already (the first live file with a preview that the body embeds). It takes a new photo as `original` (and
`preview` when the photo is not a JPEG, PNG or GIF), kept as a file with no day and no place and titled
`<page> <today>.jpg` (` 2`, ` 3`… when taken) unless `title` names it; its `result` is the kept shape. Or it takes
the `title` of a kept file with a preview, and its `result` is the sync shape. A photo with no picture, or a title
that is not a kept picture, is refused (422).

| property | type | meaning |
|---|---|---|
| `task_project` | boolean | this page is a task's project |
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
`superseded-by` (what corrected it), `captured-with`. Actions on the current reading: `correct`,
`retract`. The properties are the reading's (above) and:

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
`all-readings`, `up` (metrics), `habits`, `page` (the metric's page). Embedded: `reading` links. Actions: `record`
(this metric), and for a habit `check-in` and `stop-habit`, else `start-habit` when unitless. A live metric is a page
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
| `habit_periods[]` | list | the periods it was a habit, oldest first |
| `habit_periods[].start_day` | day | from |
| `habit_periods[].end_day` | day | until, inclusive; absent while open |
| `categories` | list | the paths of the categories it is filed in (`biomarkers/iron`) |
| `id` | integer | the metric's page id; absent when the name is no live metric |
| `unit` | string | its unit, `''` for a habit, a range (`1-5`) for a scale; absent likewise |
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
[`file-pages`](#filed), `register-metric` (owner), and a `check-in` for each habit of today. A browser draws each metric
as a card: its latest reading, the line of its recent readings, and for a habit its strip of 14 days
([plan 112](docs/plans/112-metrics-as-cards.md)). `register-metric` takes `category` (a page title, made when missing)
and `habit_from` (a day, or `today`): one write makes the metric, files it and starts its habit. A refusal of any
step writes nothing. Its answer is the metric's [series](#series), with a `result` sentence
([plan 113](docs/plans/113-new-metric-on-metrics.md)).
With `?show=habits` (and `day`, `from`, `to`) it also carries the properties, links, embedded links and `check-in` actions of [habits](#habits). The browser draws that as the Habits lens of Metrics ([plan 111](docs/plans/111-one-metrics-view.md)).

| property | type | meaning |
|---|---|---|
| `metrics[]` | list | every live metric, by name |
| `metrics[].id` | integer | its page's id |
| `metrics[].name` | string | its name (the page's title) |
| `metrics[].unit` | string | the unit, `''` for a habit, a range (`1-5`) for a scale |
| `metrics[].note` | string | the page's text; absent when empty |
| `metrics[].habit` | boolean | it has a habit period |
| `metrics[].categories` | list | the category paths it is filed in; absent when none |
| `metrics[].recent[]` | list | its newest 30 current readings, oldest first: the last is its latest |
| `metrics[].recent[].id` | integer | the measurement's id |
| `metrics[].recent[].day` | day | its day |
| `metrics[].recent[].value` | number | its value |
| `groups[]` | list | the same metrics in sections: Habits, each category in tree order, Not filed |
| `groups[].title` | string | the section |
| `groups[].path` | string | the category's path; absent for Habits and Not filed |
| `groups[].page` | integer | the category's page id; absent likewise |
| `groups[].depth` | integer | the category's depth in the tree |
| `groups[].metrics[]` | list | the metrics in it (the metric shape) |
| `groups[].metrics[].id` | integer | |
| `groups[].metrics[].name` | string | |
| `groups[].metrics[].unit` | string | |
| `groups[].metrics[].note` | string | |
| `groups[].metrics[].habit` | boolean | |
| `groups[].metrics[].categories` | list | |
| `categories[]` | list | every category page, by path: what `file-pages` files in |
| `categories[].id` | integer | the category's page id |
| `categories[].title` | string | its title |
| `categories[].path` | string | the titles from the top, joined by `/` |
| `categories[].count` | integer | how many live things are filed directly in it; absent when none |
| `today` | day | the server's local day |
| `habit_days[]` | list | the habits active today, as [root](#root) `habits[]`; absent with `?show=habits` |
| `habit_days[].metric` | string | the habit's name |
| `habit_days[].state` | string | today's state |
| `habit_days[].recent` | list | its state on each of the 14 days through today, oldest first |

### habits

`GET /habits?day=&from=&to=`: the habits of a day (default today) and their completion over `[from, to]` (default
the 30 days to that day). Links: `day`, `prev`, `next`. Embedded: a `habit` link per habit to its series. Actions: a
`check-in` per habit, its `day` filled. A browser (`Accept: text/html`) is redirected to `/metrics?show=habits` with the same query; JSON, the CLI and MCP keep this resource.

| property | type | meaning |
|---|---|---|
| `day` | day | the day |
| `habits[]` | list | the habits active that day, by name |
| `habits[].metric` | string | the habit's name |
| `habits[].state` | string | `done`, `not done`, `not recorded` or `invalid` (a reading that is neither 0 nor 1) |
| `from` | day | the completion window's first day |
| `to` | day | its last day |
| `completion[]` | list | per habit, over the window |
| `completion[].metric` | string | |
| `completion[].active_days` | integer | days of the window the habit was on |
| `completion[].done` | integer | days checked in done |
| `completion[].not_done` | integer | days checked in not done |
| `completion[].not_recorded` | integer | active days with no check-in |
| `completion[].invalid` | integer | days with an invalid reading |

### tasks

`GET /tasks?include_deleted=`: every task definition, by id. Embedded: `item` links. Links: `deadlines`. Actions:
`create-task`, `deadlines` and `tombstone-tasks` (the delete of several at once, no version needed; its answer is the
class `tombstoned`, and a browser draws it as a box on each row). Planning is the [planning contract](docs/contract/planning.md): explicit intent, never a
record of what happened.

| property | type | meaning |
|---|---|---|
| `tasks[]` | list | the definitions (the task shape below) |
| `tasks[].label` | string |  |
| `tasks[].project_page_id` | integer |  |
| `tasks[].repeat_unit` | string |  |
| `tasks[].repeat_every` | integer |  |
| `tasks[].anchor_day` | day |  |
| `tasks[].repeat_until_day` | day |  |
| `tasks[].reminder_time` | string |  |
| `tasks[].reminder_zone` | string |  |
| `tasks[].id` | integer |  |
| `tasks[].import_key` | string |  |
| `tasks[].source` | string |  |
| `tasks[].version` | string |  |
| `tasks[].created_at` | instant |  |
| `tasks[].updated_at` | instant |  |
| `tasks[].deleted_at` | instant |  |
| `tasks[].project_title` | string |  |
| `tasks[].project_deleted_at` | instant |  |
| `tasks[].due_day` | day | a one-off's: the due day of its live `once`; absent for a series and an undated one-off |
| `include_deleted` | boolean | tombstoned tasks were included |

### task

`GET /tasks/{id}?from=&through=&include_deleted=`: one definition and its occurrences over an inclusive window
(default 30 days back to 90 days on): the rows written, and for a live series the virtual slots not yet written.
Links: `up` (tasks), `deadlines`, `project` (the project page). Embedded: an `occurrence` link per occurrence,
class `occurrence` or `virtual`. Actions: `edit-task`, `stop-task` (a series), `capture-occurrence`,
`tombstone-task`; then the **outcome** of each occurrence shown that is due 7 days after `today` or earlier, and of a
one-off's `once`; `revive-task` alone on a tombstoned one (class `task deleted`). An occurrence's outcome is the write
that sets its state: `capture-occurrence` with its key filled for a virtual slot, `edit-occurrence` with every current
value filled for a written one; there is none for a tombstoned occurrence.

| property | type | meaning |
|---|---|---|
| `task` | object | the definition |
| `task.label` | string | the label; repeated labels are allowed, a label is not a page |
| `task.project_page_id` | integer | the project page; absent when none |
| `task.repeat_unit` | string | `day`, `week`, `month` or `year`; absent for a one-off |
| `task.repeat_every` | integer | the interval in units; absent for a one-off |
| `task.anchor_day` | day | the first slot; absent for a one-off |
| `task.repeat_until_day` | day | the last slot, inclusive; absent while open-ended |
| `task.reminder_time` | string | the default reminder clock, `HH:MM`; absent when none |
| `task.reminder_zone` | string | its IANA zone; absent when none |
| `task.id` | integer | the task's id (its own namespace, not a page id) |
| `task.import_key` | string | an import's key; absent otherwise |
| `task.source` | string | the writer |
| `task.version` | string | the version token every task write sends back |
| `task.created_at` | instant |  |
| `task.updated_at` | instant |  |
| `task.deleted_at` | instant | when it was tombstoned; absent while it lives |
| `task.project_title` | string | the project page's title; absent when none |
| `task.project_deleted_at` | instant | when the project page was tombstoned; absent while it lives |
| `task.due_day` | day | a one-off's: the due day of its live `once`; absent for a series and an undated one-off |
| `from` | day | the window's first day |
| `through` | day | its last day |
| `include_deleted` | boolean | tombstoned occurrences (and a tombstoned task's) were included |
| `today` | day | this machine's local day: the occurrences due 7 days after it or earlier offer their outcome |
| `once` | object | a one-off's one occurrence, whatever its due day (the occurrence shape below); absent for a series and before it is written |
| `once.due_day` | day |  |
| `once.state` | string |  |
| `once.completed_at` | instant |  |
| `once.reminder_mode` | string |  |
| `once.reminder_at` | instant |  |
| `once.id` | integer |  |
| `once.task_id` | integer |  |
| `once.key` | string | `once` |
| `once.import_key` | string |  |
| `once.source` | string |  |
| `once.version` | string |  |
| `once.created_at` | instant |  |
| `once.updated_at` | instant |  |
| `once.deleted_at` | instant |  |
| `once.task_version` | string |  |
| `once.task_deleted_at` | instant |  |
| `once.project_title` | string |  |
| `once.project_deleted_at` | instant |  |
| `once.virtual` | boolean | always false |
| `once.label` | string | absent here: this is the task's own view |
| `once.reminder` | object |  |
| `once.reminder.state` | string |  |
| `once.reminder.at` | instant |  |
| `occurrences[]` | list | the occurrences in the window, by due day then key (the occurrence shape below) |
| `occurrences[].due_day` | day |  |
| `occurrences[].state` | string |  |
| `occurrences[].completed_at` | instant |  |
| `occurrences[].reminder_mode` | string |  |
| `occurrences[].reminder_at` | instant |  |
| `occurrences[].id` | integer |  |
| `occurrences[].task_id` | integer |  |
| `occurrences[].key` | string |  |
| `occurrences[].import_key` | string |  |
| `occurrences[].source` | string |  |
| `occurrences[].version` | string |  |
| `occurrences[].created_at` | instant |  |
| `occurrences[].updated_at` | instant |  |
| `occurrences[].deleted_at` | instant |  |
| `occurrences[].task_version` | string |  |
| `occurrences[].task_deleted_at` | instant |  |
| `occurrences[].project_title` | string |  |
| `occurrences[].project_deleted_at` | instant |  |
| `occurrences[].virtual` | boolean |  |
| `occurrences[].label` | string | absent here: this is the task's own view |
| `occurrences[].reminder` | object |  |
| `occurrences[].reminder.state` | string |  |
| `occurrences[].reminder.at` | instant |  |

### occurrence

`GET /tasks/{id}/occurrences/{key}`: one occurrence, written or (class `occurrence virtual`) a slot of a live
series not yet written. Links: `task`, `deadlines`, `project`. Actions: on a virtual slot `capture-occurrence`
with its key filled; on a written one `edit-occurrence` (every current value filled) and `tombstone-occurrence`;
`revive-occurrence` alone on a tombstoned one; none under a tombstoned task.

| property | type | meaning |
|---|---|---|
| `due_day` | day | the current deadline; absent when undated |
| `state` | string | `open`, `done` or `skipped` |
| `completed_at` | instant | when it was done, if known; absent otherwise |
| `reminder_mode` | string | `inherit` (the task's default), `off`, or `at` |
| `reminder_at` | instant | the absolute reminder, with mode `at`; absent otherwise |
| `id` | integer | the row's id; absent for a virtual slot |
| `task_id` | integer | the task |
| `key` | string | the slot: a day of the series, or `once` |
| `import_key` | string | an import's key; absent otherwise |
| `source` | string | the writer; absent for a virtual slot |
| `version` | string | the token every occurrence write sends back; absent for a virtual slot |
| `created_at` | instant | absent for a virtual slot |
| `updated_at` | instant | absent for a virtual slot |
| `deleted_at` | instant | when it was tombstoned; absent while it lives |
| `task_version` | string | the task's token, sent back with every occurrence write |
| `task_deleted_at` | instant | the task's tombstone; absent while it lives |
| `project_title` | string | the project page's title; absent when none |
| `project_deleted_at` | instant | the project page's tombstone; absent while it lives |
| `virtual` | boolean | not written yet: open work the series implies |
| `label` | string | the task's label |
| `reminder` | object | the reminder resolved from this intent |
| `reminder.state` | string | `none` (no reminder), `resolved`, or `unresolved` (a clock the zone cannot name) |
| `reminder.at` | instant | the UTC instant, when resolved |

### deadlines

`GET /deadlines?from=&through=&state=&include_deleted=`: every task's occurrences due in the window (default 30 days
back to 90 days on), `open` ones unless `state` is `done`, `skipped` or `all`. Overdue is this read with an earlier
`from`. Embedded: an `occurrence` link each. Links: `tasks`. Actions: `deadlines`, `create-task`, then the outcome
(see task) of each occurrence due 7 days after `today` or earlier.

| property | type | meaning |
|---|---|---|
| `from` | day | the window's first day |
| `through` | day | its last day |
| `state` | string | the filter applied |
| `include_deleted` | boolean | tombstoned rows and tasks were included |
| `today` | day | this machine's local day: the occurrences due 7 days after it or earlier offer their outcome |
| `occurrences[]` | list | the occurrences (the occurrence shape, with `label`), by due day, key, task |
| `occurrences[].due_day` | day |  |
| `occurrences[].state` | string |  |
| `occurrences[].completed_at` | instant |  |
| `occurrences[].reminder_mode` | string |  |
| `occurrences[].reminder_at` | instant |  |
| `occurrences[].id` | integer |  |
| `occurrences[].task_id` | integer |  |
| `occurrences[].key` | string |  |
| `occurrences[].import_key` | string |  |
| `occurrences[].source` | string |  |
| `occurrences[].version` | string |  |
| `occurrences[].created_at` | instant |  |
| `occurrences[].updated_at` | instant |  |
| `occurrences[].deleted_at` | instant |  |
| `occurrences[].task_version` | string |  |
| `occurrences[].task_deleted_at` | instant |  |
| `occurrences[].project_title` | string |  |
| `occurrences[].project_deleted_at` | instant |  |
| `occurrences[].virtual` | boolean |  |
| `occurrences[].label` | string | the task's label |
| `occurrences[].reminder` | object |  |
| `occurrences[].reminder.state` | string |  |
| `occurrences[].reminder.at` | instant |  |

### integrity

`GET /integrity`: the four integrity checks of the contract, and this writer's own.

| property | type | meaning |
|---|---|---|
| `ok` | boolean | everything passed |
| `integrity_check` | list | SQLite's `PRAGMA integrity_check` lines |
| `foreign_key_violations` | integer | |
| `entities_without_domain_row` | list | ids |
| `invalid_typed_links` | list | ids |
| `invalid_measurement_corrections` | list | ids |
| `invalid_task_projects` | list | ids |
| `invalid_one_off_tasks` | list | ids |
| `invalid_task_occurrences` | list | ids |
| `invalid_measurement_chains` | list | ids |
| `invalid_habit_periods` | list | ids |
| `invalid_habit_readings` | list | ids |
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
browser form is answered home with them as feedback. A refusal (a folder inside a git work tree, a name in use) is
422 `snapshot_refused`.

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
| `restore_check.invalid_task_projects` | list | |
| `restore_check.invalid_one_off_tasks` | list | |
| `restore_check.invalid_task_occurrences` | list | |
| `restore_check.invalid_measurement_chains` | list | |
| `restore_check.invalid_habit_periods` | list | |
| `restore_check.invalid_habit_readings` | list | |
| `restore_check.invalid_journal_names` | list | |
| `restore_check.full_text_index_ok` | boolean | |
| `restore_check.full_text_error` | string | |

### tombstoned

`POST /pages/tombstone` (`tombstone-pages`) and `POST /tasks/tombstone` (`tombstone-tasks`): the one field `ids` names
the pages or the tasks, separated by spaces or commas, or sent once per id as a browser form does; 500 at most. Every
id is tombstoned in one write, or none is: an id no row has is 404, and a row tombstoned already is left as it is. The
answer embeds an `item` link to each; `result` says the count in words, which a browser form shows on the list it
returns to. `self` is `/tasks` for tasks and `/` for pages.

| property | type | meaning |
|---|---|---|
| `kind` | string | `pages` or `tasks` |
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
| `day` | day | the day it was filed under |
| `embedded` | boolean | `![[title]]` was appended to its day page |
