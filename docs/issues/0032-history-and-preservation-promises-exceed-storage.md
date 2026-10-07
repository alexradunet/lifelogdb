# 0032 — History and preservation promises exceed stored evidence

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

The contract described recording timestamps as bitemporal history and a self-describing file too broadly. Mutable rows, revived tombstones, clock regressions and external writer algorithms mean those descriptions were stronger than the evidence retained.

## Reproduce

Build a correction chain whose recording timestamps regress or tie; filtering the current leaf view cannot recover a timestamp cutoff. Rename or revive an entity and observe that prior spellings are retained but prior body/lifecycle transitions and editors are not.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

A tested recursive recorded-time projection handles eligible correction descendants, retractions and equal timestamps without claiming an earlier commit snapshot. In-file summaries describe key interpretation rules; exact conforming writers still require the matching docs and vectors. Decisions clarify current-state previews, sampled snapshots, original-inserter provenance, data-preserving evolution, mentions versus encounters, and the existing habit-period withdrawal limitation. No audit store or speculative feature is introduced.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
