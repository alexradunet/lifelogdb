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
lifelog init --db ~/life/life.db
export LIFELOG_DB=~/life/life.db           # or --db on any command

lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog day --human
lifelog serve                              # http://127.0.0.1:7777 — open it in a browser; this machine only
lifelog serve --public --token-file ~/life/token   # for your other devices: every request needs the token
lifelog mcp --agent lmstudio               # MCP on stdio; rows are written as agent:lmstudio
lifelog habits --human                     # today's habits; lifelog done evening_walk
lifelog snapshot --to ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg   # a photo lifelog cannot read: send a JPEG of it
lifelog file IMG_0003.jpg --at Lakeside --radius 300   # a photo near no known place: name the place
lifelog file ~/picks/2019-06 --dry-run --human   # the few photos chosen for some days: what keeping them would do
lifelog file ~/picks/2019-06 --human             # keep them: their days linked, the ones near no place grouped
```

An import is done by a program that loads rows itself ([imports](docs/contract/imports.md)), or by a local agent
with the owner, in a conversation: the agent reads a source with its own tools and writes through the catalog's MCP
tools, and the owner says yes or no to each person, place and link ([plan 089](docs/plans/089-no-import-process.md)).

```
lifelog snapshot                                   # the owner, before an import session
lifelog mcp --agent pi                             # the agent's tools; its rows are written as agent:pi
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
  An error is an entity too: `status`, a `code` a client branches on (`stale_version`, `exists`, `not_found`,
  `invalid`, `cross_origin`, `token_required`, or the status's general name) and the message. The lists `/days`,
  `/people`, `/places`, `/files`, `/ghosts` and `/search` take `limit` (1–500) and `offset` and link `next` and
  `prev` ([plan 079](docs/plans/079-client-contract-hygiene.md)).
- **The browser gets the same entity.** `Accept: text/html` renders it with a template per class (day, page,
  person, place, metric, habits, search, lists; `internal/api/html/`, embedded) or a generic one for any other. A
  template reads only the properties the JSON carries and places only the actions the entity offers, so a form
  shows where the action is legal and nowhere else. A body is CommonMark with goldmark's safe defaults (raw HTML
  omitted), its `[[wikilinks]]` and `#tags` linked. No JavaScript: a browser form posts and is answered 303 to the
  resource it changed, so a reload never posts twice; a refused one shows the text it sent.
- **The page is semantic HTML, and the catalog says how to draw it.** A field is a `label` around its input, a card
  is an `article`, a callout an `aside`, a series a `table` with a header row; no id or class is generated for a
  template to find. What a template cannot know comes from the entity: `danger` marks a destructive action's button
  and `rows` a textarea's height, so no view decides either by an action's or a field's name.
- **The stylesheet is this application's own: two embedded sheets, no vendored framework and no CDN.** `base.css`
  styles ordinary HTML by element — type, colour, forms, tables, buttons, a print sheet, light and dark by
  `prefers-color-scheme` — and `app.css` only what this app's own vocabulary needs (`.wikilink`, `.badge`, `.chart`,
  the dense rows); a test keeps the app's names out of the base sheet, and nothing is fetched at render time, so a
  page looks the same offline in fifty years. **Rejected:** a vendored classless sheet (Simple.css, Pico). Its
  `aside` is a floated sidebar and ours is a callout in flow, its `section` a bordered band, its `h2` 2.6rem where
  ours labels a section, its tables bordered and zebra-striped where ours are dense, its form controls carry blanket
  margins where ours sit two to a row, and its `body` is a 45rem grid: keeping one meant overriding most of it, and
  re-pinning a file by hand at every upstream change.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (a browser form) or `agent:<name>` (MCP);
  the `Lifelog-Source` header sets it.
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one is a 409.
- **Metric ranges.** `/metrics/{name}` selects `(from, to]`; omitted `from` defaults to 90 days before `to`.
  `all_history=1` selects all admitted days through `to`, with no `from` allowed, preserving scope/session and
  `include_deleted`. The All readings link uses this mode. A default window reaching before year0000 uses
  the same all-history mode rather than inventing a negative-year bound.
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
- **Owner-only actions.** `register-metric`, `relocate-reading` and `snapshot` are refused to an `agent:*` writer and
  are never MCP tools. An agent that imports with the owner asks for them in the conversation.
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
  `create-task`, `edit-task`, `stop-task`, `capture-occurrence`, `edit-occurrence`, the tombstones and revives
  ([API.md](API.md)). `lifelog tasks` and `lifelog due` are shortcuts; the MCP tools and the browser's generic view
  carry the rest. Deadline reads use an explicit inclusive window (default 30 days back, 90 on), limited to 10,000
  persisted candidates and 10,000 returned occurrences; larger reads refuse instead of silently truncating. A
  reminder is resolved intent in the answer (`reminder.state`, `reminder.at`); no notification is sent by anything.
- **The schema is embedded.** `go generate ./...` copies `docs/schema/schema.sql` into `internal/db`; a test fails
  when the copy is stale. `lifelog init` refuses an existing file; `Open` refuses a file without Lifelog's
  `application_id`.
- **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md),
  [plan 082](docs/plans/082-snapshot-in-the-catalog.md)). `snapshot` is an owner-only action of the catalog
  (`POST /snapshots`): refused to an `agent:*` writer, never an MCP tool, offered at the root to the owner. It takes
  no path: the file lands in the folder `serve --snapshots DIR` names, or beside `life.db`, named by the local day
  and never overwritten; it refuses a folder inside a git work tree, and its restore check opens the snapshot with
  `Close` skipping `PRAGMA optimize`. The answer carries the path and the check. `lifelog snapshot [--to DIR]` is the
  action in-process, `--to` choosing the folder; with `--url` the server's folder is the one, and `--to` is refused.
  Restoring stays the manual procedure of the cookbook.
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
  actions are still refused by `Lifelog-Source: agent:*`, not by the token. **Origins:** `--allow-origin ORIGIN`
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
| `internal/mcp` | the catalog as MCP tools |
| `tools/copyschema` | `go generate`'s copy of `docs/schema/schema.sql` into `internal/db` |
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
