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

An import ([importing with a model](docs/guides/importing.md)) works in a workspace beside its source and on a
trial database inside it:

```
lifelog import inventory ~/ingest                                                 # survey the ingest folder: folders, counts, patterns, archives, sources; no content
lifelog import takeout inventory ~/takeout-x                                      # extraction root (including sibling Timeline.json), or Takeout folder; privacy-safe
lifelog import setup --workspace ~/import/Notebook.lifelog --from ~/life/life.db   # trial.db: a copy
lifelog mcp --workspace ~/import/Notebook.lifelog --agent lmstudio                  # the model's tools
lifelog do import-find text="Sam" --workspace ~/import/Notebook.lifelog             # import lookup; page lookup stays find
lifelog import approve rules --workspace ~/import/Notebook.lifelog                  # the owner, at a terminal
lifelog import status --workspace ~/import/Notebook.lifelog --human
lifelog import replay --to ~/life/life.db --workspace ~/import/Notebook.lifelog     # the real run, when you say so
```

For a vault plan, apply every note to the trial before a real replay. Draft rehearsal can evaluate a plan
without applying it, but does not authorize writing a real target. The writer keeps the exact original
source, note path, title and day in `applied-plan.json` in the workspace; facts and fix-plan operations
cannot edit this receipt. Owner renames keep reapply working through retained names without permitting
applied-plan title/day edits. Missing or conflicting evidence refuses rather than inferring intent from
aliases. This is filesystem evidence, not a cryptographic seal against arbitrary disk writers. Receipt
preparation does not prove a database commit. A receipt's `applied` marker means that exact binding completed
an ApplyVault pass, not owner approval, current prose/source equality or joint filesystem/SQLite atomicity.
Real replay requires the marker and the trial's identity/append evidence. Completion metadata failure is
reported even though SQL may already have committed; unchanged retry repairs it without duplicate appends.

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
  import's *apply facts* runs several of the same methods in one. Nothing writes around them.
- **Owner-only actions.** `register-metric` and `replay` are refused to an `agent:*` writer and are never MCP tools.
  `approve` is not in the API at all: `lifelog import approve` refuses without an interactive terminal, and its stamp
  (`status: approved YYYY-MM-DD (owner) sha256:…`) hashes the rest of the file, so an edit closes the gate again.
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's `rename` action run it.
- **The import workspace is fixed at startup** (`--workspace`); a request names files relative to the source and
  never leaves it. Rows are written as rules.md's `source:`. A row or a note page an agent changes directly during
  an import is allowed and reported by *status*: a replay carries only the facts and the notes.
- **Imported reading corrections are intent-backed.** A correction of an imported, keyed reading first publishes an
  immutable workspace intent and then commits SQL; if the process reports pending recovery, run import status/replay
  or retry a correction after resolving any conflict. This is recovery across process failures, not cross-file ACID
  or a power-loss guarantee.
- **A check reports every refusal.** `check-facts` runs each write in its own savepoint of the rolled-back dry run and
  returns `refused`, every write the writer would refuse with a class a client branches on (`look_alike` with its
  candidates, `not_yet` with the title it waits for, `unit`, `quote`, …); `apply-facts` keeps the file whole or not at
  all and carries the first class on the error. Look-alikes compare a new name with people, places and plain pages
  only.
- **Imported keys** are derived, never sent: `path|person|<title_key>` (and place, page), a note's path, and
  `path|reading|<metric_title_key>|<day>|<taken_at, or its place in the source file>`. Ambiguous old reading keys are
  refused during owner-local rehearsal; take a snapshot first if needed, because lifelog never auto-repairs them.
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
  The owner-stamped selected-pair preparation below is the only supplementary sidecar path.
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
| `internal/inventory` | the survey of an ingest folder without its contents ([importing](docs/guides/importing.md), "An ingest folder") |
| `internal/importer` | the import workspace, the facts checks and apply, the vault plan, status, replay |
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


### Bounded source preparations

With an import workspace, `draft-prepared`, `check-prepared`, and `apply-prepared` share the same core writes.
`draft-prepared` takes a selected source `file`, fixed `profile`, optional session `kind` and Fit `binding` JSON,
and a `metrics` JSON object mapping quantity codes to existing registered metric handles. It derives the
review records from the confined snapshot, not caller-provided normalized facts. The namespace comes from
approved rules; ordinary note facts keep their quote-evidence checks. The owner reads and stamps with
`lifelog import approve prepared` at an interactive terminal. No API/MCP action can stamp.

Supported profiles:

