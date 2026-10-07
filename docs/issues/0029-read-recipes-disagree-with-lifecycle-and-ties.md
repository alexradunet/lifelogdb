# 0029 — Read recipes disagree with lifecycle and deterministic selection

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

Ordinary metric, mood, day and session recipes exposed tombstoned metrics. Habit completion omitted invalid current values from every outcome count. Equal place circles could select different link_days behavior when an index changed visit order.

## Reproduce

Capture a reading then tombstone its metric and run the active recipes. Insert a deliberate nonbinary habit reading and total the daily categories. Create identical place circles with different ids/link_days and repeat the selection under a descending-radius index.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

Active recipes filter metric lifecycle, explicit historical queries label current lifecycle, invalid habit days are a separate category, and the final place tie-breaker is stable id. The minimum shared place query is aligned with its cookbook. Regressions demonstrate each result.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
