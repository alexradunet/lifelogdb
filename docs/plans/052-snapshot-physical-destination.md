# 052 — Check physical snapshot destinations for Git work trees

> Executor: create only synthetic databases and test-owned directory links. Never snapshot a real database during verification. Finish with plan/index IN REVIEW (owner).
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/db/db.go internal/db/db_test.go cmd/lifelog/main_test.go tests/snapshots_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** S. **Risk:** LOW/MED (platform path resolution).
- **Status:** IN REVIEW (owner). **Depends on:** none; preserve implemented plans 045/046.
- **Category:** security. **Deep-audit finding:** 6. **Confidence:** HIGH for source gap; junction cases need platform execution.

## Why

Snapshots contain the same private notes and health data as the database. A destination spelled outside a repository may resolve inside one through a symlink or Windows junction. The current lexical ancestor check then misses the privacy refusal the command promises.

## Current state and conventions

`internal/db/db.go:289–318` makes the destination absolute, stats it and calls:

```go
if repo := gitWorkTree(dir); repo != "" {
    return "", fmt.Errorf("%s is inside the git work tree %s: a snapshot holds health data and private notes, "+
        "which git history cannot forget; choose a folder outside it", dir, repo)
}
```

`gitWorkTree` searches ancestor `.git` entries of the spelling supplied, not the resolved directory. `Snapshot` uses `Copy`/`VACUUM INTO` and never overwrites. Follow `TestSnapshotRefusesAGitWorkTree`, `TestSnapshotLiteralSpecialCharacterPaths` and `TestLiteralDatabasePaths`. [D25](../decisions/D25-snapshots.md) and [the snapshot recipe](../cookbook/take-a-snapshot.md) remain the contract.

## Scope

Only `internal/db/db.go`, `internal/db/db_test.go`, `cmd/lifelog/main_test.go`, `tests/snapshots_test.go` if recipe verification needs a case, and plan/index. No Git subprocess dependency in production, snapshot naming changes, restore CLI, encryption, migrations or broader copy semantics.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/db ./cmd/lifelog -run 'TestSnapshot|TestLiteralDatabasePaths'`.
- Recipe: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/snapshots$'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add synthetic destinations linked from outside a fake work tree to its interior, including a linked ancestor and `.git` as a directory or worktree marker file. Add a safe external destination control and Windows junction cases where supported. A fake `.git` marker suffices; do not initialize a real repository containing fixtures.
   **Verify:** focus command → baseline fails linked-inside refusal; direct-inside refusal still passes.
2. Resolve the existing destination physically using the standard library and check both lexical and physical ancestor chains. Preserve conservative refusal when the user-supplied path itself is inside a work tree. Fail closed on resolution/permission errors. Use the checked physical destination for the copy so routine link retargeting does not redirect a later lexical lookup; retain no-overwrite behavior and readable errors.
   **Verify:** focus command → linked-inside destinations refuse before any snapshot file appears; safe external paths still succeed.
3. Exercise command exit behavior, duplicate snapshot names, special characters, and unchanged source bytes. Assert every refused case creates no database/journal artifact at either spelling. Document platform skips narrowly and report junction coverage honestly.
   **Verify:** recipe and final commands → exit 0; `git diff --name-only` remains scoped.

## Done criteria

- [x] Both lexical and resolved destinations are screened for `.git` ancestors before copy.
- [x] Linked/junction destinations inside repositories refuse without output files.
- [x] Safe external snapshots, restore checks, literal path handling and no-overwrite semantics still pass.
- [x] All gates pass; platform limitations reported; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): lexical/physical ancestry checks include bounded Windows junction resolution (`Lstat`/`Readlink` before `EvalSymlinks`). Eight junction cases executed; ordinary symlink cases skipped individually when privileges were unavailable. Core/CLI snapshot gates and integrated generation/vet/uncached full tests passed. No production subprocess or dependency was added.

## STOP conditions

Stop if physical resolution cannot identify the destination on a supported platform or a fix requires weakening no-overwrite checks. This is not a guarantee against arbitrary concurrent mount/directory replacement by a hostile local process; report that limit rather than claiming a sandbox. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `snapshots: inspect resolved destination ancestry (plan 052)`, listing checks. Future destination-creation features must resolve/check the newly created directory too.
