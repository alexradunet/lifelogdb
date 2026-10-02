# Plan 032: The owner's approval stamps exactly what the owner saw, and the workspace files survive editors and errors

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/importer/workspace.go internal/importer/files.go internal/importer/status.go internal/importer/vault.go internal/importer/importer_test.go cmd/lifelog/main.go`
> Plans 029–031 touch some of these files elsewhere — expected. On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: 029, 031 (same files; run after them). Independent of 030.
- **Category**: security (the approval gate) + bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

During an import the model drafts `rules.md` (aliases, "distinct" pairs) and proposes rows in `metrics.md`; nothing
in them takes effect until the **owner** runs `lifelog import approve rules|metrics` at a terminal, reads the file,
types `approve`, and the writer stamps a hash of the content (README: "an edit closes the gate again"). Two gaps
let the stamp cover something the owner did not see:

1. **Time of check vs time of stamp**: `approve` prints the file, waits for the owner to type, then `Approve` reads
   the file **again** and hashes that. A model writing through its tools (`draft-rules`, `propose-metric`) in another
   process (the MCP server) while the owner reads gets its change stamped unseen.
2. **Hidden text**: the file is printed raw. `DraftRules` keeps any control characters, so a carriage return or an
   ANSI escape sequence (planted by text in a source note — the threat model's "instructions hidden in an imported
   source") can hide an `## Aliases` or `## Distinct` line from the owner's screen; a `Distinct` pair switches off the
   look-alike check for those names.

And the workspace files are fragile:

3. A UTF-8 **BOM** (added by some Windows editors) at the start of `rules.md` makes `Gate` read "missing" — *status*
   then tells the model to draft the rules again, over the owner's file — and drops the first `ledger.md` line.
4. Rewriting the ledger (every `skip` or `apply` mark) **drops every line the parser does not recognise** — a `* [ ]`
   bullet, a comment — so a file vanishes from the import for good.
5. `writeAtomic` never syncs before the rename: after a crash a ledger can be empty.
6. *status* ignores several errors and reports "nothing found" instead (a read error of a source file, a failed dry
   run of the vault notes, a failed gate read) — a false green.

## Current state

- `cmd/lifelog/main.go:390-411` — `import approve`:
  ```go
  file := args[1] + ".md"
  b, err := os.ReadFile(filepath.Join(ws.Dir, file))
  if err != nil {
  	return err
  }
  fmt.Printf("%s\n---\nType approve to stamp %s as yours: ", b, file)
  var answer string
  fmt.Scanln(&answer)
  if answer != "approve" {
  	return errors.New("not approved")
  }
  if err := ws.Approve(file, time.Now()); err != nil {
  ```
- `internal/importer/workspace.go:161-180` — `Approve(name string, today time.Time) error` reads the file under
  `w.mu` (a **per-process** mutex), hashes `rest` (for metrics.md after `approveRows(rest)`), writes the stamp.
  Callers: `cmd/lifelog/main.go:407`, `internal/importer/importer_test.go:77` and `:89`.
- `internal/importer/workspace.go:114-120` — `read(name)`: `strings.ReplaceAll(string(b), "\r\n", "\n")`; no BOM strip.
- `internal/importer/workspace.go:96-112` — `writeAtomic`: `CreateTemp` → `Write` → `Close` → `Rename`; no `Sync`.
- `internal/importer/workspace.go:250-261` — `DraftRules(body)`: normalises `\r\n` only, checks for a status line and
  a `source:` line, writes `"status: draft\n" + body`.
- `internal/importer/files.go:141-166` — `ProposeMetric(m Metric)`; table cells go through
  `cell(s)` (`files.go:58`): `strings.ReplaceAll(strings.ReplaceAll(s, "|", \`\|\`), "\n", " ")` — `\r`, ESC and other
  controls pass.
- `internal/importer/files.go:320-353` — the ledger reader (since `cad659b`, names containing ` — ` are quoted, issue
  0002): `var ledgerLine = regexp.MustCompile(\`^- \[([ x?-])\] (.+)$\`)`; lines that do not match are skipped.
  `files.go:427-447` — `mark(file, f)`: `lines, ok, err := w.Ledger()` … `return w.writeLedger(lines)` — the file is
  regenerated from the parsed lines only.
