# 058 — Preserve mutation feedback across browser redirects

> Executor: keep Post/Redirect/Get and the JSON response contract. Use synthetic pages/photos; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/api/handler.go internal/api/html.go internal/api/html/layout.html internal/api/html_test.go internal/api/files_test.go internal/api/feedback.go internal/api/feedback_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (feedback privacy and bounded temporary state).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize handler edits with 061/064.
- **Category:** bug. **Deep-audit finding:** 12. **Confidence:** HIGH, source-confirmed; new browser flow not executed.

## Why

Mutation results contain important information: revived targets, skipped links, dry-run outcomes and photos whose positions need an owner's answer. Browser POSTs with a self link immediately redirect, discarding that result. The user sees the destination page but not the operation's qualifications.

## Current state and conventions

`internal/api/handler.go:129–133`:

```go
if wantsHTML(r) {
    http.Redirect(w, r, self, http.StatusSeeOther)
    return
}
```

`saveBody` and `addFile` populate `Entity.Result`; a fresh GET cannot reconstruct that transient information. `html/layout.html` renders `.Result` only in the generic view. Follow `TestBrowserKeepsWhatAFailedSaveSent` and existing escaped `html/template` rendering. The no-JavaScript/shared-Siren design stays; do not move private results into URLs or a database table.

## Scope

Only paths in the drift command plus plan/index. `feedback.go`/`feedback_test.go` may be new. No JavaScript framework, persistent sessions, cookies containing result data, new DDL, altered JSON mutation shape or authentication redesign.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/api -run 'TestBrowserMutationFeedback|TestBrowserKeepsWhatAFailedSaveSent|TestEveryViewRenders|TestAddFile'`.
- Package: `go test -mod=readonly -count=1 ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestBrowserMutationFeedback`: save text that revives a synthetic target, save text with rejected targets, and upload a photo needing a place answer. Follow the 303 and assert visible escaped feedback. Keep dry-run/no-self and JSON controls; refreshing the GET must never resubmit a mutation.
   **Verify:** focus command → baseline followed-redirect feedback assertions fail.
2. Add bounded per-server ephemeral result receipts, referenced by an opaque random token in the redirect and bound to the destination path. Store only feedback needed for presentation, not the whole page/request. Use expiry, an entry cap and a payload cap; suggested initial bounds are 10 minutes, 64 entries and 64 KiB per receipt. Large lists need an explicit truncated-summary notice, not silent loss. No bodies, titles, coordinates or result JSON in the URL, logs or cookie.
   **Verify:** receipt unit tests in the focus command → unknown/expired/wrong-target tokens disclose nothing; caps/eviction work under an injected clock.
3. Consume the receipt on the redirected GET and render one shared feedback component for specialized as well as generic views. Preserve safe URL handling and template escaping; never trust a query parameter as a success message. Ordinary GETs remain stateless without a valid receipt; JSON clients retain the immediate result.
   **Verify:** focus command → browser feedback survives redirect, is not replayed indefinitely, and arbitrary token/message input cannot inject content.
4. Cover concurrent independent tabs, server restart/expired receipts (page still usable), dry runs, errors preserving submitted text, and bounded-list notices. Explain transient receipt behavior in code comments without inventing a storage-contract rule.
   **Verify:** package and final commands → exit 0; scope-only diff.

## Done criteria

- [x] Revival/skipped-link/unmatched-photo feedback is visible after browser POST redirects.
- [x] Refresh never replays writes; invalid/expired tokens reveal no feedback.
- [x] Temporary state is bounded/expired and private payloads are absent from URLs/logs/cookies.
- [x] JSON/errors/dry runs remain correct; all gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): destination-bound random one-use receipts retain escaped result previews for up to ten minutes, 64 entries and 64 KiB each; larger previews visibly say they are truncated. Synthetic followed-303, independent-tab, expiry/restart, unknown-token, JSON and dry-run tests pass. Scope expansion approved for `internal/api/api_test.go` redirect assertions: validate unchanged local destination plus opaque token rather than bare Location equality. Focus/package and integrated generation/vet/uncached full tests passed. Receipts are transient bearer state, not authentication; full-result marshaling is transient and the stored preview is bounded. No browser automation was run.

## STOP conditions

Stop if this requires persistent sessions, a new table or losing PRG. If receipt limits cannot retain essential feedback even as an explicit summary, report a design choice rather than silently discarding it. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `browser: retain mutation feedback after redirects (plan 058)`, listing checks. Every new result-bearing mutation needs both JSON and followed-redirect tests.
