# 0031 — Compound diacritics escape search folding

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** synthetic schema review probes, SQLite 3.53.4

## What happened

The default unicode61 tokenizer left a precomposed Latin character with multiple diacritics unsearchable through its unaccented base, despite accent-folded search behavior for ordinary Latin text.

## Reproduce

Index the synthetic body ộ. MATCH o returns no result with remove_diacritics=1; Romanian accents and a decomposed spelling provide controls.

## Rules involved

The [canonical schema](../schema/schema.sql), its [contract](../README.md) and the repair scope and validation in [plan 074](../plans/074-schema-review-findings.md).

## Resolution

The derived FTS tokenizer explicitly selects unicode61 remove_diacritics 2. Regressions cover precomposed/decomposed and Romanian text. Documentation distinguishes token matching from pinned full-Unicode name identity; Straße still has exact key strasse without promising identical FTS folding.

These repairs enforce or clarify existing decisions and take the small-fix path in the [process](../process.md).
