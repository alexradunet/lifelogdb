# 029 — Make Takeout inventory complete, then scope one approved import family

> Executor: Phase A below is implementation-ready with synthetic fixtures. The direction brief for B/C is owner-gated design, not permission to inspect real exports or build all converters. No schema change, canonical replay or freeze is authorized. Set the Phase A index status separately from B/C.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/takeout/inventory.go internal/takeout/inventory_test.go cmd/lifelog/main.go cmd/lifelog/main_test.go README.md`. Compare changed symbols; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05; narrows/rebaselines the dated brief at `7c5391a`.
- **Priority:** P1. **Effort:** M for inventory; L is only a coarse estimate for a later selected converter.
- **Risk:** MED (false completeness and private-data disclosure).
- **Status:** Phase A IN REVIEW (owner); B/C BLOCKED (owner inventory, family selection and approvals).
- **Depends on:** none for inventory; import-safety acceptance before any real apply/replay.
- **Category:** bug + owner-gated direction. **Deep-audit finding:** 16; **direction option:** 1.
- **Confidence:** HIGH for source defects; real export formats/coverage are unknown. No real exports were inspected.

## Why and retained owner decisions

Inventory is the decision input for converter scope and overlapping-source rules. It must not omit a sibling phone Timeline export, describe unknown populated arrays as supported-empty, or miss exercise-family overlaps. Correct inventory is more useful than implementing hypothetical formats.

The owner's selected scope remains Timeline visits and Fit/Fitbit daily totals: day-to-place links, not a location track; sleep on the wake-up day; workouts as daily counts/minutes, not events; one selected source per metric/date range. The initial visit rule remains at least 15 minutes, excluding Home and excluding Work only if the owner chooses; tuning lives in approved workspace rules. Timeline conversion contributes day-to-place links, not raw coordinate storage. Missing journal days may get empty day pages. The owner extracts selected folders; the program reads folders, not archives. Photos are selected day-by-day from any source: **no archive-wide photo inventory, sidecar extraction or photo-gap measurement**. Tasks, finance and events remain out of scope.

## Current state and conventions

`internal/takeout/inventory.go:107–149` walks only `takeoutBase(folder)`, which returns `folder/Takeout` when present:

```go
if st, err := os.Stat(p); err == nil && st.IsDir() {
    return p
}
```

A sibling phone `Timeline.json` is therefore omitted. `addKnownFile` passes an empty metric family for Fit sessions, unlike Fitbit exercise. `jsonScanner.scanValue` marks a recognized array container supported before determining whether its nonempty children are recognizable records.

Follow `TestInventorySummarizesPhaseAWithoutLeakingValuesDataKeysOrFilenames`, `TestInventoryRecognizedEmptyContainersAreNotUnsupported` and `TestInventoryStreamsLargeRecordsAndIsDeterministic`. Reports contain public family names, aggregate counts, month ranges and sanitized shapes, never private values or filenames. `TestImportTakeoutInventoryDoesNotNeedDatabaseOrLeakValues` exercises the CLI without a database.

## Phase A scope and commands

Modify only `internal/takeout/inventory.go`, `internal/takeout/inventory_test.go`, `cmd/lifelog/main_test.go`, `README.md` if usage needs clarification, and this plan/index. `cmd/lifelog/main.go` is a read-only routing reference. No importer/core/DDL changes, real fixtures, archive readers, converters or new dependencies.

- Inventory: `go test -mod=readonly -count=1 ./internal/takeout` → all pass after fixes.
- CLI: `go test -mod=readonly -count=1 ./cmd/lifelog -run TestImportTakeoutInventory` → pass, no database needed.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check` → exit 0.

## Phase A steps and tests

1. Add synthetic regressions for an extraction root containing both `Takeout/` and sibling phone Timeline; direct `Takeout/` input; recognized empty arrays; populated unknown and mixed known/unknown records; Fit CSV/JSON sessions overlapping Fitbit exercise. Plant private marker values, dynamic keys and filenames in each fixture.
   **Verify:** inventory command → new completeness assertions fail on baseline; existing privacy assertions pass.