| profile | interpretation | quantity codes / units |
|---|---|---|
| `fit-date-csv-v1` | exact Date rows, unassociated readings; duplicate days refuse | `steps` / steps, `distance` / m, `reported-calories` / kcal, `mean-heart-rate` / bpm |
| `legacy-sleep-array-v1` | observed root array; exact logId, independent dateOfSleep, local or offset endpoints | `asleep-minutes`, `in-bed-minutes` / min |
| `fit-session-object-v1` | root-object session clocks only, explicit immutable key/day/exact fitnessActivity binding | none; aggregates/segments/duration excluded |

Source-reported mean HR is not resting or point HR; calories have no established active/basal/total subtype.
Missing readings remain missing, literal zero remains zero, nonzero binary64 underflow refuses. Large digit
IDs remain TEXT (string leading zeros remain distinct); malformed Unicode cannot become another identity.
Health timestamp CSV, slash-date exercise, sleep wrappers, Start/End CSV and automatic Fit aggregate meanings
remain preparation-unsupported even if structurally inventoried. Cross-provider overlap never merges or sums.
These profiles reflect limited shape evidence, not general Takeout or provider API coverage.

Sources are bounded to 1 MiB / 8192 records. Inventory is bounded per parsed file to 16 MiB, depth64,
65536 nodes, 256 columns, 8192 CSV records; exceeding a bound refuses, not successful partial import.
The stamped `prepared.md` closes on interpretation/source/mapping changes. `.prepared-binding-*` records
reserve immutable workspace logical-source interpretations before SQL; a reservation alone proves no commit.
A separate `.complete` checksum is published after SQL and before the ledger. Missing completion requires a
verified unchanged apply retry; corrupt evidence refuses replay. These are recovery evidence, not cryptographic
protection. Reapproval cannot reassign a workspace's established file namespace/profile/identity/mapping;
genuinely separate provider/account workspaces remain independent.
Preparation does not persist trial IDs: sessions resolve `(source, import_key)` in the target transaction.
Readings verify original root value/day/metric/scope, so unchanged retry preserves corrections; reapproval
cannot remap an applied quantity. Supporting writes and readings commit together; ledger publication follows
SQL commit and can fail independently. Retry verifies committed roots before finishing publication.
Replay verifies required existing trial roots and target source roots before replaying correction intents.
Workspace selection operations are serialized against the entire rehearsal/application; externally editable
selection bytes and source claims are revalidated before target effects. The admitted source-derived batches
and selected inputs are then held as an in-memory snapshot, not reopened to choose different interpretations.
The intent format and imported-scope relocation restrictions are unchanged. Status includes artifact gates and
binding checks. Status verifies required persisted original roots and completed selected history without
publishing recovery evidence. Review links are available from import status. An owner-tombstoned selected
trial file refuses replay before target effects: this bounded profile does not transport owner tombstone
history. Same-store checks/retries preserve tombstones. SQL and filesystem publication are not jointly atomic.

### Reviewed selected photo pairs

`draft-selected-photo` takes one confined selected `file`, at most one explicitly selected `sidecar`, title/text,
and JSON `choices`: `capture` is `none`, `exif`, `sidecar`, or `owner`; `gps` is `none`, `exif`, or `sidecar`.
Optional `offset`, `day`, `at`, and `radius` are explicit attribution/place choices. The owner reads exact
original/sidecar fingerprints, title-association evidence, separate claims and comparison results, then stamps
`lifelog import approve selected-photo`. `check-selected-photo` rolls back; `apply-selected-photo` uses the
existing file writer. Completed minimal selected receipts retain multiple applied pairs, fingerprints,
namespace and choices without copying originals or coordinate evidence. Verified unchanged retry finishes
interrupted marker/ledger publication; reapproval is not authority to repair prior owner history. Browser
review pages expose claims and choices; the terminal remains the only stamp boundary. A matching title is
not identity proof; mismatched/truncated title stays visible. There is
no directory search, guessed pairing, automatic claim precedence, URL following or original modification.

`photoTakenTime.timestamp` is interpreted only as a source-claimed integral Unix-seconds capture instant,
not certified camera truth. `creationTime` is a separate creation claim with unestablished Photos Takeout
meaning, never verified upload/import time or capture fallback. Local EXIF versus UTC epoch is unresolved
without offset evidence; genuinely comparable disagreement requires a stamped explicit choice. A supplied
day is attribution, not a timezone. Conflicting positions require an explicit reviewed selection; no proximity
tolerance asserts equality. Coordinates are transient matching evidence and only bounded review evidence
outside SQLite; previews are regenerated without metadata and originals remain untouched. Missing capture-local
attribution follows [keep a file](docs/cookbook/keep-a-file.md) and [the place of a photo](docs/cookbook/place-of-a-photo.md).
