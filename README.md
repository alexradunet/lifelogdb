# Lifelog

`lifelog` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people and places in them, where you were, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. A plan is words in a page. It serves the file three ways: a hypermedia API (with an HTML face for
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
lifelog snapshot --to ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg   # a photo lifelog cannot read: send a JPEG of it
lifelog cover 12 lake.jpg                  # a new photo as page 12's cover; `lifelog cover 12 --title Lake.jpg` takes a kept one
```

To run `lifelog serve` as a system service with a nightly snapshot, see [deploy/](deploy/README.md): the unit files
and the snapshot script, as a reference to compare with the installed copies before you replace them.

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
- **Server shutdown drains admitted requests.** An interrupt or a SIGTERM closes the listener, then allows ten seconds for active
  requests to finish before closing their connections. Shutdown completes before the database closes; expiration
  is reported as an error.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled (a page's `save-body` carries its body and `version`). `GET /actions` is
  the catalog; the CLI's `do` and the MCP tools are generated from it, so a new action needs no client change.
  An error is an entity too: `status`, a `code` a client branches on (`stale_version`, `exists`, `not_found`, `snapshot_refused`, `restore_check_failed`,
  `invalid`, `cross_origin`, `token_required`, or the status's general name) and the message. A fault of the server (500
  `internal`) says only "internal error": the server's log has the cause. The lists `/days`,
  `/browse` and `/search` take `limit` (1–500) and `offset` and link `next` and `prev`
  ([plan 079](docs/plans/079-client-contract-hygiene.md)).
- **The browser gets the same entity.** `Accept: text/html` renders it with a template per class (day, page,
  person, place, metric, search, lists; `internal/api/view/html/`, embedded) or a generic one for any other. A
  template reads only the properties the JSON carries and places only the actions the entity offers, so a form
  shows where the action is legal and nowhere else. A body is CommonMark with goldmark's safe defaults (raw HTML
  omitted), its `[[wikilinks]]` and `#tags` linked. A browser form posts and is answered 303 to the resource it
  changed, so a reload never posts twice; a refused one shows the text it sent. A form may name the page
  to return to in `back` (a path on this server, not a field of any action): a Done button on a list returns to the
  list. A value that is not a plain path is ignored, and a JSON client never follows it
  ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). A form with an id sends it in `form` (not a field of any
  action either), so a page that draws one action in several forms (New metric) shows a refusal in the form that
  was sent and in no other.
- **The page is semantic HTML, and the catalog says how to draw it.** A field is a `label` around its input (a flag,
  the options 0 and 1 in a field that the action does not require, one checkbox that sends 1 or nothing; a field of 2
  to 5 short options a `fieldset` of radio buttons; a `pages` field with `options` a text input with a `datalist` of
  those titles, so the reader can also type a new title), a card is an `article`, a callout an `aside`, an error a
  `role="alert"`, a series a `table` with a header row, a write's result and the search an open or a modal `dialog`,
  the phone's sheet a popover; a field the browser refuses once the owner has typed in it (`:user-invalid`) shows the
  same danger edge as one the server refused (`aria-invalid`), and the select is the app's own where the browser lets
  a sheet draw it (`appearance: base-select`); no id or class is generated for a template to find, but the id of a
  `datalist`, which the `list` of its input names (the form's id, or else the action's name, then the field's name).
  What a template cannot know comes from the entity: `danger` marks a destructive action's button, `owner` an
  owner-only one, `rows` a textarea's height, `options` a field's choices, `pages` a field whose value is a page's
  title and an error's `code` its first words, so no view decides any of them by an action's or a field's name.
- **The browser for daily use** ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). The root is today at a
  glance: the five newest day pages. A day carries its mood and the `save-body` of its day page, so the day view
  edits in place. These are properties and actions of the entities, so every client gets them ([API.md](API.md)). `/months/{YYYY-MM}` is the calendar of a month (each day, whether it has a
  page, its first line, its mood and its readings, and every other thing that names the day: a
  birthday or a death each year, a place, a page or a file; under the calendar, Day by
  day links each one: [plan 101](docs/plans/101-the-calendar-holds-every-dated-thing.md)),
  and the catalog action `open-day` goes to a day by its date, or to today with no date. Every field of the catalog
  whose type is `date` reads the word `today` as the server's local day. The handlers read today from one clock,
  which a test fixes. The day and the root carry `on_this_day`, the
  day pages of the same date in earlier years, which the margin shows. `/weeks/{YYYY-Www}` is the week (its days, its
  readings against the week before) and `/years/{YYYY}` the year (twelve months);
  People and Places list each name with the pages that mention it and the day of the last one
  ([plan 097](docs/plans/097-the-almanac-reflects.md)). A person's row starts with a round picture, its cover,
  else the icon of a person.
