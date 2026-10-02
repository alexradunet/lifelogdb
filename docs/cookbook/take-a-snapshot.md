# Take a snapshot, check it, restore it (D25)

A snapshot is the second copy of `life.db` ([D25](../decisions/D25-snapshots.md)): one file, consistent, dated, never
overwritten, and trusted only once its restore check passes. Every claim below about what SQLite does is executed.

**1. Take it** on a connection that opened `life.db` read-only, into a file that does not exist yet, named by the
local day: `life-2026-10-02.db`; a second snapshot on the same day adds the local time, `life-2026-10-02T143005.db`.
From a shell: `sqlite3 -readonly -cmd "PRAGMA trusted_schema=OFF" life.db "VACUUM INTO 'life-2026-10-02.db'"`.

```sql
-- on a connection to life.db opened read-only (mode=ro), while the writer goes on writing
VACUUM INTO :snapshot;   -- 'life-2026-10-02.db', a name no file has yet
```

- The snapshot holds every committed row, those still only in `life.db-wal` too, and nothing of a transaction still
  open: a writer holding `BEGIN IMMEDIATE` does not stop it.
- SQLite refuses a target that has content (`output file already exists`) but writes into an existing **empty**
  file, so the program that names the snapshot checks that no file has the name.
- A connection with `PRAGMA query_only = ON` cannot take it (`attempt to write a readonly database`).
- The snapshot is in rollback-journal mode: `PRAGMA journal_mode` reads `delete` on it, and it is one file, with no
  `-wal` beside it.

**Where it lives.** A snapshot holds the same health data and private notes as `life.db`, so it is never inside a git
work tree (git history cannot be scrubbed — [connection setup](../contract/connections.md)). Next to `life.db` it
protects against a bad write, not against a lost disk; carrying it off the machine is the owner's business
([non-goals](../architecture/non-goals.md)). Nothing writes to a snapshot.

**2. The restore check**, before the snapshot is trusted: open it with the writer's connection settings
([connection setup](../contract/connections.md)), run the four [integrity checks](../contract/integrity-checks.md),
and close it **without** `PRAGMA optimize`. The FTS5 check is an `INSERT`, refused on a `mode=ro` connection
(`attempt to write a readonly database`), so the check needs the writer's settings; the four checks write nothing and
leave the snapshot as it was, byte for byte, where `PRAGMA optimize` can write to it. A snapshot that fails the check is
not one to restore.

**3. Restore**, when `life.db` is damaged or lost. With nothing holding `life.db` open — no writer, no reader — move
`life.db`, `life.db-wal` and `life.db-shm` aside, copy the snapshot to `life.db`, put it back in WAL mode once, then
run the four integrity checks on it:

```sql
PRAGMA journal_mode = WAL;   -- once, on the restored life.db: the snapshot is in rollback-journal mode
```

The old `life.db-wal` must not stay beside the restored file: SQLite reads it into the copy, which then answers
`database disk image is malformed`. What was written after the snapshot is not in it.
