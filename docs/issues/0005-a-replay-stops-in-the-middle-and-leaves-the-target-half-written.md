# 0005 — A replay stops in the middle and leaves the target half-written; there is no dry run

- **Date:** 2026-10-02
- **Status:** resolved
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

A replay now rehearses before it writes ([plan 027](../plans/027-a-replay-rehearses-before-it-writes.md)): the
whole replay runs first on a throwaway `VACUUM INTO` copy of the target, going on past each failure, and the
target is written only when that rehearsal failed nowhere and its integrity checks were clean; otherwise it is
left as it was (a new target is not even created) and every failure is listed. `import replay --dry-run` is the
rehearsal alone. [Importing](../guides/importing.md), "Trial, then the real run", says so;
`TestReplayRehearsesBeforeItWrites` reproduces the half-written target and checks it no longer happens.