- **A cover for every page** ([plan 127](docs/plans/127-a-cover-for-every-page.md)). A page's cover is the first live
  file with a preview that its body embeds (`![[Ana.jpg]]`); a day page's first photo is its cover with no action. A
  file page has none: its own picture stands for it. The browser draws the cover as a square at the head of the
  margin of the page and the day (first on a phone), with the other pictures of the body under it, each one press
  from being the cover (it trades places with the cover's embed), and leaves that embed out of the body; a Browse row
  starts with it as a small square (a person's is round), and the row's `picture` gives its href. The calendar draws
  a day's cover behind its square cell, under a veil of the parchment that keeps the words readable. The schema has no link for it, by the owner's choice (2026-10-15, again
  2026-10-17): an embed is how any page shows a picture, so every surface can set it. `set-cover` sets it: a new
  photo (the browser, and `lifelog cover PAGE-ID PHOTO`) or a kept picture by its title (`lifelog cover PAGE-ID
  --title T`, and an agent, whose MCP tools carry no file).
  `GET /pages/{id}/cover` is the picker: the kept pictures as tiles, each a form of its title, and the form of a new
  photo. The new photo goes in no day page.
- **The app shell: the Almanac design system** ([plan 092](docs/plans/092-the-app-shell.md)). Every page a browser
  gets is in one shell: a sidebar with the brand and today's date, the search, New, the sections of every day (Today,
  Calendar, Metrics and Browse), this month with its written days, and the owner's links. Under 900 px it is a phone's layout
  ([plan 093](docs/plans/093-words-phone-and-today.md)): a top bar (the brand, the date, Search, New), a bottom bar of
  the sections of every day and More, and the rest as a sheet over the page (`#sections`): a popover that More
  opens and Close, Escape or a tap outside closes, with no script. Inside the page a layout asks the width of its column (`@container column`), not of the
  screen, so a day beside an open sheet lays out as in a narrow window; the title, the leaf and the sheet's padding
  scale with the column by `clamp()`, with no step at a breakpoint
  ([plan 108](docs/plans/108-the-browser-made-current.md)). The sidebar is the one part of a page that is not the
  page's entity: once a page, and for a browser only, the shell reads this
  month (`GET /months/{month}`), which every client gets from that resource. A page drawn before the handler
  (the login, a refusal of the token) has no sidebar, so it shows nothing of the database. The look is the Almanac
  bound in Hearthwood, the materials of the owner's Balaur design system
  ([plan 096](docs/plans/096-bound-in-hearthwood.md)): the page is dark oak, the sidebar and the phone's bars are
  wood, and everything written is parchment with ink; square corners, 2 px edges, bevels and 3 px hard drops, no
  blur and no gradient; gold marks where the owner is and what is today, ember is the one loud button of a screen,
  teal is a link. Words are in Atkinson Hyperlegible, every day, time and number in IBM Plex Mono,
  and a page's title, the leaf's number and a reading's value in Jersey 15; dark is the identity and light the
  daylight study of the same room, by `prefers-color-scheme`. Its tokens are custom properties of `base.css`; a
  material sets its own text through aliases (`--text`, `--text-link`, …), and a test holds every text pair at
  4.5:1 and every edge and the focus ring at 3:1 in both themes. The motions, all off under reduced motion: a press
  sinks; the step to the previous or next day moves the leaf and cross-fades the sheet through a cross-document view
  transition ([plan 097](docs/plans/097-the-almanac-reflects.md)); the search dialog and the phone's sheet
  enter and leave by `@starting-style` and a discrete transition; the phone's top bar takes its drop once the
  page has scrolled under it, by a scroll-driven animation
  ([plan 108](docs/plans/108-the-browser-made-current.md)).

  Do and don't:
  - Show one loud button a screen.
  - Show no streak, no chain and no badge for consistency.
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
  panels, sub-entities rows, an action with no visible field a button, one with
  fields a button that opens its form, an error an alert, and a write's `result` a toast at the top of the page,
  which stays until the next page because it may name what the write did not do. A day and Home are a calendar
  leaf, a sheet and its margin ([plan 095](docs/plans/095-the-leaf-and-its-margin.md)): the leaf (the month on a
  band, the day's number in mono) heads the page; the sheet holds what the page is for (the journal, then Capture);
  the margin holds what the page asks of the owner (the readings, the places), and comes after the
  sheet on a narrow screen. A page and a list use the same sheet. One button a screen is filled:
  the main write. A view can fold an
  action's optional fields under "N more fields", inside the form, so a browser sends them with no script.
  A value is drawn by its type, never by its property's name ([plan 093](docs/plans/093-words-phone-and-today.md)):
  a day in words near today ("yesterday · Sun 11 Oct"), an instant in the clock of the server's day ("today, 09:48",
  the exact instant kept in its `<time>` element), an object of the generic view as facts, and a write's result in
  sentences by the result shapes of API.md. The day carries `trends` (each metric read that day, its unit and its
  readings of the 14 days through it), so a reading's tile has its unit and its line.
  `GET /styleguide` is the style page: every component in every state, with the file each lives in, drawn by the real
  templates on synthetic words. It is presentation, like `/render`: it is not in the catalog and it reads nothing
  from the database.
  `GET /manifest.webmanifest` and `GET /assets/icon.svg` make the app installable: two shortcuts (Today, Add to
  today) and a share target that posts shared text into Capture
  (`POST /days/today/capture`). There is no service worker, since the server is the owner's own. A public listener
  refuses all three without the token, as it refuses the fonts.
- **No script** ([plan 108](docs/plans/108-the-browser-made-current.md); the owner's rule of 2026-10-14: as native
  as HTML can be, even at the cost of a small convenience). A page carries its two sheets and no `script` element,
  and a test holds it. What the one script once did ([plan 091](docs/plans/091-the-browser-for-daily-use.md),
  [plan 094](docs/plans/094-the-command-list.md)), the browser does by itself: the **search dialog** is a `dialog`
  the server draws on every page of the shell, with the search form inside, which the top bar's Search opens by its
  command (`commandfor`) and Escape or a tap outside closes (`closedby="any"`); `/search` lists the pages whose title
  starts with the words before the pages whose words match, so a page is one search away by its title. The phone's
  sheet of sections is a popover; the toast is an open `dialog` in the flow of the page, which Escape and its own
  Close close. The keys are the browser's **access keys**, with its own modifier (Alt in Chromium, Alt+Shift in
  Firefox, Ctrl+Alt on a Mac): `k` the search, `n` New, `t` Today, `c` the capture, `[` and `]` the previous and
  next page; the footer and the dialog say so. A form that writes a body offers **Preview**, a second button that
  sends it to `POST /render` in a new tab (`formaction`, `formtarget`), where the body shows as its page will show
  it, and the form keeps its words. The list that deletes several rows counts the checked boxes on its button by a
  CSS counter. **Dropped, by the owner's word:** the names after `[[`, the live preview as one types, Ctrl+Enter,
  Escape to leave a field, the question before the browser leaves text not sent, the second press of a danger
  button (a boxed danger already takes two presses, and a tombstone has Restore), and the command list's ranking of
  this page's actions.
  `POST /render` is presentation, not a capability: no catalog action names it and it answers HTML only, so it, the
  fonts, the style page, the manifest, the icon, the world outline and the pictures (`GET /pages/{id}/preview`, `GET /previews`) are the
  routes the CLI and the MCP server do not carry. **Rejected:** a separate web client. It would be a second
  implementation of every view to keep equal to the catalog, it would hold the token where JavaScript can read it,
  and it would bring a toolchain that changes every year.
