# 075 — Schema validation depth

- **Date:** 2026-10-07
- **Status:** DONE
- **Baseline:** `90c062e`.
- **Authorization:** owner requested another comprehensive investigation, current research and missing tests.
- **Scope:** canonical schema, contract recipes and their validation; implementation changes only for demonstrated contract defects. No migrations or private data.

## Work and completion criteria

Review constraints and trigger interactions, temporal and lifecycle queries, connection/file recovery guarantees, and the tests' independent answers. Reproduce defects before fixing them. Add bounded, deterministic operation-sequence and failure-path coverage where isolated rule tests leave gaps. Keep new checks in the normal baseline; document separate fuzz and extended invocations and distinguish actual runs from supported checks.

Use current primary SQLite sources and database-testing papers to select applicable techniques, without introducing a testing framework or dependency. Record evidence, resolved findings, exact validation commands and residual limits here. All new rules require mutation witnesses. Finish with generation, vet, full baseline, shuffled baseline and appropriate additional analysis; report unavailable tools and untested platforms honestly.

## Investigation and research

Primary sources checked on 2026-10-07:

| Source | Applicable evidence and choice |
|---|---|
| [SQLite 3.53.4 release](https://sqlite.org/releaselog/3_53_4.html), [news](https://sqlite.org/news.html) | The pinned engine matches the latest published maintenance release found at review time. No dependency update was needed. |
| [How SQLite is tested](https://sqlite.org/testing.html) | Independent oracles, failure injection, malformed files and configuration variation are complementary. Existing physical corruption, process-interruption and snapshot suites remain in the baseline; our runs do not establish real power-loss behavior. |
| [SQLite security guidance](https://sqlite.org/security.html) | Defensive mode and untrusted-schema settings address different risks. The corrected defensive-mode test now proves the actual shadow-table operation is refused, with the same valid SQL admitted by the permissive control. No arbitrary-SQL execution feature or new security policy was added. |
| [SQLite UPSERT](https://sqlite.org/lang_upsert.html), [conflict resolution](https://sqlite.org/lang_conflict.html) | Uniqueness handling, trigger timing, statement ABORT and transaction rollback must be distinguished. Literal probes exposed the scoped replay refusal and import prose overclaims. |
| [External-content FTS trigger report and maintainer response](https://sqlite.org/bugs/info/552f36384c7900ef72448f71118f9c6fe8aa2553e7ef617ef7323c3d0f54b104) | The August 2026 discussion concerns interaction/order of nested content updates; it is not evidence of a fixed engine defect. Our column-specific FTS triggers pass token-superset edits under both original and reversed creation order, plus positional vocabulary/rebuild comparison. |
| [Scaling Automated Database System Testing, v2 January 2026](https://arxiv.org/abs/2503.21424) | Adaptive generation extends testing across SQL dialects. This repository has one pinned engine and a specific contract, so bounded schema operation generators give more direct value than adding another test framework. |
| [Automated Discovery of Test Oracles for DBMSs, v2 March 2026](https://arxiv.org/abs/2510.06663) | Generated query equivalences can be unsound; Argus verifies its equivalences before using them. Our graph, chain and calendar expected results are independent domain models, with explicit hand-checked regressions, rather than unverified SQL rewrites. |
| [JoinEquiv, June 2026](https://arxiv.org/abs/2606.23294) | Equivalent-query comparisons can detect silent logic errors. Its published study of other engines does not establish a SQLite defect; configuration/rebuild comparisons here supplement domain oracles. |

The review also checked DDL guard and CHECK coverage, conflict-mode overflow probes, retained graph state, lifecycle queries, the mutation harness's explicit failure-witness controls and connection/version handling. Existing constraints generally have both behavior tests and mutation coverage; counting references to constraint names alone would miss substring-based mutations and is not used as a coverage quota.

## Results and validation

Resolved incidents: [0033](../issues/0033-containment-starts-from-deleted-root.md), [0034](../issues/0034-scoped-import-retry-rejected-after-tombstone.md), [0035](../issues/0035-test-fixtures-can-open-the-wrong-file.md), [0036](../issues/0036-writer-opens-unsupported-schema-versions.md).

New baseline coverage includes graph operation sequences, temporal chain/calendar models, FTS trigger-order and rebuild comparisons, deterministic WAL reader/checkpoint behavior, import conflict/transaction boundaries, literal fixture filenames and unsupported schema-version refusal. Tests were observed failing before the containment/category, scoped retry, literal filename and schema-version fixes. The obsolete shadow-table test was repaired with an existing-table assertion and an independent permissive control; its old statement failed for the wrong reason.

An initial full baseline found two outdated containment mutants after the legitimate query change: the cycle mutation still targeted the old seed, and removing the final deleted-place filter became redundant once every recursive seed/step was live-only. The cycle mutation now targets the current seed; lifecycle mutation checks a deleted intermediate hiding live descendants, alongside the new deleted-root witness. Original behavior assertions remain intact.

Validated on Linux 7.2.5-3-omarchy amd64, Go 1.27.1, modernc.org/sqlite v1.60.1 / SQLite 3.53.4. No application feature, dependency or migration added. No real owner data opened or modified.

Passed:

- `go generate ./...`; canonical and embedded schema compare byte for byte.
- `go vet ./...`.
- `TMPDIR=/var/tmp go test -count=1 ./...`: all packages, suites and 404 mutants; contract package 73.940 seconds.
- `TMPDIR=/var/tmp go test -count=1 -shuffle=on ./...`: all packages, suites and 404 mutants; contract package 72.215 seconds.
- Focused regression runs during implementation, including production measurement replay, all database package tests, new contract suites and changed mutation witnesses.
- `CGO_ENABLED=1 go test -race -count=1 ./internal/db ./internal/core ./tests -run 'TestOpen|TestPragmasAndReaders|TestRecordReplayAfterSessionTombstone|TestSchemaFixtureUsesLiteralFilename|TestSuites/(schema-adversarial|graph-model|storage-resilience|temporal-model|writers|snapshots)$|FuzzGraphTransitions'`: all three packages passed, contract package 9.937 seconds. This is focused race coverage, not a full repository race rerun.
- `go test ./tests -run '^$' -fuzz '^FuzzGraphTransitions$' -fuzztime=30s -parallel=2`: 1,267 executions; passed.
- `go test ./internal/core -run '^$' -fuzz '^FuzzTaskCalendarWindow$' -fuzztime=30s -parallel=2`: 349,651 executions; passed.
- `go test ./internal/core -run '^$' -fuzz '^FuzzPeriodMembership$' -fuzztime=30s -parallel=2`: 295,516 executions; passed.
- `git diff --check`.
- `go test ./tests -run '^TestSuites/document$' -count=1`: final document and process-record checks passed.

Independent final review found no remaining concrete defect in the changes, no private data or generated-copy drift, and no unrelated additions. The existing untracked Python caches remain excluded.

## Residual limits

Staticcheck and govulncheck are absent from PATH and were not installed or run. Windows execution, the minimum-supported SQLite engine matrix, full repository race, extended lifetime/media stress, filesystem I/O fault injection and actual power loss were not run in this round. Process interruption and snapshot checks are part of the passing baseline but are not power-loss proof. Version recognition does not fingerprint every pre-freeze schema with `user_version=1` and is not a schema-tamper audit. Tests establish these exercised behaviors, not freedom from all bugs or protection from arbitrary file modification.

The next useful evidence remains a replayable real import and platform/release validation under the existing process; additional features, migrations or speculative hardening are not implied by this review.
