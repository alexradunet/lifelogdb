# 042 — Parse HEIF item locations correctly and within bounds

> Executor: read fully, use synthetic metadata only, and run every gate from the repository root. Set the index row to IN REVIEW (owner) after verification. This plan does not add a HEIC image decoder.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/photo/photo.go internal/photo/photo_test.go internal/photo/phototest/phototest.go internal/api/files_test.go`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** M. **Risk:** MED (valid metadata compatibility).
- **Status:** TODO. **Depends on:** none. **Category:** bug / security. **Audit finding:** 9.

## Why

Photo metadata determines the day and place of a kept photo. The version-2 `iloc` reader currently consumes a 16-bit field and then another 32-bit field, instead of choosing one width. The extent check adds unsigned offsets/lengths before comparing with the available bytes; wraparound can bypass the check and panic during slicing. Invalid metadata must remain unknown, not crash the CLI/API or invent a date/place.

## Current state and conventions

`internal/photo/photo.go:180–187`:

```go
count := r.n(2)
if v == 2 {
    count = r.n(4)
}
for range count {
    id := r.n(2)
    if v == 2 {
        id = r.n(4)
    }
```

At lines 199–209:

```go
off, n := base+r.n(offSize), r.n(lenSize)
// select file or idat ...
if r.bad || n == 0 || off+n > uint64(len(src)) {
    return nil
}
out = append(out, src[off:off+n]...)
```

`reader.n` sets `bad` when bytes are missing. `Read` returns unknown metadata/default orientation rather than an error. `phototest.HEIC` currently builds version 1, including both `mdat` and `idat` constructions; it is metadata only, not a real decodable photo. `TestReadsAJPEGAndAHEIC` and `TestSaysNothingItDoesNotKnow` are the exemplars. The API reads at most the original's first megabyte for metadata.

Honor [D9](../decisions/D09-binary-files.md) and [D21](../decisions/D21-location-history.md): original files remain external, GPS is not stored, and a location comes only from metadata actually read. Manual JPEG previews for HEIC/video remain the supported picture path.

## Scope

Only `internal/photo/photo.go`, `internal/photo/photo_test.go`, `internal/photo/phototest/phototest.go`, `internal/api/files_test.go`, and plan/index status. No schema, migrations, new codec/library, preview redesign, photo-library inventory, real photos, or coordinates retained in fixtures from real data.

## Commands

- Metadata: `go test -mod=readonly -count=1 ./internal/photo`.
- Consumers: `go test -mod=readonly -count=1 ./internal/api ./internal/preview ./cmd/lifelog`.
- Fuzz: `go test -mod=readonly ./internal/photo -run '^$' -fuzz '^FuzzRead$' -fuzztime=10s` → no panic/unbounded allocation; fuzz artifacts stay synthetic.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Extend the synthetic builder or add explicit metadata-box fixtures for `iloc` versions 0, 1 and 2, with nontrivial item ids, both supported construction methods and multiple extents. Add `TestHEIFLocationVersions` and `TestHEIFLocationBounds`. Bound tests include truncated fields, unsupported versions/sizes/methods, base-plus-offset overflow, extent-end overflow and repeated extents. Also cover an available Exif item after a non-target image entry whose payload lies outside the supplied metadata head. Use small buffers; never allocate the size claimed by malformed metadata.
   **Verify:** metadata command → version-2 and overflow cases expose the current bug; existing JPEG/version-1 fixtures remain valid.
2. Select field widths before consuming bytes: count/id are 32-bit only for version 2, 16-bit otherwise. Refuse unsupported versions and unrepresentable field widths. Parse non-target entries only far enough to advance the cursor; do not reconstruct their payload or let an unavailable image payload hide an available Exif item. For the requested item, check base/relative offset addition before computing the sum, then use subtraction-based bounds: offset must fit the source and length must fit its remainder. Check aggregate reconstructed length before appending. Bound metadata reconstruction to an explicit small package budget compatible with the API's 1 MiB metadata head; reject oversized/repeated construction rather than allocating according to its claims.
   **Verify:** metadata command → all cases pass; existing method-0/method-1 metadata is unchanged. Add a boundary fixture at the selected budget.
3. Add `FuzzRead` with synthetic JPEG/HEIF/empty/truncated seeds and an API multipart regression for malformed HEIF metadata. The API should still hash/keep a well-formed file request with unknown metadata and no derived location; bad metadata must not become a panic or guessed GPS/day.
   **Verify:** fuzz and consumers commands → exit 0. If fuzzing finds a failure, minimize it into a synthetic unit regression before fixing it.
4. Keep all metadata limits implementation-local and explain the available-head limitation in a code comment citing D21. Do not introduce a database field or claim full-file metadata support.
   **Verify:** final command → exit 0.

## Done criteria

- [ ] Supported versions read fields once at the proper width; ids and both construction methods are covered.
- [ ] Arithmetic is checked before slicing/allocation; truncated/oversized metadata safely returns unknown.
- [ ] Synthetic fuzz seeds and the multipart consumer regression pass; valid JPEG/HEIF behavior remains intact.
- [ ] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if public format evidence contradicts the fixture layout, a valid supported item needs more than the proposed metadata budget, or the fix seems to require decoding HEIC pixels/storing coordinates. Do not silently treat a guessed position or partial malformed field as valid metadata.

## Git workflow and maintenance

Use an operator-selected branch/worktree and commit as `photos: bound HEIF metadata locations (plan 042); ran go generate, go vet and go test`. Push only when instructed. New binary layouts require independent synthetic width/bounds tests; fuzzing complements those fixtures rather than replacing them.
