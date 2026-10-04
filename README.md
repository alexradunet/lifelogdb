# Lifelog

`lifelog` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people and places in them, where you were, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. It serves the file three ways: a hypermedia API (with an HTML face for the browser), a CLI and an MCP server.

The database it writes is specified independently of it, so another program — in any language — can read
`life.db`, or write its own:

- **[docs/](docs/README.md)** — the contract of `life.db`: goals, the canonical [schema.sql](docs/schema/schema.sql),
  the storage contract, the decision log, the query cookbook, research, and the process (issues, proposals, plans).
  It names no implementation; [building a writer](docs/guides/building-a-writer.md) is the way in for another one.
- **[tests/](tests/README.md)** — the validation suites: every executed claim of the docs, run against the schema and
  against this writer's own extraction and save. `go test ./tests`.
- **[AGENTS.md](AGENTS.md)** — the rules for anyone, human or agent, working in this repo.

`lifelog` implements the contract and never defines it: a rule found missing or ambiguous here is an
[issue](docs/issues/README.md) against the docs, not a fix in this code alone.

```
go generate ./... && go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo
lifelog init --db ~/life/life.db
export LIFELOG_DB=~/life/life.db           # or --db on any command

lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog day --human
lifelog serve                              # http://127.0.0.1:7777 — open it in a browser
lifelog mcp --agent lmstudio               # MCP on stdio; rows are written as agent:lmstudio
lifelog habits --human                     # today's habits; lifelog done evening_walk
lifelog snapshot --to ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg   # a photo lifelog cannot read: send a JPEG of it
lifelog file IMG_0003.jpg --at Lakeside --radius 300   # a photo near no known place: name the place
```

An import ([importing with a model](docs/guides/importing.md)) works in a workspace beside its source and on a
trial database inside it:

```
lifelog import setup --workspace ~/import/Notebook.lifelog --from ~/life/life.db   # trial.db: a copy
lifelog mcp --workspace ~/import/Notebook.lifelog --agent lmstudio                  # the model's tools
lifelog import approve rules --workspace ~/import/Notebook.lifelog                  # the owner, at a terminal
lifelog import status --workspace ~/import/Notebook.lifelog --human
lifelog import replay --to ~/life/life.db --workspace ~/import/Notebook.lifelog     # the real run, when you say so
lifelog import photos inventory ~/takeout-x/Takeout/"Google Photos" --human   # counts of a Photos export (plan 032)
```

## Decisions

These are this application's own engineering decisions ([D14](docs/decisions/D14-ui-and-tools.md)).

- **One handler, three surfaces.** `internal/api` is the only thing the surfaces call. `serve` mounts it on a
  socket; the CLI and the MCP server call it in-process (`internal/client`), or a running server with `--url`.
  Writes go through `internal/core` only: no generic UPDATE or DELETE exists anywhere.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled (a page's `save-body` carries its body and `version`). `GET /actions` is
  the catalog; the CLI's `do` and the MCP tools are generated from it, so a new action needs no client change.
- **The browser gets the same entity.** `Accept: text/html` renders it with a template per class (day, page,
  person, place, metric, habits, search, lists; `internal/api/html/`, embedded) or a generic one for any other. A
  template reads only the properties the JSON carries and places only the actions the entity offers, so a form
  shows where the action is legal and nowhere else. A body is CommonMark with goldmark's safe defaults (raw HTML
  omitted), its `[[wikilinks]]` and `#tags` linked. No JavaScript: a browser form posts and is answered 303 to the
  resource it changed, so a reload never posts twice; a refused one shows the text it sent.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (a browser form) or `agent:<name>` (MCP);
  the `Lifelog-Source` header sets it.
