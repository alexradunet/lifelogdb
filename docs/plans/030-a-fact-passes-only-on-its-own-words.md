# Plan 030: A fact passes the import checks only on its own words, and a reading's key comes from the source alone

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/importer/facts.go internal/importer/importer_test.go docs/guides/importing.md docs/issues/README.md`
> Plan 029 edits `importer_test.go` (new tests only) — expected. Any change to `facts.go` or to the quoted guide lines:
> compare with the excerpts; on a mismatch, STOP.

## Status

- **Priority**: P1 — the reading keys become permanent at the first real import
- **Effort**: M
- **Risk**: MED (stricter checks refuse some facts files that pass today — intended; keys of trial rows change, so a
  trial must be rebuilt)
- **Depends on**: 029 (same test file; run after it)
- **Category**: bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

In an import a local model writes "facts" (people, places, links, readings), each with a **quote** from the source
file; the writer refuses any fact its quote does not support. The threat model relies on it: "the writer checks
every fact's quote against its source file" (`docs/contract/threat-model.md`, the row on instructions hidden in an
imported source). Several checks are weaker than the guide says (each executed by the importer audit at `9ac130f`):

- **Value check**: `value "5 ng/mL"` passes with the quote `ferritin 1.5 ng/mL`, and `"8"` passes in `4,8` (a comma
  decimal the guide sends to `kept_as_text`), and `5` in `-5 kg`. The guide: "a value matches as a whole number in
  its quote".
- **A link from the note's own day** passes with any quote of 3+ characters from the file
  (`{"link": {"from": "<the note's day>", "to": "Riverside Pool", "kind": "at"}, "quote": "mood: 4"}` passed), because
  the file's own day counts as "named" by every quote.
- **Any unitless metric with value 0 or 1** skips the value check — including **mood 1** on a 1–5 scale, which is
  not a 0/1 marker.
- **`kept_as_text`** falls back to a plain substring test: a one-letter quote (`"a"`) passes, hiding a dropped row.
- **`write-facts` with trailing junk** (`{…}}`) stores an **empty** facts file (the `json.Indent` error is ignored).
- **A reading's key depends on the facts file, not the source.** The guide promises the key's last part is the
  reading's "place … among that metric's readings of that day in the **source** file, by where its quote first appears
  — so editing the facts file never changes a key". The code counts only the readings the facts file lists: apply the
  evening reading alone (key `…|1`), add the morning one later, and the check refuses the file ("its key already holds
  52, not 48"). The guide's wording cannot be implemented as written — the writer cannot count readings the facts do
  not list — so the docs change too (with an issue, per `docs/process.md`).

## Current state

- `internal/importer/facts.go:204-245` — `wholeIndex(hay, needle)` accepts a match when it neither starts nor ends
  inside a *word* (`isWordChar`: letters, numbers, marks, `_`). `.`, `,`, `-` are not word characters, which is why
  `5` matches inside `1.5`. `lastRune` (`:239-245`) converts the whole prefix to `[]rune` on every call.
- `internal/importer/facts.go:252-279` — `numberRE = ^([+-]?\d+(?:\.\d+)?)\s*(.*)$`; `parseValue(v, unit)` returns
  `numText` (the number as written, sign included); line 264 compiles `regexp.MustCompile(\`^,\d\`)` on every call.
- `internal/importer/facts.go:333-343` — inside `checkStatic`:
  ```go
  states := func(title string) bool { // the quote names the title, or a name an alias maps to it; a daily note names its own day
  	if containsFold(q, title) || (day != "" && title == day) {
  		return true
  	}
  	...
  }
  ```
  and `:386-388` for links: `if !states(l.To) && !states(l.From) { bad(i, "the quote names neither end of the link") }`.
- `internal/importer/facts.go:404-412`:
  ```go
  num, numText, _, err := parseValue(r.Value, r.Unit)
  ...
  marker := m.Unit == "" && (num == 0 || num == 1)
  if !marker && wholeIndex(q, numText) < 0 {
  	bad(i, "the value %s is not in the quote as a whole number", numText)
  }
  ```
  `m` is the approved `Metric` from `metrics.md`; a habit is a metric whose row has a `since` (field `m.Since`, see
  `internal/importer/inspect.go:51`).
- `internal/importer/facts.go:418-424` — `kept_as_text`:
  `wholeIndex(src, collapse(k.Quote)) < 0 && !strings.Contains(srcLower, strings.ToLower(collapse(k.Quote)))` → error.
- `internal/importer/facts.go:103-117` (`parseFacts`, uses `dec.More()` to detect extra data) and `:148-150`
  (`WriteFacts`: `json.Indent(&pretty, data, "", "  ")`, error ignored, then `writeAtomic`).
- `internal/importer/facts.go:433-461` — `readingKeys(f, pos)`: a reading with `taken_at` gets
  `file|reading|metric|day|<taken_at>`; others are grouped by `metric|day`, sorted by `pos` (the byte index of the
  quote's first whole-word match in the collapsed source), and numbered `1..n` **among the readings of the facts
  file**: `file|reading|metric|day|<n>`.
- `docs/guides/importing.md:162-167` (the keys bullet; current truth, writer-neutral):
  ```
  - **Keys are derived, never written.** The writer derives each new row's `import_key` from the source
    path and the write: the kind and title for a person, place or page; for a reading, the day and
    whatever tells apart two readings of one metric on one day in one file (its `taken_at`; else its place, counted from 1,
    among that metric's readings of that day in the **source** file, by where its quote first appears — so editing the
    facts file never changes a key). A row that already exists keeps its id and key. The key is stable on every run
  ```
- `docs/guides/importing.md:215` — "(for a unitless 0/1 marker, the quote holds the result word instead)". The guide
  never defines "the result word"; this plan does not invent one (see STOP / Maintenance).
- `README.md:75-76` — "`path|reading|<metric>|<day>|<taken_at, or its place in the source file>`" — stays true.
- Tests: `internal/importer/importer_test.go:270-347` (`TestReadingsKeysAndReplay`) hard-codes the key
  `"Medical/Ferritin.md|reading|ferritin|2031-03-01|1"` (line ~311) — it must change with the key format.
  `TestRefusals` (`:222`) is the pattern for "this facts file is refused".
- Issues are numbered `NNNN`; `docs/issues/README.md` lists 0001–0006 (0004–0006 were added in `d65b14c`). This plan adds **0007** (if
  0007 is taken when you run, use the next free number and say so in the commit).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Importer tests | `go test ./internal/importer` | ok |
| Docs suites | `go test ./tests -run 'TestSuites/document' -v` | `N/N` |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/importer/facts.go`, `internal/importer/importer_test.go`, `docs/guides/importing.md` (the keys
bullet only), `docs/issues/0007-a-reading-key-cannot-count-readings-the-facts-do-not-list.md` (create),
`docs/issues/README.md` (one row).

**Out of scope**: the person/place/page key format (`path|kind|title_key`); vault note keys; `apply.go`; the
definition of "the result word" (owner decision); `schema.sql`.

## Git workflow

Branch `advisor/030-facts-checks-and-keys`; one commit, e.g. `import: values match as whole numbers, a link quote
names its other end, kept_as_text is whole words, write-facts refuses trailing data; a reading key is its quote's
place in the source (issue 0007)`, body + `Ran: …` + trailer.

## Steps

### Step 1: Failing tests first

In `internal/importer/importer_test.go` add `TestQuotesMustStateTheFact`, modelled on `TestRefusals`. Use the
fixture's `Medical/Ferritin.md` and `Journal/2031-04-11.md`, and add any one-off source file you need to the
`vault` map **only if** no existing file fits (adding a file changes ledger counts in other tests — prefer a new
fixture variable used by this test alone, written with `os.WriteFile` into `f.w.Source` before `MakeLedger`; check
the exported field name in `workspace.go`). Each case must be **refused** by `f.w.Check`:

1. value `"5 ng/mL"` with a quote containing `1.5 ng/mL` (write such a line into the test's own source file);
2. value `"8"` with quote containing `4,8`;
3. value `"5 kg"` with quote containing `-5 kg`;
4. a link `from` the note's day `2031-04-11` `to` `"Riverside Pool"`, kind `at`, quote `"mood: 4"`;
5. a reading of `mood` value `"1"` with a quote that has no `1` in it (mood is unitless, not a habit);
6. a `kept_as_text` with quote `"a"`.

And `WriteFacts(file, []byte(\`{"file":"Recipes.md","writes":[]}}\`))` must return an error, and the facts file on
disk must be unchanged (write a good one first, then the bad one, then `LoadFacts` → the good one).

Add `TestReadingKeysComeFromTheSource`: on `Medical/Twice.md` (two readings of one metric on one day), apply the
**evening** reading alone; then write facts with **both** readings; `Check` must report `1 new, 1 existing`
(not a refusal).

**Verify**: `go test ./internal/importer -run 'TestQuotesMustStateTheFact|TestReadingKeysComeFromTheSource'` → FAIL.

### Step 2: Numbers match as whole numbers

Add `numberIndex(hay, num string) int` next to `wholeIndex`: like `wholeIndex`, but a match is rejected when the
character **before** it is a digit, `.`, `,`, `+` or `-`, or the text **after** it starts with a digit, or with `.`/`,`
followed by a digit. Use `utf8.DecodeLastRuneInString` / `utf8.DecodeRuneInString` (not `[]rune` conversions); also
switch `lastRune` to `utf8.DecodeLastRuneInString`. In the reading check use `numberIndex(q, numText)` instead of
`wholeIndex(q, numText)`. Hoist the `^,\d` regexp in `parseValue` to a package-level `var`.

**Verify**: cases 1–3 refused.

### Step 3: Markers are habits only; a link names its other end; kept_as_text is whole words

- `marker := m.Unit == "" && m.Since != "" && (num == 0 || num == 1)` — only a habit's 0/1 check-in skips the value
  check. (If `Metric` has no `Since` field with that meaning, STOP.)
- Split `states` into `names(title)` (the quote names the title or an alias of it — today's body **without** the
  `title == day` shortcut) and `states(title)` = `names(title) || (day != "" && title == day)`. Keep `states` for
  person/place/page. For links: when one end is the file's own day, require `names(<the other end>)`; otherwise keep
  `states(l.To) || states(l.From)`.
- `kept_as_text`: refuse when `len([]rune(collapse(k.Quote))) < 3`, or when
  `wholeIndex(srcLower, strings.ToLower(collapse(k.Quote))) < 0`. Drop the plain `strings.Contains` fallback.

**Verify**: cases 4–6 refused; `go test ./internal/importer` — existing tests still pass (if `TestRefusals` or
`TestReadingsKeysAndReplay` now fails because a fixture fact relied on the old leniency, fix the **fixture's quote**
so it states the fact, and say so in the commit; never loosen a check).

### Step 4: `write-facts` refuses trailing data and never stores a broken file

In `parseFacts`, after `dec.Decode(&f)`, replace the `dec.More()` test with:
`if _, err := dec.Token(); err != io.EOF { return nil, refuse("facts file: one JSON object only") }`.
In `WriteFacts`, check `json.Indent`'s error and return `refuse("facts file: %v", err)` before writing.

**Verify**: the `WriteFacts` part of Step 1 passes.

### Step 5: A reading's key is its quote's place in the source

In `readingKeys`: for a reading without `taken_at`, the last key segment is `@` followed by `pos[i]` (decimal) —
the byte offset of its quote's first whole-word match in the collapsed source, already computed by `checkStatic`.
No grouping or numbering. If two readings of one metric on one day get the **same** key (same quote), the check
refuses the file: `"readings %d and %d of %s on %s quote the same words: quote each value's own words, or give
taken_at"`. Implement that refusal where the keys are used for checking (find the caller of `readingKeys` in
`apply.go`/`facts.go` and add the duplicate check there; it must run in both *check* and *apply*).

Update `TestReadingsKeysAndReplay`: replace the hard-coded `…|2031-03-01|1` lookup with a lookup of the id by value
(`SELECT id FROM measurements WHERE day = '2031-03-01' AND value = 48 AND supersedes_id IS NULL` on `f.s.DB.R`).

**Verify**: `go test ./internal/importer` → ok, including `TestReadingKeysComeFromTheSource`.

### Step 6: The guide says what the writer can do, with an issue behind it

Create `docs/issues/0007-a-reading-key-cannot-count-readings-the-facts-do-not-list.md` from
`docs/issues/template.md`: date 2026-10-02, status `resolved`; "Seen in: the writer's import, on a synthetic file with
two readings of one metric on one day"; what happened (the guide's "place, counted from 1, among that metric's readings
of that day in the source file" cannot be computed from the source alone — the writer only knows the readings a facts
file lists, so a key changed when a reading was added later); reproduce (the two-reading steps of Step 1); rules
involved (`guides/importing.md` keys bullet, `contract/imports.md`, `cookbook/import-a-row-once.md`); resolution:
"the key's last part is the place of the reading's quote in the source file (its offset), plan 030". Add its row to
`docs/issues/README.md` (status `resolved`, resolved by `plan 030`).

In `docs/guides/importing.md`, replace the parenthesis in the keys bullet with: `(its \`taken_at\`; else the place in
the **source** file where its quote first appears — so editing the facts file never changes a key, and two readings
of one metric on one day must quote different words)`. Change nothing else in the guide.

**Verify**: `go test ./tests -run 'TestSuites/document' -v` → `N/N` (the issue page is reachable from the issues
index, every link resolves).

### Step 7: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

`TestQuotesMustStateTheFact` (six refusals + write-facts), `TestReadingKeysComeFromTheSource`, updated
`TestReadingsKeysAndReplay`. Pattern: `TestRefusals`.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "strings.Contains(srcLower" internal/importer/facts.go` → no match
- [ ] `grep -n "json.Indent(&pretty, data" internal/importer/facts.go` shows its error handled
- [ ] `grep -n "counted from 1" docs/guides/importing.md` → no match
- [ ] issue 0007 exists and is listed in `docs/issues/README.md`
- [ ] `git status --short` lists only in-scope files; status row for 030 updated

## STOP conditions

- The owner's canonical `life.db` already holds imported readings (a real run happened): changing the key format would
  re-import them as new rows. STOP and ask.
- `Metric` has no field that says "this metric is a habit" — report; do not guess from the name.
- A check this plan tightens makes the owner's in-progress import workspace fail en masse (if you can see one): report
  the count, do not loosen.
- You are tempted to define "the result word" for habit markers — STOP; that is the owner's (the guide leaves it open).

## Maintenance notes

- A trial imported before this plan has positional keys `…|1`, `…|2`; rebuild the trial (`import setup` again) after
  it lands. The real run must use the new keys.
- "The result word" of a habit marker is undefined in the guide; until the owner defines it, a habit check-in passes
  the value check without one. An issue is the place to settle it.
- `numberIndex` is the place to change if units glued to numbers (`48ng/mL`) must match.