- **The stylesheet is this application's own: two embedded sheets, three embedded faces, no vendored framework and
  no CDN.** `base.css` holds the tokens and styles ordinary HTML by element — type, colour, forms, tables, buttons, a
  print sheet, light and dark — and `app.css` only what this app's own vocabulary needs (the shell and its
  components, `.wikilink`, `.chart`); a test keeps the app's names out of the base sheet. The two sheets are one
  cascade in four layers, `tokens`, `base`, `components` and `utilities`, declared once at the top of `base.css`
  and shared by the two `style` elements, and every rule is in logical properties; a test holds both
  ([plan 108](docs/plans/108-the-browser-made-current.md)). The three faces are open
  fonts (SIL OFL 1.1), latin and latin-ext, embedded with their licences and served by exact name at
  `GET /assets/fonts/{file}` with a cache of a year; like `/render`, the route is presentation and not in the catalog.
  Nothing is fetched from another host, so a page looks the same offline in fifty years. **Rejected:** a vendored classless sheet (Simple.css, Pico). Its
  `aside` is a floated sidebar and ours is a callout in flow, its `section` a bordered band, its `h2` 2.6rem where
  ours labels a section, its tables bordered and zebra-striped where ours are dense, its form controls carry blanket
  margins where ours sit two to a row, and its `body` is a 45rem grid: keeping one meant overriding most of it, and
  re-pinning a file by hand at every upstream change.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (a browser form) or `agent:<name>` (MCP);
  the `Lifelog-Source` header sets it.
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one is a 409.
- **One page a metric** ([plan 100](docs/plans/100-one-page-a-metric.md)). `/metrics/{name}` is the metric's one
  page: Record first, its readings newest first in a fold a year, and, as a metric is
  a page (D27), its page's text, links, backlinks and actions, taken from the page's own entity. Its readings are
  `(from, to]`; with no `from`, every admitted day through `to`, on every surface (`all_history=1` says the same and
  allows no `from`). The All readings link leads a window back to the
  whole. In a browser, the page of a live metric (`/pages/{id}`) answers 303 to it, with its feedback; JSON keeps
  the page.
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with `trusted_schema=OFF`, read back the same way.
- **Unicode.** Registry name keys use `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned table,
  not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  operation that writes several rows (`file-pages`) runs several of the same methods in one. Nothing writes
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
- **Owner-only actions.** `register-metric`, `snapshot`, `integrity-check`, `rebuild-search` and `resync-links` are
  refused to an `agent:*` or `import:*` writer and are never MCP tools. The full-text check of `integrity-check` holds the write
  lock while it runs, so an agent must not start it. `rebuild-search` (`POST /search/rebuild`) is the repair of the
  full-text index, which the check finds: the FTS5 `'rebuild'` in one write. `resync-links` (`POST /links/resync`)
  rewrites the links of every live page whose links differ from its body (`link_drift`), by the code of a body save
  ([integrity checks](docs/contract/integrity-checks.md)). An agent that imports with the owner asks for them in the conversation.
- **No conversation inside the writer** ([plan 116](docs/plans/116-strip-to-the-core.md), [RFC 0015](docs/rfcs/0015-no-conversation.md)).
  The owner's decision (2026-10-15): `ask`, the Conversations pages, the chat sidebar and the voice note are
  removed, with the model and transcriber endpoints they needed. The owner talks to `life.db` through a coding
  agent's MCP tools, and a recording is kept as a file with its text ([D9](docs/decisions/D09-binary-files.md)).
  The conversation pages already written stay plain pages, filed in `Conversations`.
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's `rename` action run it.
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
  (`POST /pages/tombstone`) takes `ids`, separated by spaces or
  commas, and tombstones them all in one write or none: a missing id is a 404 that writes nothing. Browse offers it
  on pages (the ghost pages too), media and documents, so the browser draws a box on each row or tile and one
  danger button, which counts the checked boxes (a CSS counter, no script) and returns to the list with the count;
  the CLI's `do` and the MCP tools carry it as any action. It is a danger, not the owner's alone, and a revive undoes it.
- **Metrics as cards, filed in groups** ([plan 112](docs/plans/112-metrics-as-cards.md)). `/metrics` draws each
  group as a grid of cards, with a jump bar to the groups: a card is a metric's latest value, its day and the line
  of its newest 30 readings, muted when the latest is older than 60 days. Every card has the same four lines, so the
  cards of a row are of one height: the name, the value (a dash and the unit with no reading), the line (a dashed
  rule with none), and a foot with the day in short words and a quiet + Add; the whole day and the count of readings
  are the day's title ([plan 120](docs/plans/120-a-tidy-board.md)). `file-pages` (`POST /pages/file`) files several pages in a category page in one write, made when it is
  missing; `move` 1 takes them out of their other categories. The page's pick bar checks the cards to file. The CLI's
  `do` and the MCP tools carry it as any action; it is not the owner's alone, as `link` is not.
- **Browse** ([plan 117](docs/plans/117-one-browse-view.md)). A category is a page, and anything is filed in it by
  `part-of` (D26); the owner's word (2026-10-15) is "category" everywhere, a thing may be in several, and a category
  shows everything filed in it, whatever its type. `GET /browse` (the catalog action `browse`) is one list with two
  filters: the kind of its rows (Pages, People, Places, Media, Documents, Metrics, each chip with its count; All
  inside a category, where it is the first chip and the default) and where it looks (Everything, what is filed
  directly in a category, or Not filed, which follows the kind). The category tree, by path with a count each, is
  beside the list; on a phone it is a closed fold above the chips. Each kind keeps its row and its order: a person
  has a face, the mentions and the last day; media are tiles, newest day first, with the picture a file keeps or the
  icon of its kind, one media kind by `media=`, and the count of each media kind in words; documents are the other
  files, as rows. The ghost pages are a filter of Pages. The pick bar puts the checked rows in a category, and
  deletes pages, media and documents. Metrics keeps its own board at `/metrics`. Days are not in Browse: a day is the time axis, found in the Calendar and by
  Search, and it meets the things through its mentions (the owner's word, 2026-10-15); a day page is never filed
  ([D26](docs/decisions/D26-metric-categories.md)), so a day page's `link` does not offer `part-of`. The sidebar has Today, Calendar,
  Metrics and Browse; a phone's bottom bar is the same four with More. The old lists (`/people`, `/places`,
  `/media`, `/files`, `/ghosts`, `/categories`) answer 308 to their Browse address, with `limit`, `offset` and
  `new` kept; the POST routes of those paths do not change. A category page has one section, Filed here: the
  sub-categories first, then the rest by title, a type badge on what is not a page, and Open in Browse; its
  margin's Backlinks does not list `part-of`. `create-page`, `create-person`, `create-place` and `add-file` take an
  optional `category`: the thing is made and filed there in one write, a missing category made as `file-pages`
  makes it, and a day as the category refuses the whole write (`entities_day_page` when the day has no page,
  `links_endpoint_types` when it has one). Browse in a category fills it in each New form, and a category page
  offers New page here. `add-file` files the page of the same original when it exists, unless that page is deleted.
  Rejected by the owner: exclusive membership, a manual order and a syntax in the body that files a page.
- **Fold and find** ([plan 118](docs/plans/118-fold-and-find.md); the owner's choice, 2026-10-15). In the browser
  only, and with no script, Browse and the Metrics board fold; the JSON and the catalog do not change. The tree folds
  each category that has categories under it, open on the path to the current branch. On Pages in Everything the top
  categories are folder tiles before the list, and they are not rows of it; inside a category, the categories under
  it are the folders. A list in title order with more than 30 rows is in letter groups with a letter bar; with more
  than 100 rows the groups are closed and are the index, a grid of letter slabs with no bar
  ([plan 119](docs/plans/119-the-finish.md)). The Metrics board folds each group and each category under it, and the
  line of a fold says its count and the day of its newest reading; the link to the category and + New metric here
  are the foot of the fold. `/metrics?view=list` draws one row a metric in place of the
  cards; `view` is a word of the page, as `new` is, and a form of the page returns to the same view.
- **One way to make a thing** ([plan 114](docs/plans/114-one-way-to-do-a-thing.md)). The rail's New is a menu of the
  kinds; each kind is made on its list of Browse, whose header has its New button (a file on the media list, a
  metric on `/metrics`; a page also on Home, beside Open a page).
  `?new=<action>` opens that action's form on any page, and a query parameter that names a field fills it in. The
  catalog's titles say one verb for one meaning: New makes a thing, Add adds to one, Remove takes away, Restore
  brings back. The action names do not change. A number card and a day's reading tile have + Add, one value for
  that day. A page and a metric show their categories as badges, each with x, and + Add (`file-pages` with this
  page). The lists of Browse and the Metrics board act on several things the same way: Select shows the boxes, and one bar holds
  the count and a button for each action the list offers.
- **New metric, new category and a reading from `/metrics`** ([plan 113](docs/plans/113-new-metric-on-metrics.md)).
  New metric is in the header of `/metrics` and under each category's heading, not on Home. It asks what you
  record: a number with its unit, a score with its range, or done or not done (a 0/1 habit). Its category is
  made when it is new. `register-metric` takes `category`, and does both in one write. New
  category opens the file bar. Each number card has a +, which records one value for today.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` (`POST /files`, `lifelog file`) streams
  the original through SHA-256 and drops it; nothing of it is stored. `internal/preview` makes every picture itself —
  from a JPEG, PNG or GIF, or a JPEG sent for a HEIC or a video frame: scaled to 1600 px, turned upright by EXIF,
  re-encoded with no metadata (no GPS). An agent sends `sha256`, `mime` and the text. `![[Title]]` renders the
  picture; `GET /pages/{id}/preview` and `GET /previews?title=` serve it. A photo's day comes from its EXIF
  (`internal/photo`: a JPEG's APP1, a HEIC's Exif item, read from the original's first megabyte); its position is
  never read ([D21](docs/decisions/D21-location-history.md), [plan 116](docs/plans/116-strip-to-the-core.md)).
  `lifelog file` keeps one file, with `--dry-run`, and `--category` files it in the same write. Unknown capture-local day follows [keep a file](docs/cookbook/keep-a-file.md).
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
  and `--addr` may name any numeric IP of this machine, in brackets only when it is IPv6. Every request must carry `Authorization: Bearer <token>` or
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

| path | what | test it at |
|---|---|---|
| `cmd/lifelog` | the binary: `init`, `serve`, `mcp`, `get`, `do`, `actions` and the shortcuts (`file`, `snapshot` among them) | the parity table `TestParityOfTheSurfaces` for a surface; `runArgs` with `testEnv` for a command line in process; `TestExecutableServeDrainsActiveRequest` for the process |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema; `dbtest` makes the fresh file of a test | `db.Init` and `db.Open` on a file in `t.TempDir()` |
| `internal/text` | the title predicate, `title_key`, wikilink and `#tag` extraction, the parse of a day and an instant, the markers that a plain-text line cuts (`BlockMark`) and the marks of a match in a snippet of the search (`SnippetOpen`, `SnippetClose`); tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) | the pure functions with table tests, and the vectors of the contract page (`TestWikilinkVectors`, `TestTitleKeyVectors`) |
| `internal/core` | the cookbook's writes and reads; the save contract; metrics; renames; files | `core.Store` on `dbtest.New`, with a fixed `Clock` for a test of today |
| `internal/preview` | the picture a file page keeps: decode, scale to 1600 px, EXIF orientation, a JPEG of at most 1 MB with no metadata | `preview.Make` and `preview.Check` on pictures that the test makes |
| `internal/photo` | what a photo's metadata says: the day and time taken and the orientation (JPEG and HEIC); `phototest` builds synthetic ones | `photo.Read` on the photos of `phototest`; `FuzzRead` |
| `internal/api` | the action catalog, the routes, Siren and HTML; the local and the `--public` network guards; `siren` holds the Siren types, the `Lifelog-Source` header, the JSON writer and the addresses of the resources, which the clients import too; `view` draws an entity as a page (the templates, the sheets and the fonts are in its `html/`) and imports no store; `apitest` makes a fresh handler and its clients for a test | `api.New` on a store, through `apitest.Fresh`, or `apitest.FreshOn` for a store with a fixed clock; a test inside package `api` cannot import `apitest` (an import cycle), so it makes its store with `dbtest.New`; the pure functions of the handler (`backOf`, `errorEntity`, `browseQueryOf`) with table tests; `view.Draw` and the pure functions of `view` with table tests |
| `internal/client` | the hypermedia client, transport only: in-process or remote; of this module it imports only `siren`, so it builds no `core`, no `db` and no SQLite | `client.InProcess` and `client.Remote` on one handler: `TestTheTwoTransportsGiveEqualAnswers` |
| `internal/mcp` | the catalog as MCP tools: the tool list, the arguments of a model and the call of a tool; it imports `client` and `siren`, and no `api` | `mcp.New` on a client of `apitest.Fresh`; the columns `mcp` and `mcp-remote` of the parity table |
| `internal/agentic` | `lifelog agentic-init`: the texts and MCP configs for a coding agent opened in the folder of the program | `agentic.Files` and `agentic.Init` on the `Paths` of a temporary folder |
| `internal/scaletest` | the synthetic personas and the lifetime workload of `tools/lifescale`: generators, an oracle and the timed checks | `scaletest.Run` with the profile `small`, in the baseline |
| `tools/copyschema` | `go generate`'s copy of `docs/schema/schema.sql` into `internal/db` | `TestSchemaCopyIsCurrent` (`internal/db`) |
| `tools/lifescale` | the lifetime and stress runner over `internal/scaletest` ([tests/README.md](tests/README.md)) | `internal/scaletest`: the runner has no test of its own |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) | the contract suites `TestSuites` and the mutants `TestMutants`, on the DDL of `docs/` |

