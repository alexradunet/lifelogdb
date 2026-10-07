# 0027 — Entity creation time can be rewritten

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

Changing entities.created_at succeeded without advancing revision, rewriting insertion evidence while integrity remained clean.

## Reproduce

Insert a named page, read creation time and revision, then UPDATE created_at to a different valid instant. Before the repair both the statement and integrity checks succeeded.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

entities_provenance_fixed now protects created_at as well as source/import_key. Synthetic fixtures provide creation time at insertion; the journal display fixture adjusts updated_at instead. Regression cases cover refusal, atomic combined edits, no-ops and allowed body edits.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
