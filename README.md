# Lifelog

`lifelog` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people and places in them, where you were, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. The database also preserves
[explicit personal tasks and reminder intent](docs/contract/planning.md); planning interfaces and notification
delivery are separate application work. It serves the file three ways: a hypermedia API (with an HTML face for
the browser), a CLI and an MCP server.

The database it writes is specified independently of it, so another program — in any language — can read
`life.db`, or write its own:

- **[docs/](docs/README.md)** — the contract of `life.db`: goals, the canonical [schema.sql](docs/schema/schema.sql),
  the storage contract, the decision log, the query cookbook, research, and the process (issues, proposals, plans).
  It names no implementation; [building a writer](docs/guides/building-a-writer.md) is the way in for another one.
- **[tests/](tests/README.md)** — the validation suites: every executed claim of the docs, run against the schema and
  against this writer's own extraction and save. `go test ./tests`.
- **[API.md](API.md)** — the wire shape for client authors: every entity class and its properties, the errors, the
  pages, how to reach a server; a test keeps it equal to the code.
- **[AGENTS.md](AGENTS.md)** — the rules for anyone, human or agent, working in this repo.

`lifelog` implements the contract and never defines it: a rule found missing or ambiguous here is an
[issue](docs/issues/README.md) against the docs, not a fix in this code alone.

```
go generate ./... && go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo
lifelog init                               # life.db beside the program; --db PATH or $LIFELOG_DB names another
lifelog agentic-init                       # AGENTS.md, skills and MCP configs here, for a coding agent opened here

lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog day --human
lifelog serve                              # http://127.0.0.1:7777 — open it in a browser; this machine only
lifelog serve --public --token-file ~/life/token   # for your other devices: every request needs the token
lifelog mcp --agent lmstudio               # MCP on stdio; rows are written as agent:lmstudio
lifelog ask "What did I do last Sunday?"  # a model answers with the actions ($LIFELOG_MODEL_URL, $LIFELOG_MODEL)
lifelog habits --human                     # today's habits; lifelog done evening_walk
lifelog snapshot --to ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg   # a photo lifelog cannot read: send a JPEG of it
lifelog file IMG_0003.jpg --at Lakeside --radius 300   # a photo near no known place: name the place
lifelog file ~/picks/2019-06 --dry-run --human   # the few photos chosen for some days: what keeping them would do
lifelog file ~/picks/2019-06 --human             # keep them: their days linked, the ones near no place grouped
```

An import is done by a program that loads rows itself ([imports](docs/contract/imports.md)), or by a coding agent
with the owner, in a conversation: the agent reads a source with its own tools and writes through the catalog's MCP
tools, and the owner says yes or no to each person, place and link ([plan 089](docs/plans/089-no-import-process.md)).
`lifelog agentic-init` prepares the folder for it ([plan 090](docs/plans/090-agentic-init.md)); then open Claude
Code, Codex, OpenCode or Pi in the folder.

```
lifelog snapshot                                   # the owner, before an import session
lifelog readings "Ferritin results" Ferritin       # readings of a registered metric from the table of a page
```

## Decisions

These are this application's own engineering decisions ([D14](docs/decisions/D14-ui-and-tools.md)).

- **One handler, three surfaces.** `internal/api` is the only thing the surfaces call. `serve` mounts it on a
  socket; the CLI and the MCP server call it in-process (`internal/client`), or a running server with `--url`.
  Writes go through `internal/core` only: no generic UPDATE or DELETE exists anywhere.
