# 0030 — Retained reference lookups scan full fact tables

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

Ghost exclusion performed correlated scans of sessions.kind_id and measurements.captured_with_id. Reading an empty session scanned the full measurement series. Existing indexes did not begin with these reference columns.

## Reproduce

Use the reproducible synthetic workload described in plan 074: 200 empty ghost candidates, 1000 sessions and 40000 readings concentrated in one session and capture source. Examine query plans and count SQLite virtual-machine progress steps.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

Add sessions_kind, partial measurements_capture and partial measurements_session(session_id,metric_id,day). Query-plan regressions require indexed searches and check present/absent results. The measured storage and insertion-work costs are recorded alongside read-work savings in plan 074.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
