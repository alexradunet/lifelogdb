# 048 — Reject malformed facts JSON without replacing the replay record

> Executor: use synthetic workspaces only. Run gates from the repository root; update this plan/index to IN REVIEW (owner) when complete.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/facts.go internal/importer/importer_test.go internal/importer/facts_json_test.go`. Compare changed symbols with this brief; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** S. **Risk:** LOW (invalid input must fail before replacement).
- **Status:** IN REVIEW (owner). **Depends on:** none. **Category:** bug.
- **Deep-audit finding:** 1. **Confidence:** HIGH, source-confirmed; new reproducer not yet run.

## Why

A facts file is durable replay evidence. The parser's top-level trailing-input check is not an EOF check, and formatting errors are ignored. Certain malformed suffixes can therefore pass initial validation and replace an existing valid facts file with empty content, losing the replay record even though the database facts remain.

## Current state and conventions

`internal/importer/facts.go:113` uses `dec.More()` after decoding one object. `More` asks about array/object membership; it does not prove a complete JSON document. `WriteFacts` at line 152 then does:

```go
var pretty bytes.Buffer
json.Indent(&pretty, data, "", "  ")
return writeAtomic(p, pretty.Bytes())
```

`parseFacts` already disallows unknown fields and requires `writes`; retain both. `writeAtomic` in `workspace.go` uses a temporary file plus rename. Use `setup`, `fixture.facts` and `TestApplyIsIdempotentAndChecked` in `importer_test.go` for synthetic fixture conventions. The [import contract](../contract/imports.md) relies on replayable facts; no facts-format change is needed.

## Scope

Only `internal/importer/facts.go`, `internal/importer/importer_test.go`, new `internal/importer/facts_json_test.go`, and plan/index status. No generic workspace persistence redesign, canonical-key changes, approval changes, schema/migrations or real workspaces.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestFactsJSONBoundary|TestApplyIsIdempotentAndChecked'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestFactsJSONBoundary`: save a valid synthetic file, capture its bytes and ledger state, then attempt replacement with a valid object followed by an unmatched closing delimiter, another object, a scalar, or garbage. Include invalid first documents, unknown fields, absent `writes`, legal empty writes and whitespace-only suffixes. Test `LoadFacts` rejects malformed stored input too.
   **Verify:** focus command → new malformed-suffix/preservation cases fail on baseline; existing valid apply passes.
2. After the first decode, perform another decode and require exactly `io.EOF`. A decoded second value or any other error is refusal. Keep the existing shape checks and useful `refuse` error style; do not use `More` as a document-boundary test.
   **Verify:** focus command → all malformed document cases are refused; legal trailing whitespace passes.
3. Check `json.Indent`'s error before calling `writeAtomic`; complete parsing/formatting before any replacement. Preserve submitted numeric/string semantics rather than round-tripping through lossy generic floats. Assert failed saves leave the existing file byte-identical, a previously absent destination absent, and no stray temporary output. A subsequent valid replacement and repeat apply must still work.
   **Verify:** package and final commands → exit 0; `git diff --name-only` is in scope.

## Done criteria

- [x] Exactly one complete JSON facts document is accepted; suffixes cannot bypass validation.
- [x] Every failed replacement preserves existing replay evidence and ledger/database state.
- [x] Both parse and formatting errors are handled; valid empty/whitespace cases remain accepted.
- [x] All gates pass; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): the second decode must return actual EOF, and formatting failures propagate. Synthetic suffix/refusal regressions preserve prior facts and persistence. Focus/package gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if a valid existing facts format depends on multiple top-level documents or requires a format migration. Do not recover missing real facts from a database in this task. Stop on unexplained drift, an out-of-scope change or two failed verification attempts.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: preserve facts on malformed JSON (plan 048)`, listing checks. Any future facts transformation must finish successfully before the old replay record is replaced.
