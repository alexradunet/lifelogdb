# Plan 026: A link to a page another file promotes is an ordering error

## Status

- **Priority**: P1 (the replay of the first real import aborted on it: [issue 0004](../issues/0004-an-about-link-applied-before-the-persons-own-note-fails.md))
- **Effort**: S
- **Category**: bug
- **Planned and built**: 2026-10-02, on top of plan 024

## Why

A vault's notes are all created as plain pages first; a note becomes a person or a place only when its own facts
file is applied. A daily note's `about` link to such a page, applied before that file, met the `link_kinds` endpoint
trigger ("link endpoint type not allowed for this kind"): a hard error, while only a *missing* row counted as "not
written yet", the one refusal the replay retries. So the replay stopped on the first daily note ledgered before its
person's file, and the only way through was to reorder `ledger.md` by hand.

## Options

- **A first pass of every person and place write before any link.** Breaks "a file's facts commit whole or not at
  all" ([importing](../guides/importing.md)): half of a file would be written in another transaction, and the
  single *apply facts* would still fail the same way.
- **Treat the trigger's "endpoint type not allowed" as retryable.** Masks real errors until the end of the replay
  (an `about` link to a day page would be retried, though no write can ever make it fit) and reads the error text
  of the DDL.
- **Chosen: a plain page where the kind needs a person or a place is "not written yet".** Before the link, the
  writer reads the kind's endpoint types from `link_kinds`; an end that is still a plain page (not a day page, not
  a redirect stub, both never promoted) where the kind accepts a person or a place but no page is refused as not
  written yet, with what to do. Every other mismatch is left to the trigger and stays a hard error. The replay,
  which already applies such a file again after the rest, needs no new rule; a file still refused when a pass
  writes nothing stops it with that refusal.

## What was built

- `core.Tx.LinkEnds(kind)`: the entity types a registered kind accepts at each end, read from `link_kinds`.
- `internal/importer/apply.go`: `notPromotedYet` before every link write; "not written yet" is a typed error
  (`errNotYet`, wrapped by `notYet`, still a `core.Error` 422) instead of a phrase in a message, so the replay
  matches the cause and a title containing those words cannot make a file retry. The message names the page, the
  kind and the type it needs: `"Bob Sample" is still a plain page, and the about link needs a person or place
  there: that person or place is not written yet; write it earlier in the file, or apply the file that writes it
  first`.
- `internal/importer/replay.go`: `Replay` retries on `waitsForAnother(err)` (one condition and the doc comment).
- [Importing](../guides/importing.md): "References are titles" names the plain-page case and the outright refusal;
  "Trial, then the real run" says the ledger's order never decides the outcome and when a replay stops.

## Verification (2026-10-02)

- `TestLinkBeforePromotion` (`internal/importer`), on the issue's two notes after *apply a vault plan*: the day's
  `about` link applied before `Contacts/Bob Sample.md` is refused as not written yet (422, naming the page); after
  Bob's file it applies; a replay with the day ledgered before Bob's file succeeds with no differences, and a
  second replay writes nothing; a link to a page no file promotes is refused alone and stops the replay naming the
  page; an `about` link to a day page is the trigger's refusal and is never retried.
- Broken on purpose, each fails the test: without the check, the issue's exact error
  (`link endpoint type not allowed for this kind`); with the replay's retry off, the replay stops on the day file.
- `go generate ./... && go vet ./... && go test ./...` — green.

## Open

- Nothing for this issue. A replay that stops still leaves the target as far as it got:
  [issue 0005](../issues/0005-a-replay-stops-in-the-middle-and-leaves-the-target-half-written.md).
