# 093 — Words, not data; the phone first; a richer Today

> Dated implementation record. Tests use temporary folders and synthetic data only. Screenshots are made from a
> synthetic database and are not kept in git.

## Status

- **Date / baseline:** 2026-10-12, `3fa2fb6` (master).
- **Priority:** P1. **Effort:** L. **Risk:** MEDIUM: every view shows times and days in new words; the result of a
  write is read back as data; the shell gets a second layout for a phone; the day and the root get two new
  properties. None for the schema.
- **Status:** TODO.
- **Resolves:** issue [0062](../issues/0062-the-browser-still-shows-data-not-words.md). No RFC: the contract in
  `docs/` does not change.

## The owner's decisions (2026-10-12)

| question | decision |
|---|---|
| what next | three areas of four: **words, not data**; **the phone first**; **a richer Today**. Not now: the command list of Ctrl K. |

## The rules for every part

- The rules of plans 091 and 092 stay: a template reads the properties the JSON carries and places the actions the
  entity offers; every form works with no script; every change to the wire is an addition.
- A value is drawn by its type, never by the name of its property: a string that is a day is a day, one that is an
  instant is an instant (`core.IsDay`, `core.IsInstant`).
- A word for a time uses the clock of the server's day: the local zone of the machine that runs `lifelog serve`, the
  zone `core.Today` reads. The exact instant stays in the page, in `<time datetime>` and its title.

```mermaid
flowchart LR
  V["a value of the entity"] --> D{"its type"}
  D -- "a day" --> W1["Mon 5 Oct (the year when it is not this year); within 7 days of today: 'yesterday', '4 days ago · Thu 8 Oct'"]
  D -- "an instant" --> W2["09:48, Mon 12 Oct, in the server's zone, in a time element with the exact instant"]
  D -- "a boolean" --> W3["yes, no"]
  D -- "an object" --> W4["facts: its names in words, each value by its type"]
  D -- "a list of objects" --> W5["a table: its headers in words, each cell by its type"]
  D -- "anything else" --> W6["as it is"]
```

## Part 1 — Words, not data

Presentation only: no entity changes.

| what | where |
|---|---|
| **The clock of the words.** The view gets today and the server's zone. `New` reads the zone of the machine (`time.Local`, the zone `core.Today` reads); the test handler `NewOn` uses UTC, so the tests have fixed words. A page drawn before the handler has no clock and writes times in UTC with "UTC". | `internal/api/handler.go`, `internal/api/shell.go`, `internal/api/export_test.go`, `internal/api/html.go` |
| **Days and times in words**, as the diagram says. `dueWords` writes the days of the design system ("7 days ago · Mon 5 Oct", "in 3 days · Thu 15 Oct", "Sun 1 Nov 2026" for another year). Every raw instant of a view goes: a page's and a task's creation, an occurrence's completion and writing, a reading's time, the series table, the day's readings. | `internal/api/planning_html.go`, `internal/api/html.go`, `internal/api/html/*.html` |
| **The generic view in words.** An object is a `dl.facts`, its names in words (`start_utc` → "start utc", `task_version` → "task version"); a list of objects is a table with headers in words; every value is drawn by its type. This is the view of sessions, a session, periods, integrity, a snapshot, a query, the catalog and any class to come. | `internal/api/html.go` (`writeValue`) |
| **The result as sentences.** The receipt keeps the result's JSON, and the toast says it by the result shapes of API.md: "Linked Garden project.", "Created Dana Example.", "Not linked: `[[a/b]]` is not a valid title.", "Filed under Mon 12 Oct.", "Wrote 12 readings; 3 were there before." Each title links its page. A key that no sentence knows is shown as a fact, so nothing of a result is lost. | `internal/api/feedback.go`, `internal/api/html.go`, `internal/api/html/layout.html` |

## Part 2 — The phone first

Presentation only: the same links and forms, in a second layout under 900 px.