- **Optimistic saves.** A body save sends the `version` (`entities.updated_at`) it read; a newer one is a 409.
- **Read-only queries.** `POST /query` runs one statement on a `mode=ro`, `query_only` connection, 5 s and
  500 rows at most.
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with `trusted_schema=OFF`, read back the same way.
- **Unicode.** `title_key` uses `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned table,
  not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  import's *apply facts* runs several of the same methods in one. Nothing writes around them.
- **Owner-only actions.** `register-metric` and `replay` are refused to an `agent:*` writer and are never MCP tools.
  `approve` is not in the API at all: `lifelog import approve` refuses without an interactive terminal, and its stamp
  (`status: approved YYYY-MM-DD (owner) sha256:…`) hashes the rest of the file, so an edit closes the gate again.
- **Renames move the text and the typed links**, as [titles and wikilinks](docs/contract/titles-and-wikilinks.md)
  ("Renames") requires and [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's
  `rename` action run it.
- **The import workspace is fixed at startup** (`--workspace`); a request names files relative to the source and
  never leaves it. Rows are written as rules.md's `source:`. A row or a note page an agent changes directly during
  an import is allowed and reported by *status*: a replay carries only the facts and the notes.
- **Imported keys** are derived, never sent: `path|person|<title_key>` (and place, page), a note's path, and
  `path|reading|<metric>|<day>|<taken_at, or its place in the source file>`.
- **The schema is embedded.** `go generate ./...` copies `docs/schema/schema.sql` into `internal/db`; a test fails
  when the copy is stale. `lifelog init` refuses an existing file; `Open` refuses a file without Lifelog's
  `application_id`.
- **Snapshots are the owner's** ([take a snapshot](docs/cookbook/take-a-snapshot.md)). `lifelog snapshot` is a CLI
  command only, never an API action or an MCP tool: it writes a file on this machine. It refuses a folder inside a
  git work tree, and its restore check opens the snapshot with `Close` skipping `PRAGMA optimize`.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` (`POST /files`, `lifelog file`) streams
  the original through SHA-256 and drops it; nothing of it is stored. `internal/preview` makes every picture itself —
  from a JPEG, PNG or GIF, or a JPEG sent for a HEIC or a video frame: scaled to 1600 px, turned upright by EXIF,
  re-encoded with no metadata (no GPS). An agent sends `sha256`, `mime` and the text. `![[Title]]` renders the
  picture; `GET /pages/{id}/preview` and `GET /previews?title=` serve it. A photo's day and position come from its EXIF
  (`internal/photo`: a JPEG's APP1, a HEIC's Exif item, read from the original's first megabyte): its day links the
  place its position is in, `at` names one, `locate` sets or moves a place's point; the position is never stored.
- **`serve` binds 127.0.0.1** and has no authentication: `life.db` holds health data, and the API is for this
  machine.

## Layout

| path | what |
|---|---|
| `cmd/lifelog` | the binary: `init`, `serve`, `mcp`, `get`, `do`, `actions` and the shortcuts (`file` among them) |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema |
| `internal/text` | the title predicate, `title_key`, wikilink and `#tag` extraction; tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) |
| `internal/core` | the cookbook's writes and reads; the save contract; habits; renames; files |
| `internal/preview` | the picture a file page keeps: decode, scale to 1600 px, EXIF orientation, a JPEG of at most 1 MB with no metadata |
| `internal/photo` | what a photo's metadata says: the day and time taken, the position, the orientation (JPEG and HEIC); `phototest` builds synthetic ones |
| `internal/takeout` | a Google Takeout export: Photos' media paired with their sidecars (every naming form), and its inventory of counts |
| `internal/importer` | the import workspace, the facts checks and apply, the vault plan, status, replay |
| `internal/api` | the action catalog, the routes, Siren and HTML |
| `internal/client` | the hypermedia client (in-process or remote) |
| `internal/mcp` | the catalog as MCP tools |
| `tools/copyschema` | `go generate`'s copy of `docs/schema/schema.sql` into `internal/db` |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) |

## Checks

`go generate ./... && go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema, the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.
