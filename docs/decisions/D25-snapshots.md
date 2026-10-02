# D25 — Snapshots: a dated `VACUUM INTO` copy, trusted once its restore check passes.

**Status:** accepted

- **Context.** `life.db` is a lifetime file with one writer ([D1](D01-single-sqlite-file.md), [D3](D03-integer-ids.md)), and
  from the freeze on it holds rows that no import can replay ([D13](D13-migrations-and-freeze.md)). The integrity checks
  find damage but repair nothing ([integrity checks](../contract/integrity-checks.md)), and a writer bug or a lost disk
  had nothing to fall back on: the [threat model](../contract/threat-model.md) said "no second copy of the file is
  kept". The freeze checklist asked the owner to decide the export, snapshot and off-box copy row of the
  [non-goals](../architecture/non-goals.md); the owner reopened snapshots alone.
- **Decision.** The second copy of `life.db` is a **snapshot**: a `VACUUM INTO` copy made through a read-only
  connection, while the writer goes on writing; a file named by the local day ([D10](D10-time-model.md)), with the
  local time added when that day has one already; never written over an existing file; kept outside any git work
  tree. It is trusted once its **restore check** passes: the four [integrity checks](../contract/integrity-checks.md)
  on the snapshot, left unchanged by them. A restore copies a checked snapshot back while nothing holds `life.db`
  open, without the old `-wal`, and puts the file back in WAL mode. The steps and the SQL are
  [take a snapshot](../cookbook/take-a-snapshot.md); the schema does not change.
- **Alternatives.**
  - *Copying the file with the operating system* (`cp life.db`): rejected — while the writer writes, the file
    alone is not the database: committed rows wait in `life.db-wal` until a checkpoint, and a copy of `life.db`
    without it does not hold them.
  - *The SQLite online backup API*: rejected as the rule — it gives the same consistent copy, but only through a C
    interface each language binds differently; `VACUUM INTO` is one statement any connection can run, and it is the
    copy the [imports](../contract/imports.md) trial already uses.
  - *A snapshot on a schedule, kept N deep, rotated*: not decided here — when and how many is the owner's routine;
    a snapshot is never overwritten, so nothing is lost by taking one more.
  - *A markdown export, a CSV dump, an off-box copy, continuous replication*: kept out
    ([non-goals](../architecture/non-goals.md)). Getting a snapshot off the machine is the owner's business, with
    the owner's tools.
- **Trade accepted.** A snapshot is as old as its date: what was written after it is lost on a restore. Next to
  `life.db` it shares that disk's fate; the mitigation is the owner's copy elsewhere. The restore check runs on a
  writer's connection to the snapshot (the FTS5 check is an `INSERT`), so it must close without `PRAGMA optimize`.
- **Sources.** Executed: a read-only connection makes the copy while a write transaction is open, with every
  committed row and nothing uncommitted, where a copy of `life.db` alone misses the rows still in its `-wal`; an
  existing file with content is refused, an empty one is written; the copy is in rollback-journal mode; the restore
  check leaves it unchanged; a restored file beside the old `-wal` is malformed
  ([take a snapshot](../cookbook/take-a-snapshot.md)).
