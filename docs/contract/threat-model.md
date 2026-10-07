# Threat model and the 2075 test

**What is protected, and from what.** The asset is `life.db`: prose, health data, the pictures of kept files and
the points of the owner's places, in one plaintext file ([D17](../decisions/D17-contract-as-data.md) — the database is deliberately not encrypted).

| Threat | Control | Residual |
|---|---|---|
| The file is damaged or lost | `synchronous=FULL` and WAL on SQLite ≥ 3.51.3, on a local disk ([connection setup](connections.md)); the integrity checks find damage ([integrity checks](integrity-checks.md)); a dated snapshot, trusted once its restore check passes, is the second copy ([take a snapshot](../cookbook/take-a-snapshot.md), [D25](../decisions/D25-snapshots.md)) | what was written after the last snapshot; a snapshot on the same disk is lost with it, and a copy off the machine is the owner's business ([non-goals](../architecture/non-goals.md)); power loss is documented, not simulated [R67](../research/references.md#r67) |
| A changed value inside the file (bit rot) | a data-checksumming filesystem (btrfs, ZFS), never `chattr +C`, a periodic `scrub` ([integrity checks](integrity-checks.md)) | structural integrity alone cannot detect an arbitrary changed value; FTS content comparison detects changes to indexed tokens, but other undetectable value changes remain |
| A buggy writer, importer or agent | one writing application; triggers for append-only facts, no hard deletes and fixed kinds and retained name ownership; `ON CONFLICT … DO NOTHING`; `BEGIN IMMEDIATE`; `source` on every entity, session, task, task occurrence, link, measurement and habit period; the foreign-key and orphan checks ([D11](../decisions/D11-tombstones.md), [connection setup](connections.md), [integrity checks](integrity-checks.md)) | the pragmas are per connection, so the application asserts them at connect |
| Instructions hidden in an imported source, read by a model ([importing with a model](../guides/importing.md)) | the checked facts workflow verifies source quotes, approved metrics and look-alike names; approval, direct metric creation and replay are owner-only operations; workspace metric registration is model-callable but requires owner-approved rows in the stamped metrics file; the model is instructed to treat source text as data, not instructions; the owner reviews status and the trial before replay ([workflow and direct-write limits](../guides/importing.md#direct-writes-and-their-limits)) | a stamp is a line in a file, so anything that can write the workspace can forge it; direct model writes are allowed and do not receive facts source-quote checks; instructions are not enforcement; status reports non-replayable writes, but does not undo them; a quoted but wrong fact can pass — the owner's review is what holds |
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
| 9 | How do body references become links, and do display labels register names? | `entities`, `entity_names` | `CommonMark`, `invalid names do not block`, `registers no alias` |
| 10 | How does rename preserve identity, and what makes a name valid? | `entity_names`, `entity_names_fixed` | `owners never change`, `Filename`, `NFC`, `Old prose is not rewritten` |
| 11 | Why do ids of different tables coincide? How are rows created? | `entities` | `SAME id`, `RETURNING` |
| 12 | How does `[[Bob Sample]]` reach a person or a place? | `entities`, `people` | `is also a page`, `handle` |
| 13 | Who may write, and with which settings? | `writers` | `BEGIN IMMEDIATE`, `read-only` |
| 14 | What is derived and can be rebuilt? | `entities_fts`, `entity_search_content` | `Rebuild`, `Derived`, `one document per entity` |
| 15 | How do imports avoid duplicates and bad rows? | `writers`, `measurements` | `DO NOTHING`, `OR IGNORE` |
| 16 | How does the schema change once the file holds data it cannot rebuild? | `evolution` | `additive`, `user_version` |
| 17 | Where is the journal? What did I write on a given day? | `entities`, `days` | `plain journal-day identity`, `YYYY-MM-DD`, `neither may transfer` |
| 18 | Which SQLite may write this file? | `sqlite` | `3.51.3`, `3.53` |
| 19 | Who or what wrote this row? | `source` | `written at insert`, `agent` |
| 20 | Does it keep personal planning distinct from recorded facts? | `schema`, `tasks`, `task_occurrences` | `personal task intentions`, `occurrence`, `reminder` |
| 21 | Where was I on a given day? | `links`, `link_kinds` | `places the owner was at`, `kind='at'` |
| 22 | Which metrics are habits, and was one meant to be done on a day? | `habit_periods` | `HABIT`, `NOT RECORDED` |
| 23 | Which metrics are biomarkers, or what the owner took in? | `metrics` | `a category is a page`, `part-of`, `NOT a category` |
| 24 | Where are the photos, the recordings and the scans? | `files` | `never stored in life.db`, `sha256`, `JPEG` |
| 25 | Where is a place, and why does a day have one? | `places` | `never where the owner was`, `radius`, `at link` |
| 26 | Can a type change reinterpret retained edges? | `typed_links`, `entities_endpoint_types` | `incoming`, `outgoing`, `retained` |
| 27 | How are stale edits rejected when write clocks coincide? | `edit_revisions` | `monotonic`, `independent`, `no-ops`, `rollback`, `exhaustion` |
| 28 | How does a session tombstone affect readings? | `measurement_scope` | `live session`, `NULL retraction`, `historical`, `scope` |
| 29 | How do planning identities, stopping and lifecycle agree? | `planning` | `task`, `occurrence`, `tombstone` |

Planning reads and reminders follow the [planning profile](planning.md), including retained outcomes after
ending a recurrence. A clock that cannot be resolved remains visible as unresolved intent; restoring a snapshot
does not send notifications or establish that old reminders should be delivered.
