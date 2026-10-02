# Plan 033: The core writes hold their own rules — corrections in range, no lost saves, renames that keep their history

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/core internal/db internal/text/text.go README.md`
> Plans 026, 029 and 031 change other functions in `internal/core` and `internal/db` — expected. Compare the
> functions quoted below; on a mismatch in one of them, STOP.

## Status

- **Priority**: P2
- **Effort**: M (eight small fixes, each with its test)
- **Risk**: LOW–MED (the version format of a page changes: clients echo it, so nothing breaks, but see Step 3)
- **Depends on**: 026, 029, 031 (same packages; run after them)
- **Category**: bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

`internal/core` is the only code that writes `life.db`. The core audit (each item executed on a throwaway database at
`9ac130f`) found places where it does not hold the rules it holds elsewhere:

1. **A correction skips the range checks.** `Record` refuses a mood outside 1–5 and a habit check-in other than 0/1
   (D6, D24: "The 0/1 range of a check-in is checked by the app, like mood's 1–5"), but `Correct` inserts any finite
   value: a mood corrected to 42.5 and a check-in corrected to 0.5 were accepted (the habit day then reads "not
   recorded").
2. **A save in the same millisecond is lost.** The optimistic version is `entities.updated_at`, which has millisecond
   resolution; two saves with the same stale version inside one millisecond both pass — 24 of 300 tries overwrote a
   body with no 409.
3. **A second rename breaks backlinks.** Renaming X → Y → Z leaves X's stub pointing at Y (`#REDIRECT [[Y]]`), a
   two-hop chain; readers follow one hop (`docs/cookbook/backlinks.md`), so every mention written as `[[X]]` is lost from
   Z's backlinks and "the days that name" Z.
4. **Titles with invalid UTF-8 are stored**: Go turns a bad byte into U+FFFD while ranging, so `ValidTitle("x\xffy")` is
   true and the raw bytes go into `pages.title` — unreadable for a writer in another language.
5. **A database path with `#` or `%` writes another file**: the SQLite URI is built without escaping;
   `db.Init(".../notes#1/life.db")` returned nil and created a file named `notes`. Separately, the connection hook decides
   "reader" by `strings.Contains(dsn, "mode=ro")`, which a path can contain.
6. **The integrity checks ignore scan errors**, so a check that fails part-way can read as passed.
7. **A rename of a page with no day stamps it with today** (the day view then lists it under the rename date).
8. **Capture drops the mood silently** when no `mood` metric exists; the **trial copy is not in WAL mode**; readers
   have no `busy_timeout`; `keyExists` swallows its error.

## Current state

- `internal/core/write.go:518-534` — `Tx.Correct(wrong int64, value *float64)`: checks only NaN/Inf, then
  `INSERT INTO measurements(...) SELECT metric_id, day, taken_at, tz, ?, ?, captured_with_id, id, Now FROM measurements WHERE id = ? RETURNING id`.
- `internal/core/write.go:465-481` (in `Tx.Record`) — the checks to share:
  ```go
  err = t.tx.QueryRow(`SELECT id, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id) FROM metrics m WHERE name = ?`,
  	m.Metric).Scan(&metric, &habit)
  ...
  if m.Metric == "mood" && !isMood(m.Value) {
  	return 0, invalid("mood is 1-5")
  }
  if habit && m.Value != 0 && m.Value != 1 {
  	return 0, invalid("%s is a habit: 1 = done, 0 = not done (D24)", m.Metric)
  }
  ```
- `internal/core/write.go:240-260` — `SaveBody(id, body, version)`: `SELECT e.updated_at, e.deleted_at FROM entities e JOIN pages p …`;
  `if version != updated { return Sync{}, conflict(...) }`. `internal/core/read.go:45-56` — `PageByID` scans
  `e.updated_at` into `p.Version`. The schema trigger `pages_touch` (`docs/schema/schema.sql:360-363`) sets
  `updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')` (milliseconds). README.md "Optimistic saves": "A body save sends
  the `version` (`entities.updated_at`) it read; a newer one is a 409."
- `internal/core/rename.go:15-94` — `Tx.Rename(id, newTitle)`: refuses deleted, non-page, day page, stub, invalid and
  same-key titles; creates or reuses the target; `t.SetBody(id, "#REDIRECT [["+…+"]]")`; inserts the `redirect` link;
  moves typed links. It never looks at **incoming** `redirect` links of `id`. Lines 38-42:
  ```go
  var day string
  t.tx.QueryRow(`SELECT coalesce(day, '') FROM pages WHERE id = ?`, id).Scan(&day)
  if to, _, _, err = t.CreatePage(newTitle, old.Body, day, ""); err != nil {
  ```
  and `CreatePage` (`write.go:274-292`) turns `day == ""` into `Today()`. `insertPage` (`write.go:133-147`) takes
  `day any` and stores it as given (`nil` → NULL).
