# 0005 — A replay stops in the middle and leaves the target half-written; there is no dry run

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the import, during `import replay --to <db>`

## What happened

`Replay` walks the ledger file by file. One file that fails is fatal: the replay returns an error and
the target keeps whatever was already written. There is no way to know in advance whether a replay will
succeed.

Observed: a replay aborted on the first daily note (issue 0004) after creating every planned page in the
target. The next replay reported `pages_created: 0, unchanged: 559` — the target had been changed by the
run that failed. Recovery worked only because every write is idempotent, but the target was in a
half-imported state in between, and nothing said so.

## Reproduce

On a fresh database and a source folder whose ledger lists a daily note before the person note it names
(the ordering in issue 0004):

1. `lifelog import setup --workspace <source>.lifelog --from <empty db>`, approve rules and metrics, `apply-vault`, write and apply the facts.
2. `lifelog import replay --to <fresh db>` → 422 on the daily note.
3. `lifelog --db <fresh db> query "SELECT count(*) FROM pages"` → the pages from the failed replay are there.

## Rules involved

- [Importing](../guides/importing.md) — "Trial, then the real run".
- [Integrity checks](../contract/integrity-checks.md) — they run at the end of a replay, which is exactly
  the point that never gets reached.

## Resolution

_(open)_ A `replay --dry-run` that runs every facts check against the trial and reports the failures
before touching the target would make the real run a formality.
