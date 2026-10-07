# 0020 — Session exports do not fit daily measurements without losing scope or inventing time

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** owner-authorized structural sampling of Fit and Google Health exports, after selecting daily summaries plus individual sleep/exercise sessions.

## What happened

A bounded local inspection found these structures; personal values, source filenames, actual dates and IDs are not
recorded here:

- Fit session JSON has `startTime`, `endTime`, `fitnessActivity`, `duration`, activity segments and numeric aggregates
  identified by `metricName` with `floatValue` or `intValue`.
- The Fit activity CSV directory contains both `Date`-based summaries and rows with `Start time`/`End time` headers.
- Legacy sleep JSON has `logId`, `dateOfSleep`, `startTime`, `endTime`, duration and stage summaries. Sampled local
  timestamps lack an offset; exercise exports also contain offset-free local date/time strings.
- Health CSVs include explicit units and a `data source` column; examples of headers are `weight grams` and
  `beats per minute`. Some fields are empty.

The current measurement model has no session association. Treating a workout's totals as another daily total loses
its scope; attaching a fabricated UTC suffix to local times invents an instant. Folder names and similar metrics
alone do not establish record equivalence across exports.

This is source-shape evidence and an owner import requirement, not an attempted import failure. Sampling did not
establish complete format coverage, historical timezone information, source precedence or actual duplicated records.

## Reproduce

With synthetic fixtures representing the observed field shapes, consider:

1. A daily step total of 10,000 and a workout's 4,000 steps for the same day. Their sum is not a justified new daily total.
2. Two workouts on one day, each with a distinct source ID and heart-rate summary. Keep the summaries attached to the correct workout.
3. An overnight sleep with offset-free local endpoints and a separate source-assigned sleep date. Do not assume UTC or use its start date as the reporting day.
4. Repeated exports of the same source IDs, and another provider describing a possibly overlapping session.

A fresh [schema](../schema/schema.sql) can keep numeric readings and prose, but not the required session relationship
and explicit unresolved time basis. These scenarios are proposed permanent fixtures; their acceptance tests do not
exist yet.

## Rules involved

- [D7](../decisions/D07-measurements.md), [D10](../decisions/D10-time-model.md) and the source-based reopen trigger in [D22](../decisions/D22-events.md).
- [Imports](../contract/imports.md): stable replay keys, source-specific identity and no guessed place.
- [Fitbit sleep semantics](https://dev.fitbit.com/build/reference/web-api/sleep/get-sleep-log-by-date/) and [time representation](https://dev.fitbit.com/build/reference/web-api/developer-guide/application-design/). API documentation helps interpret fields; it does not prove every export version uses the same format.

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes lightweight sessions, an explicit measurement
association and conservative import rules. Raw sensor streams, GPS tracks and a raw-export mirror inside the
database are not requested. No real import or new storage capability has been authorized by this inspection.
