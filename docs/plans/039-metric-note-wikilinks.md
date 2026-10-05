# 039 — Synchronize wikilinks in newly registered metric notes

> Executor: follow the gates from the repository root. Set the index row to IN REVIEW (owner), and do not change the body-save contract or metric identity.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/core/habits.go internal/core/categories_test.go internal/importer/importer_test.go`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P2. **Effort:** S. **Risk:** LOW (existing link-sync machinery).
- **Status:** IN REVIEW (owner). **Depends on:** none. **Category:** bug. **Audit finding:** 6.

## Why

A metric is a page and its note is that page's CommonMark body. Registering a fresh metric currently stores the body without extracting its links, while filling an existing ghost invokes the proper save machinery. The same note therefore produces different backlinks depending on whether its title existed beforehand.

## Current state and conventions

`internal/core/habits.go:25–31`, fresh-title branch:

```go
id, _, err := t.insertPage("metric", name, text.TitleKey(name), nil, note, "")
if err != nil {
    return false, err
}
_, err = t.tx.Exec(`INSERT INTO metrics(id, unit) VALUES (?, ?)`, id, unit)
return err == nil, err
```

The promotion branch calls `t.SetBody(p.ID, note)` only when the existing body is empty. `SetBody` in `write.go:266` calls `syncWikilinks`; that sync uses a savepoint per target and reports/skips invalid targets. `insertPage` itself does not synchronize bodies.

Use `TestMetricsArePagesFiledInCategories` in `internal/core/categories_test.go`, `fresh` and `titles` from `core_test.go`, and `TestAVaultNoteBecomesItsMetricsPage` in the importer suite. Honor [D27](../decisions/D27-a-metric-is-a-page.md) and the [save contract](../contract/titles-and-wikilinks.md): “saving a body keeps the page's links equal to what its text names”. Existing metric notes/text are not overwritten by re-registration.

## Scope

Only `internal/core/habits.go`, `internal/core/categories_test.go`, `internal/importer/importer_test.go`, and plan/index status. No global `insertPage` refactor, signature/API-result change, category redesign, DDL, migrations, or rewriting existing metric text. Synthetic databases only.

## Commands

- Core: `go test -mod=readonly -count=1 ./internal/core -run 'TestMetricNoteLinks|TestMetricsArePagesFiledInCategories'`.
- Importer: `go test -mod=readonly -count=1 ./internal/importer -run 'TestApprovedMetricNoteLinks|TestAVaultNoteBecomesItsMetricsPage|TestMetricsAreFiledFromTheirCategory'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestMetricNoteLinks`: register a fresh metric whose note names a person/page, a tag, a repeated target and an invalid target. Assert exact outgoing links and reciprocal backlinks. Repeat via ghost promotion and assert equivalent links; re-register with another note and assert the original text/links remain unchanged.
   **Verify:** core command → the fresh metric's link assertions fail, while the promoted metric uses the existing correct path.
2. In the fresh branch, insert the metric row and synchronize the note through the existing `SetBody`/`syncWikilinks` machinery before returning success. Preserve the existing registration return shape and transactional rollback on a real sync error. Do not add a second extractor or make invalid targets block registration. An empty note must remain a valid empty body.
   **Verify:** core command → all pass; metric row and links share the same transaction.
3. Add `TestApprovedMetricNoteLinks` using `fixture.metrics`' approved-registration pattern: a note with wikilinks/tags is registered, replayed to a fresh target, and replayed again. Assert identical graph content and zero link growth on repeat.
   **Verify:** importer command, then final command → exit 0.

## Done criteria

- [ ] Fresh and promoted metric notes produce the expected graph, including tags and deduplication.
- [ ] Re-registration does not replace the owner's body or links; invalid targets are skipped as on ordinary saves.
- [ ] Approved metric registration/replay remains idempotent; full verification passes.
- [ ] Only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if a global page-insert change is needed, if link-sync return data would require changing public registration responses, or if existing tests intentionally exempt metric bodies from the save contract. Do not change the docs/suites to invent an exemption.

## Git workflow and maintenance

Use an operator-selected branch/worktree and commit as `metrics: sync links in new notes (plan 039); ran go generate, go vet and go test`. Push only on instruction. Review every new page-writing path for both body storage and link synchronization; insertion alone is not a save.
