# 0016 — An accepted handle cannot be expressed by its literal wikilink

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** a synthetic writer audit of title creation followed by body capture.

## What happened

The writer accepted the title `Lab [old]`. Saving `See [[Lab [old]]]` created no reference to it because the wikilink
grammar excludes brackets inside a target. Title validity and ordinary reference addressability disagree.
This was observed with a temporary synthetic probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows.

The two individual rules are documented; the incident is their inconsistent composition, not an undocumented
parser failure. Introducing aliases without resolving the mismatch would give aliases the same problem.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), through the writer:

1. Create `Lab [old]` with a synthetic body.
2. Capture a day containing `See [[Lab [old]]]`.
3. Inspect the resulting wikilink targets and the lab note's backlinks.

Observed: creation succeeds, but no link to the accepted handle is extracted. Expected: either reject an
unaddressable reference name up front or specify an unambiguous way to write its reference.

## Rules involved

- `pages_title_safe` in [schema.sql](../schema/schema.sql).
- [D5](../decisions/D05-pages-and-day-pages.md), [D19](../decisions/D19-wikilink-save-contract.md) and the [title predicate and grammar](../contract/titles-and-wikilinks.md).

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes aligning reference names and grammar before
introducing aliases. A bracket-only fix is not evidence that every CommonMark-sensitive name round-trips; the
reference profile and its punctuation/Unicode vectors still need review and permanent tests.
