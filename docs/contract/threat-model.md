# Threat model and the 2075 test

**What is protected, and from what.** The asset is `life.db`: prose and health data in one
plaintext file ([D17](../decisions/D17-contract-as-data.md) — the database is deliberately not encrypted).

| Threat | Control | Residual |
|---|---|---|
| The file is damaged or lost | `synchronous=FULL` and WAL on SQLite ≥ 3.51.3, on a local disk ([connection setup](connections.md)); the integrity checks find damage ([integrity checks](integrity-checks.md)); a dated snapshot, trusted once its restore check passes, is the second copy ([take a snapshot](../cookbook/take-a-snapshot.md), [D25](../decisions/D25-snapshots.md)) | what was written after the last snapshot; a snapshot on the same disk is lost with it, and a copy off the machine is the owner's business ([non-goals](../architecture/non-goals.md)); power loss is documented, not simulated [R67](../research/references.md#r67) |
| A changed value inside the file (bit rot) | a data-checksumming filesystem (btrfs, ZFS), never `chattr +C`, a periodic `scrub` ([integrity checks](integrity-checks.md)) | no check *inside* SQLite sees it; on a filesystem without checksums nothing does |
| A buggy writer, importer or agent | one writing application; triggers for append-only facts, no hard deletes and fixed kinds and titles; `ON CONFLICT … DO NOTHING`; `BEGIN IMMEDIATE`; `source` on every entity, link, measurement and habit period; the foreign-key and orphan checks ([D11](../decisions/D11-tombstones.md), [connection setup](connections.md), [integrity checks](integrity-checks.md)) | the pragmas are per connection, so the application asserts them at connect |
| Instructions hidden in an imported source, read by a model ([importing with a model](../guides/importing.md)) | the model writes facts files only — never SQL, never an approval; the writer checks every fact's quote against its source file and refuses an unapproved metric or a look-alike name; *approve* is not among the model's tools; the source text is data, never instructions; the owner reviews the trial before the real run | a stamp is a line in a file, so anything that can write the workspace can forge it; a fact that is quoted but wrong passes every check — the owner's review is what holds |
| Another tool editing rows | exploration tools open the file read-only ([connection setup](connections.md), [D14](../decisions/D14-ui-and-tools.md)) | anything with write access to the file bypasses every control |
| A stolen disk | the disk holding `life.db` is encrypted at rest ([D17](../decisions/D17-contract-as-data.md)) | a stolen *unlocked* machine has everything |
| Health data or private notes leaking through git | `life.db` and its `-wal`/`-shm` are never committed; no credentials or account numbers, ever ([non-goals](../architecture/non-goals.md)) | page bodies and `note` fields are free text — the owner's discipline |
| The data exposed on a network | Datasette on localhost only and read-only; nothing that runs arbitrary SQL is reachable from outside ([D17](../decisions/D17-contract-as-data.md)) | a wrong bind address |
| A reader in fifty years without these docs | the 2075 test, below | — |

Out of scope: a hostile local user, malware running as the owner, and legal compulsion — those
need the database itself encrypted ([non-goals](../architecture/non-goals.md)).

**The 2075 test.** A stranger holds `life.db` and nothing else — no documentation, no application.
Every question below must be answerable from `.schema` and `SELECT * FROM lifelog_meta`. The table is
executed: each place named in the third column — a `lifelog_meta` key, or a table, view or trigger
whose `CREATE` statement `.schema` prints — must exist in a fresh database and its text must contain
each phrase in the last column, and **every key of `lifelog_meta` must be used by some question**.

| # | Question | Where the answer is | The answer says |
|---|---|---|---|
| 1 | What is this, and where are its rules? | `schema` | `lifelog`, `CREATE statement` |
| 2 | How is an instant stored? | `instants` | `UTC`, `ISO-8601` |
| 3 | What is a `*_day` column? | `days` | `LOCAL`, `never recomputed`, `IS, not =` |
| 4 | In which time zone was a reading taken? | `measurements` | `IANA` |
| 5 | When was a row written, versus when did it happen? | `instants`, `measurements` | `never back-dated`, `created_at` |
| 6 | Can anything be deleted? | `deletes`, `entities_no_delete` | `tombstone`, `links`, `registries` |
| 7 | Are measurements kept? How is one corrected? | `measurements`, `measurement_values` | `append-only`, `RETRACTS` |
| 8 | Which link kinds exist, and who may link what? | `link_kinds` | `CLOSED registry` |
| 9 | How do `[[wikilinks]]` and `#tags` become links? | `pages` | `CommonMark`, `invalid target makes no link` |
| 10 | Why is a page never renamed? What makes a title valid? | `pages`, `pages_title_fixed` | `never renamed`, `file name`, `NFC` |
| 11 | Why do ids of different tables coincide? How are rows created? | `entities` | `SAME id`, `RETURNING` |
| 12 | How does `[[Bob Sample]]` reach a person or a place? | `entities`, `people` | `is also a page`, `handle` |
| 13 | Who may write, and with which settings? | `writers` | `BEGIN IMMEDIATE`, `read-only` |
| 14 | What is derived and can be rebuilt? | `pages_fts`, `pages` | `rebuild`, `derived` |
| 15 | How do imports avoid duplicates and bad rows? | `writers`, `measurements` | `DO NOTHING`, `OR IGNORE` |
| 16 | How does the schema change once the file holds data it cannot rebuild? | `evolution` | `additive`, `user_version` |
| 17 | Where is the journal? What did I write on a given day? | `pages` | `day page`, `YYYY-MM-DD`, `title equals its day` |
| 18 | Which SQLite may write this file? | `sqlite` | `3.51.3`, `3.53` |
| 19 | Who or what wrote this row? | `source` | `written at insert`, `agent` |
| 20 | Does it keep to-dos and plans? | `schema` | `not a project manager` |
| 21 | Where was I on a given day? | `pages`, `link_kinds` | `places the owner was at`, `kind='at'` |
| 22 | Which metrics are habits, and was one meant to be done on a day? | `habit_periods` | `HABIT`, `NOT RECORDED` |
| 23 | Which metrics are biomarkers, or what the owner took in? | `metric_categories` | `a tree`, `top-level category`, `NOT a category` |