- **Server shutdown drains admitted requests.** An interrupt closes the listener, then allows ten seconds for active
  requests to finish before closing their connections. Shutdown completes before the database closes; expiration
  is reported as an error.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled (a page's `save-body` carries its body and `version`). `GET /actions` is
  the catalog; the CLI's `do` and the MCP tools are generated from it, so a new action needs no client change.
  An error is an entity too: `status`, a `code` a client branches on (`stale_version`, `exists`, `not_found`, `snapshot_refused`, `restore_check_failed`,
  `invalid`, `cross_origin`, `token_required`, or the status's general name) and the message. The lists `/days`,
  `/people`, `/places`, `/files`, `/ghosts` and `/search` take `limit` (1–500) and `offset` and link `next` and
  `prev` ([plan 079](docs/plans/079-client-contract-hygiene.md)).
- **The browser gets the same entity.** `Accept: text/html` renders it with a template per class (day, page,
  person, place, metric, habits, search, lists; `internal/api/html/`, embedded) or a generic one for any other. A
  template reads only the properties the JSON carries and places only the actions the entity offers, so a form
  shows where the action is legal and nowhere else. A body is CommonMark with goldmark's safe defaults (raw HTML
  omitted), its `[[wikilinks]]` and `#tags` linked. A browser form posts and is answered 303 to the resource it
  changed, so a reload never posts twice; a refused one shows the text it sent. A form may name the page
  to return to in `back` (a path on this server, not a field of any action): a Done button on a list returns to the
  list. A value that is not a plain path is ignored, and a JSON client never follows it
  ([plan 091](docs/plans/091-the-browser-for-daily-use.md)).
- **The page is semantic HTML, and the catalog says how to draw it.** A field is a `label` around its input (a field
  of 2 to 5 short options a `fieldset` of radio buttons), a card is an `article`, a callout an `aside`, an error a
  `role="alert"`, a series a `table` with a header row; no id or class is generated for a template to find. What a
  template cannot know comes from the entity: `danger` marks a destructive action's button, `owner` an owner-only
  one, `rows` a textarea's height, `options` a field's choices, `pages` a field whose value is a page's title and an
  error's `code` its first words, so no view decides any of them by an action's or a field's name.
- **The browser for daily use** ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). The root is today at a
  glance: what is due today and overdue, today's habits and the five newest day pages. A day carries what is due on
  it, its mood, its habits and the `save-body` of its day page, so the day view edits in place. The tasks, a task, an
  occurrence and the deadlines have their own views, and the deadlines are grouped by their due day against today.
  Each occurrence due 7 days after today or earlier offers its outcome action, which the browser draws as Done and
  Skip, or Reopen; a habit's check-in is Done and Not done. These are properties and actions of the entities, so every
  client gets them ([API.md](API.md)). `/months/{YYYY-MM}` is the calendar of a month (each day, whether it has a
  page, its first line, its mood and its readings, and every other thing that names the day: the work due on it, a
  birthday or a death each year, a period boundary, a session, a place, a page or a file; under the calendar, Day by
  day links each one and Periods says what the month falls in: [plan 101](docs/plans/101-the-calendar-holds-every-dated-thing.md)),
  and the catalog action `open-day` goes to a day by its date. The handler reads
  today from one clock, which a test fixes. The day and the root carry `on_this_day`, the day pages of the same date
  in earlier years, which the margin shows after Due. `/weeks/{YYYY-Www}` is the week (its days, its habits as
  "N of M days", its readings against the week before, the work due) and `/years/{YYYY}` the year (twelve months);
  People and Places list each name with the pages that mention it and the day of the last one
  ([plan 097](docs/plans/097-the-almanac-reflects.md)).
