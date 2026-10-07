# 0028 — Semantic integrity misses damaged relationships

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

The four check groups reported success for a missing symmetric edge, mismatched shared note, correction cycle, overlapping habit periods, nonbinary habit readings and journal aliases owned outside the canonical day identity.

## Reproduce

On a synthetic file, temporarily remove the relevant guard, insert or update the invalid relationship, restore the guard and run all documented checks. Structural integrity and foreign keys remain clean for these cases.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

The semantic group now audits these existing invariants. Cycle detection traverses rooted correction chains and reports unreachable rows. Both language-neutral SQL and the writer checker have independent damaged-state regressions; the document explicitly states the limits of data integrity checks.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
