# 0026 — Typed detail identities can be reassigned

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

Synthetic direct SQL moved people, file and metric details to a different named owner without preserving the original identity. Habit-period row ids could also change. The move was accepted through every SQLite rowid alias.

## Reproduce

Create two matching typed identities and attach a detail row to the first. UPDATE its id, rowid, _rowid_ or oid to the second; for people/files, a temporary third identity permits exchanging both original details while all foreign keys remain valid.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

Four identity guards now refuse these moves atomically, including combined content edits, while preserving identity no-ops. Habit metric reassignment remains the documented mutable relationship. The schema-safeguards suite supplies regression witnesses and guard-removal mutants.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
