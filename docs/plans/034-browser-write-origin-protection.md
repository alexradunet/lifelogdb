# 034 — Reject cross-origin browser writes

> Executor: read this entire plan, work from the repository root, and run every gate. Do not implement other audit findings. Stop on the conditions below. Set the index row to IN REVIEW (owner) when finished; the owner accepts and retires completed plans.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/api/handler.go internal/api/api_test.go internal/api/replay_test.go README.md`. Compare the excerpts with live code before editing. Unexplained drift is a STOP condition.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** S. **Risk:** MED (browser and in-process client compatibility).
- **Status:** TODO. **Depends on:** none. **Category:** security. **Audit finding:** 1.

## Why

The API intentionally has no authentication and binds to loopback by default. That does not make browser-origin requests trustworthy. Every POST currently reaches the mutation handler without checking request origin. Add request-authenticity protection without turning this single-user application into an authentication system.

## Current state and conventions

`internal/api/handler.go:31–95` constructs the common handler used by HTTP, CLI and MCP:

```go
post := func(p string, f func(*http.Request, string) (*Entity, error)) {
    m.HandleFunc("POST "+p, h.serve(func(r *http.Request) (*Entity, error) {
        src, err := source(r)
        if err != nil {
            return nil, err
        }
        return f(r, src)
    }))
}
// ... all ordinary and optional import routes ...
return m
```

`source` defaults to `ui` or `api`; it is provenance, not authentication. `form` accepts ordinary browser forms. `TestBrowserGetsHTMLAndSourceUI` in `internal/api/api_test.go:104` is the browser/redirect exemplar; `fresh` creates a temporary database and returns the handler and in-process client. Errors use `core.Error` and the Siren/HTML response machinery.

The module requires Go 1.27.1. `go doc net/http.CrossOriginProtection` confirms the standard-library guard permits safe methods and clients without browser-origin headers, and rejects unsafe cross-origin requests. Preserve the local, unauthenticated design documented in `README.md`; this is application request protection, not a schema change.

## Scope

Modify only `internal/api/handler.go`, `internal/api/api_test.go`, `internal/api/replay_test.go`, `README.md`, and this plan/index status. No authentication, cookies, configurable trusted origins, CORS expansion, dependency changes, schema edits, or migrations. Never inspect a real database or workspace; use synthetic data under `t.TempDir()`.

## Commands

- Baseline/final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...` → exit 0.
- API: `go test -mod=readonly -count=1 ./internal/api` → all pass after the fix.
- Guard reference: `go doc net/http.CrossOriginProtection` → documented API available.

## Steps

1. Add `TestBrowserOriginProtection` alongside the existing browser test. Exercise same-origin browser requests, unsafe cross-origin requests using origin/fetch metadata, malformed origin, no-browser-header CLI requests, and safe GET/HEAD. Assert denied POSTs return 403 and create no day page or reading. Include one workspace POST using the synthetic setup of `TestReplayDryRunAction`; protection must cover mounted import routes too.
   **Verify:** `go test -mod=readonly -count=1 ./internal/api -run TestBrowserOriginProtection` → fails specifically because unsafe cross-origin requests are accepted, not because fixtures are invalid.
2. Wrap the completed mux in one `http.CrossOriginProtection` guard in `api.New`. Use its deny-handler hook to return the existing error entity with 403, so non-browser clients still receive Siren errors. Do not add bypasses or trusted origins. Do not treat `Lifelog-Source` as an authorization credential.
   **Verify:** `go test -mod=readonly -count=1 ./internal/api` → all pass, including ordinary browser redirects and in-process calls.
3. Update only the application's local-serving paragraph in `README.md`: mention browser-origin protection and its non-browser-client compatibility. Leave the language-neutral database contract unchanged.
   **Verify:** run the baseline/final command → exit 0.

## Test plan and done criteria

- [ ] `TestBrowserOriginProtection` covers ordinary and import mutations with no database changes on denial.
- [ ] Same-origin browser forms and headerless in-process clients still work; GET/HEAD remain readable.
- [ ] Existing API, CLI and MCP-consuming tests pass under the full command above.
- [ ] `git diff --name-only` contains only the scope paths and plan/index metadata; canonical and embedded DDL are unchanged.
- [ ] Index status is IN REVIEW (owner).

## STOP conditions

Stop if the installed Go API differs from the documented guard, a legitimate supported client requires a bypass, a baseline failure is unrelated to this change, or any schema/dependency/authentication change appears necessary. Two unsuccessful attempts at a non-characterization gate require reporting rather than widening scope.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit one logical change, matching history: `api: protect browser writes (plan 034); ran go generate, go vet and go test`. Do not push implementation work unless separately authorized. Future unsafe methods must remain behind the common guard; reviewers should reject per-route bypasses and new GET mutations.
