# 029 — Google Takeout: places and daily health totals, with no schema change

- **Date:** 2026-10-04, at commit `7c5391a`.
- **Priority:** P1. **Effort:** L (four phases; the last is the owner's).
- **Status:** Phase A code implemented for Timeline/Fit/Fitbit inventory; owner inventory output is still needed before any Phase B/C format support. The whole plan is not done.

Implementation note: this Phase A slice intentionally stops before converter/facts generation and does not inspect real exports. The authorized inventory reports public product-family counts, month ranges, overlaps and shape paths from synthetic/public formats only; archive-wide photo inventory and photo gap measurement are out of scope under the current owner decisions.

## Why

The owner requested a Google Takeout export of over 400 GB. Two parts of it fill what the journal leaves empty: where the
owner was on each day (Timeline) and a decade of daily health totals (Fit, Fitbit). Both fit today's schema: a place
the owner was at is an `at` link from that day's page ([D16](../decisions/D16-places.md)), and a daily total is a
measurement ([D7](../decisions/D07-measurements.md)). **This plan changes no table, no column, no link kind and no
contract rule.** It adds code that turns the export into the facts files the [import guide](../guides/importing.md)
already defines, and one section of that guide.

## The owner's decisions (interview of 2026-10-04)

| subject | decision |
|---|---|
| what comes in | **Timeline** visits and **Fit / Fitbit** daily totals. Nothing else from the export |
| location | **day → places only**: a qualifying visit becomes an `at` link from that day's page. No GPS track, no coordinates in `life.db` ([D21](../decisions/D21-location-history.md) stays deferred) |
| visit filter | a visit of at least 15 minutes, excluding Home (and Work if the owner says so). The threshold and the excluded places live in the workspace rules, tunable and re-run on the trial copy |
| health | **daily totals** as measurements (steps, resting HR, weight, active minutes, …). No intraday tier (non-goal kept) |
| sleep | sleep hours (and score/stages where Fitbit has them), as measurements on the **wake-up day** |
| workouts | **daily totals, not events**: a `Workouts` count and minutes per activity (`Running`, …) per day. "How many workouts this year" is a `SUM` ([metric series](../cookbook/metric-series.md)) |
| several sources for one metric | **one source per metric**, named in the rules, with a date range only where the source changed (e.g. steps: Fit until 2019-06, then Fitbit); the others are not imported |
| days with no journal | an **empty day page** is created to carry the day's `at` links and readings |
| how it is imported | **hybrid**: deterministic code converts the export and writes the mechanical facts from the owner's approved rules; the local model drafts rules sections, proposes metrics and place titles, and asks questions; the existing *check / apply / replay* writes every row |
| archives | the owner **extracts only the needed folders** (a few GB) with one 7-Zip or `tar` command; the code reads a plain folder, never a `.zip` or `.tgz` |
| left out | **events** ([D22](../decisions/D22-events.md) stays deferred): Calendar is not imported. **Partial birth days** and Contacts birthdays: not imported. **Photos**: deferred (below). Gmail, Chat, YouTube, Search, My Activity, Keep, Google Pay |

**Photos are deferred, not dropped.** Photo GPS adds a place only on days Timeline missed, and matching it needs place
coordinates and code. The inventory measures that gap. Photo matching comes back as its own plan only if the gap
is real.

## Privacy: a hard rule for whoever executes this plan

The export, its workspace, the converted files, `trial.db` and the real `life.db` are the owner's private data.
**No hosted model reads any of them**: not a file, not a listing, not a count it was not handed (memory rules
`no-hosted-access-to-real-data`, `import-local-llm-only`). The executing agent works only on code, docs and
**synthetic** fixtures it writes itself. Every fact about the real export comes from a command the owner runs
locally and output the owner chooses to paste. Keep the export and its workspace out of the repository (or under
the ignored `/import/`, workspace `*.lifelog/`).

## Phase 0 — the owner extracts the needed folders

Takeout's folder names follow the account's language. List them first (`7z l takeout-*.zip | findstr /i "Takeout/"`
or `tar -tzf …`), then extract only the Timeline and fitness folders, and the photo sidecars for the gap
measurement. For example, with 7-Zip:

```
7z x "takeout-*.zip" -oD:\takeout-x -ir!"Takeout\Location History*" -ir!"Takeout\Fit\*" -ir!"Takeout\Fitbit\*" -ir!"Takeout\Google Photos\*.json"
```

Google moved Timeline onto the phone in 2024. If the export's Timeline stops then, also export the phone's
`Timeline.json` (Settings → Location → Timeline → Export) into the same folder.

## Phase A — the inventory (code only)

**Goal:** know what the real export holds without anyone reading it.

`lifelog import takeout inventory <folder>` reads the extracted folder **read-only**. It prints, and writes nothing:

1. per top folder: file count, bytes, and file extensions with their counts;
2. per known family, the record count and the first and last **month** (never a day, a name or a value):
   - Timeline: semantic visits (`Semantic Location History/YYYY/YYYY_MONTH.json`), raw `Records.json` (count
     only), and the on-device `Timeline.json` if present. Say which forms were found and which months each
     covers;
   - Fit: the daily aggregate CSVs, sessions by activity, the raw data-source folders (count only);
   - Fitbit: each file family with its count (sleep, steps, heart rate, weight, exercise);
3. **overlaps**: per metric family, the months where more than one source has data (this is what the rules'
   `## Sources` must settle);
4. **the photo gap**: the number of days with a GPS photo sidecar (`geoData` not 0,0) and no Timeline visit,
   per year;
5. **shapes**: for each JSON and CSV family, the key paths or column headers with their types and how often each
   appears. Never a value. An object whose keys are data (dates, ids) is folded into one `<key>` entry.

**Done when:**
- Synthetic fixtures under `internal/takeout/testdata/` cover every family above. They are tiny and hand-written,
  following the public format descriptions.
- A test plants marker strings in every value field and asserts that none is printed.
- `go generate ./... && go vet ./... && go test ./...` is green. The README's command list names the command.
- **The owner** runs it and pastes what they choose. The pasted shapes decide which formats phases B and C
  support. A format with no rows in the real export is not implemented.

## Phase B — the converter (code only)

`lifelog import takeout convert <folder> <workspace>` turns the export into a **converted source**: one Markdown
file per local day, which the existing workflow already reads (*inspect* returns frontmatter and tables as rows; a
facts quote is checked against them). It is the guide's own move: "a journal export is first converted to a folder
of one Markdown file per day". The converter never opens `life.db`.

```
<workspace>/converted/
  days/2019/2019-03-01.md    one file per local day that has a visit or a total
  drafts/places.md           place id | Google name | address | visits | minutes | first day | last day | semantic type
  drafts/metrics.md          metric | source | unit | first day | last day | days
  drafts/zones.md            month | UTC offsets seen | countries seen (from visit addresses)
```

A day file:

```markdown
---
day: 2019-03-01
zone: Europe/Bucharest
---
## Visits
| place id | name | arrive | leave | minutes |
## Totals
| metric | value | unit | source |
```

Rules the converter follows, each with a synthetic test:

- **The local day** ([D10](../decisions/D10-time-model.md), `lifelog_meta.days`) comes from `## Zones` in
  `rules.md`: one home zone, plus a date range for each trip elsewhere, drafted from `drafts/zones.md` and approved
  by the owner. An instant with an offset in the source (the on-device `Timeline.json`) uses that offset. Fit's daily
  CSVs are already per local day and are taken as they are. Use the standard library's zone data with `time/tzdata`
  embedded, so Windows needs no system database. Times in a day file are local `HH:MM`.
- **Visits**: only those meeting the `## Visits` threshold and not in its excluded list. A visit across midnight
  appears on each day it covers.
- **Totals**: only the source `## Sources` names for that metric and day. Sleep goes to the day its session
  **ends**. Workout sessions are summed per day into `Workouts` (a count) and minutes per activity.
- **Deterministic**: the same export and rules give byte-identical files (a test runs it twice).

Before the rules are approved, the converter writes only `drafts/`.

**Done when:** every family the owner's inventory showed has a fixture and a converter test, covering at least the
midnight-crossing visit, a DST day, a trip in another zone, the wake-day rule, two sources for one metric, and two
workouts on one day. The suite is green.

## Phase C — the facts generator and the guide (code and docs)

`lifelog import takeout facts <workspace>` writes `facts/converted/…json` from the converted day files and the
**approved** `rules.md` and `metrics.md`. These are the same facts files a model writes, using only kinds that exist
today (`page`, `place`, `link`, `reading`), so *status*, *check*, *apply* and *replay* are unchanged. For each day
file:

| row in the day file | write | needs approved |
|---|---|---|
| (the file itself) | `page` with the day's title: the empty day page when there is none | — |
| a visit | `place` (its approved title) and `link` `at` from the day | the place's title in `## Places` (Google name → title, as an alias where they differ) |
| a total | `reading` | the metric in `metrics.md` |

The quote of each write is its row, copied from the day file. A row that needs an approval it does not have is left
out of `writes`, raises one batched question ("these 40 places have no title"), and puts the file in `waiting`. This
is the guide's existing flow.

The local model's part (with [importing](../guides/importing.md)'s rules, one step per turn): draft `## Zones`,
`## Places` titles (telling chain stores apart: `Starbucks (Piața Romană)`), `## Sources` and the metrics from the
`drafts/`, and turn the owner's answers into aliases. **The owner** approves.

