# 043 — Use canonical metric identity in derived reading keys

> Executor: this changes import identity. Complete the legacy-reference compatibility gate before changing key generation. Work from the repository root, use only synthetic databases/workspaces, and set the index row to IN REVIEW (owner) after full verification.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/importer/facts.go internal/importer/apply.go internal/importer/replay.go internal/importer/corrections.go internal/importer/importer_test.go internal/importer/replay_test.go internal/importer/corrections_test.go internal/core/imports.go internal/core/core_test.go README.md docs/guides/importing.md`. Rebaseline the explicit prerequisite changes; STOP on other unexplained drift.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** L (legacy compatibility, not just a casefold call). **Risk:** HIGH (immutable keys and correction references).
- **Status:** IN REVIEW (owner). **Depends on:** [037](037-import-correction-lineage.md), [038](038-reading-source-evidence.md), [041](041-durable-import-corrections.md), [044](044-source-filename-identity.md). **Category:** bug. **Audit finding:** 10.

## Why

Metric lookup and approval resolve a title through NFC/casefold, but reading keys embed the raw submitted metric spelling. A re-run using another capitalization can therefore insert the same measurement again. Canonicalize both timed keys and untimed grouping, while continuing to recognize existing roots and correction references. Existing facts are immutable: do not “repair” them with UPDATEs.

## Current state and conventions

`internal/importer/facts.go:417` approves with `approved[text.TitleKey(r.Metric)]`; `core.Record` also looks up `text.TitleKey(m.Metric)`. `readingKeys` at lines 474–477 instead uses:

```go
keys[i] = strings.Join([]string{f.File, "reading", r.Metric, r.Day, r.TakenAt}, "|")
// ... when there is no instant ...
g := r.Metric + "|" + r.Day
```

Untimed keys rank quote positions within each raw-name/day group. `apply.go` checks `KeyedValue` under that exact derived key. `MeasurementByKey` in `core/imports.go` resolves correction roots by exact source/key and folded metric. An existing key such as `Medical/Ferritin.md|reading|Ferritin|2031-03-01|1` must remain discoverable even when new derivation uses the folded metric token.

Use `TestReadingsKeysAndReplay` for reordered/untimed facts, `TestRepeatedImportedCorrectionsReplay` from plan 037, and plan 041's legacy/event recovery tests. The [import guide](../guides/importing.md) requires writer-derived identical keys and replayable corrections. [D13](../decisions/D13-migrations-and-freeze.md) permits owner-led import-only rebuilds before freeze, never a migration runner.

## Scope

Only the drift-check paths, the status/API test call-site updates required by the canonical key change, and plan/index status. No DDL/migrations, measurement UPDATE/DELETE, title-key algorithm changes, model-supplied keys, path normalization changes, row merging, unit conversion, source-position algorithm replacement, or real data. Preserve raw facts text and approvals.

## Legacy compatibility policy

The compatibility seam is one immutable-key alias layer, never an UPDATE or merge of old facts. The writer validates the whole checked reading group before accepting any row, including a row whose stored key exactly equals the newly derived canonical key. Exact textual key equality is not sufficient when historical mixed-spelling untimed groups could have made that key refer to a different source-position identity, or when a canonical root and a legacy root both match one identity. Original root value corroborates an identity; it is never the sole selector.

A reading may be accepted only when the checked source facts and original root row metadata prove a bijection for the complete source-file/canonical-metric/day group. Portable metadata are the import source, source file, canonical metric title key, local day, timed instant when present, time zone when present, captured-with page identity rather than rebuilt database row id, and original root value or retraction. Corrections and durable correction events keep their stored root keys, immutable intent files, and legacy fingerprints; replay resolves those root references through the same in-memory one-to-one alias map before correcting, without mutating the identity used to verify an existing event row or fingerprint. Current leaf values after owner corrections are never alias evidence.

Ambiguous historical groups fail closed with a diagnostic that names the source file and reading group. In particular, mixed raw spellings that formerly had separate untimed ordinal groups and now collapse into one canonical ordinal group are not auto-repaired when the stored key could name another source-position identity or when more than one historical root matches the checked facts. Untimed readings in one canonical source-file/metric/day group also fail closed when two facts have the same source quote position; the writer keeps the current source-position ranking algorithm and does not guess from facts-file order or values. Distinct quote positions continue to rank normally. Rehearsal reports the ambiguous root before the real target is opened for write, leaving the existing target and ledger unchanged. Repeated apply/replay must leave total measurement row counts unchanged.

## Commands

- Identity: `go test -mod=readonly -count=1 ./internal/importer -run 'TestReadingKeyIdentity|TestReadingsKeysAndReplay|TestRepeatedImportedCorrectionsReplay|TestLegacyReadingKeyCompatibility'`.
- Integration: `go test -mod=readonly -count=1 ./internal/core ./internal/importer ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestReadingKeyIdentity`: a single source, source spelling changes only, timed and untimed readings, ASCII case plus NFC/casefold-equivalent Unicode names, and reordering. Assert the second apply adds zero total measurement rows. Add an untimed source whose fact spellings vary within one canonical metric/day group; quote order, not spelling or facts-file order, must tell distinct readings apart.
   **Verify:** identity command → case-spelling and canonical-group regressions fail on current derivation.
2. Before implementation, add `TestLegacyReadingKeyCompatibility` using old-format roots/keys inserted synthetically through the existing writer operations. Include root corrections, retractions and plan 041 event references. Cover old single-case groups and conflicting/mixed-case groups whose ordinals can no longer be mapped unambiguously. Specify the compatibility outcome in this plan: exact legacy matches first, proven one-to-one aliases next, refusal on ambiguity. Do not choose a row by matching only its current value or timestamp.
   **Verify:** `go test -mod=readonly -count=1 ./internal/importer -run TestLegacyReadingKeyCompatibility` → fixtures execute and establish the current behavior; the intended compatibility assertions may be red until steps 3–4. STOP for reviewer agreement if a legacy-to-new identity mapping is not provable from original facts/metadata.
3. Change `readingKeys` to use `text.TitleKey(r.Metric)` in the timed token and untimed group. Keep the source path, local day, instant and quote-position ranking unchanged. Add a private compatibility resolver in apply: resolve existing rows for the same source, canonical metric and source-file reading identity before inserting. Preserve the existing stored key when an unambiguous legacy row is found and compare the original root value, not a corrected leaf. If multiple historical keys might represent the identity, refuse with a diagnostic rather than merging or inserting a third row.
   **Verify:** identity command → new/casefold-equivalent and supported legacy applies add zero rows on repeat; ambiguous fixtures refuse with no ledger/database mutation.
4. Resolve legacy correction references during rehearsal/replay too. Build any alias mapping from checked source facts and the trial's original root metadata, and require one-to-one correspondence to a target reading; current numeric value alone is insufficient evidence. Retain old JSON/event records and their root keys. A missing/ambiguous mapping must be reported in rehearsal before touching the target. Rebuild synthetic targets from fresh canonical DDL and confirm latest correction/retraction plus zero row growth on replay two.
   **Verify:** integration command → legacy roots and both correction formats survive fresh replay; ambiguity blocks the real run and does not mutate the target.
5. Update the application's imported-key description and clarify the guide's identity terminology only as needed, language-neutrally. Include an owner-local snapshot/rehearsal warning for ambiguous historical keys; no code or hosted agent opens or repairs a canonical database automatically.
   **Verify:** final command → exit 0; canonical/embedded DDL diff is empty.

## Done criteria

- [x] Casefold/NFC-equivalent names yield the same new reading identity and canonical untimed grouping.
- [x] Supported old keys remain stable/discoverable; no stored fact/provenance is rewritten.
- [x] Legacy and event correction references survive fresh replay; repeat apply/replay adds zero measurement rows.
- [x] Ambiguous historical groups fail closed during rehearsal rather than merging or dropping readings.
- [x] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if source-position identity is already ambiguous, legacy grouping cannot be mapped one-to-one, the database has duplicate historical roots needing owner adjudication, or compatibility appears to require a migration/new schema field. Record the synthetic case and ask the owner; do not guess a mapping or access real workspaces.

## Git workflow and maintenance

Use an isolated operator-selected branch/worktree. Commit as `imports: canonicalize reading identity (plan 043); ran go generate, go vet and go test`; push only when instructed. Any future key change must test old correction references and use total-row idempotency assertions, not only equality of current readings.
