# Plan 027: A replay rehearses before it writes

## Status

- **Priority**: P1 (the real run of the first real import; [issue 0005](../issues/0005-a-replay-stops-in-the-middle-and-leaves-the-target-half-written.md))
- **Effort**: S
- **Category**: bug
- **Planned and built**: 2026-10-02, on top of plan 024

## Why

[Issue 0005](../issues/0005-a-replay-stops-in-the-middle-and-leaves-the-target-half-written.md): *replay* walked the
ledger file by file into the target, and the first file that failed stopped it with everything before it already
written — the target half-imported, and nothing said so. There was no way to learn in advance whether a replay would
succeed, and the integrity checks that close a replay were never reached.

Three ways were weighed:

- **One transaction for the whole replay** — rejected. The vault, the metrics, each facts file and the corrections
  each commit in their own `BEGIN IMMEDIATE` transaction through the same operations the API runs, and the retry of a
  file that names a row not written yet depends on that file rolling back alone; one transaction would mean
  threading it through every operation and savepoints for the retry, would hold the target's write lock and grow its
  WAL for the whole import, and would still stop at the first failure, listing one.
- **A dry run only** (`replay --dry-run` on a copy) — needed, but on its own it leaves the real run as unguarded as
  before for an owner who skips it.
- **Both: the real run always rehearses first** — chosen. The rehearsal is the whole replay on a throwaway
  `VACUUM INTO` copy of the target (the trial's own recipe, [imports](../contract/imports.md) step 1: a read-only
  connection, so the one-writer rule holds), and the dry run is that rehearsal alone. The cost is a copy of the
  target's size and the replay run twice; a replay happens a few times in the life of the file (the real run, then
  once more to see that it writes nothing), and `life.db` holds no attachments (D9).

## What was built

- **`internal/importer/replay.go`.** `Replay` keeps its signature and its result; it calls `Rehearse` first and
  refuses (422, every failure one line) unless the rehearsal failed nowhere and its integrity checks were clean;
  only then is the target initialised (when it does not exist) and written. `Rehearse` (new) makes the copy in a
  `.rehearsal-*` folder inside the workspace (it holds the owner's data, like `trial.db`), or a new database when
  the target does not exist, replays onto it going on past each failing step or file, runs the four
  [integrity checks](../contract/integrity-checks.md) and the comparison with the trial, closes the copy and removes
  the folder; a folder it cannot remove is an error. The body of the old `Replay` became `replayInto` (rehearsing,
  `ReplayResult.failed` records a failure and the rest goes on; otherwise the first failure stops it, as before);
  the integrity checks and counts became `compareWithTrial`. `replayCorrections` names every correction whose
  reading was not written, not only the first. `ReplayResult` gained `dry_run` and `failures` (step, file, error).
- **Surfaces.** The `replay` action has an optional field `dry_run` (`1` or `0`; the browser form offers `1` first);
  it stays owner-only and never an MCP tool. The CLI: `lifelog import replay --to PATH --dry-run`, which exits
  non-zero when the rehearsal found failures.
- **Docs.** [Importing](../guides/importing.md), "Trial, then the real run": a dry run first, the rehearsal and what
  it guarantees, the corrections in the replay's order, the copy's space and place; the operations table and one
  line in "What an implementation must get right". Writer-neutral; nothing in `schema.sql` changed.

## Verification (2026-10-02)

- `go generate ./... && go vet ./... && go test ./...` — green.
- `internal/importer/replay_test.go` reproduces the issue: a replay whose second ledger file fails leaves a new
  target uncreated and an existing target byte-for-byte unchanged; a dry run reports the failure, and with two
  failing files both, writing nothing; once the owner decides the look-alikes the replay writes and a second replay
  writes nothing; no rehearsal folder is left in the workspace. `internal/api/replay_test.go`: `dry_run=1` writes
  nothing, `dry_run=yes` is a 422, an agent gets a 403, the real run writes.
- The fix broken on purpose, one at a time, each fails the test: the rehearsal's failures ignored (the target is
  half-written), the rehearsal stopping at the first failure, the copy not closed before removal, the copy not
  removed.

## Open

- A failure of the real run after a clean rehearsal (a full disk, the target changed by another writer in between)
  still leaves the writes made before it; the error says so, and every write is idempotent. Making the real run
  atomic would need the one-transaction design rejected above.
- The vault step stops at its first failing note, in the rehearsal too: a vault failure is reported once, not per
  note.
- The workspace is not locked between the rehearsal and the real run; the owner runs *replay* while no model is
  writing facts.