Docs, in the same commit:

- [importing](../guides/importing.md) gains a section "A structured export". It is converted first, like a journal.
  The converter may write facts from approved rules, so the model's part shrinks to drafts and questions. Quotes
  are rows of the converted files. The three parties' table gains the converter's row. The section stays
  language-neutral and names no application.
- The README's command list.

**Done when:** an end-to-end test runs on a synthetic export: *inventory* → *convert* → *facts* → *check* →
*apply* into a fresh trial database → the four [integrity checks](../contract/integrity-checks.md) are green →
a second *facts* + *apply* writes nothing → *replay* into another fresh file gives the same counts.

## Phase D — the real run (the owner only, locally)

1. A snapshot of `life.db` ([take a snapshot](../cookbook/take-a-snapshot.md)); the workspace beside the extracted folder.
2. *inventory* → *convert* (drafts) → the local model drafts the rules sections and metrics → the owner approves →
   *convert* → *facts* → the questions loop → *apply* on `trial.db`.
3. Look at a handful of days in the trial's day view. Tune the visit threshold and the excluded places, and re-run
   on a fresh trial.
4. *replay* onto the real `life.db` only on the owner's word. Record the counts (the freeze checklist: "a trial
   import ran on a copy and its counts are recorded; a second run wrote nothing").

Nothing of this phase is reported to a hosted model beyond counts the owner chooses to share.