- `internal/text/text.go:53-71` — `ValidTitle` ranges over runes; no `utf8.ValidString`.
- `internal/db/db.go:50-55` — hook: `if strings.Contains(dsn, "mode=ro") { return nil }`; `db.go:104` —
  `return "file:" + filepath.ToSlash(path) + "?" + q.Encode()`; `db.go:186` (`Copy`) — `"file:"+filepath.ToSlash(from)+"?mode=ro"`.
- `internal/core/integrity.go:18-57` — three `for rows.Next()` loops; `rows.Scan` results ignored; `rows.Err()` never read.
- `internal/core/write.go:231-234` — Capture's mood: `INSERT INTO measurements(...) SELECT id, ?, ?, ?, ?, Now FROM metrics WHERE name = 'mood'` (zero rows inserted is not an error).
- `internal/core/write.go:294-298` — `keyExists`: `t.tx.QueryRow(...).Scan(&n)` error ignored; returns bool.
- `internal/db/db.go:177-193` — `Copy` runs `VACUUM INTO ?` on a read-only connection; the copy is in rollback-journal
  mode (`PRAGMA journal_mode` reads `delete`). `docs/schema/schema.sql:12`: `PRAGMA journal_mode = WAL;`.
- `internal/db/db.go:93-96` — reader DSN has `mode=ro`, `query_only(1)`, `trusted_schema(0)` (plan 026 adds
  `_defensive=1`); no `busy_timeout`.
- Tests: `internal/core/core_test.go` (`fresh`, `status`), `internal/core/habits_test.go`, `internal/db/db_test.go`,
  `internal/text/text_test.go`.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Packages | `go test ./internal/core ./internal/db ./internal/text ./internal/api` | ok |