- `internal/importer/status.go:174` — `src, _ := w.ReadSource(f.File)` (error dropped);
  `status.go:216-234` — `pendingNotes`: `s.DryRun(...)`'s error dropped and `ByImportKey` errors dropped;
  `status.go:238-262` — `editedNotes`: `s.DryRun(...)` error dropped, `t.ByImportKey`/`t.Body`/`t.Lookup` errors
  dropped. `internal/importer/vault.go:197` — `if g, _ := w.Gate("rules.md"); g != "approved"`.
  **Do not touch `status.go:192-212`** (the two `s.Query` calls): plan 037 replaces them.
- Test fixture: `internal/importer/importer_test.go:73-92` — `approveRules` and the metrics helper call
  `f.w.Approve(name, time.Now())`. `TestGates` (`:122`) and `TestLedger` (`:142`) are the patterns.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Importer | `go test ./internal/importer` | ok |
| Build | `go build ./cmd/lifelog` (then delete the binary) | exit 0 |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/importer/workspace.go`, `internal/importer/files.go` (ledger reader, `mark`, `cell`,
`ProposeMetric`), `internal/importer/status.go` (lines 174 and 214–262 only), `internal/importer/vault.go` (line 197
only), `internal/importer/importer_test.go`, `cmd/lifelog/main.go` (`import approve` only).

**Out of scope**: `status.go:192-212` (plan 037); a cross-process file lock (see Maintenance); `docs/` (the guide
already requires an atomic ledger and an approval that covers content).

## Git workflow

Branch `advisor/032-approval-and-workspace-files`; message e.g. `import: approve stamps the bytes it showed, shows
control characters, refuses them in drafts; BOMs, unknown ledger lines and crashes no longer lose work; status reports
its errors`, body + `Ran: …` + trailer.

## Steps

### Step 1: Tests first

In `internal/importer/importer_test.go`:
- `TestApproveStampsWhatWasShown`: draft rules, read the file's bytes (`shown`), then `DraftRules` again with an added
  alias, then `f.w.Approve("rules.md", time.Now(), shown)` → error mentioning `changed`; `Gate("rules.md")` is
  `draft`. With the current bytes → nil and `approved`.
- `TestDraftsRefuseControlCharacters`: `DraftRules` with a body containing `"\r"` alone, `"\x1b[8m"` and `"‮"`
  → each refused; with `"\t"` → accepted. `ProposeMetric` with a note containing `"\x1b"` → refused.
- `TestWorkspaceFilesSurviveEditors`: write `rules.md` with a leading `"﻿"` and a valid stamp computed the
  normal way (approve first, then prepend the BOM to the file) → `Gate` is `approved`. Write a ledger with a BOM on the
  first line, a `* [ ] Extra.md` line and a `<!-- owner note -->` line; `Ledger()` lists the BOM'd file and
  `Extra.md`; after `Skip` of another file, the comment line is still in `ledger.md`, byte for byte.

**Verify**: `go test ./internal/importer -run 'TestApproveStampsWhatWasShown|TestDraftsRefuseControlCharacters|TestWorkspaceFilesSurviveEditors'`
→ fails (compile error for the new `Approve` signature is expected).

### Step 2: `Approve` takes the bytes the owner saw

Change the signature to `func (w *Workspace) Approve(name string, today time.Time, shown []byte) error`. Inside, after
reading the file (`read` returns the normalised text — compare the **raw** bytes instead: read with `os.ReadFile`),
refuse with `fmt.Errorf("%s changed while you were reading it: run approve again", name)` when
`!bytes.Equal(raw, shown)`. Update both test helpers to read the file and pass its bytes, and update
`cmd/lifelog/main.go` to pass `b`.

**Verify**: `go build ./cmd/lifelog` → exit 0; `TestApproveStampsWhatWasShown` passes.

### Step 3: Control characters are refused in drafts and shown when approving

- Add `func visible(s string) string` in `workspace.go` that replaces every rune that is a control character other than
  `\n` and `\t` (`unicode.IsControl`), and every bidirectional formatting character (U+061C, U+200E, U+200F,
  U+202A–U+202E, U+2066–U+2069), with `fmt.Sprintf("\\u{%04X}", r)`; and `func hasHidden(s string) bool` (true when
  `visible(s) != s`).
- `DraftRules`: after the `\r\n` normalisation, `if hasHidden(body) { return refuse("the rules hold control or
  direction characters: write plain text") }`.
- `ProposeMetric`: refuse when any of `m.Name, m.Unit, m.Note, m.From, m.Doubts` has hidden characters.
- `cmd/lifelog/main.go` `import approve`: print `importer.Visible(string(b))` (export it as `Visible`) instead of `b`;
  keep passing the raw `b` to `Approve`.

**Verify**: `TestDraftsRefuseControlCharacters` passes.

### Step 4: BOMs and unknown ledger lines

- `read(name)`: strip one leading `"﻿"` after the `\r\n` normalisation. (The stamp hash covers `rest`, the text
  after the status line, so a BOM before the status line does not change it.)
- Ledger reader: accept `-`, `*` or `+` bullets: `^[-*+] \[([ x?-])\] (.+)$`.
- `mark`: stop regenerating the file. Read the raw text (via `read`), split it into lines, find the index of the line
  whose parsed `File` equals `file`, replace **only that line** with the newly formatted line (reuse the formatting of
  `writeLedger` for one line — factor out `formatLedgerLine(l Line) string`), and write the joined text back with
  `writeAtomic`. `MakeLedger` keeps using `writeLedger`.

**Verify**: `TestWorkspaceFilesSurviveEditors` passes; `TestLedger` still passes.

### Step 5: Workspace writes reach the disk

`writeAtomic`: call `tmp.Sync()` after `Write` and before `Close`; on error remove the temp file and return it.

**Verify**: `go test ./internal/importer` → ok.

### Step 6: *status* says when it could not check

- `status.go:174`: on a `ReadSource` error, `st.Mismatches = append(st.Mismatches, f.File+": "+err.Error())` and skip
  the reading keys of that file.
- `pendingNotes`: return `(int, error)`; propagate `ByImportKey` errors out of the closure and the `DryRun` error to
  the caller, which appends `"vault notes: " + err.Error()` to `st.Mismatches`.
- `editedNotes`: the same — propagate errors from `t.ByImportKey`, `t.Body`, `t.Lookup` and `DryRun`, and append them
  to `st.Mismatches`; a `ReadSource` error of a note is a mismatch too (`n.Path + ": " + err.Error()`), not a silent
  `continue`.
- `vault.go:197`: `g, err := w.Gate("rules.md"); if err != nil { return nil, err }`.

Add a test: a done ledger file whose source file was deleted from disk → `Status` lists a mismatch naming it.

**Verify**: `go test ./internal/importer` → ok.

### Step 7: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

Four new tests (Steps 1 and 6), patterned on `TestGates` and `TestLedger`. Existing importer tests keep passing with
the updated `Approve` calls.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n 'fmt.Printf("%s\\n---' cmd/lifelog/main.go` → no match (the raw print is gone)
- [ ] `grep -n "tmp.Sync()" internal/importer/workspace.go` → one match
- [ ] `grep -n "src, _ := w.ReadSource" internal/importer/status.go` → no match
- [ ] `git status --short` lists only in-scope files; status row for 032 updated

## STOP conditions

- The stamp hash would change for files approved before this plan (it must not: the BOM is outside `rest`) — if
  `TestGates` fails on an old stamp, STOP.
- Replacing only the marked ledger line breaks issue 0002's quoting (a name with ` — `) — report.
- You find a third writer of `rules.md`/`metrics.md` besides `DraftRules`, `ProposeMetric` and `Approve` — report it.

## Maintenance notes

- **Issue [0006](../issues/0006-one-edited-word-in-a-stamped-file-stops-the-whole-import.md)** (filed by the owner in
  `d65b14c`, after this plan was written): approve prints no diff, so every re-approval means re-reading the whole file.
  Step 3's `Visible` print is the natural place to show a diff against the last approved text — not in this plan's
  scope; do not mark 0006 resolved here.
- The workspace mutex is per process; the CLI, `serve` and `mcp` are separate processes. Concurrent marks from two
  processes can still lose one update. If the owner runs `serve` and `mcp` on one workspace at once, add an OS lock file
  around read-modify-write (deferred: no portable stdlib lock).
- `Visible` is the one place that decides what the owner sees; reuse it if any other model-written text is shown at a
  terminal for a decision.
