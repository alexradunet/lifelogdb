# 054 — Restrict vault append recovery to a complete trailing block

> Executor: preserve the existing best-effort recovery protocol; this plan does not invent exactly-once provenance. Use synthetic workspaces and set plan/index IN REVIEW (owner) after verification.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/vault.go internal/importer/importer_test.go internal/importer/vault_append_test.go internal/importer/replay_test.go`. Serialize with plans 051 and 062, which edit the same file.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** S. **Risk:** MED (retry deduplication).
- **Status:** IN REVIEW (owner). **Depends on:** none semantically; execute after 051 to avoid overlapping edits.
- **Category:** bug. **Deep-audit finding:** 8. **Confidence:** HIGH, source-confirmed; new cases not executed.

## Why

An independent piece of journal prose that happens to contain the imported note causes the importer to skip that note and mark it appended. Substring equality is not evidence that the append operation happened. Restrict the fallback to the documented complete suffix produced by capture, keeping its limitations explicit.

## Current state and conventions

`internal/importer/vault.go:279–298` comments that recovery recognizes the page “ending with the text”, but checks:

```go
} else if p != nil && (n.Appended || strings.Contains(p.Body, body)) {
    res.Same++
    return nil
}
```

`Capture` appends a blank line before nonempty existing text. `applyPlan` persists `Appended=true` after the database transaction, so an interrupted plan-file save is why fallback recognition exists. Use `TestVault` in `importer_test.go` and replay tests for structure. The [import guide](../guides/importing.md) describes repeatable trial/replay, not a new durable append marker.

## Scope

Only `internal/importer/vault.go`, `internal/importer/importer_test.go`, new `internal/importer/vault_append_test.go`, `internal/importer/replay_test.go`, and plan/index. No DDL, hidden body markers, new plan format, append event table, source rewrites or whole-replay atomicity.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestVaultAppendBoundary|TestVault|TestReplayRehearsesBeforeItWrites'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestVaultAppendBoundary` with an incoming note appearing only in the middle of unrelated prose, as a substring inside a larger final line, as an exact entire body, and as an exact final block after the capture separator. Include source trailing newlines and empty notes.
   **Verify:** focus command → baseline incorrectly skips the embedded-substring cases.
2. Replace substring fallback with byte-exact recognition of the capture result: the body equals the note, or ends with the note preceded by the full append boundary. Preserve existing `Appended` handling and avoid trimming/normalizing substantive text to create equality. Only confirmed appended/skipped outcomes may update plan bookkeeping.
   **Verify:** focus command → independent prose gets the note appended; a real complete trailing append is not duplicated.
3. Simulate losing the plan's appended flag after a successful append and retry. Test repeat apply and fresh replay (where the copied plan resets append state) preserve expected text/link counts. Explicitly retain the known ambiguity: independently identical complete trailing text cannot be distinguished from a lost receipt without new provenance. Do not claim exactly-once under subsequent manual edits.
   **Verify:** package and final commands → exit 0; unchanged source bytes and scope-only diff.

## Done criteria

- [x] Interior/partial matches cannot suppress a new journal note.
- [x] Exact complete trailing-block recovery and ordinary repeat apply remain idempotent.
- [x] Appended flags, body bytes and synced links agree in trial/fresh replay tests.
- [x] All gates pass; residual identical-suffix ambiguity reported; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): only a complete trailing append block satisfies interrupted-apply recovery. Interior/partial matches, repeat applies and fresh replay are covered; focus/package and integrated generation/vet/uncached full tests passed. Independently written identical complete suffixes remain indistinguishable without new provenance; no provenance convention was invented.

## STOP conditions

Stop if requirements demand distinguishing independently identical trailing text, or if later edits must be reconciled automatically. That needs a separate owner-approved provenance design, not another substring heuristic. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: match complete vault append boundaries (plan 054)`, listing checks. Keep append construction and recovery recognition paired in tests whenever separators change.