| Suites (they run the writer's save) | `go test ./tests -short` | ok |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/core/write.go`, `internal/core/read.go` (`PageByID` version only), `internal/core/rename.go`,
`internal/core/integrity.go`, `internal/core/core_test.go`, `internal/core/habits_test.go`, `internal/db/db.go`,
`internal/db/db_test.go`, `internal/text/text.go` (`ValidTitle` only), `internal/text/text_test.go`, `README.md` (the
"Optimistic saves" bullet only).

**Out of scope**: `docs/` (no contract change: the title predicate already means code points, and the version is the
writer's own); `SaveBody` on a redirect stub (left as is — an open question for issue 0001 / plan 041); the day-page
check of link targets (`dayOfTitle`, a negligible departure, considered and rejected).

## Git workflow

Branch `advisor/033-core-rules`; a commit per step is welcome. Final message e.g. `core: corrections hold mood and
habit ranges, a save's version includes its body, renames repoint older stubs, titles are valid UTF-8, paths are
escaped, integrity reports its errors`, body + `Ran: …` + trailer.

## Steps

Each step: write the test, see it fail, fix, see it pass.

### Step 1: Corrections hold the ranges

Test (`core_test.go`): record mood 3, correct it to 42.5 → status 422; register a unitless metric, start a habit, check
in 1, correct to 0.5 → 422; correct to 0 → ok; retract (nil) → ok.

Fix: factor the checks of `Record` into `func (t *Tx) checkValue(metricID int64, metric string, v float64) error`
(it runs the `EXISTS habit_periods` query itself). Call it from `Record`, and from `Correct` when `value != nil`
after reading the wrong row's `metric_id` and metric name (`SELECT me.metric_id, m.name FROM measurements me JOIN
metrics m ON m.id = me.metric_id WHERE me.id = ?`; no row → `notFound`).

### Step 2: Capture refuses a mood it cannot record

Test: delete the `mood` metric row on a fresh database (`s.DB.W.Exec("DELETE FROM metrics WHERE name = 'mood'")` — allowed
while nothing references it), capture with mood 3 → 404, and the day page has no new text (the transaction rolled back).

Fix: check `RowsAffected()` of the mood insert; 0 → `notFound("no metric mood: the owner registers metrics")`.

### Step 3: A save's version includes its body

Test: create a page, read its version `v`, `SaveBody(id, "A", v)`, then immediately `SaveBody(id, "B", v)` → 409,
in a loop of 200 iterations on fresh pages (no sleep). Today this fails some iterations.

Fix: the version is `updated_at + "~" + h`, where `h` is the first 12 hex characters of the SHA-256 of the body. Add
`func version(updated, body string) string` in `write.go`; `PageByID` builds `p.Version` with it (it already reads the
body); `SaveBody` selects `e.updated_at, e.deleted_at, p.body` and compares `version != version(updated, body)`. A
save of the same body with a stale version still passes when nothing changed (same hash, same `updated_at`).
Update README's "Optimistic saves" bullet: "A body save sends the `version` it read (`entities.updated_at` and a hash
of the body); any other is a 409."

Verify also: `go test ./internal/api -run TestSaveCarriesItsVersion` → ok (the API echoes the version it served).

### Step 4: A rename repoints the stubs that pointed at the old page

Test: page `Xold` with body text; a capture `see [[Xold]]` on a day; rename `Xold` → `Ymid`; rename `Ymid` → `Zfinal`.
Expect: `Xold`'s body is `#REDIRECT [[Zfinal]]`; its only `redirect` link points at `Zfinal`'s id; `Zfinal`'s backlinks
(the cookbook read in `read.go`, one hop) include the day.

Fix: in `Tx.Rename`, after the new stub of `id` is written, select every `from_id` of `links WHERE kind = 'redirect' AND
to_id = id`; for each: `t.SetBody(from, "#REDIRECT [["+<target title>+"]]")`, then delete that link row and insert
`links(from, to, 'redirect', …)` exactly as the stub of `id` is linked (links rows may be deleted —
`lifelog_meta.deletes`). Reuse `t.Unlink` if it deletes the redirect kind; otherwise a plain `DELETE FROM links WHERE
from_id = ? AND to_id = ? AND kind = 'redirect'`.

### Step 5: A renamed page keeps its day

Test: create a link-target page (via a capture `[[Ghost]]` dated 2020-01-01), give it a body, rename it → the new page's
`day` is NULL (not today) — read `SELECT day FROM pages WHERE id = ?`.

Fix: in `Rename` read `day sql.NullString` (and **check** the Scan error); when the day is NULL, create the new page
through `insertPage("page", newTitle, text.TitleKey(newTitle), nil, old.Body, "")` plus `syncWikilinks` (what
`CreatePage` does after its day defaulting — keep its look-alike/existing checks: call `t.Lookup(newTitle)` first as
`CreatePage` does); when it is set, keep `CreatePage(newTitle, old.Body, day.String, "")`.

### Step 6: Titles are valid UTF-8

Test (`text_test.go`): `ValidTitle("x\xffy") == false`; `ValidTitle("Zoë") == true`.
Fix: first line of `ValidTitle`: `if !utf8.ValidString(t) { return false }`. Also refuse bodies that are not valid UTF-8
in `SaveBody`, `SetBody`, `CreatePage` and `Capture`: `invalid("the text is not valid UTF-8")`. Test one of them.
Run `go test ./internal/text ./tests -short` — the vector tables must still pass.

### Step 7: Paths are escaped; the hook asks the query, not the path

Test (`db_test.go`): `Init` then `Open` in a folder named `notes#1` and in one named `100%`; the file exists at exactly
that path afterwards (`os.Stat`), and no stray file appears in the parent folder.

Fix: in `dsn` and `Copy`, escape the path for a SQLite URI: `strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(filepath.ToSlash(path))`
(one helper, `uriPath`). In `checkConnection`, decide the role from the query only:
`_, q, _ := strings.Cut(dsn, "?")`; `v, _ := url.ParseQuery(q)`; reader when `v.Get("mode") == "ro"`.
Add `q.Add("_pragma", "busy_timeout(5000)")` to the reader branch of `dsn`.

### Step 8: Integrity reports its errors; the trial is WAL; keyExists reports errors

- `integrity.go`: check every `rows.Scan` error and `rows.Err()` after each loop; return the error (the caller reports
  it) — never a partial verdict.
- `db.Copy`: after `VACUUM INTO`, open the copy with the **writer** DSN, run `PRAGMA journal_mode = WAL`, close. Test:
  after `Copy`, `PRAGMA journal_mode` on the copy reads `wal`.
- `keyExists` → `func (t *Tx) keyExists(importKey string) (bool, error)`; update its caller in `CreatePage` to return
  the error.

### Step 9: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

One test per step (Steps 1–8) in the package tests named above, modelled on `core_test.go`'s `fresh(t)` + `status(err)`
style and `db_test.go`'s `Fresh`. The suites in `tests/` (which run the writer's save) must stay green.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n 'strings.Contains(dsn, "mode=ro")' internal/db/db.go` → no match
- [ ] `grep -n "rows.Scan(&line)$" internal/core/integrity.go` → no match (errors handled)
- [ ] `grep -n "utf8.ValidString" internal/text/text.go` → one match
- [ ] every step's test present and passing; `git status --short` only in-scope files; status row for 033 updated

## STOP conditions

- Step 6 makes a vector of `docs/contract/titles-and-wikilinks.md` fail — the contract would then disagree; report.
- Step 4 needs a link kind or trigger behaviour the schema refuses (e.g. deleting a `redirect` row is blocked) — report
  the error text.
- `_defensive` or `busy_timeout` on readers conflicts with a modernc DSN rule — report; do not drop `mode=ro`.

## Maintenance notes

- The version is opaque to clients: they echo what they read. If another field becomes editable through a versioned
  action, fold it into `version()`.
- Plan 041 (renames) will record in an RFC that a rename repoints older stubs; if the owner decides otherwise, Step 4 is
  the place to change.
