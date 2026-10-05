# 047 — Fill the missing day when an embed ghost becomes a file

> Executor: work from the repository root, keep promotion identity/text intact, and run every gate. Set the index row to IN REVIEW (owner) after verification.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/core/files.go internal/core/files_test.go internal/core/places_test.go internal/api/files_test.go`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P2. **Effort:** S. **Risk:** LOW (fill NULL day only).
- **Status:** TODO. **Depends on:** none. **Category:** bug. **Audit finding:** 14.

## Why

A day-page embed can create a plain, empty target before the original file is kept. Keeping that original promotes the target in place, preserving its id/backlinks, but never fills its missing `pages.day`. The result is a kept file absent from the day's dated-file listing even though a fresh file with the same inputs gets the correct day.

## Current state and conventions

`internal/core/files.go:96–102` chooses explicit day, then EXIF day, otherwise `Today()` with `own=false`. The free-title branch at line 134 stores that day:

```go
if k.ID, _, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), day, f.Body, ""); err != nil {
    return k, err
}
```

The existing plain-page branch changes entity type/revival and possibly body, but never `pages.day`. `internal/core/read.go:193` selects dated files by `p.day = :day`. `TestAddFilePromotesAGhost` creates a ghost through capture and checks id/title/body/backlinks, not its day.

Honor [D9](../decisions/D09-binary-files.md): a file's page carries its own day when known, otherwise the day kept. [D21](../decisions/D21-location-history.md) separately requires a day of the photo's own for automatic embedding/location links. Filling the file page's fallback date must not turn an undated photo into a dated-photo link.

## Scope

Only `internal/core/files.go`, `internal/core/files_test.go`, `internal/core/places_test.go`, `internal/api/files_test.go`, and plan/index status. No schema/migrations, body merging, hash identity changes, title renames, existing non-NULL day replacement, source metadata storage, or batch report redesign. Synthetic photos/databases only.

## Commands

- Core: `go test -mod=readonly -count=1 ./internal/core -run 'TestFilePromotionDay|TestAddFilePromotesAGhost|TestAPhotoWithNoDayLinksNoDay'`.
- API: `go test -mod=readonly -count=1 ./internal/api -run 'TestPromotedFileDay|TestAPhotosPlaceAndDay|TestAddFileDryRunWritesNothing'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestFilePromotionDay` using `fresh`, `sha`, `smallJPEG` and the ghost-capture pattern. Keep a ghost with an explicit day and another with `Taken`; assert `PageByID.Day` and inclusion in `Store.Day`'s dated pages, stable id/title/body and existing backlinks. Test an undated keep uses the local capture-day fallback but creates no automatic photo-day/location link. Avoid a midnight race by sampling `Today()` before and after and accepting only those bracketed dates.
   **Verify:** core command → missing-day promotion assertions fail; the fresh-file and no-own-day behavior remains valid.
2. Within the existing promotion transaction, fill `pages.day` only when it is SQL NULL, using the already selected `day`. Keep the entity promotion, file insert, body conflict checks and link synchronization in the same transaction. Do not override an existing non-NULL date or infer a date from an embed's referring page. Preserve `own` when calling `placeAndDay`.
   **Verify:** core command → all pass, including original-text preservation and undated-photo behavior.
3. Add regressions for re-send/hash deduplication and `TryAddFile`: dry run must report what would happen while leaving the ghost plain and undated in storage. Add `TestPromotedFileDay` through the add-file API with synthetic inputs; verify the returned page and day view agree. A body conflict must leave type/day/file rows unchanged.
   **Verify:** API command, core command, then final command → exit 0.

## Done criteria

- [ ] Promoted NULL-day ghosts receive explicit/EXIF/fallback date exactly as fresh file pages do.
- [ ] Their id, handle, text and existing backlinks remain intact; no non-NULL date is overwritten.
- [ ] An undated photo still creates no inferred own-day embedding/at link; deduplication does not change.
- [ ] Dry run and conflict rejection leave stored ghosts unchanged; API/day-view regression passes.
- [ ] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if existing non-NULL dates must be reconciled with different EXIF/explicit dates: that needs an owner policy and is not this missing-day fix. Also stop if a day must be inferred from referrers, bodies must be merged, or promotion would change a permanent handle/provenance.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit as `files: fill promoted ghosts' missing day (plan 047); ran go generate, go vet and go test`; push only when instructed. Keep file-page capture dates separate from the “photo has its own day” flag used for automatic journal/place links.
