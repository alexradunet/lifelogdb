# 064 — Bound JSON and multipart metadata buffering

> Executor: bound buffered metadata without capping streamed originals as if they were JSON. Use generated synthetic readers/files. Set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/api/handler.go internal/api/files.go internal/api/api_test.go internal/api/files_test.go internal/api/request_limits_test.go README.md`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (long transcript and upload compatibility).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize handler edits with 058/061.
- **Category:** security/bug. **Deep-audit finding:** 20. **Confidence:** HIGH for absent bounds; peak memory not measured.

## Why

JSON action bodies are decoded without a size bound. Multipart text fields have a per-field cap but arbitrary unique field names can accumulate many capped buffers in a map. A local HTTP request can consume disproportionate memory. Bound the retained data while preserving large-original streaming, which is deliberate.

## Current state and conventions

`internal/api/handler.go:202` decodes directly from `r.Body`. `internal/api/files.go:71–102` loops over parts and does:

```go
b, err = io.ReadAll(io.LimitReader(part, maxField+1))
// ... per-field length check ...
vals[name] = string(b)
```

`maxField` is 16 MiB; `maxPicture` is 64 MiB. Originals are hashed as streams, with bounded preview/metadata prefixes. Follow `TestAddFileFromAnAgentAndWithAPicture`, `TestAddFileDryRunWritesNothing` and `TestBrowserKeepsWhatAFailedSaveSent`. File originals remain outside the database under [D9](../decisions/D09-binary-files.md).

## Scope

Only paths in the drift command plus plan/index. New `request_limits_test.go` is permitted. No general rate limiter, global cap on original-file bytes, file storage, authentication change, DDL or dependencies.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/api -run 'TestRequestBufferLimits|TestMultipartAggregateLimits|TestAddFile|TestBrowserKeepsWhatAFailedSaveSent'`.
- Package: `go test -mod=readonly -count=1 ./internal/api ./cmd/lifelog ./internal/mcp`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add boundary tests with readers generating data incrementally: JSON at/above a limit, absent or misleading Content-Length, chunked input, many individually legal multipart fields, repeated original/preview parts and long transcripts. Assert refusal before any page/measurement/file write. Include a large streamed original that still succeeds with bounded metadata.
   **Verify:** focus command → baseline accepts the newly oversized aggregate cases.
2. Define named finite budgets. Proposed defaults: 20 MiB encoded JSON body, existing 16 MiB per text field plus 16 MiB aggregate multipart text, at most 64 parts, and at most one original/preview each. Check real synthetic fixtures fit before fixing these defaults; do not shrink picture limits accidentally. Apply the JSON limit to actual reads, not only headers, and require EOF after one object so a suffix cannot bypass the limit.
   **Verify:** focus command → just-under/at/over cases behave predictably; overflow returns a structured 413, not a panic/500 or partial success.
3. Count multipart parts and all text bytes, including repeated/unknown fields, before retaining them. Reject duplicate binary parts and avoid unbounded buffering of unknown fields. Close parts/bodies on early exit. Keep originals streaming and existing bounded preview paths; no blanket `MaxBytesReader` cap over the whole file upload.
   **Verify:** focus command → many-small-fields/duplicate cases refuse without writes; generated large-original control passes.
4. Cover JSON, URL-encoded and browser error rendering, checking limit errors survive wrappers. Document limits in application README only. Do not duplicate them in the language-neutral database contract.
   **Verify:** package and final commands → exit 0; scope-only diff.

## Done criteria

- [x] Actual JSON reads, multipart part count and aggregate text retention have tested finite bounds.
- [x] Over-limit requests return useful 413 errors and leave the database untouched.
- [x] Legitimate long transcripts/previews and arbitrarily large streamed originals retain their intended behavior.
- [x] All gates pass; limits documented; index IN REVIEW (owner).

Implementation evidence (2026-10-05): generated JSON/form boundaries, multipart aggregate/part/duplicate limits, untouched-database assertions and a 65 MiB streamed-original control pass. Parent review found unknown multipart field names were retained outside value-byte accounting; a four-by-64-KiB generated-name regression reproduced it. Only the eight recognized text fields are now retained under short canonical keys; unknown values are discarded while still counting their bytes and parts. A focused internal projection test was added within the approved follow-up test scope. Focus/package, worker full generation/vet/tests and integrated follow-up tests passed; application limits are documented in README.

Limits are per request, not rate/concurrency limits. Go's multipart MIME-header budget is per part, not a whole-request wire-byte or exact memory ceiling; one excess part's headers are parsed before the part-count refusal. Originals remain unlimited streams. The synthetic 65 MiB control demonstrates streaming, not every possible size.

## STOP conditions

Stop if supported inputs exceed the proposed metadata budgets; present measured synthetic requirements before choosing different values. Stop if a fix would buffer originals whole or alter file identity. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `api: bound request metadata buffering (plan 064)`, listing checks. New buffered fields count against aggregate budgets even if their individual limits are small.