- **The app shell: the Almanac design system** ([plan 092](docs/plans/092-the-app-shell.md)). Every page a browser
  gets is in one shell: a sidebar with the brand and today's date, the search, New, the sections of every day (Today,
  Calendar, Due, Tasks) and of the record (People to Ghost pages), this month with its written days, and the
  owner's links. Under 900 px it is a phone's layout
  ([plan 093](docs/plans/093-words-phone-and-today.md)): a top bar (the brand, the date, Search, New), a bottom bar of
  the sections of every day and More, and the rest as a sheet over the page while it is the target (`#sections`),
  which needs no script. The sidebar is the one part of a page that is not the
  page's entity: once a page, and for a browser only, the shell reads the Due count (the root's `due`) and this
  month (`GET /months/{month}`), which every client gets from those two resources. A page drawn before the handler
  (the login, a refusal of the token) has no sidebar, so it shows nothing of the database. The look is the Almanac
  bound in Hearthwood, the materials of the owner's Balaur design system
  ([plan 096](docs/plans/096-bound-in-hearthwood.md)): the page is dark oak, the sidebar and the phone's bars are
  wood, and everything written is parchment with ink; square corners, 2 px edges, bevels and 3 px hard drops, no
  blur and no gradient; gold marks where the owner is and what is today, ember is the one loud button of a screen and
  the word "overdue", teal is a link. Words are in Atkinson Hyperlegible, every day, time and number in IBM Plex Mono,
  and a page's title, the leaf's number and a reading's value in Jersey 15; dark is the identity and light the
  daylight study of the same room, by `prefers-color-scheme`. Its tokens are custom properties of `base.css`; a
  material sets its own text through aliases (`--text`, `--text-link`, …), and a test holds every text pair at
  4.5:1 and every edge and the focus ring at 3:1 in both themes. The two motions: a press sinks, and the step to the previous
  or next day moves the leaf and cross-fades the sheet through a cross-document view transition, off under reduced
  motion ([plan 097](docs/plans/097-the-almanac-reflects.md)).

  Do and don't:
  - Show one loud button a screen.
  - Show no streak, no chain and no badge for consistency. A habit is "N of M days".
  - Write in sentence case. Use no capitals for effect.
  - Use mono for days, times and numbers only.
  - Let a material set its own text through the aliases.
  - Use no colour outside the one token block. Its `light-dark()` pairs are held by the contrast test at 4.5:1, with
    the APCA Lc logged beside it.
  - Draw a value by its type, never by its name.
  - Make every form work with no script.
  - Answer a refused form on its own page, filled, with the alert first.
  - Make every empty state name the way forward.

  Each part
  of an entity has one component: the title and the `prev`/`next` links a page header, properties facts and
  panels, sub-entities rows (an occurrence's with Done and Skip), an action with no visible field a button, one with
  fields a button that opens its form, an error an alert, and a write's `result` a toast at the top of the page,
  which stays until the next page because it may name what the write did not do. A day and Home are a calendar
  leaf, a sheet and its margin ([plan 095](docs/plans/095-the-leaf-and-its-margin.md)): the leaf (the month on a
  band, the day's number in mono) heads the page; the sheet holds what the page is for (the journal, then Capture);
  the margin holds what the page asks of the owner (Due, the habits, the readings, the places), and comes after the
  sheet on a narrow screen. A page, a list and the planning views use the same sheet. One button a screen is filled:
  the main write; a row's Done is an outline, Skip is quiet, and an overdue row says so in a word. A view can fold an
  action's optional fields under "N more fields", inside the form, so a browser sends them with no script.
  A value is drawn by its type, never by its property's name ([plan 093](docs/plans/093-words-phone-and-today.md)):
  a day in words near today ("yesterday · Sun 11 Oct"), an instant in the clock of the server's day ("today, 09:48",
  the exact instant kept in its `<time>` element), an object of the generic view as facts, and a write's result in
  sentences by the result shapes of API.md. The day carries `trends` (each metric read that day, its unit and its
  readings of the 14 days through it) and each habit its `recent` states, so a reading's tile has its unit and its
  line and a habit its strip of 14 days; a series shows its next 5 later slots on its own page.
  `GET /styleguide` is the style page: every component in every state, drawn on synthetic words, with the file each
  lives in. It is presentation, like `/render`: it is not in the catalog and it reads nothing from the database.
  `GET /manifest.webmanifest` and `GET /assets/icon.svg` make the app installable: three shortcuts (Today, Add to
  today, What is due) and a share target that posts shared text into Capture (`POST /days/today/capture`, where
  `today` is the local day). There is no service worker, since the server is the owner's own. A public listener
  refuses all three without the token, as it refuses the fonts.
- **One script, which no page needs** ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). `app.js` is this
  application's own, embedded and written into each page like the two sheets: no framework, no CDN, no build step.
  It suggests the names of pages after `[[` and in a field for a page's title (from `GET /titles`), shows a preview of a body as it will show when
  saved (from `POST /render`), sends the form of a textarea with Ctrl+Enter, leaves a field with Escape, takes keys
  outside a field (`/` search, `c` capture, `t` today, `[` and `]` the previous and next page, `?` the command list
  with the legend of every key), shows the names after `[[` as a popover, and asks before
  the browser leaves text that was not sent. It gives the toast a Close (and Escape), makes a danger button ask once
  in the page before it sends ([plan 092](docs/plans/092-the-app-shell.md)), and closes the phone's sheet of sections
  with Escape ([plan 093](docs/plans/093-words-phone-and-today.md)). Ctrl+K, from anywhere, and the phone's Search
  button open the **command list** ([plan 094](docs/plans/094-the-command-list.md)): a typed day, this page's actions,
  the sidebar's sections, the pages by the start of their title (`GET /titles`), the root's actions (`GET /`, once a
  page) and a search of the bodies. It never builds a form: an action of this page opens the form the server drew for
  it (every form carries its action's catalog name in `data-action`), and an action of the root goes to `/#do-{name}`,
  where Home opens that form. It reads nothing else, writes nothing and keeps nothing in the browser. Every form posts
  and every link works without it; the tests check the pages without running it, and a test keeps its reads to those
  three routes.
  `POST /render` is presentation, not a capability: no catalog action names it and it answers HTML only, so it, the
  fonts, the style page, the manifest, the icon and the pictures (`GET /pages/{id}/preview`, `GET /previews`) are the
  routes the CLI and the MCP server do not carry. **Rejected:** a separate web client. It would be a second
  implementation of every view to keep equal to the catalog, it would hold the token where JavaScript can read it,
  and it would bring a toolchain that changes every year.
- **The stylesheet is this application's own: two embedded sheets, three embedded faces, no vendored framework and
  no CDN.** `base.css` holds the tokens and styles ordinary HTML by element — type, colour, forms, tables, buttons, a
  print sheet, light and dark — and `app.css` only what this app's own vocabulary needs (the shell and its
  components, `.wikilink`, `.chart`); a test keeps the app's names out of the base sheet. The three faces are open
  fonts (SIL OFL 1.1), latin and latin-ext, embedded with their licences and served by exact name at
  `GET /assets/fonts/{file}` with a cache of a year; like `/render`, the route is presentation and not in the catalog.
  Nothing is fetched from another host, so a page looks the same offline in fifty years. **Rejected:** a vendored classless sheet (Simple.css, Pico). Its
  `aside` is a floated sidebar and ours is a callout in flow, its `section` a bordered band, its `h2` 2.6rem where
  ours labels a section, its tables bordered and zebra-striped where ours are dense, its form controls carry blanket
  margins where ours sit two to a row, and its `body` is a 45rem grid: keeping one meant overriding most of it, and
  re-pinning a file by hand at every upstream change.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (a browser form) or `agent:<name>` (MCP);
  the `Lifelog-Source` header sets it. The agent of `ask` writes as `agent:chat`, whoever asked.
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one is a 409.
- **One page a metric** ([plan 100](docs/plans/100-one-page-a-metric.md)). `/metrics/{name}` is the metric's one
  page: Record first (Check in while it is a habit), its readings newest first in a fold a year, and, as a metric is
  a page (D27), its page's text, links, backlinks and actions, taken from the page's own entity. Its readings are
  `(from, to]`; with no `from`, every admitted day through `to`, on every surface (`all_history=1` says the same and
  allows no `from`), preserving scope/session and `include_deleted`. The All readings link leads a window back to the
  whole. In a browser, the page of a live metric (`/pages/{id}`) answers 303 to it, with its feedback; JSON keeps
  the page.
