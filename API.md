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
  for `add-file`, `multipart/form-data`.
- **Writes answer with the resource they changed** (200, with `Location`), and `result` says what the write did. A
  browser form is answered 303 to that resource instead.

## The entity

Every answer is one [Siren](https://github.com/kevinswiber/siren) entity:

| field | what |
|---|---|
| `class` | the kind of resource: one of the classes below, sometimes with a second word (`day-page`, `deleted`, `dry-run`) |
| `title` | a human title |
| `properties` | the resource's own data, per class below |
| `entities` | embedded links: `{rel, href, title, class}` each, the things this resource lists or links to |
| `links` | `{rel, href, title}`: `self`, `index` (home), `prev`/`next` (a neighbour or the next page), and the rels named per class |
| `actions` | what may be done here: `{name, title, description, method, href, type, fields, owner}`, each field `{name, type, title, value, required, options, in}` with its current value filled in; `owner: true` marks the owner's alone; `in: "path"` marks a field of a templated href (`GET /actions` only) |
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
| `code` | string | what went wrong, to branch on: `stale_version` (a save with an old `version`), `exists` (a title in use; the link `existing` names the page), `cross_origin` (a browser write from another origin), `token_required` (a public listener without a credential), or the status's general name: `bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `too_large`, `invalid` (422), `timeout`, `internal` |
| `message` | string | the text, for a person |

## Pages of a list

`/days`, `/people`, `/places`, `/files`, `/ghosts` and `/search` take `limit` (1–500; defaults 60 for days, 50 for
search, 200 otherwise) and `offset` (0 or more), carry both in their properties, and link `next` when a further row
exists and `prev` past the first page. Orders: days newest first; people, places and files by title; ghosts oldest
first; search by rank.

## Classes

### root

`GET /`. Links: `today`, `days`, `sessions`, `periods`, `people`, `places`, `files`, `metrics`, `habits`, `ghosts`,
`actions`. Actions: `capture` (for today), `search`, `find`, `capture-session`, `create-period`, `create-page`,
`create-person`, `create-place`, `add-file`, `record`, `register-metric` (owner), `query`.

| property | type | meaning |
|---|---|---|
| `today` | day | this machine's local day |

### actions

`GET /actions`: no properties; `actions` is the whole catalog with templated hrefs. With an import workspace the
import actions are included and the link `import` leads to its status.

### days, people, places, files, ghosts

`GET /days`, `/people`, `/places`, `/files`, `/ghosts`: one page of a list, each row an embedded `item` link to the
page (with `class` the page's type where it has one). `people`, `places` and `files` offer `create-person`,
`create-place` and `add-file`.

| property | type | meaning |
|---|---|---|
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |

### search

`GET /search?q=` (FTS5 syntax over titles and bodies). Hits are also embedded as `item` links. Action: `search`.

| property | type | meaning |
|---|---|---|
| `q` | string | the query |
| `hits[]` | list | the hits of this page, best first |
| `hits[].id` | integer | the page's id |
| `hits[].title` | string | its title |
| `hits[].snippet` | string | the matching text, matches in `[`…`]` |
| `limit` | integer | the page size used |
| `offset` | integer | the rows skipped |

### day

`GET /days/{day}` (`/days/today` redirects). Links: `prev`, `next` (the neighbouring days), `page` (the day page,
once it exists), `metrics`. Embedded: `reading` links to each measurement. Actions: `capture`, `record`. The write
`capture` answers with this entity and a `result` of the sync shape.

| property | type | meaning |
|---|---|---|
| `day` | day | the day |
| `page_id` | integer | the id of the day page; absent when none exists yet |
| `view[]` | list | the day view of the cookbook, one row each, in reading order |
| `view[].what` | string | `day page` (its body), `page` or `page (edited)` (a page written that day), `at` (a place the day was at), `habit` (a habit and its state), or a metric's title (a reading not part of a habit) |
| `view[].at` | instant | when, for a page or a reading; absent otherwise |
| `view[].detail` | string | the body, the page title, the place, `Metric: done`/`not done`/`not recorded`/`invalid`, or `value unit` |
| `readings[]` | list | the day's readings, each a reading (below) with its id, to correct one |

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
| `readings[].session_id` | integer | the session it belongs to; absent when unassociated |
| `readings[].session_deleted_at` | instant | when that session was tombstoned; absent while it lives |
| `readings[].scope` | string | `unassociated` or `session` |

### page, person, place, file, period

`GET /pages/{id}`; `GET /pages?title=` redirects to it. The class is the page's type; a day page adds `day-page`, a
tombstoned page adds `deleted`. Links: `day` (a day page's day view), `preview` (a file's picture), `map` (a place's
point). Embedded: every outgoing link under its kind, every incoming one under `backlink` and its kind. Actions by
what is legal: `save-body` (with the current `body` and `version` filled), `promote`, `promote-period`, `rename`,
`edit-period`, `locate`, `link` and `unlink` (with the kinds this type may start), `tombstone`, or `revive` alone on
a deleted page. The writes answer with this entity; `create-page` and `save-body` carry a `result` of the sync shape,
`add-file` the kept shape.

| property | type | meaning |
|---|---|---|
| `session_kind` | boolean | this page names a kind of session |
| `task_project` | boolean | this page is a task's project |
| `id` | integer | the page's id, permanent |
| `entity_type` | string | `page`, `person`, `place`, `metric`, `file` or `period` |
| `title` | string | its preferred title; a day page's is its day |
| `day` | day | the page's day: a day page's own, or the day a note or file belongs to; absent otherwise |
| `body` | string | the body, CommonMark with `[[wikilinks]]` and `#tags` |
| `created_at` | instant | when it was created |
| `version` | string | the version token a `save-body` must send back |
| `deleted_at` | instant | when it was tombstoned; absent while it lives |
| `is_day_page` | boolean | it is a journal day page |
| `period` | object | a period's boundaries; absent otherwise |
| `period.id` | integer | the page's id again |
| `period.title` | string | its title again |
| `period.start_boundary` | day | the start, `null` when unknown |
| `period.end_boundary` | day | the end, `null` when unknown, `..` when ongoing |
| `period.version` | string | the version token `edit-period` sends back |
| `period.deleted_at` | instant | absent while it lives |
| `period.membership` | string | in a periods list only: how the asked day falls in it; absent here |
| `person` | object | a person's details; absent otherwise |
| `person.name` | string | the full name |
| `person.birth_day` | day | born; absent when unknown |
| `person.death_day` | day | died; absent when unknown |
| `file` | object | a file's identity; absent otherwise |
| `file.sha256` | string | the SHA-256 of the original, hex |
| `file.mime` | string | its media type |
| `file.preview` | boolean | a picture is kept (`GET /pages/{id}/preview`) |
| `point` | object | a place's point; absent until located |
| `point.lat` | number | latitude |
| `point.lon` | number | longitude |
| `point.radius_m` | integer | the radius in metres a photo inside is matched by |
| `point.link_days` | boolean | its days are linked (`false`: recognised, never linked, like home) |
| `links[]` | list | the typed links this page starts |
| `links[].kind` | string | the link kind (`wikilink`, `at`, `about`, `located-in`, `parent-of`, `part-of`, `friend`, `family`, `related`) |
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
`superseded-by` (what corrected it), `session`, `captured-with`. Actions on the current reading: `correct`,
`retract`, `relocate-reading` (owner; when its scope may move). The properties are the reading's (above) and:

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
| `session_id` | integer | |
| `session_deleted_at` | instant | |
| `scope` | string | |
| `current` | boolean | this is the reading that stands (not corrected, not retracted) |
| `supersedes_id` | integer | the reading it corrects; absent for a first reading |
| `superseded_by` | integer | the reading that corrects it; absent while it stands |
| `retraction` | boolean | it retracts the chain: its value is void |

### series

`GET /metrics/{name}?from=&to=&scope=&session_id=&include_deleted=&all_history=`: a metric's readings over
`(from, to]` (default the 90 days to today; `all_history=1` instead of `from`). Links: `all-readings`, `up`
(metrics), `habits`, `page` (the metric's page). Embedded: `reading` links. Actions: `record` (this metric), and for a
habit `check-in` and `stop-habit`, else `start-habit` when unitless; `link` titled "File in category".

| property | type | meaning |
|---|---|---|
| `metric` | string | the name |
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
| `readings[].session_id` | integer | |
| `readings[].session_deleted_at` | instant | |
| `readings[].scope` | string | |
| `habit_periods[]` | list | the periods it was a habit, oldest first |
| `habit_periods[].start_day` | day | from |
| `habit_periods[].end_day` | day | until, inclusive; absent while open |
| `categories` | list | the paths of the categories it is filed in (`biomarkers/iron`) |
| `scope` | string | `unassociated` (the default) or `session` |
| `session_id` | integer | the session selected, 0 for none |
| `include_deleted` | boolean | readings of tombstoned sessions were included |
| `all_history` | boolean | every admitted day through `to` was selected |

### metrics

`GET /metrics`: the registry. Embedded: `item` links to each series. Action: `record` with the metrics as options.

| property | type | meaning |
|---|---|---|
| `metrics[]` | list | every live metric, by name |
| `metrics[].id` | integer | its page's id |
| `metrics[].name` | string | its name (the page's title) |
| `metrics[].unit` | string | the unit, `''` for a habit, a range (`1-5`) for a scale |
| `metrics[].note` | string | the page's text; absent when empty |
| `metrics[].habit` | boolean | it has a habit period |
| `metrics[].categories` | list | the category paths it is filed in; absent when none |
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

### habits

`GET /habits?day=&from=&to=`: the habits of a day (default today) and their completion over `[from, to]` (default
the 30 days to that day). Links: `day`, `prev`, `next`. Embedded: a `habit` link per habit to its series. Actions: a
`check-in` per habit, its `day` filled.

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

### sessions

`GET /sessions?day=&kind=&include_deleted=`: recorded sessions. Embedded: `item` links. Action: `capture-session`.

| property | type | meaning |
|---|---|---|
| `sessions[]` | list | the sessions (the session shape), by reporting day then id |
| `sessions[].kind` | string | |
| `sessions[].day` | day | |
| `sessions[].import_key` | string | |
| `sessions[].start_at` | instant | |
| `sessions[].start_local` | string | |
| `sessions[].start_offset` | string | |
| `sessions[].start_zone_unverified` | string | |
| `sessions[].end_at` | instant | |
| `sessions[].end_local` | string | |
| `sessions[].end_offset` | string | |
| `sessions[].end_zone_unverified` | string | |
| `sessions[].id` | integer | |
| `sessions[].kind_id` | integer | |
| `sessions[].kind_deleted_at` | instant | |
| `sessions[].source` | string | |
| `sessions[].version` | string | |
| `sessions[].created_at` | instant | |
| `sessions[].updated_at` | instant | |
| `sessions[].deleted_at` | instant | |
| `sessions[].elapsed_milliseconds` | integer | |
| `sessions[].time_basis` | string | |
| `include_deleted` | boolean | tombstoned sessions were included |
| `order` | string | a note on the order |

### session

`GET /sessions/{id}`. Links: `kind` (the page naming its kind), `index` (the sessions list). Actions: `edit-session`,
`tombstone-session`, or `revive-session` on a tombstoned one, their fields filled.

| property | type | meaning |
|---|---|---|
| `kind` | string | the kind: the title of a plain page |
| `day` | day | the reporting day |
| `import_key` | string | an import's key; absent otherwise |
| `start_at` | instant | the start, when known in UTC; absent otherwise |
| `start_local` | string | the start as an unresolved local clock; absent otherwise |
| `start_offset` | string | a known offset of the start; absent otherwise |
| `start_zone_unverified` | string | a zone claimed for the start, unverified; absent otherwise |
| `end_at` | instant | the end in UTC; absent otherwise |
| `end_local` | string | the end as a local clock; absent otherwise |
| `end_offset` | string | a known offset of the end; absent otherwise |
| `end_zone_unverified` | string | a zone claimed for the end; absent otherwise |
| `id` | integer | the session's id |
| `kind_id` | integer | the kind page's id |
| `kind_deleted_at` | instant | when the kind page was tombstoned; absent while it lives |
| `source` | string | the writer that recorded it |
| `version` | string | the version token an edit sends back |
| `created_at` | instant | |
| `updated_at` | instant | |
| `deleted_at` | instant | when it was tombstoned; absent while it lives |
| `elapsed_milliseconds` | integer | the length, when both ends are known in UTC; absent otherwise |
| `time_basis` | string | `known UTC interval`, `missing end` or `unresolved` |

### periods

`GET /periods?day=&as_of=&include_deleted=`: recorded life periods, with their membership of `day` when one is asked.
Embedded: `item` links to the period pages. Action: `create-period`.

| property | type | meaning |
|---|---|---|
| `periods[]` | list | the periods (a page's `period` object, with `membership`) |
| `periods[].id` | integer | |
| `periods[].title` | string | |
| `periods[].start_boundary` | day | |
| `periods[].end_boundary` | day | |
| `periods[].version` | string | |
| `periods[].deleted_at` | instant | |
| `periods[].membership` | string | how `day` falls in the period, when asked |
| `day` | string | the day asked, or empty |
| `as_of` | string | the as-of day asked, or empty |
| `membership_basis` | string | a note: membership is by the supplied day, not by interval overlap |
| `include_deleted` | boolean | tombstoned periods were included |

### tasks

`GET /tasks?include_deleted=`: every task definition, by id. Embedded: `item` links. Links: `deadlines`. Actions:
`create-task`, `deadlines`. Planning is the [planning contract](docs/contract/planning.md): explicit intent, never a
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
| `include_deleted` | boolean | tombstoned tasks were included |

### task

`GET /tasks/{id}?from=&through=&include_deleted=`: one definition and its occurrences over an inclusive window
(default 30 days back to 90 days on): the rows written, and for a live series the virtual slots not yet written.
Links: `up` (tasks), `deadlines`, `project` (the project page). Embedded: an `occurrence` link per occurrence,
class `occurrence` or `virtual`. Actions: `edit-task`, `stop-task` (a series), `capture-occurrence`,
`tombstone-task`; `revive-task` alone on a tombstoned one (class `task deleted`).

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
| `from` | day | the window's first day |
| `through` | day | its last day |
| `include_deleted` | boolean | tombstoned occurrences (and a tombstoned task's) were included |
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
`from`. Embedded: an `occurrence` link each. Links: `tasks`. Actions: `deadlines`, `create-task`.

| property | type | meaning |
|---|---|---|
| `from` | day | the window's first day |
| `through` | day | its last day |
| `state` | string | the filter applied |
| `include_deleted` | boolean | tombstoned rows and tasks were included |
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
| `invalid_session_kinds` | list | ids |
| `invalid_measurement_scopes` | list | ids |
| `invalid_task_projects` | list | ids |
| `invalid_one_off_tasks` | list | ids |
| `invalid_task_occurrences` | list | ids |
| `invalid_symmetric_links` | list | ids |
| `invalid_measurement_chains` | list | ids |
| `invalid_habit_periods` | list | ids |
| `invalid_habit_readings` | list | ids |
| `invalid_journal_names` | list | ids |
| `full_text_index_ok` | boolean | |
| `full_text_error` | string | absent when the index is fine |

### result

The class of an answer that is not a resource: `POST /query` (one read-only statement, 500 rows at most), the
`record` of a reading whose `import_key` was seen before (`message` alone), and `add-file` with `dry_run=1` (class
`result dry-run`, with the kept shape in `result`).

| property | type | meaning |
|---|---|---|
| `columns` | list | a query's column names |
| `rows` | list | its rows, each a list of values |
| `truncated` | boolean | more rows existed than were returned |

A dry run's properties are `title`, `sha256`, `mime` and `picture` (boolean: a picture was sent).

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
| `restore_check.invalid_session_kinds` | list | |
| `restore_check.invalid_measurement_scopes` | list | |
| `restore_check.invalid_task_projects` | list | |
| `restore_check.invalid_one_off_tasks` | list | |
| `restore_check.invalid_task_occurrences` | list | |
| `restore_check.invalid_symmetric_links` | list | |
| `restore_check.invalid_measurement_chains` | list | |
| `restore_check.invalid_habit_periods` | list | |
| `restore_check.invalid_habit_readings` | list | |
| `restore_check.invalid_journal_names` | list | |
| `restore_check.full_text_index_ok` | boolean | |
| `restore_check.full_text_error` | string | |

### login

`GET /login` on a public listener: the one page served without a credential. `POST /login` with `token` sets the
cookie and redirects home; `POST /logout` clears it.

| property | type | meaning |
|---|---|---|
| `failed` | boolean | the last attempt was refused |
| `logged_in` | boolean | this browser already carries the cookie |

### import, …

With `--workspace`, the `/import` routes answer with class `import` and a second word (`status`, `ledger`,
`questions`, `find`, …). They are the model's surface during an import and are described by
[importing with a model](docs/guides/importing.md), not here.

## Result shapes

### result of capture, create-page, save-body

What the save contract did with the body's `[[wikilinks]]` and `#tags`:

| property | type | meaning |
|---|---|---|
| `linked` | list | the titles linked |
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
| `sync.created` | list | |
| `sync.revived` | list | |
| `sync.skipped` | list | |
| `day_sync` | object | the sync shape of its day page, when it was appended there |
| `day_sync.linked` | list | |
| `day_sync.created` | list | |
| `day_sync.revived` | list | |
| `day_sync.skipped` | list | |
| `day` | day | the day it was filed under |
| `place` | string | the place matched or named |
| `linked` | boolean | its day got an `at` link to that place |
| `point_set` | boolean | the place named took this position as its point |
| `unmatched` | object | a position near no known place |
| `unmatched.lat` | number | |
| `unmatched.lon` | number | |
| `unmatched.map` | string | a map link to it |
| `embedded` | boolean | `![[title]]` was appended to its day page |

### result of relocate-reading

| property | type | meaning |
|---|---|---|
| `retraction_id` | integer | the retraction written for the old scope |
| `replacement_id` | integer | the independent replacement reading |
