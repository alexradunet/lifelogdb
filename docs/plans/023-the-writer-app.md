# Plan 023: The writer — `app/`, one Go binary serving a hypermedia API, a CLI and an MCP server

## Status

- **Priority**: P1 (the step the status line of `docs/README.md` names: the capture path, then the first real import)
- **Effort**: L
- **Category**: feature
- **Planned and built**: 2026-10-02, on top of `4d84261` and plans 014–022

## Why

The contract had no writer since plan 008 removed the first one. The owner wants, in order: a read view, an API to
query and update `life.db`, a CLI over that API that AI agents (local models, see [importing](../guides/importing.md))
can drive, and an MCP server. Goals: minimal and portable; HATEOAS.

## Owner decisions (2026-10-02)

- **Go**, one static cgo-free binary: `modernc.org/sqlite` (SQLite 3.53.4 with FTS5 and `SQLITE_DBCONFIG_DEFENSIVE`),
  `golang.org/x/text` (NFC and the full case fold), `goldmark` (CommonMark), the official MCP Go SDK, stdlib `net/http`.
  Bun was the alternative; JavaScript has no full case fold and Bun's SQLite version follows the Bun release.
- **The CLI speaks HTTP** to the same handler — in-process by default, `--url` for a running `lifelog serve`.
- **Hypermedia: Siren-style JSON and plain HTML** from one representation, by content negotiation. The browser view
  is a generic template over any entity, so it is item 1 (the read view) for free.
- **The code lives in `app/`** in this repo; `docs/` stays writer-neutral and never names it.
- The non-goal row "Agent CLI/API" is deleted (plan 021, Q3).

## Shape

- One action catalog (`GET /actions`): every write of the cookbook as a named action with fields. The API renders
  each one on the resource where it is legal (no `promote` on a day page, `at` only from a day page, only `revive`
  on a tombstone), the CLI's `do` and the MCP tools are built from the same catalog. No generic UPDATE or DELETE.
- Reads: the cookbook's queries as resources (`/days/{day}`, `/pages/{id}` with backlinks, `/metrics/{name}`,
  `/search`, `/ghosts`, ...) and `POST /query`, one statement on a `mode=ro` + `query_only` connection.
- `source` per surface: `cli`, `api`, `ui` (browser forms), `agent:<name>` (MCP).
- A body save carries the page's `version` (`entities.updated_at`); a stale one is a 409.

## Verification (2026-10-02)

- `cd app && go vet ./... && go test ./...` — green: the title-key and wikilink vectors read from
  `docs/contract/titles-and-wikilinks.md` (two rules broken on purpose make them fail), the schema copy equals
  `docs/schema/schema.sql`, the pragmas read back, `BEGIN IMMEDIATE` holds the lock, the save contract adds and
  deletes links and revives a tombstoned target, actions appear only where legal.
- End to end on a throwaway file: `init`, `capture --mood`, `promote`, an `at` link (refused from a non-day page),
  a versioned save (a stale one refused), `serve` with curl (JSON, HTML, a POST) and the CLI with `--url`, MCP over
  stdio (18 tools, a capture written as `agent:test`, a mood of 9 refused); then the four
  [integrity checks](../contract/integrity-checks.md) — clean.
- `python tests/run_all.py` — green (the docs changed only in `architecture/non-goals.md`).

## Open

- The `Cn` rule names no Unicode version. Go and `x/text` ship Unicode 17.0, the reference (Python 3.12) 15.0; `app/`
  pins the assigned-code-point table to 15.0 so it never accepts a title the reference rejects. Whether the contract
  should state a version is an owner question (rejected finding of the second run: "state a version when a second
  writer exists").
- Not built yet: habits (`start`/`stop`/check-in), renames (`#REDIRECT` stub + `redirect` link), the import guide's
  flow through the CLI. Each waits for a real use.
- `version` is an instant with milliseconds: two saves of one page within the same millisecond share it.
