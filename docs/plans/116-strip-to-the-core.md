# 116 — Strip to the core: remove the features the owner does not use

> Dated implementation record. Tests use temporary folders and synthetic data. The owner's live `life.db` is never
> opened by a test.

## Status

- **Date / baseline:** 2026-10-15, `518c3bb` (master).
- **Priority:** P1. **Effort:** XL, in eight steps. **Risk:** MEDIUM: four steps change the schema, which is
  possible only before the freeze ([D13](../decisions/D13-migrations-and-freeze.md)); each of those rewrites the
  owner's rows as text before its table goes, so no word is lost.
- **Status:** IN PROGRESS.
- **Resolves:** the owner's decision (2026-10-15): "I am looking to strip all the unwanted or complex features in
  order to have a simpler more reliable software." The owner chose every candidate of the inventory below. Each
  schema step has its own issue and RFC, as [RFC 0013](../rfcs/0013-no-sessions.md) and
  [RFC 0014](../rfcs/0014-no-periods.md) had.

## Why

`life.db` is a life log and its backup. Around that core the writer grew features that cost more than they give:
each is a table, a contract page, a set of actions on every surface, its suites and its mutants, and at the freeze
whatever is still in the schema stays for good. The owner reviewed the inventory and kept the core only.

```mermaid
flowchart LR
  K["kept: day pages, pages, people, places, categories, files with text and picture, metrics and readings, search, rename, tombstone and restore, snapshots, integrity"]
  C1["the conversation: ask, Conversations, the voice note"] -.->|"step 1"| X["removed"]
  C2["read-only SQL"] -.->|"step 2"| X
  C3["readings from a table"] -.->|"step 3"| X
  C4["the public listener"] -.->|"step 4"| X
  C5["photo positions, a place's point, locate"] -.->|"step 5"| X
  C6["typed links other than wikilink, at, part-of"] -.->|"step 6"| X
  C7["habits"] -.->|"step 7"| X
  C8["tasks and occurrences"] -.->|"step 8"| X
```

## The rules

1. **A removal is complete.** The actions leave the catalog, so the CLI, the MCP tools and the browser lose them
   together; the routes, the views, the Go, the docs pages, the suites and the mutants go in the same commit; the
   counts in `tests/README.md` stay true. No feature flag, no deprecated path.
2. **The docs say the current truth.** A decision that described the feature is rewritten in place to say that
   the thing is not kept and what holds its content instead (as [D22](../decisions/D22-events.md) does for spans).
   The issue and the RFC are the dated record.
3. **No word of the owner's is lost.** Before a table goes, its rows are written as text into the page they
   belong to: a place's point into the place page, a typed link as a wikilink line in the page's body, a habit's
   periods into the metric page, a task into a page of tasks. The program that does it is prepared and tested on a
   synthetic file, and the owner runs it on `life.db` (service stopped, snapshot first, integrity check after).
4. **Each step ships alone.** One commit per step, baseline green, pushed and deployed. A step that changes the
   schema deploys its binary before the owner's file changes: the writer opens a file by `user_version` only, so a
   table the new binary does not know is left alone until the owner's program drops it.

## The steps

| step | removes | record |
|---|---|---|
| 1 | `ask`, `/conversations`, the Conversations pages, the voice note (`capture` audio, the transcriber), the chat, agentic and speech packages, the model configuration | issue 0075, RFC 0015; the non-goals row on AI generation rewritten; README |
| 2 | `query` (`POST /query`), `core/query.go`, the CLI `query` | README (an application decision) |
| 3 | `readings-from-table` (`POST /measurements/from-table`), `core/tables.go` | README |
| 4 | `serve --public` and the token: `serve` is local-only | README |
| 5 | the `places` table (point, radius, link_days), `locate`, the map link, the photo's position and its place matching, the folder import of photos; a photo keeps its own day | issue 0076, RFC 0016; D21 and D16 rewritten; cookbook place-of-a-photo removed |
| 6 | `link_kinds` rows `about`, `located-in`, `parent-of`, `friend`, `family`, `related`; `link` and `unlink` keep `at` only | issue 0077, RFC 0017; D8 and D16 rewritten; cookbook inside-a-place removed |
| 7 | the `habit_periods` table, `start-habit`, `stop-habit`, `check-in`, `/habits` | issue 0078, RFC 0018; D24 rewritten: a habit is a metric with a daily reading |
| 8 | the `tasks` and `task_occurrences` tables, `lifelog_meta.planning`, the planning contract, the eleven task actions, `/tasks`, `/deadlines` | issue 0079, RFC 0019; D23 and D15 rewritten: planning is text in a page |

## Tests

- Each step: `go generate ./... && go vet ./... && go test ./...` green; the catalog test lists no removed action;
  the reference test keeps `API.md` equal to the code; the shell test's sidebar matches.
- Steps 5 to 8: the owning suite and its mutants go with the rule; `tests/README.md` counts change in the same
  commit and the commit message says so. The one-off program for `life.db` is tested on a synthetic file made by
  the writer: the rows become text, the table goes, the schema equals a fresh file from `schema.sql`, and the four
  integrity checks pass.

## Result

- **Step 1 (the conversation).** Removed: `ask`, `/conversations`, the chat page, the chat sidebar and its
  styles, the portraits, `capture`'s `audio` field and the CLI's `--audio`, the packages `internal/chat` and
  `internal/speech`, the Chat entry of the sidebar, the share target's file. `New` no longer takes an asker or a
  transcriber. Record: issue 0075, RFC 0015, a non-goals row; issues 0071 and 0072 closed the other way; the README
  bullets of plans 102, 103, 106 and 109 replaced by one. Checks: `go generate ./... && go vet ./... && go test ./...`
  green. The service unit still sets `LIFELOG_MODEL_URL` and `LIFELOG_MODEL`: they are read by nothing now.
- **Step 2 (read-only SQL).** Removed: `query` (`POST /query`), the CLI `query`, `core/query.go` with its statement
  allow-list and `Result`, and the ad hoc reader of `internal/db`. The tests that used `query` as a probe of what a
  surface wrote read the row from the file instead (`freshStore`, `scalar`). Record: README; API.md's `result`
  class is the dry run's shape alone. Checks: `go generate ./... && go vet ./... && go test ./...` green.
- **Step 3 (readings from a table).** Removed: `readings-from-table` (`POST /measurements/from-table`), the CLI
  `readings` and `--column`, `core/tables.go` with the source `import:table`, the agentic skill
  `lifelog-readings-from-table`. A reading is recorded one at a time with `record`. Record: README, API.md.
  Checks: `go generate ./... && go vet ./... && go test ./...` green.