2. Separate physical traversal from family-relative classification. Inventory the selected extraction root once, unwrapping a `Takeout/` prefix for classification without excluding recognized siblings or double-counting descendants. Keep Photos excluded and unknown names folded into public buckets.
   **Verify:** inventory command → direct and wrapped layouts have expected equivalent family counts plus the sibling phone contribution; no duplicated records or leaked names.
3. Distinguish genuinely empty recognized containers from populated unsupported shapes. Report unsupported contents conservatively, including mixed cases, using existing report fields unless those cannot express the distinction. Attribute Fit sessions and Fitbit exercise to the same public overlap family only where timestamp evidence exists. Never invent overlap from a missing column or unrecognized record.
   **Verify:** inventory and CLI commands → all pass, with empty-container, unknown-shape, month-overlap and privacy assertions.
4. Run large synthetic streaming/determinism cases twice, confirm bounded parsing is retained, and clarify extraction-root usage in the application README only if necessary.
   **Verify:** final command → exit 0; only Phase A scope paths changed.

## Direction brief — one family end to end (B/C)

**Blocked until the owner shares sanitized inventory output and chooses one family.** The recommendation is the smallest populated, well-understood daily-total family; the owner may choose Timeline instead. No hosted agent may browse exports, workspaces or databases. The owner runs inventory locally and chooses what summary to share.

1. After Phase A acceptance, ask for the selected family, supported shape, units, local-day/timezone rule and metric/source/date-range policy. Record decisions in this plan, not private rows. If there is no supported populated family, stop rather than manufacturing converter demand.
   **Verify:** `git diff -- docs/plans/029-google-takeout.md` → contains only sanitized decisions and an explicit owner selection; otherwise status remains BLOCKED.
2. Write a separate, newly numbered implementation brief before converter work. Define deterministic converted Markdown, exact quotes, source identity, approved rules/metrics gates, questions for unsupported rows, and synthetic fixtures matching only the selected shape. Read the [import guide](../guides/importing.md) and [contract](../contract/imports.md); do not invent a second import engine or register metrics through agent tools.
   **Verify:** `go test -mod=readonly ./tests -run 'TestSuites/document$'` → pass; the new brief has explicit approval/dependency gates and no implementation changes.
3. Require that future brief to prove inventory → conversion → facts → check → apply → integrity → second-run no-op → fresh replay equivalence on synthetic data. Include midnight/DST/wake-day or source-overlap fixtures only as relevant to the selected family. Canonical replay remains a separate owner's action after import-safety plans are accepted.
   **Verify:** review the new brief's named tests/commands and owner approval before changing B/C from BLOCKED; planning alone is not that approval.

## Done criteria

- [x] Phase A detects sibling exports, unsupported populated shapes and actual exercise overlaps without leaking markers or filenames.
- [x] Empty known containers remain supported; streaming/determinism, CLI and final checks pass.
- [x] Phase A is IN REVIEW (owner); B/C remains BLOCKED unless separately approved.
- [x] No real data, photo-library scope, converter, schema change or canonical database write is introduced.

Implementation evidence (2026-10-05): synthetic inventory/CLI regression gates passed; reviewed integration passed generation, vet and uncached full tests. Traversal includes recognized extraction-root siblings; populated unknown/mixed records and timestamp-backed Fit/Fitbit exercise overlap are covered. B/C authorization is unchanged.

## STOP conditions

Stop on unexplained drift, a gate failing twice, privacy leakage, a needed report-schema change, or a format requiring owner data to understand. Unknown formats are inventory findings, not permission to inspect values. Stop if converter scope needs a new table/link kind: use the [change process](../process.md). Never auto-select overlapping sources or approve rules for the owner.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push without instruction. Suggested Phase A commit: `takeout: report complete inventory evidence (plan 029)`, listing checks. Keep inventory privacy tests adversarial as formats grow; public containers alone do not prove supported record shapes.
