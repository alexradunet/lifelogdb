# 0038 — Habit readers hide invalid check-ins

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a schema review comparing the writer's habit readers with the habits cookbook, on a synthetic file
  with a nonconforming check-in added by direct SQL.

## What happened

[Habits](../cookbook/habits.md) and the [day view](../cookbook/day-view.md) label a habit day `invalid` when a
current unassociated check-in is outside 0/1, even beside a valid one, and completion counts such days in their own
category. The writer's day habits, day view and completion read only `max(value)`: a lone `2` was shown as
`not recorded` and counted in no category, so the categories no longer summed to the active days; a `1` beside a
`-1` was shown and counted as `done`.

## Reproduce

Start a habit on a unitless metric through the writer, record `1` on one day, then insert a current unassociated
measurement with value `2` on another active day and one with `-1` beside the `1`. Read both days and the
completion over them.

## Rules involved

[D24](../decisions/D24-habits.md) (a writer stores only 0/1 check-ins) and the diagnostic reads of
[habits](../cookbook/habits.md) and [day view](../cookbook/day-view.md). No contract change: the writer did not
implement the recipes as written.

## Resolution

The writer's three readers use the cookbook's `invalid` rule, completion reports an `invalid` count, and the habits
page shows it. Tests cover invalid-only, valid-beside-invalid, corrected and retracted invalid readings, a
session-scoped invalid reading and a tombstoned habit, through the store and through the API's JSON and HTML.
