# 0024 — Ghost cleanup includes pages referenced by sessions and measurements

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic session capture and mood-only journal capture through the production writer

## What happened

An empty session-kind page and a mood-only journal day appeared in the ghost cleanup list once older than
30 days. Both have retained life-data references, contrary to the cleanup list's purpose. Tombstoning the kind
from that list prevents recording new sessions of that kind. No facts are hard-deleted by cleanup.

## Reproduce

Create an empty `Sleep` page and capture a session of that kind. Separately capture a mood with no prose on a
journal day. Age only the synthetic entities' creation timestamps beyond 30 days, then query `ghost_pages`.
Before the fix both appear. An empty unused typo is the positive cleanup control.

## Rules involved

The `ghost_pages` view in [schema.sql](../schema/schema.sql), [D5](../decisions/D05-pages-and-day-pages.md),
and the [cleanup recipe](../cookbook/ghost-pages.md).

## Resolution

The view excludes retained `sessions.kind_id` and `measurements.captured_with_id` references, including tombstoned
sessions and corrected or retracted readings. Writer-built fixtures reproduced the failure and verify the fix;
one mutant removes each exclusion. Focused checks and the full baseline pass (`go generate ./...`, `go vet ./...`,
`TMPDIR=/var/tmp go test -count=1 ./...`, Go 1.27.1 / Linux amd64). This preserves
D5's cleanup purpose without changing stored identities or introducing a new decision ([process](../process.md)).
