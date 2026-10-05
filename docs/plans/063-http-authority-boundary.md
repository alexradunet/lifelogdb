# 063 — Verify and enforce the network HTTP authority boundary

> Executor: this is defensive hardening with an explicit policy gate. Do not claim a demonstrated browser exploit. Use synthetic HTTP handlers and no external DNS/browser services. Set plan/index IN REVIEW (owner) after approved policy and gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/api/handler.go internal/api/authority.go internal/api/authority_test.go cmd/lifelog/main.go cmd/lifelog/main_test.go README.md`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (legitimate listener/hostname compatibility).
- **Status:** BLOCKED (owner approval of allowed authorities and custom/wildcard/proxy policy). Step 1 characterized. **Depends on:** implemented 034 origin protection.
- **Category:** security. **Deep-audit finding:** 19. **Confidence:** MED; arbitrary Host acceptance is source-confirmed, DNS-rebinding exploitation untested.

## Why

Cross-origin write protection does not authenticate the hostname used to reach a local server. A same-origin request can use an arbitrary authority while reaching the loopback listener, and read routes also hold private data. Define a bounded network authority policy without reopening the accepted local unauthenticated architecture.

## Current state and conventions

`internal/api/handler.go:95–101` wraps routes in `http.NewCrossOriginProtection`, with no authority check. `cmd/lifelog/main.go:215` mounts that handler on the configured address. In-process clients intentionally use `http://lifelog.local` (`internal/client/client.go`), so a network-only guard must not break CLI/MCP dispatch.

Follow `TestBrowserOriginProtection` for existing request-authenticity expectations and `httptest` for synthetic request matrices. [The application README](../../README.md) documents loopback/no-auth deployment; [non-goals](../architecture/non-goals.md) do not authorize a new authentication service.

## Scope

Only paths in the drift command plus plan/index; `authority.go`/`authority_test.go` may be new. No CORS relaxation, authentication/TLS/proxy product, DNS lookups to decide trust, DDL or dependencies. Apply the guard at network mounting so in-process clients keep their existing contract.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/api ./cmd/lifelog -run 'TestHTTPAuthority|TestServeAuthority|TestBrowserOriginProtection'`.
- Packages: `go test -mod=readonly -count=1 ./internal/api ./cmd/lifelog ./internal/client ./internal/mcp`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Characterize network requests with configured loopback IP, localhost, IPv6 loopback, correct/wrong ports and an arbitrary synthetic authority. Confirm GET and POST reach the handler under current behavior, independent of origin checks. Record supported `--addr` forms from command parsing. This proves acceptance, not browser exploitability.
   **Verify:** focus command → characterization passes; future denial expectations fail on baseline. Stop for owner input if a supported custom/wildcard/proxy deployment needs an authority policy.
2. Adopt an explicit allowed-authority set for network serving: the concrete configured address and approved loopback aliases at the listener port. Parse host/port structurally; reject malformed/unapproved authorities before all routes, including reads/previews. Never trust Forwarded/X-Forwarded-Host or resolve arbitrary hostnames to “prove” locality. A wildcard bind must not imply wildcard Host trust; require an explicit owner-approved configuration or refuse that unsupported combination with a clear startup error.
   **Verify:** focus command → the approved host/port matrix passes and denied requests never invoke the inner handler.
3. Wrap network `serve` only; preserve the cross-origin guard inside it. Add origin/authority combinations, IPv6 formatting, case handling and configured-port tests. Keep in-process `lifelog.local` usable without granting it implicitly to the public network guard. Document the selected application policy in README, not the storage contract.
   **Verify:** packages and final commands → exit 0; ordinary CLI/MCP and existing origin tests remain green.

## Done criteria

- [ ] Approved authority policy is recorded; custom/wildcard cases are not guessed.
- [ ] Every network route rejects unapproved Host/port before reading or writing data.
- [ ] In-process clients and cross-origin write protection remain intact.
- [ ] All gates pass; report distinguishes handler evidence from untested browser exploitation; index IN REVIEW (owner).

## Implementation evidence — 2026-10-05

Step 1 ran on synthetic IPv4 and IPv6 loopback HTTP listeners: 72 cases covered nine authorities per listener and GET/headerless POST/matching-Origin POST/cross-origin POST. Arbitrary names and wrong ports reached GET and same-origin/headerless POST handlers; cross-origin POST remained 403. `--addr` parsing passes strings through (including custom/wildcard forms); parser acceptance is not proof of a successful bind or an approved deployment. No browser exploit, external DNS, proxy or non-loopback deployment was tested.

`go test -mod=readonly -count=1 -v ./internal/api ./cmd/lifelog -run 'TestHTTPAuthority|TestServeAuthority|TestBrowserOriginProtection'`, client/MCP package tests and `git diff --check` passed in the isolated investigation worktree. Its baseline-acceptance tests are deliberately not integrated as hardened policy tests. No production guard was changed.

**Owner decision still required:** allow only loopback IPs and `localhost` at the listener port, rejecting custom hostnames, wildcard binds and proxy deployments until separately approved? No approval has been inferred. Steps 2–3 and their full gates remain blocked.

## STOP conditions

Stop if supported deployments require unapproved aliases/proxy trust or robust enforcement cannot be placed at the network boundary. Do not weaken the guard to `allow all` to pass old fixtures. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `http: constrain network request authorities (plan 063)`, listing checks. New listener/proxy modes must define their authority boundary explicitly.
