# Plan 028: A stale gate says what changed

## Status

- **Priority**: P1 (a one-line clarification of `rules.md` left a finished import unable to replay until the owner
  re-read the whole file)
- **Effort**: S
- **Category**: feature
- **Planned and built**: 2026-10-02, on top of `d65b14c`

## Why

[Issue 0006](../issues/0006-one-edited-word-in-a-stamped-file-stops-the-whole-import.md): the owner's stamp on
`rules.md` and `metrics.md` carries a sha256 of the body, so any edit closes the gate — rightly — and every check,
apply and replay refuses. But nothing said *what* changed: `status` reported `stale`, and `lifelog import approve`
printed the whole file again. Each re-approval meant re-reading everything in a terminal.

## What was built

The security property is unchanged: only the owner stamps (the CLI's `approve`, at an interactive terminal, never
an API action or an MCP tool), and any edit after approval closes the gate. The gate still reads only the file's
own stamp.

- **The approved copy** (`internal/importer/approval.go`). When `Approve` stamps a file it writes the same stamped
  text to `<workspace>/.approved/<file>`. No model operation can write there: every other workspace write is a
  fixed file name or a facts path, and a facts path refuses a hidden folder. The copy is used only when its stamp
  covers its own body and, if the file still carries a stamp, it is that same stamp; otherwise the review falls
  back to the whole text.
- **`Review`**: the body approving would stamp (in `metrics.md` every proposed row reads approved), as a
  unified-style line diff against the copy, with two lines of context and hunk headers in the file's own line
  numbers; the whole body the first time. A standard-library LCS over the lines between the common head and tail.
- **`Approve(name, today, shown)`** stamps only the body whose hash the review showed: a file changed while the
  owner read it is refused, nothing stamped.
- **`lifelog import approve`** prints the review: the diff since the last approval, "no line changed", or the whole
  text when there is no usable copy.
- **`status`**: `changed_since_approval` holds the diff (no context, at most 20 lines) for a closed gate with a
  usable copy, and the "do now" sentence names the lines: `Stop: rules.md is stale (changed since the approval:
  line 5); …`.
- **The guide** ([importing](../guides/importing.md)): `.approved/` in the workspace table, *approve* and *status*
  in the operations table, and a paragraph in "Gates" — the copy serves the review, never the gate, and the honest
  limit covers it as it covers the stamp.

## Verification (2026-10-02)

- `go generate ./... && go vet ./... && go test ./...` — green.
- `internal/importer/approval_test.go`: approve, edit one word — the gate is stale, `status` names line 5 and shows
  exactly that `-`/`+` pair; the re-approval diff is one hunk holding only that change; the first approval shows the
  whole body; an added metric row is the only change shown; a copy forged to match the edited body (its own stamp
  correct) leaves the gate stale and the review falls back to the whole text, and a copy edited in place is not
  used; facts paths cannot reach `.approved/`; a file edited after the review is not stamped; the diff itself on
  additions, removals and two hunks.
- Three rules broken on purpose each fail a test: the copy's cross-check against the file's stamp removed, the
  review showing the whole body instead of the diff, `Approve` ignoring what was shown.

## Open

- After `draft-rules` the file reads `status: draft`, with no stamp to check the copy against; the copy is then
  checked only against its own stamp. Anything that can write the workspace directly could make a diff hide a
  change — the guide's honest limit, the same as for the stamp itself.