| what | where |
|---|---|
| **A top bar**: the brand, today's date, a Search link (`/search`) and New. | `internal/api/html/layout.html`, `internal/api/html/app.css` |
| **A bottom bar**, fixed, with the safe-area inset: Today, Calendar, Due with its count, Tasks, and More. It is the sidebar's first group, laid out again; no link is drawn twice. | `internal/api/html/app.css` |
| **More** is a link to `#sections`: the rest of the sidebar (the sections of the record, this month and the owner's links) is a sheet over the page while it is the target (CSS `:target`), with a Close link. It needs no script. The script closes it with Escape. | `internal/api/html/layout.html`, `internal/api/html/app.css`, `internal/api/html/app.js` |
| **The page header on a phone**: the pager's buttons small, and an action's form opens in place, full width, not floating. | `internal/api/html/app.css` |

## Part 3 — A richer Today

| what | where |
|---|---|
| **Trends.** The day gets the property `trends[]`: for each metric with a current unassociated reading on the day, its `metric`, `unit` and `readings[]` (`day`, `value`): its current unassociated readings of the 14 days through the day, oldest first (the series of the cookbook). A reading tile shows the unit (a scale `1-5` as "of 5") and draws the line of those readings, the day's value marked. | `internal/api/handler.go` (`day`), `internal/api/html/day.html`, `API.md` |
| **The last days of a habit.** Each item of `habits[]` on the day and on the root gets `recent[]`: the habit's state on each of the 14 days through the day, oldest first: `done`, `not done`, `not recorded`, `invalid`, or `inactive` (no period of the habit holds the day). The Habits panel draws them as a strip, with "8 of the last 14 days" in words. The state is the one `core.Habits` reads, day by day, so the rule has one home. | `internal/api/habits.go`, `internal/api/handler.go`, `internal/api/html/day.html`, `API.md` |
| **A series on its own page**: the group Later shows the next 5 slots, then "and N more until {through}". The other groups stay whole. | `internal/api/planning_html.go`, `internal/api/html/planning.html` |

## Done criteria

1. Go tests beside their packages, on a real SQLite file under `t.TempDir()`, built through the writer, with
   hand-checked values and the fixed clock (`NewOn`):
   - **words:** days and instants as a table of cases (today, yesterday, 7 and 8 days, another year, a value that is
     not a day or an instant); the server's zone and UTC; the generic view of a session and of integrity in facts;
     no raw instant on a page, a task, an occurrence or a reading.
   - **result:** each result shape of API.md (the sync shape with every list, add-file with a day, a place and an
     unmatched point, readings-from-table, relocate-reading, a snapshot) as sentences; an unknown key as a fact;
     a title with markup is escaped.
   - **phone:** the bottom bar holds the first group and More; the sheet holds the rest; one link marked current;
     the pages carry the same forms as before.
   - **trends:** a day with readings of two metrics over 14 days, one outside the window, a session-scoped one (not
     in the trend), a corrected and a retracted one; the unit; a day with none (`[]`).
   - **habits:** `recent` with every state, a day before the habit's start and after its end (`inactive`), on the
     day and on the root.
   - **task page:** a series with more than 5 later slots, and one with fewer.
   - **parity:** `trends` and `recent` through the CLI's `get` and the MCP `get` tool; `TestAPIReferenceMatchesTheCode`.
2. **In a browser:** the 21 checks of plan 092 again, and: More opens the sheet and Close closes it with no script;
   Escape closes it with the script. Screenshots of Today, Home, a task, a session, a page and a write's result, at
   1280 px and 390 px, in light and in dark.
3. Baseline green on Windows: `go generate ./... && go vet ./... && go test ./...`; `gofmt`;
   `go test -count=1 -shuffle=on ./...`; staticcheck and govulncheck if available (reported as not run if not). The
   cost of the day's new reads is measured once on the synthetic database and recorded.
4. Three commits, one for each part, each with the baseline green. The issue is resolved and this plan is DONE in the
   commit of Part 3.
