# 0034 — Scoped import retry is rejected after a session tombstone

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** literal SQL and production writer replay of synthetic scoped readings.

## What happened

A keyed reading was recorded, its session tombstoned, and the exact reading retried. The session admission trigger ran before the unique-key no-op and refused the retry. Existing facts were safe, but idempotent replay failed.

The import contract also overstated error handling: a skipped duplicate with a dangling capture/correction reference can succeed without validating that reference, and ordinary statement `ABORT` leaves earlier statements in the transaction intact.

## Reproduce

Record a non-NULL reading with a source/import key and session, tombstone the session, repeat `INSERT … ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING`. Separately, retry a key with a dangling reference and compare with a new key; insert a valid statement then an invalid statement inside one transaction.

## Rules involved

[Imports](../contract/imports.md), [measurement scope](../contract/measurement-scope.md), `measurements_session_live` and [D7](../decisions/D07-measurements.md).

## Resolution

The liveness trigger checks rows after insertion, so only admitted rows face new-value admission. New values still fail atomically, and NULL retractions remain allowed. SQL and production regressions cover replay and refusal. Imports explicitly distinguish skipped-payload validation from admission and require the caller to roll back on any statement or commit error. See [plan 075](../plans/075-schema-validation-depth.md).
