# 053 — Require valid EXIF hemisphere references before trusting GPS

> Executor: use hand-built EXIF/JPEG/HEIC fixtures only, never personal photos. Set plan/index to IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/photo/photo.go internal/photo/photo_test.go internal/photo/phototest/phototest.go internal/api/files_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** S. **Risk:** LOW (malformed GPS becomes unknown).
- **Status:** IN REVIEW (owner). **Depends on:** none; retain plan 042's bounded HEIF parsing.
- **Category:** bug. **Deep-audit finding:** 7. **Confidence:** HIGH, source-confirmed; new malformed-reference cases not run.

## Why

Latitude/longitude rationals are unsigned magnitudes; the references supply their hemispheres. Missing or invalid references currently default to north/east. An otherwise malformed photo can then assign a wrong place point or automatic journal-place link, persisting false location facts.

## Current state and conventions

`internal/photo/photo.go:333–344` reads the magnitudes and only negates recognized prefixes:

```go
if strings.HasPrefix(gps[0x0001].ascii(t, bo), "S") {
    lat = -lat
}
if strings.HasPrefix(gps[0x0003].ascii(t, bo), "W") {
    lon = -lon
}
```

Successful rationals/ranges alone set `HasGPS`. `internal/photo/phototest/phototest.go` builds TIFF in either byte order and wraps it in synthetic JPEG/HEIC. Follow existing `photo_test.go` cases and `TestAddFileKeepsMalformedHEIFWithUnknownMetadata`. [D9](../decisions/D09-binary-files.md) and [the photo recipe](../cookbook/place-of-a-photo.md) keep originals outside and use metadata only when known.

## Scope

Only `internal/photo/photo.go`, `internal/photo/photo_test.go`, `internal/photo/phototest/phototest.go`, `internal/api/files_test.go`, and plan/index. No new metadata fields, source-coordinate storage, EXIF rewrite, image decoder/dependency change, schema or migrations.

## Commands

- Parser: `go test -mod=readonly -count=1 ./internal/photo/...`.
- API: `go test -mod=readonly -count=1 ./internal/api -run 'TestPhotoGPSReferences|TestAddFileKeepsMalformedHEIF|TestAPhotosPlaceAndDay'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add cases with each reference absent, empty, wrong axis, unexpected text, wrong TIFF type/count or truncated data. Add all four valid hemisphere combinations, standard NUL termination, zero on one axis and the existing 0,0 rejection. Cover both TIFF byte orders and JPEG/HEIC wrappers.
   **Verify:** parser command → new malformed-reference assertions fail on baseline; valid metadata tests pass.
2. Require a valid latitude reference N/S and longitude reference E/W before publishing GPS. Validate the complete field rather than a prefix, allowing only the format's defined termination. Keep orientation and taken-day extraction even if GPS is invalid; leave `HasGPS=false` rather than manufacturing a sign or refusing to keep the entire original.
   **Verify:** parser command → all cases pass with correct signed coordinates and unknown malformed GPS.
3. Add `TestPhotoGPSReferences` through file upload using synthetic metadata. A malformed reference must not create a place point or automatic `at` link; keeping text/preview and known own-day embedding remains possible under existing rules. Valid south/west metadata must still match the intended place.
   **Verify:** API and final commands → exit 0; scope-only diff.

## Done criteria

- [x] GPS is published only with complete valid magnitude/reference pairs.
- [x] Malformed GPS never persists a guessed place point/link; unrelated metadata/file keeping survives.
- [x] Valid hemispheres, byte orders and existing bounded HEIF cases pass.
- [x] All gates pass; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): synthetic EXIF fixtures exercise missing/malformed reference pairs and both byte orders; file keeping remains independent of invalid GPS. Focus/package gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if a real format requires a nonstandard reference policy or the fix needs to reinterpret existing stored coordinates. Ask for evidence; never auto-repair old places. Stop on unexplained drift, out-of-scope edits or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `photos: validate GPS hemisphere references (plan 053)`, listing checks. Metadata fields that jointly encode a fact must be validated as a group, not independently defaulted.
