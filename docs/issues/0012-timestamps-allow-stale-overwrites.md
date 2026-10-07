# 0012 — A timestamp used as an edit version permits a stale overwrite

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** a synthetic writer audit of conditional body saves.

## What happened

The writer compares the client's last-read `entities.updated_at` with the stored timestamp before replacing a body.
A successful save can retain the same millisecond timestamp. A second save using the old token was then accepted
and persisted `Stale overwrite` instead of reporting a conflict.

This was observed in a temporary probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows, without altering the
clock or schema. The diagnostic looked for a collision over at most 1,000 sequential committed saves and found one.
Its timing-dependent search is evidence of the failure, not an acceptable deterministic regression-test design.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), through the writer:

1. Create `Version witness` and read its body and edit token.
2. Save a different body with that token and commit.
3. If the stored timestamp still equals the token, attempt another different save with the old token.
4. Check both the result and persisted body.

Observed on the recorded run: step 3 succeeds and overwrites step 2. A run with no timestamp collision does not
refute the defect. A permanent test must arrange equal clock readings deterministically, without making real
SQLite/filesystem I/O depend on a fake-clock scheduler.

## Rules involved

- The timestamp rationale in [D10](../decisions/D10-time-model.md) and `pages_touch` in [schema.sql](../schema/schema.sql).
- The atomic write in [save a body](../cookbook/save-a-body.md).
- [D12](../decisions/D12-no-revision-tables.md): rejecting stale writes does not require retaining old bodies.

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes a monotonic edit revision independent of
wall-clock timestamps. Validation must cover committed changes, no-ops, rollback, cancellation and stale-write
refusal through applicable client surfaces. No fix or permanent regression test has been added. Which writes advance
that revision, a page's creation and an incoming link, is the subject of [0041](0041-fresh-pages-read-as-edited.md)
and [0042](0042-incoming-link-invalidates-an-open-edit.md).