- **Read-only queries.** `POST /query` runs one allowed read statement (`SELECT`, `WITH`, `VALUES`, `EXPLAIN` of
  those reads, or an allowlisted introspection `PRAGMA`) on a short-lived `mode=ro`, `query_only` connection, 5 s
  and 500 rows at most; scripts, transactions, setters and maintenance commands are refused.
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with `trusted_schema=OFF`, read back the same way.
- **Unicode.** Registry name keys use `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned table,
  not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  operation that writes several rows (`readings-from-table`) runs several of the same methods in one. Nothing writes
  around them.
- **The folder of the program** ([plan 090](docs/plans/090-agentic-init.md)). Without `--db`, `LIFELOG_DB` or
  `--url`, a command uses the `life.db` beside the executable, and `lifelog init` with no path makes it there; a
  missing file is still refused. `lifelog agentic-init [--force]` writes into that folder, from templates embedded
  in the binary: `AGENTS.md` (the folder, the modes, the tools, the rules and a privacy note), a `CLAUDE.md` that
  imports it, three skills in `.agents/skills/` and `.claude/skills/`, and the MCP config of Claude Code
  (`.mcp.json`), Codex (`.codex/config.toml`), OpenCode (`opencode.json`) and Pi (`.pi/mcp.json`), each starting
  `lifelog mcp --agent NAME` on that `life.db` with absolute paths. A file with another text is kept unless
  `--force`; run it again after the folder moves. Like `init`, it is a setup command of the machine, not a catalog
  action: it writes files beside the program.
- **Owner-only actions.** `register-metric`, `relocate-reading` and `snapshot` are refused to an `agent:*` or `import:*` writer and
  are never MCP tools. An agent that imports with the owner asks for them in the conversation.
- **A conversation with lifelog** ([plan 102](docs/plans/102-a-conversation-with-lifelog.md)). `ask` is an action of
  the catalog, so it is on every surface: `lifelog ask`, the MCP tool `ask`, the form on Home and on the chat page.
  A model the owner configures answers it with the same tools as the MCP server, minus `ask` itself, as `agent:chat`
  (`internal/chat`). The model is an OpenAI chat-completions endpoint, read from the environment of the process that
  opens the file: `LIFELOG_MODEL_URL` (the base URL, ending in `/v1`), `LIFELOG_MODEL` and optional
  `LIFELOG_MODEL_KEY`; without them `ask` answers 503 and the rest is unchanged. With `--url`, the server's model
  answers. A model writes with no approval step; it asks the owner in the chat when it is not sure. At most 16 model
  steps a question and 64 KiB a tool result; the loop runs in the request and stops with it. **Rejected:** rewriting
  `lifelog` in another language to embed an agent SDK, a second application beside it, and a table of conversations
  before a question needs one ([RFC 0012](docs/rfcs/0012-a-conversation-with-lifelog.md)).
- **One continuous conversation** ([plan 106](docs/plans/106-one-continuous-conversation.md)). The owner's rule
  (2026-10-14): one conversation, not sessions. Each turn is appended to the page `Conversation YYYY-MM-DD` of the
  server's local day, filed by `part-of` in the page `Conversations` and created at the day's first question; `ask`
  takes the question alone. At the first question of a day the agent first compacts the earlier days' conversations
  not yet summed up, the oldest first and at most three a question: one model call in words, `capture` of the
  summary on that day's journal page as `agent:chat` with a `[[link]]` to the transcript, then the trailer
  `**Compacted** into [[day]]` on the transcript (the summary first, so a stale trailer never loses one). A
  compaction that fails is a `compact` step with its error, and stops the compaction until the next question; the
  question is still answered. The model's memory of the days before is the journal page of the nearest earlier day
  within seven days, given whole, then today's turns. The transcript format, the trailer included, is written and
  read in `internal/api/conversations.go` alone. **Rejected:** a timer or a goroutine for the compaction, a link
  kind or a filing page as the compacted mark (the schema, or a second home), a table of conversations.
- **The conversation on the screen** ([plan 103](docs/plans/103-the-conversation-on-the-screen.md)). `/conversations`
  is today's conversation and `/conversations/{id}` the conversation of one day, its turns read from its page; in a
  browser they are the chat page (the thread, a note that the previous day's conversation is summed up in that day's
  page, the composer on today's alone), and a conversation's `/pages/{id}` answers 303 there (`?view=page` keeps
  the page). The chat page takes the screen's height and scrolls its thread alone, so the head and the composer
  never move and the page never scrolls under the owner's hands. Every other page of the shell has the chat sidebar: a right column that the Chat button opens and
  closes, a sheet on a phone. The script fills it from the chat page's own HTML. The draft of the chat page and of
  the sidebar sends its question with `fetch`, shows it at once as sent and clears itself, and draws the thread
  again from the answer; when there is no answer the words go back into the draft. Whether the sidebar is open is
  kept in `localStorage`, and the chat page sets it open, so the conversation follows the owner off the chat page. A question still runs in its request: the script asks before a page with a waiting
  question is left.
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's `rename` action run it.
- **Readings from a table** ([plan 089](docs/plans/089-no-import-process.md)). `readings-from-table` reads the first
  table of a page that has a column of days and writes each row with a day and a plain number as a reading of a
  registered metric; the unit (in the cell, a `unit` column, or the header in `( )` or `[ ]`) must be the metric's,
  and nothing is converted. A sign (`<5`), a word, a comma decimal, another unit or a second value for one day is
  reported, not written. Its rows are written under the source `import:table` with the key
  `table|<page title_key>|<day>`, so a run from any surface finds what an earlier one wrote and writes nothing.
- **Planning on every surface** ([plan 081](docs/plans/081-planning-on-every-surface.md)). Core task operations
  implement the [planning contract](docs/contract/planning.md), and the catalog carries them: `/tasks`, a task with
  its occurrences over a window, an occurrence (written, or a virtual slot of a series), `/deadlines` across tasks;
  `create-task`, `edit-task`, `stop-task`, `capture-occurrence`, `edit-occurrence`, the tombstones and revives, and
  `tombstone-tasks`, several tasks in one write ([API.md](API.md)). `lifelog tasks` and `lifelog due` are shortcuts; the MCP tools and the browser's generic view
  carry the rest. Deadline reads use an explicit inclusive window (default 30 days back, 90 on), limited to 10,000
  persisted candidates and 10,000 returned occurrences; larger reads refuse instead of silently truncating. A
  reminder is resolved intent in the answer (`reminder.state`, `reminder.at`); no notification is sent by anything.
- **The schema is embedded.** `go generate ./...` copies `docs/schema/schema.sql` into `internal/db`; a test fails
  when the copy is stale. `lifelog init` refuses an existing file; `Open` refuses a file without Lifelog's
  `application_id`.
- **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md),
  [plan 082](docs/plans/082-snapshot-in-the-catalog.md)). `snapshot` is an owner-only action of the catalog
  (`POST /snapshots`): refused to an `agent:*` or `import:*` writer, never an MCP tool, offered at the root to the owner. It takes
  no path: the file lands in the folder `serve --snapshots DIR` names, or beside `life.db`, named by the local day
  and never overwritten; it refuses a folder inside a git work tree, and its restore check opens the snapshot with
  `Close` skipping `PRAGMA optimize`. The answer carries the path and the check. `lifelog snapshot [--to DIR]` is the
  action in-process, `--to` choosing the folder; with `--url` the server's folder is the one, and `--to` is refused.
  Restoring stays the manual procedure of the cookbook.
- **Several rows deleted at once** ([plan 107](docs/plans/107-delete-several-at-once.md)). `tombstone-pages`
  (`POST /pages/tombstone`) and `tombstone-tasks` (`POST /tasks/tombstone`) take `ids`, separated by spaces or
  commas, and tombstone them all in one write or none: a missing id is a 404 that writes nothing. The files list
  offers the first and the tasks list the second, so the browser draws a box on each row of Files and Tasks, Select
  all, and one danger button that returns to the list with the count; the CLI's `do` and the MCP tools carry them as
  any action. Both are dangers, not the owner's alone, and a revive undoes each.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` (`POST /files`, `lifelog file`) streams
  the original through SHA-256 and drops it; nothing of it is stored. `internal/preview` makes every picture itself —
  from a JPEG, PNG or GIF, or a JPEG sent for a HEIC or a video frame: scaled to 1600 px, turned upright by EXIF,
  re-encoded with no metadata (no GPS). An agent sends `sha256`, `mime` and the text. `![[Title]]` renders the
  picture; `GET /pages/{id}/preview` and `GET /previews?title=` serve it. A photo's day and position come from its EXIF
  (`internal/photo`: a JPEG's APP1, a HEIC's Exif item, read from the original's first megabyte): its day links the
  place its position is in, `at` names one, `locate` sets or moves a place's point; the position is never stored.
  `lifelog file` keeps several (paths, or a folder's photos and videos), with `--dry-run` and a report per day and
  per group of photos near no place. Unknown capture-local day follows [keep a file](docs/cookbook/keep-a-file.md).
- **`serve` is local-only unless the owner says `--public`.** Without the flag `serve` is what it was: `life.db`
  holds health data, and the API is for this machine. The default bind is `127.0.0.1:7777`; `--addr` accepts numeric
  loopback IPs or `localhost` with a numeric port (0 selects an ephemeral port, printed at startup), and nothing else.
  `localhost` binds to `127.0.0.1` without DNS resolution. Network requests must name a loopback IP or ASCII
  case-insensitive `localhost` at the actual listener port, with brackets for IPv6. An omitted HTTP port means 80, not
  the listener's port. Custom hostnames, service-name ports and proxy deployments are unsupported. Forwarded headers
  confer no trust; absolute-form request targets are refused. The network guard protects reads, previews and writes;
  in-process CLI/MCP dispatch is unchanged. Browser-origin protection inside it rejects cross-origin writes while
  keeping same-origin forms and non-browser clients that send no browser origin headers working. `--token-file`,
  `--allow-origin`, `--tls-cert` and `--tls-key` are refused without `--public`, never ignored.
- **`--public` opens the listener to other devices** ([plan 078](docs/plans/078-network-server.md)) and requires a
  **token**: `--token-file PATH` (one line of at least 32 characters; the command line never carries the secret) or
  `LIFELOG_TOKEN`; `--public` without one is refused before the listener opens. The default bind becomes `0.0.0.0:7777`
  and `--addr` may name any numeric IP of this machine. Every request must carry `Authorization: Bearer <token>` or
  the cookie `/login` sets; one without is answered 401 with nothing of the database, and a browser is sent to
  `/login`. The `Host` check is dropped: it kept an unauthenticated local server from a DNS-rebinding page, and the
  token defeats that page on its own; absolute-form targets stay refused. A token holder is the owner: owner-only
  actions are still refused by `Lifelog-Source: agent:*` or `import:*`, not by the token. **Origins:** `--allow-origin ORIGIN`
  (repeatable, exact `scheme://host[:port]`) answers CORS preflights, adds the CORS headers for that origin and makes
  it a trusted origin for browser writes; any other origin is refused as before. **TLS:** `--tls-cert` and `--tls-key`
  serve HTTPS with the standard library; without them the token travels in clear, which is acceptable only on a
  private network (a VPN, a mesh such as Tailscale), and the startup line says so. A reverse proxy that terminates
  TLS works only because it forwards the bearer header unchanged. **The browser:** `GET /login` is the one page served
  without a credential; `POST /login` with the token sets `lifelog_token` (HttpOnly, SameSite=Strict, Secure over
  TLS) and `POST /logout` clears it; cross-site forms with the cookie are what the origin guard refuses. The CLI and
  the MCP server with `--url` send `LIFELOG_TOKEN`.

## Layout

| path | what |
|---|---|
| `cmd/lifelog` | the binary: `init`, `serve`, `mcp`, `get`, `do`, `actions` and the shortcuts (`file`, `snapshot` among them) |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema |
| `internal/text` | the title predicate, `title_key`, wikilink and `#tag` extraction; tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) |
| `internal/core` | the cookbook's writes and reads; the save contract; habits; renames; files |
| `internal/preview` | the picture a file page keeps: decode, scale to 1600 px, EXIF orientation, a JPEG of at most 1 MB with no metadata |
| `internal/photo` | what a photo's metadata says: the day and time taken, the position, the orientation (JPEG and HEIC); `phototest` builds synthetic ones |
| `internal/api` | the action catalog, the routes, Siren and HTML; the local and the `--public` network guards |
| `internal/client` | the hypermedia client (in-process or remote) |
| `internal/mcp` | the catalog as MCP tools (the tool list is `internal/client`'s, shared with `internal/chat`) |
| `internal/chat` | the agent of `ask`: the model client, the loop over the catalog's tools, the conversation page |
| `internal/agentic` | `lifelog agentic-init`: the texts and MCP configs for a coding agent opened in the folder of the program |
| `internal/scaletest` | the synthetic personas and the lifetime workload of `tools/lifescale`: generators, an oracle and the timed checks |
| `tools/copyschema` | `go generate`'s copy of `docs/schema/schema.sql` into `internal/db` |
| `tools/lifescale` | the lifetime and stress runner over `internal/scaletest` ([tests/README.md](tests/README.md)) |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) |

## Checks

`go generate ./... && go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema, the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.

### Request metadata limits

Action JSON and URL-encoded bodies are limited to 20 MiB of encoded data; JSON
must contain exactly one object. Multipart uploads allow up to 64 parts, 16 MiB
per text field and 16 MiB total text (including unknown or repeated fields),
and at most one original and one preview part. A supplied preview may be up to
64 MiB. Over-limit requests return HTTP 413 without writing. Originals are
hashed as streams without a total size limit; originals above the 64 MiB picture
budget are kept without an automatically generated preview.