## Checks

`go generate ./... && go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema, the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.

### Request metadata limits

Action JSON and URL-encoded bodies are limited to 20 MiB of encoded data; JSON
must contain exactly one object. Multipart uploads allow up to 64 parts, 16 MiB
per text field and 16 MiB total text (including unknown or repeated fields),
and at most one original and one preview part. A supplied preview may be up to
64 MiB. Over-limit requests return HTTP 413 without writing; after a JSON or
URL-encoded body over the limit, the server also closes an HTTP/1.1 connection (over
HTTP/2 it resets the stream, and the connection stays open). Originals are hashed as streams without a total size limit; originals above the 64 MiB picture
budget are kept without an automatically generated preview.

## Engineering references

These primary sources inform the application policies of [AGENTS.md](AGENTS.md); they do not override this
repository's database contract or turn optional techniques into universal Go rules.

- Go: [review comments](https://go.dev/wiki/CodeReviewComments), [module layout](https://go.dev/doc/modules/layout),
  [error wrapping and API boundaries](https://go.dev/blog/go1.13-errors), [context](https://pkg.go.dev/context).
- Testing: [test review comments](https://go.dev/wiki/TestComments), [testing APIs](https://pkg.go.dev/testing),
  [fuzzing](https://go.dev/doc/security/fuzz/), [synctest](https://pkg.go.dev/testing/synctest),
  [race detector and prerequisites](https://go.dev/doc/articles/race_detector).
- Tools: [Staticcheck](https://staticcheck.dev/docs/),
  [govulncheck and its limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
- Measurement: [benchmark loops](https://go.dev/blog/testing-b-loop),
  [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), [profiling](https://go.dev/doc/diagnostics),
  [SQLite performance methodology](https://sqlite.org/cpu.html), [query-plan caveats](https://sqlite.org/eqp.html).
- Model-based testing: [stateful reference-model example](https://hypothesis.readthedocs.io/en/latest/stateful.html)
  (the technique, not a Python dependency).
