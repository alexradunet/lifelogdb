# Product concepts

| Product concept | Schema mechanism |
|---|---|
| Journaling | the day page, titled `YYYY-MM-DD`: capture appends to it ([capture](../cookbook/capture.md)); the day view adds what else that day holds ([the day view](../cookbook/day-view.md)) |
| "When did I see Ana / go to Lakeside?" | the day pages that link `[[Ana]]` or `[[Lakeside]]` ([the days that name someone](../cookbook/days-that-name.md)) |
| "Where was I that day?" | `links(kind='at')` from the day page to each place; the days at a place are its `at` backlinks ([where was I](../cookbook/where-was-i.md), [D16](../decisions/D16-places.md)); a photo kept links the day it was taken to the place its position is in ([the place of a photo](../cookbook/place-of-a-photo.md), [D21](../decisions/D21-location-history.md)) |
| Mood tracking | the `mood` metric in `measurements`, each row optionally pointing at its day page ([mood over time](../cookbook/mood-over-time.md)) |
| Notes, wiki and tags | `entities.body` + `links(kind='wikilink')` kept equal to what the body names ([titles and wikilinks](../contract/titles-and-wikilinks.md), [save a body](../cookbook/save-a-body.md)); `#health` names `health` without rewriting the body |
| Backlinks | `links WHERE to_id = ?` ([backlinks](../cookbook/backlinks.md)) |
| People and places in prose | `[[Bob Sample]]` links to the person itself, because the person is a page ([D20](../decisions/D20-named-pages.md), [a person or a place](../cookbook/person-or-place.md)) |
| Life graph ("everything about my son") | `links` in both directions from his id ([everything about](../cookbook/everything-about.md)) |
| Recorded sessions | independent lightweight kind-based records with reporting-day attribution and preserved [time evidence](../contract/session-time.md); [scope](../contract/measurement-scope.md) distinguishes summaries from daily facts |
| Recorded periods | named overlapping spans with partial/qualified boundaries and explicit day-membership results ([profile](../contract/period-boundaries.md), [D22](../decisions/D22-events.md)) |
| Habits | a 0/1 metric with active periods: the day's habits done, not done or not recorded, and completion over a period ([habits](../cookbook/habits.md), [D24](../decisions/D24-habits.md)) |
| To-dos, reminders, projects | not here: a life log, not a project manager ([D23](../decisions/D23-no-tasks.md)) |
| Birthdays | a query over `people.birth_day` |
| Biomarkers / quantified self | `metrics` + `measurements` ([a metric series](../cookbook/metric-series.md)), filed in category pages ([metrics by category](../cookbook/metrics-by-category.md), [D26](../decisions/D26-metric-categories.md)) |
| Imported notes (a vault) | `entities.import_key`, unique per `source`: a re-run inserts nothing, a changed note updates its page ([import a row once](../cookbook/import-a-row-once.md)) |
| Photos, recordings, scans | a file page: its text (a transcript, a caption) in the body, a small JPEG in `files.preview`, the original left outside; `![[Lake.jpg]]` in a day page links it ([keep a file](../cookbook/keep-a-file.md), [D9](../decisions/D09-binary-files.md)) |
| Search | `entities_fts` ([full-text search](../cookbook/full-text-search.md)) |
| "Which of my agents wrote this?" | `source` on every entity, session, link, measurement and habit period (`lifelog_meta.source`, [D10](../decisions/D10-time-model.md)) |
