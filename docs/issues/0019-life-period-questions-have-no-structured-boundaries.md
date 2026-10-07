# 0019 — Jobs, study and trips cannot define honest query windows

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** the owner's questions about past life periods and their association with health, routines and choices.

## What happened

The owner selected jobs/study and trips as the first periods to record and wants approximate remembered dates to
remain approximate. Day-page prose can describe a period, but the current schema has no boundaries with which to
select its measurements or distinguish an ongoing period from one whose end is unknown.

This is an owner-question incident, not a failed SQL query or a claim that any real period was imported. All
examples below are synthetic. The owner approved drafting a proposal, not implementing or freezing its time model.

## Reproduce

Using the concepts available in a fresh database built from [schema.sql](../schema/schema.sql), ask:

- Compare sleep and mood during `Study at North College`, known only to have started sometime in `2018-09`.
- Show the journal and selected files from `Coastal trip`, remembered as around summer 2019.
- Distinguish a job known to be ongoing from a past job whose ending date has not yet been recovered.

A page's single `day` cannot supply these boundaries. Spreading mentions across day pages does not establish that
the intervening days belong to the period. Inventing first-of-month dates or seasonal boundaries would replace
uncertainty with facts the owner did not provide.

## Rules involved

- The question-based reopen trigger in [D22](../decisions/D22-events.md).
- [D5](../decisions/D05-pages-and-day-pages.md), [D10](../decisions/D10-time-model.md) and [non-goals](../architecture/non-goals.md).
- [EDTF](https://www.loc.gov/standards/datetime/edtf.html) distinguishes reduced precision, uncertainty, approximation and open/unknown endpoints.

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes named life periods with overlapping spans
and a small explicit date profile. Approximate period boundaries must not loosen the representation of exact
measurement days or write timestamps. Query treatment of uncertain boundaries remains an acceptance gate.
