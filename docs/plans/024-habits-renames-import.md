# Plan 024: Habits, renames and the import flow in `app/`

## Status

- **Priority**: P1 (the first real import, an Obsidian vault driven by a local model, needs the import flow)
- **Effort**: L
- **Category**: feature
- **Planned and built**: 2026-10-02, on top of plan 023

## Owner decisions (2026-10-02)

- **Import scope**: all of [importing](../guides/importing.md), in phases: the facts pipeline, the Obsidian vault,
  the replay.
- **Model files**: the model writes facts, questions, rule drafts and metric proposals through the writer's
  operations (MCP tools), which write only inside the workspace and check the format as they write.
- **One MCP server**: with `--workspace` the import tools are added beside capture, save-body and the rest. A row an
  agent writes directly is not in the facts, so a replay does not carry it; *status* reports it.
- **Rename into an existing title**: only when the renamed page is empty (a typo's ghost); otherwise refused.

## What was built

- **Composable core.** Every write is a method on `core.Tx` (one `BEGIN IMMEDIATE` transaction); the API, the CLI,
  MCP and *apply facts* run the same functions. Entity inserts take an optional `import_key`
  ([import a row once](../cookbook/import-a-row-once.md)).
- **Habits** ([habits](../cookbook/habits.md), D24): register a metric (the owner's), start and stop a habit (a
  re-sent period carries its `end_day`), check in 0/1, the day's habits and completion over a period, `/habits`.
- **Renames**: a plain page only; the text moves to the new page, the old page becomes a `#REDIRECT` stub with a
  `redirect` link, its typed links move. The open choices are [issue 0001](../issues/0001-a-rename-has-no-recipe.md).
- **Owner-only actions**: `register-metric` and `replay` are refused to an `agent:*` writer and are never MCP tools;
  `approve` exists only as a CLI command that refuses without an interactive terminal. Its stamp carries a sha256 of
  the rest of the file, so an edit after approval closes the gate.
- **The import** (`app/internal/importer`): workspace fixed at startup, setup (the trial as a `VACUUM INTO` copy),
  status with a "do now" sentence and mismatches (every done file dry-run; keyed rows no facts file explains; rows and
  note pages agents changed directly), ledger, skip, inspect, find, draft rules, propose metrics, ask and close
  questions, write/check/apply facts with the guide's checks and derived keys, register metrics, the vault plan
  (plan, fix, apply, Obsidian link rewriting, append-once), replay (vault, metrics, facts in ledger order with retry,
  the owner's corrections, integrity, counts compared), the four integrity checks.

## Verification (2026-10-02)

- `cd app && go vet ./... && go test ./...` — green. Three rules broken on purpose (no look-alike check, reading keys
  in facts order, appends not recorded) each fail a test.
- End to end on synthetic data: a vault and a "real" `life.db`; `import setup --from`; the model's steps over MCP stdio
  (draft rules, plan and apply the vault, propose metrics, ledger, skip, write/check/apply facts); `approve` refused
  without a terminal; an owner-only tool unknown to the model; an agent's direct edit reported by *status*; `import
  replay` into the real file, then again (nothing written); the four integrity checks clean.
- `python tests/run_all.py` — green (the docs changed only in `plans/` and `issues/`).

## Open

- [Issue 0001](../issues/0001-a-rename-has-no-recipe.md): renames.
- The `Cn` rule's Unicode version (plan 023, Open).
- The approval is a line in a file: anything that can write the workspace can forge it (the guide's honest limit).
