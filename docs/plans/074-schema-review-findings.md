# 074 — Repair schema review findings

- **Date:** 2026-10-07
- **Status:** DONE
- **Scope:** schema, language-neutral contract, cookbook and validation; minimal shared integrity/place behavior needed to implement the contract.
- **Authorization:** owner requested “let's fix all the findings” after the schema review.

## Incidents and completion criteria

Resolve [issues 0026–0032](../issues/README.md), retaining the single pre-freeze init DDL. Every demonstrated refusal or query failure needs a regression; new rules need mutants. Keep schema counts, embedded DDL, decisions and executable recipes consistent. Run generation, vet and the full baseline; run shuffle for affected fixtures and available static analysis for changed Go.

| Review concern | Disposition |
|---|---|
| Typed detail ownership; habit-period row identity | Immutable ids, alias/atomicity/no-op regressions |
| Entity creation evidence | Immutable creation timestamp; insertion-time synthetic fixtures |
| Incomplete semantic integrity | Mirror, cycle, habit and journal audits; explicit checker boundaries |
| Active reads and nonbinary habit counts | Lifecycle filters and invalid-day category |
| Reference scans | Three measured indexes, query-plan regression |
| Historical cutoff semantics | Executed recorded-time projection; no exact commit-history claim |
| In-file preservation promise | Interpretation summaries, precise dependency on matching writer docs/vectors |
| Compound accents and Unicode identity | Corrected derived tokenizer; distinct exact-name policy |
| Preview history, lifecycle revival, provenance | Explicit current-state and retained-evidence limits |
| Habit-period withdrawal | Existing update-only limitation documented; no speculative tombstone model |
| Place selection ties | Stable id tie-breaker in recipe and shared query |
| Mentions versus encounters; evolving reader compatibility; import status | Corrected claims and source of current status |
| Deferred finance, tracks, project workflows, clinical coding, embeddings and universal audit/event models | Remain deferred: no demonstrated incident or authorization to add these features |

## Index evidence

Working tree based on `5a6b1c7`, Linux 7.2.5-3-omarchy amd64, AMD Ryzen 7 7840HS (16 logical CPUs), `/tmp` on tmpfs. Python SQLite 3.53.4; hardened file connections, WAL, synchronous FULL. Go validation uses Go 1.27.1 and modernc.org/sqlite v1.60.1 (SQLite 3.53.4). Queries use the fixture-building connection without evicting caches; no latency or physical-disk claim is made. Direct SQL synthetic fixture, not application throughput or cold-disk timing: 202 named pages (200 empty candidates), 1000 sessions, 40000 measurements concentrated in one session/capture source. Existing DDL is identical between runs except the three indexes. A progress callback counts approximate VM steps in blocks of 100; queries are fully consumed, fixture construction excluded from read counts.

| Measure | Without new indexes | With new indexes |
|---|---:|---:|
| Ghost count / VM steps | 200 / 32,810,200 | 200 / 10,600 |
| Empty-session readings / VM steps | 0 / 240,000 | 0 / fewer than 100 |
| 40,000 measurement INSERT VM steps | 6,840,000 | 7,560,000 |
| Allocated database pages in bytes | 4,534,272 | 5,951,488 |

The indexes cost 1,417,216 bytes on this fixture (capture 471,040; session scope 929,792; session kind 16,384). Measurement insertion VM work rises about 10.5%. This supports the existing lookup workloads; it does not establish lifetime latency, realistic media storage or end-to-end import throughput. The ordinary Go query-plan tests preserve access-path/result coverage without imposing a wall-clock threshold.

## Validation

Regression failures were observed before their fixes for ownership, creation evidence, compound accents, semantic integrity, tombstoned active reads, habit counts and place ties. An initial full run exposed date-vector fixtures that rewrote creation time; these now probe insertion with the same vectors. Two pre-existing correction mutants needed their literal query targets aligned with the new lifecycle joins; their intended assertions remain unchanged.

Passed on Linux amd64:

- `go generate ./...` — embedded DDL equals the canonical init byte for byte.
- `go vet ./...`.
- `TMPDIR=/var/tmp go test -count=1 ./...` — every package, contract suite and all 400 mutants; contract package 100.488 seconds.
- Focused contract/core regressions during implementation, including date boundaries, schema safeguards, recorded-time reads and semantic corruption.
- `git diff --check`.

Also passed:

- `TMPDIR=/var/tmp go test -count=1 -shuffle=on ./...` — every package and all 400 mutants; contract package 97.041 seconds.
- `go test -race -count=1 ./internal/core -run '^Test(Integrity|APhoto|Place|AutomaticEmbed)'` — focused race check, 13.500 seconds.

Staticcheck and govulncheck were not found on PATH or in the inspected local tool directories and were not installed. Windows execution, full race, extended fuzz and lifetime media-scale runs were not performed; this change claims no coverage from those environments. The canonical pre-freeze init and embedded copy are updated; no existing database was migrated and no migration runner was added.
