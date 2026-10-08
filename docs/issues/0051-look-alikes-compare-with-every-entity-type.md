# 0051 — A new name is a look-alike of a day page, a metric or a file that shares a word

- **Date:** 2026-10-08
- **Status:** open
- **Seen in:** the 2026-10 import of a notes folder (people named in daily notes) and a contacts export

## What happened

The look-alike check compares a new person, place or page with every live entity of every type. A person "Andreea"
was refused for the note "Dimensiuni Alex & Andreea" (a page of body measurements); a place "Mia Pia beach" for the
person "Mia Pia"; a person "Nasim Al Tamimi" for a person titled "al"; a person "Radu" for the note "Radu
Alexandru - Career Master Document". The first candidate found is the only one reported, so the owner decides
against one name while others may be the right match.

Expected: a new person, place or page is compared with live persons, places and plain pages — the kinds that can be
the same identity — never with day pages, metrics, files or periods; and every candidate is reported with its
relation, so the owner's decision is made once.

## Reproduce

1. A plain page "Riverside Pool Notes" (a vault note) and a metric "Pool".
2. A facts file writing the place "Riverside Pool": refused as a look-alike of the page (more words) — the metric
   would be next.

## Rules involved

- [importing with a model](../guides/importing.md), "The checks" (look-alikes), "What an implementation must get right"
  ("Look-alike checks cover pages too")

## Resolution

Open.
