# Lifelog — Database Schema v1

**Status:** freeze candidate. No canonical database exists yet; until one does, §3 is edited in place (D13). The next step is the capture path and one real import (§2.8), not another review.
**Scope of the project:** A lifetime personal database (a journal of day pages, notes and wiki pages,
tasks, people, places, health metrics, location history, personal finance — holdings, balances, net worth; events
and file attachments deferred — D22, D9) in a single SQLite file, plus a custom UI for data entry and daily use.
Other devices are clients of the one writing application (D3); everything else (view generators, AI
features, sync or merge between copies of the database) is explicitly out of scope.

This document is self-contained and written to be reviewed cold. Each rule has **one home**: a table's
rules are the constraints, triggers and comments of its `CREATE` statement in §3; the conventions the DDL
cannot hold (time, the wikilink grammar, connection settings, imports) are in §2; §5 says *why*, citing
constraint names instead of restating them; §6 shows the SQL in use. *Executed* means that a suite in
`tests/` runs the claim against §3 (`tests/README.md` lists the suites).

---

## Table of contents

1. [Goals and design principles](#1-goals-and-design-principles)
2. [Storage contract (what the DDL cannot hold)](#2-storage-contract-what-the-ddl-cannot-hold)
3. [The schema (canonical DDL)](#3-the-schema-canonical-ddl)
4. [Entity model overview](#4-entity-model-overview)
5. [Decision log](#5-decision-log)
6. [Query cookbook](#6-query-cookbook)
7. [Explicit non-goals and deferred work](#7-explicit-non-goals-and-deferred-work)
8. [References](#8-references)

---

## 1. Goals and design principles

**The goal:** one database that a single human writes to for ~50 years and that remains
readable and meaningful long after the current application is gone. The dominant risk is
not SQLite durability (settled — see D1); it is *meaning living only in application code*,
and *canonical data being corrupted by uncontrolled writers*.

**Principles, in priority order:**

1. **Pareto / KISS.** Choose the 20% of mechanism that delivers 80% of the value. Every
   table and column must justify itself against "what if we just didn't have it."
   Complexity that was researched and *deliberately cut* is listed in §7 so it is not
   silently re-added. The freeze makes this urgent: after it, change is additive only
   (principle 4), so whatever is in the schema then stays.
2. **The schema is the documentation.** Real tables, real column names, real types, real
   constraints. A stranger in 2075 should understand the database from `.schema` output and
   `SELECT * FROM lifelog_meta` alone — so the rules each table needs are comments *inside* its
   `CREATE` statement, the only comments the file keeps, and `lifelog_meta` holds the few rules that
   span tables. (SQLite's own "application file format" essay makes exactly this argument — see [R1].)
3. **Single writing application.** One application (later with CLI/API/agent
   front-ends) owns all writes to the database. Many
   *processes* are fine — one *writer* owning the conventions. No other app is ever
   pointed at the canonical data with write access (see D3).
4. **Additive-only evolution after freeze.** Once real data exists, schema changes are
   `ADD COLUMN` / `CREATE TABLE` / new indexes and numbered forward-only migrations.
   SQLite explicitly blesses additive change as its compatibility mechanism
   ("adding new tables or columns does not change the meaning of prior queries" [R1]).
5. **Derived data is disposable.** The FTS index and `title_key` can be dropped and rebuilt
   from canonical data at any time; nothing else is derived. `life.db` is irreplaceable.
   (Binary files are out of v1 entirely — see D9.)
6. **One home per concept, one home per rule.** A concept is stored once (D6: mood lives in
   `measurements` and nowhere else; a named entity's name is its page title, D20), and a rule is
   written once (the reading guide above). A fact that can be derived from another column is not
   stored beside it.
7. **Real use drives change.** A new constraint, trigger or convention needs a real incident behind
   it — a failed import, a bug in the writing application, a question the data could not answer —
   or it must replace something it makes redundant. Hypothetical writers are not incidents.

---

## 2. Storage contract (what the DDL cannot hold)

These conventions are part of the schema's meaning but cannot be expressed as a constraint. The
ones that span tables are also rows of `lifelog_meta` (§3), so the file carries them.

### 2.1 Time

- **Instants** (`*_at` columns): UTC, ISO-8601, millisecond precision,
  e.g. `2026-06-09T21:14:03.482Z`. Written by the app, never by SQLite defaults
  (SQLite's `CURRENT_TIMESTAMP` is second-precision and non-ISO; `strftime('%Y-%m-%dT%H:%M:%fZ','now')`
  is the in-DB form used by triggers) [R5][R6][R28]. Milliseconds because `%f` always renders
  `SS.SSS`, which gives one fixed-width string that the round-trip CHECK can compare, that sorts
  chronologically as plain text, and that keeps rows written within the same second in order (a
  balance and its correction) (executed).
- **Local days** (`*_day` columns): the *local calendar date where the thing happened or
  was captured*, TEXT `YYYY-MM-DD`, written at insert time in the zone of the device that captured
  it — a phone's, never the clock or zone of a hub on a server (D3).
  **Never derived from the UTC instant at query time.** This survives timezone changes,
  DST, and travel: "the day I graduated" is a local-date fact, not an instant [R7][R8][R9].
- **Round-trip CHECKs.** Every day column is checked with `date(x) IS x`, every instant with
  `strftime('%Y-%m-%dT%H:%M:%fZ', x) IS x`. The `IS` matters: a CHECK passes when it evaluates
  to NULL, and `date()` returns NULL for malformed input, so `date(x) = x` silently *accepts*
  `2026-9-3` (executed).
- **Written versus happened.** `created_at` — on `entities`, `links`, `measurements`, `balances` and
  `positions` alike — is when the row was written to `life.db`, never back-dated, so it is an audit
  trail. When a thing *happened* is its own `day` / `*_at`. (An entry in a day page has no time
  of its own; a time worth keeping is written in its text: a known limit, D5.)
- **Zone.** `entities.tz`, `measurements.tz` and `positions.tz` store the capturing device's IANA zone
  (`Europe/Berlin`; NULL = unknown), so a UTC instant can be read as local time. Only capture time
  can supply it.

### 2.2 Identity and provenance

- **Entity rows first, ids by `RETURNING`.** A domain row's `id` equals its `entities.id`. The app
  inserts the `entities` row with `INSERT … RETURNING id`, keeps the id in a variable and binds it
  in the same transaction (§6.1). Never `last_insert_rowid()` across statements: any insert in
  between — a link, a ghost page, a measurement — moves it, and the next row silently points at the
  wrong entity (executed).
- **A person, place or holding is a page (D20).** It is one id with three rows: `entities`
  (`entity_type = 'person'`), `pages` (`entity_type = 'person'`, titled — the title is the handle that
  `[[wikilinks]]` write) and `people`. Insert them in that order in one transaction (§6.19). A ghost
  page an earlier `[[Name]]` created is *promoted* instead: `UPDATE entities SET entity_type = 'person'`
  (the foreign key cascades it to `pages.entity_type`), then insert the `people` row. The foreign keys
  refuse a person without a page and a person turned back into a page (executed). Title uniqueness already refuses a second `Sam`, so two people called Sam are told apart
  in the handle (`Sam (barber)`); `people.name` is the editable full name.
- **Provenance.** `source` on `entities`, `links`, `measurements`, `balances` and `positions` names the writer
  of the row — `ui`, `cli`, `api`, `agent:<name>`, `import:<name>` (lowercase `[a-z0-9_:.-]`, 1–64
  characters). Only the moment of writing knows it, so it is required at insert and never changes;
  with agents among the writers (D3) it is how a wrong row is traced to the writer that made it. An
  importer's `import_key` — on entities and on facts — is unique per `source`, so its name is also the
  deduplication namespace.
  What kind of figure a balance is (a statement, an estimate) goes in its `note`.

### 2.3 Deletion and corrections

- **Nothing is deleted except `links` rows.** Entities are tombstoned (`entities.deleted_at`), and
  `BEFORE DELETE` triggers reject deleting an `entities` row or any domain row, also on a connection
  that forgot `foreign_keys` (executed). Every read path filters `deleted_at IS NULL`. Junk captured by
  accident is tombstoned like everything else.
- **Facts are corrected by inserting, never by editing.** `measurements` and `balances` reject
  `UPDATE` and `DELETE`. A measurement is corrected by a row whose `supersedes_id` names it (at most
  one per row; correct the correction to change it again); a balance by a newer row for the same
  `(holding_id, day)`. A NULL `value` / `amount` **retracts**. The views `measurement_values` and
  `balance_values` are the read rule. `positions` rejects both too, and has no correction at all: a
  GPS fix is raw sensor output, so a doubtful one is left out when read, by `accuracy_m` (D21).
- **Imports insert with `ON CONFLICT(…) DO NOTHING`**, never `INSERT OR IGNORE` (it also skips rows
  that violate a CHECK or NOT NULL, silently) and never `OR REPLACE` (a delete, blocked only when
  `recursive_triggers=ON`, §2.6) (both executed).

**A correction never overwrites.** One reading, corrected, retracted and restored — what
`measurement_values` shows after each insert (§6.10). `balance_values` works the same way, with the
newest row per `(holding_id, day)` in place of `supersedes_id`.

```mermaid
%% diagram: correct-measurement
stateDiagram-v2
    direction LR
    state "view shows 71.2" as V1
    state "view shows 70.8" as V2
    state "view shows nothing (retracted)" as V3
    state "view shows 71.4" as V4
    [*] --> V1: INSERT row 1, value 71.2
    V1 --> V2: INSERT row 2, value 70.8, supersedes 1
    V2 --> V3: INSERT row 3, value NULL, supersedes 2
    V3 --> V4: INSERT row 4, value 71.4, supersedes 3
```

### 2.4 Prose, wikilinks and titles

`pages.body` is CommonMark text. Wiki references are written inline as `[[Page Title]]`; a `#tag`
is read as `[[tag]]`, so tags are just pages (D5). The app never rewrites the body: `#health` stays
`#health` in the database.

**The save contract (D19).** Saving a page body is one `BEGIN IMMEDIATE` transaction (§6.13): the
body, then the page's `links(kind='wikilink')` rows, made **equal to the set of pages the body
names** — missing rows added, rows the body no longer supports deleted — so a re-save changes nothing
and every link can be rebuilt from the bodies alone. The `links` table is the source of truth for the
graph; the body text is the source of truth for prose. The rules, all executed against the vectors
below:

- *What is read.* The CommonMark **text** of `pages.body`, after NFC normalisation — not code
  spans, code blocks, raw HTML, link destinations or image alt text. A conformant CommonMark parser
  yields exactly this, so nothing is hand-parsed (the app uses goldmark; the reference in `tests/` uses
  markdown-it-py [R59], which loses a code span that follows an unclosed `[`, where the CommonMark
  reference implementation keeps it).
- *Wikilink.* `[[title]]` or `[[title|alias]]`, with no `[`, `]` or line break inside, and the
  brackets and title in **one** run of plain text (`[[Health *Diet*]]` is not a link, and
  `[[Diet]](url)` is a Markdown link). The title is the text before the first `|`, trimmed of
  spaces (U+0020 only, like the DDL's `trim`); the alias is display text the database ignores.
  There is no `#anchor` form (`#` is an ordinary title character, so `[[C#]]` works) and no
  escape (`\[[x]]` still links; write a literal `[[x]]` in a code span). Nothing else is
  normalised: `[[Health  Diet]]` with two spaces is a different page from `[[Health Diet]]`.
- *Tag.* `#` followed by words of letters, marks, digits and `_` (Unicode categories L, M, N)
  joined by single `-`, where the `#` is **not** glued to a preceding such character, `/` or `#`
  — so `C#`, `a#b`, `http://x/#frag` and `##x` are not tags — and the word is not all digits
  (`#12` and `#2024` are not tags, `#2024-review` is). Wikilinks are read first and their text
  is not re-read for tags (`[[Project #alpha]]` is one title). `# Heading` (with a space) is a
  heading; `#Heading` is not a CommonMark heading, so it is a tag. `#Health` and `#health` are
  one page.
- *Stub pages.* A body that starts with `#REDIRECT [[` (any case, leading whitespace allowed) is
  a rename stub (below): it gets no wikilinks and no tags — its one edge is the `redirect` link
  the app writes — so a stub never shows up as a backlink. Elsewhere the word `#redirect` alone
  is never a tag, so nothing can create a page called `redirect`.
- *An invalid target makes no link and never blocks a save.* A title `pages_title_safe` rejects
  (`[[Health/Diet]]`, `[[Re: plan]]`, the tag `#con`) is skipped. The app checks the title rules
  before inserting — its predicate agrees with the DDL's CHECKs on more than 40 000 generated
  strings — and creates each target inside its own `SAVEPOINT` (§6.13), so even a target the
  predicate wrongly let through is rolled back alone: the page is saved and no orphan `entities`
  row is left. The UI reports skipped targets; nothing is stored about them.
- *A page never links to itself* (`[[Diet]]` inside the page `Diet` is ignored), and *a
  tombstoned target is revived*, not duplicated: the unique index covers tombstoned pages, so the
  save un-tombstones the page it resolves — any save that names it, an old day page edited years
  later included, so the UI tells the owner.
- *Named pages.* A person's, place's or holding's page is a page like any other, so `[[Bob
  Sample]]` is an ordinary wikilink and nothing in the save contract knows about people (D20).
- *Known limits.* A body that also defines a reference (`[Ref]: http://r`) turns `[[Ref]]` into
  a Markdown link; a `#` written as an entity (`&#35;x`) is decoded before the scan and counts as
  a tag; a typo (`[[Sm]]`) makes a ghost page like any other (§6.12).

Test vectors — every writer must reproduce them (`\n`, `́`, `̈` stand for a line
break and combining marks; a body is shown in a code span):

| body | links to (`title`s, in order) |
|---|---|
| `See [[Diet plan]].` | `Diet plan` |
| `[[Diet plan\|my diet]]` | `Diet plan` |
| `[[  Diet plan  ]]` | `Diet plan` |
| `[[C#]] and [[Page#Section]]` | `C#`, `Page#Section` |
| `[[Health/Diet]]` | — |
| `[[Re: plan]]` | — |
| `[[]] [[ ]] [[\|alias]]` | — |
| `[[Café]] [[CAFÉ]] [[Café]] [[cafe]]` | `Café`, `cafe` |
| `[[Café notes]]` | `Café notes` |
| `\[[escaped]]` | `escaped` |
| `` text `[[code]]` text `` | — |
| `a\n\n~~~\n[[fence]]\n~~~\n\nb` | — |
| `[[Diet]](http://y)` | — |
| `[[Health *Diet*]]` | — |
| `[[Ref]]\n\n[Ref]: http://r` | — |
| `[[CON]] [[nul]] [[Com1]] [[CONSOLE]] [[COM10]] [[LPT0]]` | `CONSOLE`, `COM10`, `LPT0` |
| `[[CON.backup]] [[nul.txt]] [[COM¹]] [[LPT².x]] [[a.CON]] [[CONSOLE.txt]]` | `a.CON`, `CONSOLE.txt` |
| `Feeling good #health today` | `health` |
| `#Health and #health and #HEALTH` | `Health` |
| `#tag. #tag2, (#paren) "#quoted" #end-` | `tag`, `tag2`, `paren`, `quoted`, `end` |
| `#café #zürich` | `café`, `zürich` |
| `# Heading\n\n## Sub\n\n### Sub sub` | — |
| `#Heading` | `Heading` |
| `I write C# and F# and a#b` | — |
| `[a](http://x/#frag) <http://x/#auto> http://x/#bare` | — |
| `issue #12 and #2024 but #2024-review` | `2024-review` |
| `[[Project #alpha]] #beta [[#gamma]]` | `Project #alpha`, `beta`, `#gamma` |
| `#con #nul #console` | `console` |
| `#REDIRECT [[New Title]]` | — |
| `see #REDIRECT [[New Title]]` | `New Title` |

(Also: a 240-byte title is a link, a 241-byte one is not; 80 × `日` = 240 bytes is, 81 is not.)

**Renames.** A title never changes (`pages_title_fixed`, D5). To fix one: create the new page, make
the old page a one-line stub (`#REDIRECT [[New Title]]`) and add `links(kind='redirect', from=old,
to=new)`. Consumers follow one hop; `redirect` links are excluded from backlink queries (§6.5).

**Titles.** The rules are the CHECKs `pages_title_len` and `pages_title_safe` (§3): 1–240 bytes,
trimmed, and a valid file name on Linux, macOS and Windows — the strict direction on purpose (D5).
The app is stricter in one way: it also rejects code points Unicode has not assigned yet (category
`Cn`), whose case fold a later Unicode version could define — which would silently change
`title_key` (executed). Unicode promises a stable case fold only for assigned characters, and formally
only for text in NFKC form [R74]; a title with a compatibility character (full-width `Ｃａｆé`, a
ligature, `x²`) is outside that promise, a known limit (§7).

**`title_key`.** Uniqueness is on the key, not on the title. The function is fixed:
`title_key = NFC(casefold(NFC(title)))` — in Python
`unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())`. A writer in another
language must reproduce these vectors exactly:

| title | `title_key` |
|---|---|
| `Café notes`, `Café notes` (NFD), `CAFÉ NOTES` | `café notes` |
| `Straße`, `STRASSE` | `strasse` |
| `ΣΑΣ`, `σας` (final sigma) | `σασ` |
| `Ǆ` | `ǆ` |
| `ﬁle` (ligature) | `file` |
| `İstanbul` | `i̇stanbul` (`i` + U+0307) |
| `日本語 ノート`, `Diet` | `日本語 ノート`, `diet` |

The database verifies what it can (`pages_key_*`: trimmed, no ASCII capitals, `lower(title)` for a
pure-ASCII title); that a non-ASCII key is the *right* fold is the writing application's duty
(principle 3) — a writer that computes it wrongly gets uniqueness wrong and nothing else. Resolve a
`[[wikilink]]` with `WHERE title_key = :key`, a search on the unique index `pages_title` (executed).

**Day pages.** The journal is one page per local day, titled with that day: `2026-09-29`. Its `day`
is its title (`pages_day_page`), so a day page is the page whose title equals its day, and its key is
its title (a pure-ASCII title). Capture appends to today's page and creates it on the first write
(§6.1); `[[2026-09-29]]` reaches it like any other title, and a link that names a day before anything
was written that day creates that day's page, empty (§6.13). A day page is an ordinary page in every
other way: its `[[links]]` say who the day was with and where (§6.3).

### 2.5 Integrity checks

Four checks tell whether a file still obeys the schema. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import (§2.8) or a
migration (D13), and after any writer crashed. Every claim below was executed on the **live**
file, not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages WHERE entity_type = 'page' UNION SELECT id FROM tasks UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM holdings);   -- no rows
INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1);   -- no error
```

- **`integrity_check` — the file's structure.** It caught a zeroed table page, a file truncated by
  three pages, and an index entry that no longer matches its row (a flipped byte in a `title_key`
  inside `pages_title`).
- **`foreign_key_check` — what the first cannot see.** A writer that forgot `PRAGMA foreign_keys=ON`
  (§2.6 — per connection; `STRICT` does not enforce foreign keys) stored a balance for a holding that
  does not exist, and `integrity_check` said `ok`.
- **The orphan query — the one check no constraint can express.** An `entities` row with no domain
  row (a writer that died between its inserts, or a person with a page but no `people` row): both
  other checks are clean on it.
- **The FTS5 integrity-check — the index against its content.** `pages_fts` is an external-content
  index over `pages`; if the two drift apart, searches return wrong rows and `integrity_check` still
  says `ok`. The FTS5 command with rank `1` compares the index with `pages` and fails. The index is
  derived: `INSERT INTO pages_fts(pages_fts) VALUES('rebuild')` repairs it. Writing it is a write, so
  it runs on a writer connection.
- **What none of them sees: a changed value.** A flipped byte inside a body passed
  `integrity_check` — SQLite keeps no page checksums. The cheap guard is below the file: keep
  `life.db` on a filesystem that checksums data (btrfs and ZFS do by default; never `chattr +C` the
  file or its folder, which turns btrfs checksums off) and scrub it now and then. SQLite's own
  `cksumvfs` [R64] does the same per page inside the file, at the cost of an extension every writer
  must load; it is not used.

### 2.6 Connection setup (every writer, mandatory)

```sql
PRAGMA journal_mode = WAL;     -- persistent; set once by the init DDL (§3)
PRAGMA synchronous  = FULL;    -- per connection. NORMAL in WAL "might roll back following a power loss" [R54]
PRAGMA foreign_keys = ON;      -- MANDATORY per connection: SQLite's default is OFF
PRAGMA recursive_triggers = ON;  -- MANDATORY per connection: with OFF, INSERT OR REPLACE / REPLACE INTO
                               -- deletes the conflicting row WITHOUT firing the append-only DELETE triggers
PRAGMA busy_timeout = 5000;    -- wait instead of failing instantly on SQLITE_BUSY
PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)
```

A writer reads these back at connect time and refuses to run if `foreign_keys` or
`recursive_triggers` is 0 or `synchronous` is not 2 (FULL, which is also SQLite's default,
executed) — none of these is stored in the file, and `PRAGMA foreign_keys` is a silent no-op inside
a transaction (executed). It also refuses to run on a SQLite older than **3.51.3**: every version from
3.7.0 to 3.51.2 has a WAL race in which a write that lands while two checkpoints overlap can be lost
— rare, but this design has several writer processes and readers on one file, which is exactly the
condition [R65]. Migrations need 3.53 (D13).

Two more settings cost nothing. `SQLITE_DBCONFIG_DEFENSIVE` (a C-level switch, in Python
`conn.setconfig(sqlite3.SQLITE_DBCONFIG_DEFENSIVE, True)`) makes the FTS shadow tables and
`writable_schema` untouchable from SQL; with it and `trusted_schema = OFF` the whole schema and every
§6 query still work (executed). `PRAGMA optimize` when a connection closes keeps the planner's
statistics fresh.

The driver must not open transactions of its own. Python's `sqlite3` in its default mode silently
sends a deferred `BEGIN` before the first `INSERT`/`UPDATE`/`DELETE`; open the connection with
`autocommit=True` (Python 3.12+) or `isolation_level=None` and issue `BEGIN IMMEDIATE` yourself.

**Every write transaction starts with `BEGIN IMMEDIATE`.** A deferred `BEGIN` that reads first —
resolve a wikilink, then create the page (§6.13) — fails **at once** with `database is locked`
if another writer committed in between: `busy_timeout` does not apply to that lock upgrade
(executed). `BEGIN IMMEDIATE` takes the write lock up front, so a second writer waits
(up to `busy_timeout`) and then sees the first one's rows (executed). Keep such transactions short.

Readers need no setup but must be **read-only**: open the file with `?mode=ro` (SQLite then
refuses every write — `attempt to write a readonly database`, executed) or `sqlite3 -readonly`.
Datasette does this by itself (executed by the optional Datasette suite). Under WAL a reader sees the
live file while the app writes and never blocks it (executed). sqlite-web can edit rows, which would
make it a second writer, so it is not used (D14). Three reader traps [R65][R66]:
- **Never `immutable=1`** (Datasette's `-i`): it tells SQLite the file cannot change, so a reader of
  the live file sees stale or inconsistent pages while the app writes. Use the default `mode=ro`.
- **Keep read transactions short.** A checkpoint cannot reset the WAL while any reader holds a
  snapshot; a reader that never lets go makes `life.db-wal` grow without bound.
- **One machine, a local disk.** WAL needs shared memory between the processes, so `life.db` never
  lives on a network file system (NFS, SMB) or in a folder a sync client (Dropbox, Syncthing,
  iCloud) copies while it is open.

`life.db`, `life.db-wal` and `life.db-shm` are never committed to git: binary churn, and git
history cannot be scrubbed of finance data.

"Single writing application" (principle 3, D3) does not mean a single OS process: the
app, its CLI, the API service and local agents are all the same *writer* as long as
they go through the one application stack that owns the insert conventions.

```mermaid
%% diagram: writers
flowchart LR
    ui["UI"] --> app
    cli["CLI and API"] --> app
    agents["agents"] --> app
    app["the one writing application<br/>insert conventions, wikilink sync,<br/>title_key, pragmas checked at connect"]
    app -->|"BEGIN IMMEDIATE, then write"| db[("life.db<br/>SQLite, WAL")]
    db -.->|"readers never block the writer"| ro["read-only tools<br/>Datasette, mode=ro"]
```

### 2.7 Money (D18)

The rules are in §3 (`currencies`, `holdings`, `balances`, `balance_values`). What the DDL cannot say
is what to model: **a holding is anything with a balance or a value, and its balance is the value in
the holding's currency on that day, as the statement, the app or your own estimate says.** Record
**your own share** of joint items.

| You own | Make a holding | Balance is |
|---|---|---|
| deposit, savings, cash | `category 'deposit'` / `'cash'`, its own currency | the statement balance |
| stocks, funds, pension | one per broker or wrapper (`'brokerage'`, `'pension'`); one per currency if you hold several | the portfolio's market value |
| crypto | one per exchange or wallet (`'crypto'`), valued in the fiat you track it in | its market value that day |
| house, car, watch, art | `'property'`, `'vehicle'`, `'valuables'` | your estimate (say so in `note`) |
| mortgage, loan, card | `side = 'liability'` | the amount owed, positive |

Sell everything or dispose of it: record a final balance and set `closed_day`. Net worth is
derived on read, one figure per currency (§6.15–§6.16). **Never store** credentials, PINs or full
account/card numbers in `life.db` or in a page: the file is plaintext (D17); a holding's page may
hold the last four digits.

```mermaid
%% diagram: money-flow
flowchart LR
    stmt["statement, app or own estimate<br/>the value on a local day"] -->|"INSERT, never edit"| bal[("balances<br/>integer minor units<br/>append-only")]
    bal -->|"balance_values:<br/>newest row per day,<br/>NULL retracts"| held["each open, live holding<br/>latest balance on or before the day<br/>asset adds, liability subtracts"]
    acc[("holdings<br/>side, currency")] --> held
    held --> sum["sum per currency<br/>in that currency's minor units"]
    sum --> nw(["net worth on that day<br/>one figure per currency<br/>derived, never stored"])
    cur[("currencies<br/>subunits")] -.->|"whole units"| nw
```

### 2.8 Threat model, the 2075 test, and imports

**What is protected, and from what.** The asset is `life.db`: prose, health and finance in one
plaintext file (D17 — the database is deliberately not encrypted).

| Threat | Control | Residual |
|---|---|---|
| The file is damaged or lost | `synchronous=FULL` and WAL on SQLite ≥ 3.51.3, on a local disk (§2.6); the integrity checks find damage (§2.5) | nothing recovers it: no second copy of the file is kept (§7); power loss is documented, not simulated [R67] |
| A changed value inside the file (bit rot) | a data-checksumming filesystem (btrfs, ZFS), never `chattr +C`, a periodic `scrub` (§2.5) | no check *inside* SQLite sees it; on a filesystem without checksums nothing does |
| A buggy writer, importer or agent | one writing application; triggers for append-only facts, no hard deletes and fixed kinds and titles; `ON CONFLICT … DO NOTHING`; `BEGIN IMMEDIATE`; `source` on every row; the foreign-key and orphan checks (§2.3, §2.6, §2.5) | the pragmas are per connection, so the application asserts them at connect |
| Another tool editing rows | exploration tools open the file read-only (§2.6, D14) | anything with write access to the file bypasses every control |
| A stolen disk | the disk holding `life.db` is encrypted at rest (D17) | a stolen *unlocked* machine has everything |
| Finance, health or location data leaking through git | `life.db` and its `-wal`/`-shm` are never committed; no credentials or full account numbers, ever (§2.6, §2.7) | page bodies and `note` fields are free text — the owner's discipline |
| The data exposed on a network | Datasette on localhost only and read-only; nothing that runs arbitrary SQL is reachable from outside (D17) | a wrong bind address |
| A reader in fifty years without this document | the 2075 test, below | — |

Out of scope: a hostile local user, malware running as the owner, and legal compulsion — those
need the database itself encrypted (§7).

**The 2075 test.** A stranger holds `life.db` and nothing else — no `SCHEMA.md`, no application.
Every question below must be answerable from `.schema` and `SELECT * FROM lifelog_meta`. The table is
executed: each place named in the third column — a `lifelog_meta` key, or a table, view or trigger
whose `CREATE` statement `.schema` prints — must exist in a fresh database and its text must contain
each phrase in the last column, and **every key of `lifelog_meta` must be used by some question**.

| # | Question | Where the answer is | The answer says |
|---|---|---|---|
| 1 | What is this, and where are its rules? | `schema` | `lifelog`, `CREATE statement` |
| 2 | How is an instant stored? | `instants` | `UTC`, `ISO-8601` |
| 3 | What is a `*_day` column? | `days` | `LOCAL`, `never recomputed`, `IS, not =` |
| 4 | In which time zone was a row written? | `entities`, `measurements` | `IANA` |
| 5 | When was a row written, versus when did it happen? | `instants`, `measurements` | `never back-dated`, `created_at` |
| 6 | Can anything be deleted? | `deletes`, `entities_no_delete` | `tombstone`, `links` |
| 7 | Are measurements kept? How is one corrected? | `measurements`, `measurement_values` | `append-only`, `RETRACTS` |
| 8 | In what unit are amounts? How do I get net worth? | `balances`, `holdings` | `minor units`, `never stored`, `per currency` |
| 9 | Which balance row wins for a day? | `balances` | `highest id` |
| 10 | Which link kinds exist, and who may link what? | `link_kinds` | `CLOSED registry` |
| 11 | How do `[[wikilinks]]` and `#tags` become links? | `pages` | `CommonMark`, `invalid target makes no link` |
| 12 | Why is a page never renamed? What makes a title valid? | `pages`, `pages_title_fixed` | `never renamed`, `file name`, `NFC` |
| 13 | Why do ids of different tables coincide? How are rows created? | `entities` | `SAME id`, `RETURNING` |
| 14 | How does `[[Bob Sample]]` reach a person, a place or a holding? | `entities`, `people` | `is also a page`, `handle` |
| 15 | Who may write, and with which settings? | `writers` | `BEGIN IMMEDIATE`, `read-only` |
| 16 | What is derived and can be rebuilt? | `pages_fts`, `pages` | `rebuild`, `derived` |
| 17 | How do imports avoid duplicates and bad rows? | `writers`, `measurements` | `DO NOTHING`, `OR IGNORE` |
| 18 | Does anything repeat? | `tasks` | `do not repeat` |
| 19 | How does the schema change after real data exists? | `evolution` | `additive`, `user_version` |
| 20 | Where is the journal? What did I write on a given day? | `pages` | `day page`, `YYYY-MM-DD`, `title equals its day` |
| 21 | Which SQLite may write this file? | `sqlite` | `3.51.3`, `3.53` |
| 22 | Who or what wrote this row? | `source` | `written at insert`, `agent` |
| 23 | Where was I at a given moment? Where is a place? | `positions`, `places` | `WGS84`, `append-only`, `query time` |

**Imports** — the path for data that already exists elsewhere (a journal archive, a health export,
statements). Every step was executed on 1 000 synthetic rows:

1. **Trial run first.** Rows are never deleted, so a bad import can only be retracted row by row
   (a NULL-value correction for a measurement or a balance, a tombstone for an entity). Do the
   first run of any new importer on a *copy*: `sqlite3 life.db "VACUUM INTO '/tmp/trial.db'"`.
2. **Load the rows into a scratch database, never into `life.db`** (`sqlite3 scratch.db ".import
   --csv weights.csv staging"`), then insert in one `BEGIN IMMEDIATE` transaction per batch:

```sql
ATTACH 'scratch.db' AS s;
BEGIN IMMEDIATE;
INSERT INTO measurements(metric_id, day, taken_at, tz, value, created_at, source, import_key)
SELECT (SELECT id FROM metrics WHERE name = 'weight'), day, NULLIF(taken_at, ''), NULLIF(tz, ''), value,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:scale', id
  FROM s.staging WHERE true
ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
COMMIT;
DETACH s;
```

   Three traps, each executed. **`WHERE true`** is required: in `INSERT … SELECT … FROM … ON CONFLICT`
   SQLite reads the `ON` as a join's, and answers "a JOIN clause is required before ON" (or `near "DO":
   syntax error`). **The conflict target repeats the index's `WHERE`**: the unique
   index is partial, and without it SQLite answers "ON CONFLICT clause does not match any PRIMARY
   KEY or UNIQUE constraint". **Never `CAST(value AS REAL)`**: it turns `'abc'` and `''` into `0.0`
   and `'12.5kg'` into `12.5`, silently — a plain insert into the STRICT column converts `'12.5'` and
   rejects `'abc'` and `''`. A CSV empty field is `''`, not NULL, so optional columns go through
   `NULLIF(…, '')`; a `''` in `taken_at` fails its CHECK. A fourth, for links: **never `INSERT OR
   REPLACE` into `links`** — on a symmetric kind the replace and the two mirror triggers keep firing
   each other, and SQLite stops with `too many levels of trigger recursion` (executed). Use
   `ON CONFLICT(from_id, to_id, kind) DO NOTHING`.
3. **Identity and time.** `source` names the importer (`import:<name>`), `import_key` is the source's
   own id, `day` / `taken_at` / `tz` say when it happened, `created_at` is when you imported it — on
   every table, `created_at` is the write time (§2.1). An imported task or page carries
   its key on `entities` (§6.21). **The key must come out the same on every run**: the source's own id
   (a Health Connect record's id; a to-do app's task id). A source without ids gets a key built from
   fields it never changes (a note's file name, the day of a reading) — and a change to one of them then
   looks like a new row. The key deduplicates within one `source` only: the same reading from two
   sources is two rows, for the app to match and the owner to retract one.
4. **What a failure does.** `ON CONFLICT … DO NOTHING` skips only a duplicate key: a malformed
   day, an impossible value or a dangling foreign key still raises and the **whole batch rolls back**.
   Fix the data and run the batch again.
5. **Check afterwards:** the four checks of §2.5, per-source counts (`SELECT source, count(*),
   min(day), max(day) FROM measurements GROUP BY source`), and **run the importer a second time — it
   must insert nothing.**
6. **Before the freeze**, run steps 1–5 once with a real export on a copy: a real import is the one
   test this schema has never had.

## 3. The schema (canonical DDL)

This is the canonical init DDL. Until the freeze it is edited **in place** here — there
is no `0001_init.sql` file yet (D13); a test database is created by applying this block
to a fresh file (`tests/lib/docsql.py` extracts it).

```sql
-- ============================================================
-- Lifelog schema v1: the single init file, edited in place until the freeze;
-- numbered migrations begin only after real data exists (D13).
-- Each table's rules are comments INSIDE its CREATE statement, so .schema shows them (a comment
-- outside a statement is not stored in the file); the rules that span tables: SELECT * FROM lifelog_meta;
-- Every writer connection: SQLite >= 3.51.3; PRAGMA foreign_keys = ON;
-- PRAGMA recursive_triggers = ON; PRAGMA synchronous = FULL; PRAGMA trusted_schema = OFF;
-- and every write transaction starts with BEGIN IMMEDIATE (section 2.6).
-- ============================================================
PRAGMA application_id = 0x4C494645;   -- 'LIFE' — recognizable to file(1) and tools
PRAGMA user_version  = 1;
PRAGMA journal_mode  = WAL;           -- persistent; readers (Datasette) don't block the writer

CREATE TABLE lifelog_meta (
  -- the rules that span tables, readable with a SELECT by someone who has only this file;
  -- the rules of one table are comments inside its own CREATE statement
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
INSERT INTO lifelog_meta(key, value) VALUES
  ('schema',    'lifelog v1: the journal (one page per day), wiki, tasks, people, places, health metrics, location history and money of one person; the rules of each table are comments inside its CREATE statement (.schema), the rules that span tables are these rows'),
  ('instants',  'every *_at column is a UTC ISO-8601 TEXT instant with milliseconds, e.g. 2026-06-09T21:14:03.482Z, written by the app; CHECK strftime(''%Y-%m-%dT%H:%M:%fZ'', x) IS x; created_at, on every table that has it, is when the row was written to life.db, never back-dated (when a thing happened is its day or its other *_at)'),
  ('days',      'every *_day column (and day) is the LOCAL calendar date YYYY-MM-DD where the thing happened, written at insert, never recomputed from an instant; CHECK date(x) IS x (IS, not =: a CHECK passes on NULL, and date(''2026-9-3'') is NULL)'),
  ('deletes',   'nothing is deleted except links rows: an entity is a tombstone (entities.deleted_at), measurements and balances are corrected by inserting rows, positions are never corrected; BEFORE DELETE triggers enforce it'),
  ('source',    'entities, links, measurements, balances and positions: source names the writer of the row (ui, cli, api, agent:<name>, import:<name>); written at insert, never changed; the import_key a writer gives a row is unique per source'),
  ('writers',   'one writing application; every connection sets foreign_keys=ON, recursive_triggers=ON, synchronous=FULL, trusted_schema=OFF and starts write transactions with BEGIN IMMEDIATE; every other tool opens the file read-only; imports use INSERT ... ON CONFLICT DO NOTHING, never OR IGNORE (skips CHECK/NOT NULL violations silently) or OR REPLACE (a delete)'),
  ('sqlite',    'writers need SQLite >= 3.51.3 (fixes a WAL race between concurrent writers and checkpoints); migrations need >= 3.53 (ALTER TABLE ADD/DROP CONSTRAINT); CHECKs use only functions every such version has'),
  ('evolution', 'after the first real data: numbered forward-only SQL migrations, additive only, counted in PRAGMA user_version; every CHECK is named, so any rule can be widened or tightened with ALTER TABLE DROP/ADD CONSTRAINT');

CREATE TABLE entities (
  -- The shared spine: one row per linkable thing (page, task, person, place, holding). Its domain
  -- row has the SAME id: the app inserts this row first with INSERT ... RETURNING id and binds that id in
  -- the same transaction. UNIQUE(id, entity_type) plus the composite FK (id, entity_type) of every domain
  -- table make a row's type and its table agree.
  -- A person, place or holding is also a page (D20): one id, with a pages row whose title is the handle
  -- [[wikilinks]] write, and a people/places/holdings row whose FK points at that pages row. A ghost page
  -- is promoted by UPDATE entities SET entity_type = 'person' (the FK cascades it to pages.entity_type).
  -- Nothing is ever deleted: deleted_at is the tombstone (D11), enforced by BEFORE DELETE triggers.
  -- import_key: the key a writer that may send the row twice gives it (an importer, an offline phone, a
  -- retrying agent); whether the row was imported is source, not import_key. Unique per source, written at
  -- insert and never changed. Insert with ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL
  -- DO NOTHING RETURNING id: no id back = imported before, so no domain row is inserted (section 6.21).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL CONSTRAINT entities_entity_type
                  CHECK (entity_type IN ('page','task','person','place','holding')),
  created_at  TEXT NOT NULL CONSTRAINT entities_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),   -- the write time (lifelog_meta.instants)
  updated_at  TEXT NOT NULL CONSTRAINT entities_updated_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at),   -- kept by the *_touch triggers
  deleted_at  TEXT     CONSTRAINT entities_deleted_at CHECK (deleted_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at),   -- the tombstone
  tz          TEXT     CONSTRAINT entities_tz CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),   -- IANA zone of the device that captured the row ('Europe/Berlin'); NULL = unknown
  source      TEXT NOT NULL CONSTRAINT entities_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key  TEXT,                        -- the sender's key, unique per source; NULL = sent once. Imported or not: source
  UNIQUE (id, entity_type)
) STRICT;
CREATE UNIQUE INDEX entities_import ON entities(source, import_key) WHERE import_key IS NOT NULL;

CREATE TABLE pages (
  -- All prose (D5): every page is titled, unique and linkable: an essay, a reference page, a tag, the page
  -- of a person, place or holding (its entity_type says which, D20), and the journal. The journal is one
  -- DAY PAGE per local day, titled YYYY-MM-DD ('2026-09-29'): its title equals its day (pages_day_page), so
  -- [[2026-09-29]] reaches it. Capture appends to today's page, created on the first write.
  -- A title is permanent and a valid file name on every OS: a page is never renamed (create the new page,
  -- make the old one a '#REDIRECT [[New]]' stub, add links(kind='redirect')).
  -- Uniqueness is on title_key = NFC(casefold(NFC(title))), computed by the app because SQLite cannot fold
  -- Unicode: 'Café' = 'CAFÉ' = NFD 'Café'. Look a page up with WHERE title_key = :key. The key is derived.
  -- links(kind='wikilink') from a page always equal the [[titles]] and #tags its CommonMark text names,
  -- rebuilt on every save; an invalid target makes no link and never blocks the save (D19).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'page' CONSTRAINT pages_entity_type CHECK (entity_type IN ('page','person','place','holding')),   -- 'page', or the named entity this page is
  title       TEXT NOT NULL,              -- filename-safe, immutable; a day page's is its day
  title_key   TEXT NOT NULL,              -- NFC(casefold(NFC(title))), app-computed, unique
  day         TEXT,                       -- local day it was written: a day page's day; a page written on purpose has one, a link target the app created has none
  body        TEXT NOT NULL DEFAULT '',   -- CommonMark; [[Wiki Links]] inline
  UNIQUE (id, entity_type),               -- the parent key of people, places and holdings
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type) ON UPDATE CASCADE,   -- a promoted page follows its entity's type
  CONSTRAINT pages_day_page CHECK (date(title) IS NOT title OR day IS title),   -- a page titled with a day is that day's page
  CONSTRAINT pages_key_folded CHECK (length(title_key) >= 1 AND title_key = trim(title_key)
                               AND title_key NOT GLOB '*[A-Z]*'),      -- a folded key has no ASCII capitals
  CONSTRAINT pages_key_ascii CHECK (title GLOB '*[^ -~]*' OR title_key = lower(title)),   -- pure-ASCII titles: the DB verifies the key
  CONSTRAINT pages_title_len CHECK (title = trim(title) AND length(title) >= 1
                           AND length(CAST(title AS BLOB)) <= 240),  -- bytes: a filename limit is 255 bytes
  CONSTRAINT pages_title_safe CHECK (   -- a title must be a valid file name on Linux, macOS and Windows: keep it safe
         title NOT GLOB '*[/\:*?"<>|]*'            -- path separators and Windows-reserved characters
         AND title NOT GLOB ('*[' || char(1) || '-' || char(31) || char(127) || '-' || char(159) || char(173) || char(1564)
                             || char(8203) || char(8206) || '-' || char(8207) || char(8234) || '-' || char(8238)
                             || char(8288) || '-' || char(8292) || char(8294) || '-' || char(8297) || char(65279) || ']*')
                                                   -- control characters (C0, DEL, C1) and invisible or bidi ones: soft hyphen, Arabic
                                                   -- letter mark, zero-width space, LRM/RLM, embeddings and overrides, word joiner and
                                                   -- invisible operators, isolates, BOM. ZWNJ/ZWJ (U+200C/D) stay: scripts and emoji need them
         AND instr(title, char(0)) = 0
         AND substr(title, 1, 1) <> '.' AND substr(title, -1) <> '.'   -- no hidden files, '..', trailing dot
         AND upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)
             NOT IN ('CON','PRN','AUX','NUL',                -- Windows device names, bare or before an extension (CON.backup)
               'COM1','COM2','COM3','COM4','COM5','COM6','COM7','COM8','COM9','COM¹','COM²','COM³',
               'LPT1','LPT2','LPT3','LPT4','LPT5','LPT6','LPT7','LPT8','LPT9','LPT¹','LPT²','LPT³')),
  CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)
) STRICT;
CREATE UNIQUE INDEX pages_title ON pages(title_key);
CREATE INDEX pages_day ON pages(day);
CREATE TRIGGER pages_title_fixed BEFORE UPDATE OF title ON pages
  WHEN NEW.title IS NOT OLD.title
BEGIN
  -- a rename would repoint every [[Old Title]] in decades of prose (D5); title_key is derived and may be recomputed
  SELECT RAISE(ABORT, 'titles are immutable: create the new page and make this one a #REDIRECT stub');
END;

CREATE VIRTUAL TABLE pages_fts USING fts5(
  -- derived: external-content FTS5 kept in sync by the three triggers below; rebuild with
  -- INSERT INTO pages_fts(pages_fts) VALUES('rebuild'). Tokenizer unicode61 folds accents
  -- (Zurich finds Zürich); a CJK run is ONE token (section 7).
  title, body, content='pages', content_rowid='id'
);
CREATE TRIGGER pages_fts_insert AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;
CREATE TRIGGER pages_fts_delete AFTER DELETE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
END;
CREATE TRIGGER pages_fts_update AFTER UPDATE OF title, body ON pages BEGIN
  -- only the indexed columns: a promotion does not re-index the body
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;

CREATE TABLE people (
  -- people in the owner's life; relationships between them are links (friend, family, parent-of).
  -- A person is also a page with the same id (D20): its title is the permanent handle [[wikilinks]] write
  -- ('Sam (barber)' tells two Sams apart), its body holds the prose; name is the editable full name.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'person' CONSTRAINT people_entity_type CHECK (entity_type = 'person'),
  name        TEXT NOT NULL,
  nickname    TEXT,
  birth_day   TEXT CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR date(birth_day) IS birth_day),
  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR date(death_day) IS death_day),
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),
  CONSTRAINT people_death_day_order CHECK (death_day IS NULL OR birth_day IS NULL OR death_day >= birth_day)
) STRICT;

CREATE TABLE places (
  -- named locations (D16): a day page names where the day was with [[Place]], located-in links nest them (Tokyo -> Japan).
  -- A place is also a page with the same id (D20): the page title is its name and its handle.
  -- lat/lon: its representative point, WGS84 decimal degrees, or neither (D21); a misplaced point is fixed
  -- by UPDATE. A GPS fix in positions is matched to a place at query time (section 6.20), never stored.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'place' CONSTRAINT places_entity_type CHECK (entity_type = 'place'),
  lat         REAL CONSTRAINT places_lat CHECK (lat IS NULL OR lat BETWEEN -90 AND 90),
  lon         REAL CONSTRAINT places_lon CHECK (lon IS NULL OR lon BETWEEN -180 AND 180),
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),
  CONSTRAINT places_coords_pair CHECK ((lat IS NULL) = (lon IS NULL))   -- a point, or no point
) STRICT;

CREATE TABLE tasks (
  -- fleeting: open until completed_at is set, together with the LOCAL completed_day it was done (D10).
  -- Abandoning a task is erasure (a tombstone), not a recorded decision. Tasks do not repeat (D15).
  id            INTEGER PRIMARY KEY,
  entity_type   TEXT NOT NULL DEFAULT 'task' CONSTRAINT tasks_entity_type CHECK (entity_type = 'task'),
  name          TEXT NOT NULL,   -- an editable label, not a handle
  due_day       TEXT CONSTRAINT tasks_due_day CHECK (due_day IS NULL OR date(due_day) IS due_day),
  completed_at  TEXT CONSTRAINT tasks_completed_at CHECK (completed_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', completed_at) IS completed_at),
  completed_day TEXT CONSTRAINT tasks_completed_day CHECK (completed_day IS NULL OR date(completed_day) IS completed_day),
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),
  CONSTRAINT tasks_completed_pair CHECK ((completed_at IS NULL) = (completed_day IS NULL))   -- done = both set, open = neither
) STRICT;
CREATE INDEX tasks_open ON tasks(due_day) WHERE completed_at IS NULL;

CREATE TABLE metrics (
  -- a tiny registry that keeps time series canonical: 'weight' is one series forever, never
  -- 'Weight' or 'weight kg' (names are snake_case). Seeded with 'mood' (D6). The unit gives every
  -- stored value its meaning, so it never changes (metrics_unit_fixed).
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL UNIQUE,                 -- snake_case canonical: 'weight', 'mood'
  unit  TEXT NOT NULL DEFAULT '',             -- 'kg', 'bpm', 'h'; '' for 1-5 scales
  note  TEXT,
  CONSTRAINT metrics_name CHECK (length(name) >= 1 AND name NOT GLOB '*[^a-z0-9_]*')   -- lowercase snake_case, so no case variants
) STRICT;
CREATE TRIGGER metrics_unit_fixed BEFORE UPDATE OF unit ON metrics
  WHEN NEW.unit IS NOT OLD.unit
BEGIN
  -- changing the unit would silently reinterpret the whole series
  SELECT RAISE(ABORT, 'metrics.unit is fixed: it defines what every stored value means; register a new metric instead');
END;
INSERT INTO metrics(name, unit, note) VALUES
  ('mood', '', '1-5; attached to its day page via measurements.captured_with_id when posted');

CREATE TABLE measurements (
  -- one row per data point (the FxLifeSheet shape). The table is append-only, enforced by triggers: never
  -- UPDATE or DELETE. A correction is a new row whose supersedes_id names the row it corrects, at most one
  -- per row (chain: correct the correction); a correction with a NULL value RETRACTS the row it corrects.
  -- Read through the view measurement_values. day is when the value was true, created_at when it was written
  -- down, taken_at + tz when and where it was measured (tz: IANA zone; NULL = unknown).
  -- Imports: INSERT ... ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
  -- never OR IGNORE (it silently skips CHECK / NOT NULL violations).
  id               INTEGER PRIMARY KEY,
  metric_id        INTEGER NOT NULL REFERENCES metrics(id),
  day              TEXT NOT NULL CONSTRAINT measurements_day CHECK (date(day) IS day),  -- local date the value refers to
  taken_at         TEXT CONSTRAINT measurements_taken_at CHECK (taken_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  tz               TEXT CONSTRAINT measurements_tz CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),
  value            REAL,                        -- numeric only, by design (D7); NULL only on a correction: it RETRACTS the row it supersedes
  created_at       TEXT NOT NULL CONSTRAINT measurements_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source           TEXT NOT NULL CONSTRAINT measurements_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key       TEXT,                        -- importer's dedup key, unique per (source, metric)
  captured_with_id INTEGER REFERENCES entities(id),      -- provenance: the page (a day page) this reading was captured with
  supersedes_id    INTEGER REFERENCES measurements(id),  -- optional: corrects an earlier row
  CONSTRAINT measurements_not_self CHECK (supersedes_id IS NULL OR supersedes_id <> id),
  CONSTRAINT measurements_first_has_value CHECK (value IS NOT NULL OR supersedes_id IS NOT NULL),   -- a first reading has a value; only a correction may retract
  CONSTRAINT measurements_value_finite CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)   -- finite: rejects ±Infinity (a NaN arrives as NULL)
) STRICT;
CREATE INDEX measurements_series ON measurements(metric_id, day);
CREATE UNIQUE INDEX measurements_import
  ON measurements(source, import_key, metric_id) WHERE import_key IS NOT NULL;
CREATE UNIQUE INDEX measurements_one_correction
  ON measurements(supersedes_id) WHERE supersedes_id IS NOT NULL;  -- one correction per row; also serves measurement_values
CREATE INDEX measurements_day ON measurements(day);                -- day view
CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are append-only: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_no_delete BEFORE DELETE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are never deleted: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_supersede_metric AFTER INSERT ON measurements
  WHEN NEW.supersedes_id IS NOT NULL
BEGIN
  -- a correction must correct an EXISTING row of the SAME metric (IS NOT, not <>: a dangling
  -- supersedes_id yields NULL, and NULL <> x is NULL = pass). supersedes_id is set only at INSERT
  -- and must name an existing row, so correction chains can never form a cycle.
  SELECT RAISE(ABORT, 'supersedes_id must reference a measurement of the same metric')
   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;
END;
CREATE VIEW measurement_values AS
  -- the canonical read rule: rows nothing has corrected, minus retractions
  SELECT me.*
    FROM measurements me
   WHERE me.value IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM measurements x WHERE x.supersedes_id = me.id);

CREATE TABLE positions (
  -- the location history (D21): one GPS fix per row, where the owner was at taken_at, as WGS84 decimal
  -- degrees. Append-only, enforced by triggers: never UPDATE or DELETE, and there is no correction row; a
  -- doubtful fix is left out when read, by accuracy_m (metres, the fix's horizontal accuracy radius).
  -- day is the LOCAL date of taken_at, tz the IANA zone there (NULL = unknown), both written at insert.
  -- Imports: INSERT ... ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING; an export
  -- without ids of its own uses taken_at as import_key. Never OR IGNORE (it skips CHECK violations silently).
  id          INTEGER PRIMARY KEY,
  taken_at    TEXT NOT NULL CONSTRAINT positions_taken_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  day         TEXT NOT NULL CONSTRAINT positions_day CHECK (date(day) IS day),
  tz          TEXT CONSTRAINT positions_tz CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),
  lat         REAL NOT NULL CONSTRAINT positions_lat CHECK (lat BETWEEN -90 AND 90),     -- NOT NULL also refuses a NaN
  lon         REAL NOT NULL CONSTRAINT positions_lon CHECK (lon BETWEEN -180 AND 180),
  accuracy_m  REAL CONSTRAINT positions_accuracy_m CHECK (accuracy_m IS NULL OR accuracy_m BETWEEN 0 AND 1e7),
  created_at  TEXT NOT NULL CONSTRAINT positions_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source      TEXT NOT NULL CONSTRAINT positions_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key  TEXT,                        -- importer's dedup key, unique per source
  CONSTRAINT positions_not_null_island CHECK (NOT (lat = 0 AND lon = 0))   -- 0, 0 is how photo metadata says "no location"
) STRICT;
CREATE INDEX positions_time ON positions(taken_at);                -- where was I at an instant
CREATE INDEX positions_day ON positions(day, taken_at);            -- a day's track, in order
CREATE UNIQUE INDEX positions_import
  ON positions(source, import_key) WHERE import_key IS NOT NULL;
CREATE TRIGGER positions_no_update BEFORE UPDATE ON positions
BEGIN
  SELECT RAISE(ABORT, 'positions are append-only: a fix is never edited');
END;
CREATE TRIGGER positions_no_delete BEFORE DELETE ON positions
BEGIN
  SELECT RAISE(ABORT, 'positions are never deleted: a doubtful fix is left out when read, by accuracy_m');
END;

CREATE TABLE currencies (
  -- money (D18) is an exact tier of its own, never rows in measurements. This is the closed registry
  -- of currencies you hold or value things in; subunits = minor units per whole unit (EUR 100, JPY 1),
  -- so a stranger can turn an INTEGER amount into a number without the app. subunits never changes.
  -- Never store credentials or full account/card numbers anywhere in life.db.
  code     TEXT PRIMARY KEY CONSTRAINT currencies_code CHECK (length(code) BETWEEN 3 AND 10 AND code NOT GLOB '*[^A-Z0-9]*'),  -- ISO 4217 code: 'EUR', 'JPY' (a coin you hold by quantity may be registered too)
  name     TEXT NOT NULL,
  subunits INTEGER NOT NULL CONSTRAINT currencies_subunits CHECK (subunits BETWEEN 1 AND 1000000000),  -- minor units per 1 whole unit
  note     TEXT
) STRICT;
INSERT INTO currencies(code, name, subunits) VALUES
  ('USD', 'US dollar',      100),
  ('EUR', 'Euro',           100),
  ('GBP', 'Pound sterling', 100),
  ('CHF', 'Swiss franc',    100),
  ('CAD', 'Canadian dollar',100),
  ('AUD', 'Australian dollar', 100),
  ('JPY', 'Japanese yen',   1);
CREATE TRIGGER currencies_subunits_fixed BEFORE UPDATE OF subunits ON currencies
  WHEN NEW.subunits IS NOT OLD.subunits
BEGIN
  -- changing subunits would silently rescale every balance ever recorded in that currency
  SELECT RAISE(ABORT, 'currencies.subunits is fixed: it defines what every stored amount means; register a new currency code instead');
END;

CREATE TABLE holdings (
  -- anything with a balance or a value: bank, deposit, brokerage, crypto wallet, pension, cash,
  -- property, vehicle, valuables, loan, mortgage, card, valued in the holding's currency on a day.
  -- Record your OWN share of joint items. side and currency define what every balance means and never
  -- change (holdings_meaning_fixed). A holding is also a page with the same id (D20): the page title is its
  -- name and handle. Net worth is derived, never stored, and summed per currency: amounts of different
  -- currencies are never added (section 6.15).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'holding' CONSTRAINT holdings_entity_type CHECK (entity_type = 'holding'),
  side        TEXT NOT NULL CONSTRAINT holdings_side CHECK (side IN ('asset','liability')),
  currency    TEXT NOT NULL REFERENCES currencies(code),
  category    TEXT COLLATE NOCASE,                   -- free taxonomy: 'cash','deposit','brokerage','crypto','pension','property','valuables','loan'
  institution TEXT,
  opened_day  TEXT CONSTRAINT holdings_opened_day CHECK (opened_day IS NULL OR date(opened_day) IS opened_day),
  closed_day  TEXT CONSTRAINT holdings_closed_day CHECK (closed_day IS NULL OR date(closed_day) IS closed_day),   -- last day the holding counts (inclusive)
  FOREIGN KEY (id, entity_type) REFERENCES pages(id, entity_type),
  CONSTRAINT holdings_closed_day_order CHECK (closed_day IS NULL OR opened_day IS NULL OR closed_day >= opened_day)
) STRICT;
CREATE TRIGGER holdings_meaning_fixed BEFORE UPDATE OF side, currency ON holdings
  WHEN NEW.side IS NOT OLD.side OR NEW.currency IS NOT OLD.currency
BEGIN
  -- changing either would rewrite history; the WHEN clause keeps full-row ORM updates working
  SELECT RAISE(ABORT, 'a holding''s side and currency are fixed: close it and open a new holding instead');
END;

CREATE TABLE balances (
  -- append-only snapshots of a holding's VALUE on a local day, in INTEGER minor units of the holding's
  -- currency (never REAL; whole units = amount / currencies.subunits). The newest row per
  -- (holding_id, day) wins, newest = recorded last = highest id; a row with a NULL amount RETRACTS
  -- that day. A wrong entry is corrected by another row, never UPDATE/DELETE (triggers; they need
  -- PRAGMA recursive_triggers=ON to stop a REPLACE). Read through balance_values. Net worth is
  -- derived, never stored, per currency (section 6.15).
  id          INTEGER PRIMARY KEY,
  holding_id  INTEGER NOT NULL REFERENCES holdings(id),
  day         TEXT NOT NULL CONSTRAINT balances_day CHECK (date(day) IS day),   -- LOCAL as-of date: the end-of-day balance / valuation
  amount      INTEGER,                                  -- minor units of holdings.currency, as the institution states it
                                                        -- (a mortgage of 200 000 is +20000000: side says it is owed);
                                                        -- NULL = retraction of this (holding, day)
  created_at  TEXT NOT NULL CONSTRAINT balances_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source      TEXT NOT NULL CONSTRAINT balances_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  import_key  TEXT,                                     -- importer's dedup key, unique per source
  note        TEXT                                      -- 'statement', 'estimate', 'after selling the ETF'
) STRICT;
CREATE INDEX balances_series ON balances(holding_id, day);   -- newest-per-day and as-of lookups (rowid is the last key part)
CREATE UNIQUE INDEX balances_import ON balances(source, import_key) WHERE import_key IS NOT NULL;
CREATE TRIGGER balances_no_update BEFORE UPDATE ON balances
BEGIN
  SELECT RAISE(ABORT, 'balances are append-only: correct by inserting a newer row for the same (holding, day)');
END;
CREATE TRIGGER balances_no_delete BEFORE DELETE ON balances
BEGIN
  SELECT RAISE(ABORT, 'balances are never deleted: retract by inserting a row with NULL amount');
END;
CREATE VIEW balance_values AS
  -- the canonical read rule: the newest row (highest id) per (holding, day), unless it is a retraction
  SELECT b.*
    FROM balances b
   WHERE b.amount IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM balances x
                      WHERE x.holding_id = b.holding_id AND x.day = b.day AND x.id > b.id);

CREATE TABLE link_kinds (
  -- the CLOSED registry of link kinds: a link's kind must be registered first (FK), and a kind's
  -- structure (symmetric flag, allowed endpoint entity types) is fixed at registration and enforced
  -- by a trigger on every link. Registering a kind is a deliberate INSERT, so a typo cannot create
  -- one. from_types / to_types: NULL = any entity type, else a comma list of entities.entity_type values
  -- ('task,page'); a misspelt token fails CLOSED (every link of that kind is rejected).
  kind       TEXT PRIMARY KEY CONSTRAINT link_kinds_kind CHECK (kind = lower(kind) AND length(kind) > 0 AND kind NOT GLOB '*[^a-z0-9_-]*'),
  symmetric  INTEGER NOT NULL DEFAULT 0 CONSTRAINT link_kinds_symmetric CHECK (symmetric IN (0,1)),
  from_types TEXT CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (from_types NOT GLOB '*[^a-z,]*' AND from_types NOT GLOB ',*'
                         AND from_types NOT GLOB '*,' AND from_types NOT GLOB '*,,*')),
  to_types   TEXT CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (to_types   NOT GLOB '*[^a-z,]*' AND to_types   NOT GLOB ',*'
                         AND to_types   NOT GLOB '*,' AND to_types   NOT GLOB '*,,*')),
  note       TEXT,
  CONSTRAINT link_kinds_mirror_valid CHECK (symmetric = 0 OR from_types IS to_types)   -- a mirrored edge must be valid in both directions
) STRICT;
INSERT INTO link_kinds(kind, symmetric, from_types, to_types, note) VALUES
  ('wikilink', 0, 'page,person,place,holding', 'page,person,place,holding', 'extracted from [[body]] on save; body is the truth'),
  ('redirect', 0, 'page',      'page',         'old stub page → its replacement; renames, D5'),
  ('spawned',  0, 'task,page', 'page',         'task/page created from a page (a line of a day page)'),
  ('subtask',  0, 'task',      'task',         'child task → parent task'),
  ('about',    0, NULL,        'person,place,holding', 'entity → person/place/holding it is about'),
  ('visited',  0, 'person',    'place',        'person → place; the day the owner was there is the day page that names it (D16)'),
  ('located-in', 0, 'place',   'place',        'containment: Tokyo → Japan; transitive — walk it with a recursive CTE (section 6.18)'),
  ('parent-of', 0, 'person',   'person',       'parent → child; ''family'' stays the symmetric catch-all'),
  ('friend',   1, 'person',    'person',       NULL),
  ('family',   1, 'person',    'person',       NULL),
  ('related',  1, NULL,        NULL,           'anything ↔ anything');
CREATE TRIGGER link_kinds_structure_fixed BEFORE UPDATE OF symmetric, from_types, to_types ON link_kinds
  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types
BEGIN
  -- changing a kind's structure would leave edges that violate it, or half-edges; register a new kind
  SELECT RAISE(ABORT, 'a link kind''s structure (symmetric, endpoint types) is fixed at registration; register a new kind instead');
END;

CREATE TABLE links (
  -- one graph for everything: wiki backlinks, relationships, the page a task came from, subtasks,
  -- redirects. Rows are hard-deleted (the one such table, D11) and immutable otherwise (delete and
  -- re-insert). Symmetric kinds are mirrored by trigger on insert AND delete, so a half-edge cannot
  -- exist and backlinks need only to_id. Cycles (e.g. subtask) are not prevented (D8).
  -- source names the writer, as on entities; written at insert, never changed.
  id         INTEGER PRIMARY KEY,
  from_id    INTEGER NOT NULL REFERENCES entities(id),
  to_id      INTEGER NOT NULL REFERENCES entities(id),
  kind       TEXT NOT NULL REFERENCES link_kinds(kind),  -- closed registry
  note       TEXT,
  created_at TEXT NOT NULL CONSTRAINT links_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source     TEXT NOT NULL CONSTRAINT links_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (lifelog_meta.source)
  UNIQUE (from_id, to_id, kind)
) STRICT;
CREATE INDEX links_to ON links(to_id);   -- backlinks query (from_id is served by the UNIQUE index)

CREATE TRIGGER links_fixed BEFORE UPDATE OF from_id, to_id, kind, source ON links
  WHEN NEW.from_id IS NOT OLD.from_id OR NEW.to_id IS NOT OLD.to_id OR NEW.kind IS NOT OLD.kind
    OR NEW.source IS NOT OLD.source
BEGIN
  -- only note may change; to change anything else, delete and re-insert
  SELECT RAISE(ABORT, 'links are immutable: delete and re-insert');
END;
CREATE TRIGGER links_endpoint_types BEFORE INSERT ON links
BEGIN
  -- the registry is closed even on a connection that forgot PRAGMA foreign_keys=ON; an unknown
  -- endpoint id counts as type '?' and so is rejected for typed kinds
  SELECT RAISE(ABORT, 'link kind is not registered in link_kinds')
   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);
  SELECT RAISE(ABORT, 'link endpoint type not allowed for this kind (see link_kinds.from_types / to_types)')
   WHERE EXISTS (SELECT 1 FROM link_kinds k
                  WHERE k.kind = NEW.kind
                    AND ((k.from_types IS NOT NULL
                          AND instr(',' || k.from_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.from_id), '?') || ',') = 0)
                      OR (k.to_types IS NOT NULL
                          AND instr(',' || k.to_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?') || ',') = 0)));
END;
CREATE TRIGGER links_mirror_insert AFTER INSERT ON links
  WHEN NEW.from_id <> NEW.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = NEW.kind) = 1
BEGIN
  -- mirror a symmetric edge; OR IGNORE makes the re-fire find the row present and stop
  INSERT OR IGNORE INTO links(from_id, to_id, kind, note, created_at, source)
  VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at, NEW.source);
END;
CREATE TRIGGER links_mirror_delete AFTER DELETE ON links
  WHEN OLD.from_id <> OLD.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = OLD.kind) = 1
BEGIN
  DELETE FROM links WHERE from_id = OLD.to_id AND to_id = OLD.from_id AND kind = OLD.kind;
END;

CREATE VIEW ghost_pages AS
  -- empty plain pages nobody points at, 30 days old: a link target created by a capture-time typo and
  -- never written (renames never create ghosts). The page of a person, place or holding is never a
  -- ghost, however empty (D20). The UI lists them; tombstoning is the owner's act.
  SELECT p.id, p.title, e.created_at
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL
     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.from_id = p.id);

CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN
  -- updated_at is kept in the DB, so every writer (CLI, agents, scripts) gets it right
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER tasks_touch AFTER UPDATE ON tasks BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER people_touch AFTER UPDATE ON people BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER places_touch AFTER UPDATE ON places BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER holdings_touch AFTER UPDATE ON holdings BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities
  WHEN NEW.deleted_at IS NOT OLD.deleted_at
BEGIN
  -- tombstoning and un-tombstoning are changes too; watching deleted_at only, it cannot re-fire itself
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER entities_provenance_fixed BEFORE UPDATE OF source, import_key ON entities
  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key
BEGIN
  -- provenance is captured at insert, and a changed key would let a re-run import the row again;
  -- the WHEN clause lets full-row updates through
  SELECT RAISE(ABORT, 'entities.source and import_key are written at insert and never changed');
END;

CREATE TRIGGER entities_no_delete BEFORE DELETE ON entities
BEGIN
  -- no hard deletes (D11): an entity and its domain rows are tombstoned, never removed; under
  -- PRAGMA recursive_triggers=ON these triggers also stop REPLACE from deleting a row. Only links rows are deleted.
  SELECT RAISE(ABORT, 'entities are never deleted: set entities.deleted_at (tombstone)');
END;
CREATE TRIGGER pages_no_delete BEFORE DELETE ON pages
BEGIN SELECT RAISE(ABORT, 'pages are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER tasks_no_delete BEFORE DELETE ON tasks
BEGIN SELECT RAISE(ABORT, 'tasks are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER people_no_delete BEFORE DELETE ON people
BEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER places_no_delete BEFORE DELETE ON places
BEGIN SELECT RAISE(ABORT, 'places are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER holdings_no_delete BEFORE DELETE ON holdings
BEGIN SELECT RAISE(ABORT, 'holdings are never deleted: tombstone the entity (entities.deleted_at)'); END;
```

**14 tables + 1 FTS5 virtual table + 3 views** (`measurement_values`, `balance_values`, `ghost_pages`)
**+ 32 triggers.** That is the entire system. Every `CHECK` is named (`CONSTRAINT <table>_<rule>`), so
any rule can be dropped or re-added by name after the freeze (D13).

---

## 4. Entity model overview

The diagrams draw tables, keys and relationships only; the other columns are in §3. They are part of
the contract: `tests/` checks every table, key column and foreign key they draw against §3, so a
diagram cannot drift from the DDL without a test failing. (`pages_fts`, the FTS5 index over `pages`,
is derived and rebuildable and is not drawn.)

### 4.1 Entities and the graph

One supertype row per linkable thing (`entities`), one domain row per entity with the *same* id — the
composite foreign key `(id, entity_type)` makes the type and the table agree — and one polymorphic graph
(`links`) over the supertype, whose `kind` is a foreign key to the closed registry `link_kinds`. A
person, place or holding is also a page (D20): its `people` / `places` / `holdings` row hangs off its
`pages` row, which hangs off its `entities` row — one id, three rows.

```mermaid
%% diagram: er-core
erDiagram
    entities ||--o| pages    : "id"
    entities ||--o| tasks    : "id"
    pages    ||--o| people   : "id"
    pages    ||--o| places   : "id"
    pages    ||--o| holdings : "id"
    entities ||--o{ links    : "from_id"
    entities ||--o{ links    : "to_id"
    link_kinds ||--o{ links  : "kind"

    entities {
        INTEGER id PK
    }
    pages {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    tasks {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    people {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    places {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    holdings {
        INTEGER id PK, FK
        TEXT entity_type FK
        TEXT currency FK
    }
    links {
        INTEGER id PK
        INTEGER from_id FK
        INTEGER to_id FK
        TEXT kind FK
    }
    link_kinds {
        TEXT kind PK
    }
```

### 4.2 Facts, money and registries

Facts are not entities. `measurements`, `balances` and `positions` are append-only (D7, D18, D21). A
correction is a new row, and `measurements.supersedes_id` points back at the row it corrects;
`measurements.captured_with_id` records provenance (a mood reading points at its day page). A position has no
foreign key: it is matched to a place by distance at query time (§6.20). `currencies` gives an amount
its meaning. `lifelog_meta` stands alone: the rules that span tables (D17).

```mermaid
%% diagram: er-facts
erDiagram
    metrics     ||--o{ measurements : "metric_id"
    entities    |o--o{ measurements : "captured_with_id"
    measurements |o--o| measurements : "supersedes_id"
    holdings    ||--o{ balances     : "holding_id"
    currencies  ||--o{ holdings     : "currency"

    metrics {
        INTEGER id PK
    }
    measurements {
        INTEGER id PK
        INTEGER metric_id FK
        INTEGER captured_with_id FK
        INTEGER supersedes_id FK
    }
    entities {
        INTEGER id PK
    }
    holdings {
        INTEGER id PK, FK
        TEXT currency FK
    }
    balances {
        INTEGER id PK
        INTEGER holding_id FK
    }
    currencies {
        TEXT code PK
    }
    positions {
        INTEGER id PK
    }
    lifelog_meta {
        TEXT key PK
    }
```

### 4.3 Who may link what

An arrow is a `link_kinds` row; a double-headed arrow is a symmetric kind, mirrored by trigger so that one
direction suffices for backlinks. A node that names several types stands for each of them; `any entity`
is an endpoint with no restriction (`from_types` or `to_types` NULL).

```mermaid
%% diagram: link-map
flowchart LR
    any(["any entity"])
    person["person"]
    place["place"]
    task["task"]
    page["page"]
    named["person, place, holding"]
    titled["page, person, place, holding"]

    any -->|"about"| named
    any <-->|"related"| any
    person -->|"visited"| place
    person -->|"parent-of"| person
    person <-->|"friend, family"| person
    place -->|"located-in"| place
    task -->|"subtask"| task
    task -->|"spawned"| page
    page -->|"redirect, spawned"| page
    titled -->|"wikilink"| titled
```

### 4.4 The life of a page

A page starts as a ghost when a link names a title that does not exist yet, or as a written page when
the owner creates it on purpose — a day page when the first thing is captured that day (D5). Either can
become the page of a person, place or holding (D20).

```mermaid
%% diagram: page-life
stateDiagram-v2
    direction LR
    state "Ghost (empty)" as Ghost
    state "Written page" as Written
    state "Redirect stub" as Stub
    state "Named (person, place, holding)" as Named
    [*] --> Ghost: a link names a title that does not exist yet
    [*] --> Written: created on purpose, with a day, or the day page on the day's first capture
    [*] --> Named: a person, place or holding is created
    Ghost --> Written: body saved, the day unchanged
    Ghost --> Named: promoted, entities.entity_type changes
    Written --> Named: promoted, entities.entity_type changes
    Written --> Written: body edited or appended to, the title never changes
    Written --> Stub: renamed, so the old page becomes a stub and a redirect link is added
```

Any row can be tombstoned (`entities.deleted_at`, D11); a save whose link resolves a tombstoned title
revives that page instead of duplicating it (§6.13). A ghost that nothing links to is listed by
`ghost_pages` after 30 days (§6.12) — the view only lists, tombstoning stays the owner's act.

### 4.5 Product concepts

| Product concept | Schema mechanism |
|---|---|
| Journaling | the day page, titled `YYYY-MM-DD`: capture appends to it (§6.1); the day view adds what else that day holds (§6.2) |
| "When did I see Ana / go to Lakeside?" | the day pages that link `[[Ana]]` or `[[Lakeside]]` (§6.3) |
| A to-do written in a day page | a task + `links(kind='spawned')` to the page it came from (§6.9) |
| Mood tracking | the `mood` metric in `measurements`, each row optionally pointing at its day page (§6.4) |
| Notes, wiki and tags | `pages` + `links(kind='wikilink')` kept equal to what the body names (§2.4, §6.13); `#health` is the page `health` |
| Backlinks | `links WHERE to_id = ?` (§6.5) |
| People, places, holdings in prose | `[[Bob Sample]]` links to the person itself, because the person is a page (D20, §6.19) |
| Life graph ("everything about my son") | `links` in both directions from his id (§6.6) |
| Subtasks | `links(kind='subtask', child → parent)`; recursive CTE (§6.11) |
| Reminders and habits | a reminder is a task with a `due_day`; "did I do it each month" is a 0/1 habit metric (D15) |
| Birthdays | a query over `people.birth_day` |
| Biomarkers / quantified self | `metrics` + `measurements` (§6.7) |
| Net worth over time | derived from `balance_values`, per currency (§6.15–6.16) |
| Imported tasks and pages (a to-do app, a vault) | `entities.import_key`, unique per `source`: a re-run inserts nothing, a changed task updates its row (§6.21) |
| Location history ("where was I?") | `positions`, one GPS fix per row; the place it was is the nearest `places` point (§6.20, D21) |
| Search | `pages_fts` (§6.8) |
| "Which of my agents wrote this?" | `source` on every entity, link, measurement, balance and position (§2.2) |

---

## 5. Decision log

Each decision: **context → decision → alternatives rejected → rationale → sources.** A decision cites
the constraints that carry it; the rule itself is in §3 or §2.

### D1 — Container: a single SQLite file.

- **Decision.** SQLite, one file (`life.db`). Binary files are not stored in it at all (D9).
- **Alternatives.** Postgres/server DB (rejected: operational burden for single user,
  no longevity benefit); plain files only (see D4); NoSQL embedded stores (rejected:
  weaker durability guarantees, no standard query language for future readers).
- **Rationale.** The US Library of Congress lists SQLite as a *Recommended Storage
  Format* for datasets — one of only four, alongside XML, JSON, CSV [R10][R11]. The file
  format has been backwards-compatible since 2004 and the developers commit to reading
  today's files "for decades into the future," planning through 2050 [R12][R13]. SQLite's
  application-file-format essay: "Data lives longer than code … an SQLite database remains
  readable long after all traces of the original application have been lost" [R1].
- **Consequence.** The remaining risk is meaning living in app code — which D2, D4 and D17 address.

### D2 — Typed tables with real columns, `STRICT` mode; no JSON property bags, no EAV.

- **Decision.** Per-domain typed tables; every column real, named, and typed; every table
  declared `STRICT` (SQLite ≥ 3.37): every value must be *losslessly* convertible to the
  declared column type or the statement fails — `'xyz'` into INTEGER is rejected, `12.0` into
  INTEGER is stored as `12` (executed). No JSON columns anywhere. No free-form key/value tables.
- **Alternatives.**
  - *Single `objects` table + JSON properties*: rejected. `json_extract()` re-parses text
    on every call for every row; indexed JSON requires generated-column scaffolding
    [R15][R16]; benchmarks of tagging strategies show `json_each()` scans far slower than
    plain indexed tables [R17]. Practitioner reports converge on JSON columns becoming
    "an unmapped wasteland of inconsistent keys" within months [R18].
  - *EAV (entity-attribute-value)*: rejected — the classic documented anti-pattern once
    attributes are known: no type integrity, no referential integrity, torturous queries [R19].
  - *Non-STRICT tables*: rejected — STRICT is one keyword per table and moves validation
    from app code into the durable artifact itself (principle 2).
- **Long-tail fields** ("bike serial number") are added by additive migration, *not* by a
  JSON escape hatch: the migration forces an explicit decision that a field deserves to exist.
- **Sources.** [R1][R14]–[R19].

### D3 — IDs: `INTEGER PRIMARY KEY`; UUIDs rejected.

- **Decision.** Every entity, fact and join table is keyed by `INTEGER PRIMARY KEY` (a rowid
  alias). The registries — `lifelog_meta`, `link_kinds`, `currencies` — keep their natural key
  (`key`, `kind`, `code`): an integer surrogate would only hide the name. No `AUTOINCREMENT`
  (extra CPU/IO/bookkeeping, "usually not needed" [R4]). No UUIDs.
- **Alternatives.** UUIDv7/v4 TEXT keys: benchmarked *slower* (random TEXT keys scatter
  inserts across the B-tree) and larger; their only real advantage — collision-free IDs for
  multi-device merge — buys nothing while sync is a non-goal (§7) [R26][R27].
- **An id is a permanent reference.** Nothing but `links` rows is deleted, so an entity id is never
  reused, and a named entity also has its permanent title (D20).
- **Trade accepted.** If merging two databases ever becomes real, integer IDs can collide.
  Mitigation then: re-key with one script, or add an `entities.uid` column — additive after the
  freeze up to unique and `NOT NULL` (`ADD COLUMN`, a backfill, a unique index, `ALTER COLUMN uid SET
  NOT NULL`; executed); a backfilled uid loses nothing, since nothing outside pointed at the old rows.
- **Multiple devices: a hub and its clients.** The hub is one live `life.db` on a machine the owner
  holds; it may move to another (a copy, then the checks of §2.5), never run in two places at once.
  Everything else is a *client* of it — the UI, the owner's own apps, AI agents, a phone — writing
  through the app's API. That is multiple processes or connections, not multiple divergent
  databases: WAL + `busy_timeout` + `BEGIN IMMEDIATE` serialize concurrent writers to one file
  safely, and the "single writer" rule (principle 3) means *single writing application*, not single
  process. A phone keeps a read-only copy (§2.6); offline it queues only *new* rows and replays them
  through the API, so nothing can conflict. A replay, like a re-run importer, must insert nothing
  twice: the client gives each new row an `import_key`, the key facts and entities have (§6.21).
- **Rejected: devices that each hold a copy and merge (CRDTs).** cr-sqlite, the SQLite extension for
  it, allows no checked foreign keys, no UNIQUE constraint but the primary key and no CHECK across
  columns in a merged table [R75] — the composite FKs, the unique `title_key` and the paired CHECKs
  this schema rests on.
- **Sources.** [R4][R26][R27][R75].

### D4 — Text ownership: the database is canonical.

- **Context.** This was the hardest decision. *Files canonical*: any app, plugin, or future tool
  pointed at the folder becomes a legitimate writer of canonical data → dialect drift and
  corruption. Evidence: the Logseq↔Obsidian ecosystem needs dedicated conversion tools (journal
  filename formats, URL-encoded filenames, block-reference syntax, property formats, task
  statuses) [R29][R30][R31][R32]. *DB canonical*: the fear of meaning trapped in the app.
- **Decision.** `pages.body` in SQLite is the single source of truth for prose. No folder of
  files holds canonical text, and nothing may write canonical data except this app. The ability to
  leave rests on the file format itself (D1) and on the schema being its own documentation.
- **Why not files-canonical with discipline (linters + git as recovery net)?** Git is a
  *recovery* net, not a *guard*. The owner's own multi-app history (Obsidian, Logseq, Trilium,
  each leaving residue) is direct evidence that the discipline requirement fails in practice.
- **Why not files-canonical with a single writer?** It inherits every engineering complaint Logseq
  documented when they *split their product in two* over this exact axis: live editing rewrites
  whole files; renaming a page must rewrite every referencing file; files lack persistent IDs and
  timestamps [R34][R35][R36].
- **Costs accepted.** Prose is edited only through this app's UI/CLI/API.
- **Sources.** [R29]–[R32], [R34]–[R36].

### D5 — One `pages` table for all prose; the journal is a page per day; titles are permanent.

- **Decision.** One text entity `pages`, every row titled, unique and linkable — an essay, a reference
  page, a tag, a person, and the journal.
  - The journal is **one day page per local day**, titled with the day (`2026-09-29`); its `day` is
    its title (`pages_day_page`), so `[[2026-09-29]]` reaches it and the day page of a day is one
    lookup by key. Capture appends to today's page and creates it on the first write (§6.1, §2.4).
  - *Dated is a property, not a type:* the app sets `day` on a page the owner creates on purpose and
    leaves it NULL on a page it creates as a link target, so the day view (§6.2) shows what was
    **written** that day, not what was **mentioned** — except a day page, whose title is its day.
  - **Tags are pages**: one graph, one syntax.
  - **Titles** are permanent (`pages_title_fixed`), unique by `title_key` (§2.4) and safe as a file
    name everywhere (`pages_title_safe`).
- **Why a page per day.** The first real import, an Obsidian vault of one note per day, met untitled
  journal entries: no day could be linked, and every `[[2026-08-20]]` the vault wrote made a second,
  empty page beside that day's entries — two homes for one day. Untitled entries also needed a second
  kind of page with its own CHECKs, and an inbox column nobody used.
- **Alternatives.**
  - *Untitled memos, the day page a query over them (a Memos-style stream, which is also the inbox)*:
    rejected — the incident above; and something to act on is a task (§6.9), not a second state of a
    page.
  - *A `journal` table beside `pages`*: rejected — a day would not be linkable (`[[…]]` reaches only
    pages), and prose would have two homes.
  - *A day page with a free title and a unique `day`*: rejected — `[[2026-09-29]]` could not find it
    without a second lookup rule, and two pages could claim one day by title and by column.
  - *Separate `note` and `wiki` kinds*: rejected — they would differ only in the day rule and share
    one title namespace; a `[[link]]` to a title that does not exist yet creates a page, and anything
    linked before it was written would keep whatever kind the link guessed.
  - *Renames*: rejected — renaming silently repoints every `[[Old Title]]` in decades of prose, or
    leaves ghosts if it doesn't; a redirect stub keeps both working (§2.4).
  - *ASCII-only case-insensitive uniqueness (`COLLATE NOCASE`)*: rejected — `Café notes` and
    `CAFÉ NOTES` (and NFC vs NFD spellings) would be distinct rows. *ASCII-only titles*: rejected —
    a life log has `日本語` and `Zürich` in it.
  - *An ICU or app-registered collation*: rejected — a database whose index needs a collation only one
    program supplies can be read by anyone but not written or integrity-checked (`no such collation
    sequence`, executed).
  - *Id-named files, so that titles need no file-name rules*: rejected — the rules are the strict
    direction: loosening `pages_title_safe` after the freeze is one `DROP CONSTRAINT` + `ADD
    CONSTRAINT` (D13), while tightening it later would meet titles that already break the new rule.
    *Reopen only if* a title you actually want is forbidden (`Re: plan`) often enough to hurt.
- **Costs accepted.** An entry in a day page has no time of its own: a time worth keeping is written
  in the text. There is no inbox. A title cannot be corrected in place: a new page and a stub. An
  empty page created on purpose that nothing links to shows in `ghost_pages`.
- **Sources.** Kaydet [R41]; FxLifeSheet [R9][R42]; Windows reserved names [R58].

### D6 — Mood: the `mood` metric in `measurements`, not a column on `pages`.

- **Decision.** Mood is a time series like any other: a seeded metric `mood` whose rows are appended
  to `measurements`. When a mood is attached to a day page, the row's `captured_with_id` is the page's id. The
  1–5 range is app-level validation on one metric row, not a schema CHECK.
- **Rule.** One home per concept, forever (principle 6): a standalone mood tap needs no second
  mechanism, and mood charts uniformly with every other series.
- **Alternatives.** *`pages.mood` column*: rejected — it splits the concept across two tables the
  moment a text-less mood tap happens. *Both*: rejected — two homes for one concept drift.

### D7 — Measurements: one FxLifeSheet-shaped table + tiny metric registry; append-only.

- **Decision.** `metrics` keeps series canonical (`metrics_name`: lowercase snake_case, so 'Weight'
  cannot become a second series; `metrics_unit_fixed`). `measurements` holds one row per data point
  and is **bitemporal** [R68]: `day`/`taken_at` is *valid time*, `created_at` and the append-only
  rows are *transaction time*, so "what did I believe my weight was on 1 March, as of 1 April" stays
  answerable. The table is append-only (`measurements_no_update`, `measurements_no_delete`); a
  correction supersedes (`measurements_one_correction`, `measurements_supersede_metric` — the one
  supersede invariant that could silently corrupt a series); a NULL value retracts
  (`measurements_first_has_value`); values are finite (`measurements_value_finite`: a `REAL` column
  stores `1e999` as infinity, and one such row poisons every average). SQLite turns a bound `NaN` into
  NULL before any CHECK sees it: as a first reading that is rejected, but as a correction it is a
  retraction the database cannot tell from an intended one (executed) — so the app never binds NaN.
  `measurement_values` is the one read rule; two independent readings on one day are both returned.
  The unique index on `supersedes_id` doubles as the index the view's `NOT EXISTS` needs (executed:
  the plan uses it).
- **`captured_with_id`** is provenance (the day page the reading was captured with), not "about
  this person": the owner is the only subject of measurements.
- **This is the most battle-tested part of the design.** FxLifeSheet's actual schema is a single
  `raw_data` table carrying 380k data points over 6+ years with zero schema drama [R9][R42]. Open Brane
  runs one append-only table with keyed idempotent writes at 942k rows [R43]. We keep three of their
  devices: `import_key` idempotency, denormalized local `day`, `source` provenance.
- **Deliberate simplifications** (§7): `value REAL` only (no text-valued measurements — prose belongs
  in pages); no LOINC/UCUM/reference ranges [R8]; no raw/normalized two-tier wearable mirror [R8]; no
  multi-resolution rollups (~5 GB/lifetime of sensor data queries fine raw) [R45].
- **Sources.** [R8][R9][R42][R43][R45][R68].

### D8 — One `entities` supertype + one polymorphic `links` graph; a closed kind registry; symmetry in-DB.

- **Decision.** The five linkable types share one ID space through `entities`; all relationships live
  in one `links(from_id, to_id, kind)` table with real foreign keys (`UNIQUE(from_id, to_id, kind)`
  allows several kinds between one pair, never a duplicate edge). `links.kind` references the closed
  registry `link_kinds`, whose structure is fixed at registration (`link_kinds_structure_fixed`);
  `links_endpoint_types` checks the kind and both endpoint types on every insert, including mirror rows
  — also on a connection with `foreign_keys=OFF`, executed in autocommit. Symmetric kinds are mirrored
  by trigger on insert *and* delete, so a half-edge cannot exist whatever the writer, and both mirror
  triggers terminate under `recursive_triggers=ON` (executed). Links are immutable except `note`
  (`links_fixed`). Cycles (e.g. `subtask`) are not prevented; §6.11 caps its walk. Widening a
  kind's endpoint types is a deliberate migration: drop `link_kinds_structure_fixed`, update the row,
  recreate the trigger, in one transaction (executed).
- **Alternatives.**
  - *No supertype; discriminator pairs* (`from_kind TEXT, from_id INT`): rejected — no foreign keys,
    so edges can dangle silently forever.
  - *Per-relationship tables* (`friendships`, `attendance`, …): rejected — N tables and N code paths
    for one concept, and "everything about X" becomes a union over an open-ended set.
  - *A CHECK-list on `links.kind`*: rejected — relationship taxonomy is personal and grows
    ('godmother', 'college-roommate'). Structural enums (`entities.entity_type`, `pages.entity_type`) ARE
    constrained: **constrain structure, leave taxonomy open — but never implicit.**
  - *Free-text kinds auto-registered on first use*: rejected — a typo (`Friend`) would register a
    permanent kind.
  - *Symmetry as discipline or as app double-writes*: rejected — every graph query must remember the
    OR, or any future writer (API, CLI, agent) can silently create a half-edge.
  - *A `pending_links` table for unresolved wikilinks*: rejected — a second source of truth; the body
    is the record of an unresolved mention (D19).
- **Rationale.** The graph is where a life database earns its keep: ark's "killer feature" is its
  SQLite edge tables answering "everything about my son" in one query [R46].
- **Sources.** [R46][R43].

### D9 — Binary files: DEFERRED out of v1. The design is kept here for the day it returns.

- **Decision.** No `attachments` table, no `media/` directory, no binary files at all. The design
  below is **perfectly additive later** (a future `attachments` table touches nothing else). Until
  then, a page that needs a file references it in prose.
- **The deferred design.** Binary files live in `media/`, named by SHA-256; `attachments` rows carry
  `(entity_id, sha256, ext, mime, size)`; path = `media/<sha256[0:2]>/<sha256><ext>`, derived, never
  stored. Dedup is automatic. No inline BLOBs: SQLite's own benchmarks put the break-even around
  100 KB [R2][R3], and "To BLOB or Not To BLOB" agrees [R47]. ark and Open Brane converged here
  [R46][R43]. No GC: orphans accumulate, and a one-query sweep exists for the day it matters.
- **Reopen trigger.** An actual attachment need appears (photos in day pages, scanned documents).
- **Sources.** [R2][R3][R43][R46][R47].

### D10 — Time model: UTC instants + denormalized local days, both TEXT.

- **Decision.** §2.1. The zone is a column because only capture time can supply it: a UTC instant
  alone cannot say whether `22:30Z` was 14:30, 22:30 or 07:30 the next morning. The DB checks the
  shape of a zone name, not that it is a real zone; readers convert with a tz database, which keeps
  renamed zones (`Europe/Kiev` → `Europe/Kyiv`) as links. The standard way to write an instant with
  its zone is RFC 9557 [R69]: `2026-06-09T21:14:03.482Z[Europe/Berlin]` — exactly `*_at` plus `tz`.
  `tasks.completed_day` makes "what I
  finished on day X" a plain lookup instead of the query-time UTC-to-local derivation this decision
  forbids. Provenance (`source`) is a column now for the same reason as the zone (§2.2).
- **Alternatives.**
  - *Instants only, local day computed at query time*: rejected — a timezone move or DST rule
    silently rewrites history. This is why FxLifeSheet needed a dedicated `tag_days` importer to
    reconstruct local dates [R7], and why health-mcp denormalizes the local date at write time [R8].
  - *Unix epoch / Julian day integers*: unreadable in the file and in ad-hoc queries (principle 2).
    SQLite's own docs list TEXT ISO-8601 as the canonical representation [R5][R6].
  - *SQLite `CURRENT_TIMESTAMP` defaults*: rejected — second precision, non-ISO (executed) [R28].
- **Sources.** [R5][R6][R7][R8][R28][R69].

### D11 — Deletion: tombstones, never hard deletes.

- **Decision.** §2.3; the `*_no_delete` triggers. Tombstoning and un-tombstoning bump
  `entities.updated_at` (`entities_touch`, watching `deleted_at` only, so it cannot re-fire itself
  under `recursive_triggers=ON`, executed).
- **Rationale.** In a biography database, *erasure is itself biographical*: in 20 years it should be
  possible to see what the 2027 version of the owner deleted, and when. Hard deletes also break the
  `links` graph. Storage cost is irrelevant at this scale.
- **Alternatives.** Hard delete + `ON DELETE CASCADE` (destroys evidence, cascades surprises);
  trash-with-expiry (a policy layer that can be added on top of tombstones later).
- **Facts.** `measurements`, `balances` and `positions` are not even tombstoned: the first two are
  corrected by inserting rows, as medical records are, and a GPS fix is never corrected (D21). The triggers guard against mistakes, not against a writer that drops
  them, so "tamper-evident" would overstate it.
- **Tasks.** Abandoning a task is erasure (a tombstone), not a recorded decision: there is no
  `dropped` state, and a completed task stays completed.
- **Known asymmetry.** `links` rows are hard-deleted — no tombstone, no audit. Wikilink removal is
  *required* (the body is the truth, D19). Authored links (`friend`, `family`, …) go the same way,
  weighed against a split policy and **rejected**: the *evidence* (the day pages that name
  people together) survives, and relationship links are a summary over it. Relationship removal is therefore invisible. The split
  policy stays additive later (a `deleted_at` column + partial unique index + a flag in `link_kinds`),
  and `sqlite-history` triggers [R48] are the documented retrofit.

### D12 — Audit trail: no revision tables.

- **Decision.** No revision or history tables. The temporal metadata is row-level: `created_at` (on every
  table), `updated_at`, the tombstone, `source`, and the append-only facts. A commit must
  survive power loss, so connections use `synchronous = FULL` (§2.6) [R54].
- **Alternatives.** *Full revision snapshots per edit*: rejected — significant code for a history
  nobody has asked to query. *Trigger-based history tables* (`sqlite-history` [R48]): rejected **for
  now**; it retrofits onto the current schema with no redesign if a real need appears.
- **Cost accepted.** An `UPDATE` to a mutable row (a page body, a task, a person) overwrites the old
  value, and nothing recovers it.
- **Sources.** [R48][R54].

### D13 — Migrations: numbered plain SQL + `PRAGMA user_version`; freeze-and-migrate.

- **Decision.** No ORM, no migration framework, no down-migrations. **Until the freeze there are no
  migrations:** §3 is edited in place and test databases are recreated; `user_version` stays 1. After
  real data exists: numbered plain-SQL files, `db/migrations/0002_*.sql`, … applied in order, progress
  in `PRAGMA user_version` [R20][R21][R22]; additive only (new tables, columns, indexes; a column rename
  is allowed and recorded in its migration). `PRAGMA application_id = 'LIFE'` lets `file(1)` and
  future tools recognize the database [R1].
- **Every CHECK is named, so every rule can change without a rebuild.** Widening an enum (a new
  entity type, a holding's `side`), letting partial dates into `birth_day` or loosening the title rules is a
  two-statement transactional migration — `ALTER TABLE … DROP CONSTRAINT <name>; … ADD CONSTRAINT
  <name> CHECK (…)` (SQLite ≥ 3.53 [R55]) — **only because the CHECK has a name**: an unnamed CHECK
  cannot be dropped (`no such constraint`), and adding a looser second CHECK does not relax the first
  (both apply). `ADD CONSTRAINT` checks the existing rows, so tightening is as safe as loosening. All
  executed on a populated database, with integrity and foreign-key checks clean after. The alternative
  is SQLite's 12-step table rebuild, with FTS triggers, composite FKs and tombstone triggers to recreate.
- **Down-migrations** are rejected as a category. A migration runs on a *copy* first (`VACUUM INTO`,
  as for an importer, §2.8) and the four checks of §2.5 must pass on the copy before it touches
  `life.db`.
- **Sources.** [R1][R20][R21][R22][R55].

### D14 — UI: thin custom app for capture/browse; off-the-shelf tools for exploration.

- **Decision.** Build only what the product needs: a capture composer that appends to today's page
  (with mood, D6), a day view, simple metric charts, forms for tasks/people/places/holdings,
  a search box over `pages_fts`, and a backlinks panel. For ad-hoc exploration: **Datasette** pointed
  at `life.db`, read-only (§2.6) [R49]. `sqlite-web` is **not used** [R50]: it can insert, update and
  delete rows — a second writer that bypasses the insert conventions (principle 3).
- **The writing application** is `app/`: one Go binary that is the CLI, the REST API and the MCP
  server, so the owner's UIs, AI agents and importers all write through it. Its own decisions are in
  `app/README.md`.
- **Rejected.** Building a generic admin UI — Datasette already is one, maintained by someone else.
- **Sources.** [R49][R50].

### D15 — Recurrence: DEFERRED out of v1. The design is kept here for the day it returns.

- **Decision.** Nothing repeats: tasks have no recurrence columns (and there are no events, D22). A
  lifelog records what happened; a repeating appointment is planning, which the owner's calendar already does. What the
  schema still covers: **birthdays** are a query over `people.birth_day`; **"did I do it each month"**
  is a 0/1 habit metric in `measurements`, whose history charts for free; a **reminder** is a task
  with a `due_day`, or the next one created when the last is done (link the two with `related`).
- **The deferred design** — additive later, on `tasks` (`due_day` as the anchor) and on events if they
  return (D22):
  structured, readable columns `repeat` (`none|daily|weekly|monthly|yearly`), `repeat_every`
  (NULL = 1), `repeat_weekdays` (`'mo,we,fr'`, weekly only) and `repeat_until` (inclusive); a repeating
  row is a *template*, and occurrences are expanded at read by one window-bounded recursive CTE, never
  materialized. Weeks count calendar weeks (Mon–Sun) from the week of the start; months and years
  clamp to the month's last day. The expander and its oracle are in the git history of `tests/`.
- **Alternatives kept rejected for that day.** *An RFC-5545 RRULE string*: meaning lives in a parser,
  unreadable cold. *Materialized occurrence rows*: the three-way edit problem (this / this and future /
  all) and a regeneration job [R51][R52]. *A task whose one `status` repeats*: completing it ends the
  series.
- **Reopen trigger.** A recurring event the owner wants in this database rather than the calendar,
  or a task to tick off per occurrence that a habit metric cannot hold.
- **Sources.** [R51][R52].

### D16 — Places: an entity type.

- **Decision.** A place is an entity and a page (D20); its name is the page title. A day page names
  where the day was with `[[Place]]`, so the days spent somewhere are its backlinks (§6.3); `about`
  links connect anything to a place, `visited` a person to a place, `located-in` nests places (so
  "everything in Japan" is answerable, §6.18). There is no `lives-in` kind: it would be undated; where
  the owner lived is written in prose until dated spans return with events (D22). A place may also
  carry a point, and a GPS fix is matched to it (D21).
- **Alternatives.** A place as plain text in prose, not a page (the place queries fail, and
  backfilling 10 years of free text is the painful path); a `places` table outside the supertype (no
  links, no tombstones).
- **Sources.** [R46].

### D17 — The contract as data: `lifelog_meta`, comments inside the statements, in-DB guards.

- **Decision.** The rules of a table are comments inside its `CREATE` statement: a comment *outside* a
  statement is not stored in the file (executed), and `.schema` prints the statements. The few rules
  that span tables are rows of `lifelog_meta(key, value)`, queryable with `SELECT *`. Alongside, the
  contract is enforced where CHECK constraints reach: date and instant round-trips (§2.1), GLOBs only
  as character-class guards, and the composite FKs that make a row's type and its table agree. A CHECK
  uses only functions every SQLite the contract allows has (`lifelog_meta.sqlite`) — a SQLite that
  lacks one cannot write the table or integrity-check it — so no `octet_length()` where
  `length(CAST(x AS BLOB))` does the same.
- **The 2075 test** (§2.8) is executed: every question must be answered from `.schema` and
  `lifelog_meta`, and every `lifelog_meta` key must answer some question. A new rule that spans tables
  therefore needs a key and a row in that table; a table's own rule needs a comment in its statement.
- **Threat model.** The database is deliberately not encrypted (§2.8): finance never enters git; the
  disk is encrypted at rest; Datasette listens on localhost only and opens the file read-only; no
  credentials or full account numbers, ever.
- **Alternatives.** Comments only in a separate document (lives outside the artifact, rots); all
  rules as `lifelog_meta` rows (a second copy of what the statements already say).

### D18 — Money: holdings, balances, currencies. Net worth is derived, per currency.

- **Context.** The lifelong database also holds *net worth over time* and entries like it. Money must
  be **exact**, has a **currency** (several over 50 years, and currencies are redenominated), and its
  history is **audited**: a wrong balance is corrected visibly, not overwritten.
- **Decision.**
  - **Exact integers** in minor units of the holding's currency; `currencies.subunits` (immutable,
    `currencies_subunits_fixed`) gives them meaning — any integer, so a 1/5 subunit like MRU works.
  - **`holdings`** — an entity and a page (D20): the graph, the tombstone, `updated_at` and a page for
    prose come for free. `side` and `currency` are immutable (`holdings_meaning_fixed`, with a `WHEN`
    clause so full-row updates work). One rule covers every holding (§2.7).
  - **`balances`** — append-only snapshots; the newest row per `(holding_id, day)` wins and a NULL
    amount retracts, read through `balance_values`. *Newest* means **recorded last — the highest `id`**,
    not the most trustworthy source: the first import of old statements, run after a manual correction
    of the same days, wins over that correction; re-running the import changes nothing (its rows are
    duplicates), so to keep the manual value, enter it again after the import. Like `measurements`, the
    table is **bitemporal** [R68].
  - **Net worth is derived, never stored, and reported per currency** (§6.15–6.16): amounts of
    different currencies are never added — a minor unit of EUR and one of JPY are not the same size.
    Nothing converts one currency into another, so no figure can be re-stated behind your back by a
    corrected rate.
- **Alternatives rejected.**
  - *Money as `measurements`*: `REAL` drifts (`0.1 + 0.2` is `0.30000000000000004`, executed), the unit
    lives on the metric, there is no per-day retraction, and a holding would not be linkable.
  - *Decimal as TEXT / `NUMERIC`*: SQLite has no decimal type — TEXT decimals sort as strings
    (`'10.25'` before `'9.5'`) and `NUMERIC` stores a long decimal as REAL (executed).
  - *One signed column and no `side`*: a loan typed positive by mistake flips net worth silently.
  - *Store net-worth totals*: loses the drivers. The one legitimate use, a spreadsheet of totals, is a
    pseudo-holding closed when detailed holdings begin (§6.16).
  - *`supersedes_id` chains for balances*: `balances` has a natural key `(holding, day)`, so "newest
    row wins" is simpler; measurements have none (several readings a day are valid). Two shapes of
    data, two correction shapes.
  - *Full double-entry ledger*, *quantity × price per holding*, *exchange rates*: deferred, §7 — each
    is additive later and nothing in the schema blocks it.
  - *Holdings outside the entity supertype*: no links, no tombstone, no page.
- **Costs accepted.** A balance carries forward until replaced, so a stale holding keeps counting
  (§6.17 exposes that); a life in two currencies has two net-worth series; there is no return
  analysis (needs flows, §7); joint holdings are recorded as your share.
- **Sources.** [R53][R54][R55][R56][R57][R68].

### D19 — The wikilink save contract: one transaction, links follow the body, a bad target never blocks a save.

- **Decision.** §2.4 and §6.13. The DDL does not implement it: the contract lives in the writing
  application, and the DDL checks what it can — endpoint types, filename-safe unique titles.
- **Why.** Without it: the auto-created page for `[[Health/Diet]]` is rejected by the title CHECK; a
  writer that swallows the error and commits leaves an orphan `entities` row, and one that does not
  loses the page's text. An "upsert" of links leaves a link behind after the body dropped it. Read literally,
  the tag rule turns a stub's `#REDIRECT` into a page called `REDIRECT`.
- **Alternatives.**
  - *Make the database skip a bad target* (a trigger that swallows the page insert, or a title CHECK
    loose enough for any `[[text]]`): a CHECK cannot skip a row, and a title the filesystem cannot hold
    is what the CHECK exists to stop (D5).
  - *A regular expression over the raw body*: rejected — it cannot tell code, URLs and raw HTML from
    prose without re-implementing a CommonMark parser.
  - *Expand `#health` into `[[health]]` in the body*: rejected — it rewrites the owner's text.
  - *Obsidian-style `[[Page#Heading]]` / `^block` targets*: rejected — `#` is legal in a title
    (`[[C#]]`); `|alias` is kept because `|` can never be in a title.
  - *Keep a stub's wikilink and exempt only its tag*: rejected — the stub would show up as a backlink
    of its own replacement, duplicating the `redirect` edge.
- **Costs accepted.** The database does not check that `links` matches the bodies. That drift is
  detectable and repairable: a rebuild from the bodies gives the same links as 400 incremental random
  edits (executed). A skipped target is remembered only in the body's own text.
- **Sources.** [R58][R59].

### D20 — A person, place or holding is a page: one id, and `[[Name]]` reaches it directly.

- **Decision.** A named entity is one id with three rows: `entities` (its type), a titled `pages` row
  whose `entity_type` is that type, and its domain row, whose composite FK references the `pages` row
  (`people`/`places`/`holdings` → `pages` → `entities`). The page title is the entity's handle, and the
  unique `title_key` forces two people called Sam apart (`Sam (barber)`). Places and holdings have no
  name column (the title is the name); `people.name` is the editable full name. The save contract is
  untouched: `[[Bob Sample]]` is an ordinary wikilink, and it lands on the person's own id, so
  "everything about Bob" is one pair of link queries (§6.6).
- **Promotion.** A ghost page made by an earlier `[[Bob Sample]]` becomes the person by
  `UPDATE entities SET entity_type = 'person'` — the `ON UPDATE CASCADE` foreign key carries the new type to
  `pages.entity_type` — and one `people` insert. The foreign keys refuse a person without a page, and
  undoing a promotion (the `people` row's FK); `pages_entity_type` refuses turning a page into a task
  (both executed).
- **Why.** The owner writes `Today I met [[Bob Sample]]` and wants that day's page attached to the
  person.
  A wikilink can only land on a page, so the person must be one. Giving the person and the page the
  same id means the backlinks of the person *are* the backlinks of the page: no second id to resolve,
  no `page_id` pointer and its rules, one row fewer per named thing.
- **Alternatives.**
  - *A separate page entity pointed at by `entities.page_id`*: rejected — two ids for one person (the
    page `[[…]]` reaches and the person `about` points at), a pointer column with a CHECK, a
    UNIQUE and two triggers, a third leg in every "everything about X" query, and a place or holding
    name that had to be unique twice (its own `name` and its title).
  - *A `mention` link kind, page → person, resolved by matching names on save*: rejected — a page and
    a person with one name need a precedence rule, and a link would depend on the `people` table at
    save time, so a rename or a new person changes what a re-save produces.
  - *A `[[@Name]]` prefix for people*: rejected — the same resolution problem with a namespace in front.
  - *Everything is a page (tasks too)*: not taken — a task's name is a label, not a unique permanent
    handle, and would collide (`Dentist`).
- **Costs accepted.** Every person, place and holding needs a unique handle, even one never mentioned;
  an importer makes one (§6.19). A handle is permanent like any title: a changed name is `people.name`,
  and `[[Old name]]` keeps working. A typo in a name makes a ghost page like any wikilink typo. A page
  names a person by wikilink *or* by `about`; the two are separate rows, and §6.6 reads both.

### D21 — Location: a GPS track in `positions`, a point on each place.

- **Decision.** Where the owner was is a fact tier of its own. `positions` holds one GPS fix per row —
  `taken_at`, the local `day` and `tz`, WGS84 `lat`/`lon` [R71], an optional `accuracy_m` — with
  `source` and `import_key` as on `measurements`. It is append-only (`positions_no_update`,
  `positions_no_delete`) and has no correction row, so a fix at exactly 0°, 0° is refused
  (`positions_not_null_island`): Google Photos metadata writes 0.0, 0.0 for a photo without a location,
  and imported as a fix it could never be taken back. A place may carry one point (`places_lat`,
  `places_lon`, `places_coords_pair`), which is an attribute of the place and so is edited in place.
  A fix is matched to a place at query time, never stored (§6.20).
- **Where the fixes come from.** An Android phone, the location in Google Photos metadata and
  Google's location history (Timeline), each through an importer with its own `source`.
- **Why.** The owner asked "where was I at a given moment?", and the schema could only answer with the
  places a day page names. A track also dates what no page records: the walk, the drive, the day
  nobody wrote down.
- **No correction.** A fix is raw sensor output, not a statement the owner makes. A wrong one is left out
  when read, by `accuracy_m`, and a wrong import is caught by the trial run on a copy (§2.8). A
  retraction can be added later without touching a row (§7).
- **Distance without math functions.** `sin`/`cos` exist only in SQLite builds compiled with
  `SQLITE_ENABLE_MATH_FUNCTIONS` [R72], so §6.20 uses only `+ - *`: the app binds the metres per degree of
  longitude at the fix's latitude (`111320·cos(lat)`) and the query ranks places by the squared
  equirectangular distance. Within a city it picks the same place as the great-circle distance, and its
  distance is within 0.5 % of it, so only a place within 1 % of the radius can fall either side (executed,
  against a haversine oracle). **Known limit:** across the ±180° meridian two nearby points
  are 360° apart, so a place there is not found; GeoJSON cuts geometry at the meridian for the same
  reason [R71].
- **Alternatives.**
  - *Two metrics, `latitude` and `longitude`, in `measurements`*: rejected — nothing pairs the two rows
    of one fix, a correction could supersede one half, and a chart of either alone means nothing.
  - *Each fix an entity*: rejected — a phone logs hundreds a day, and nothing links to a fix; a
    tombstone per row buys nothing a filter by accuracy does not.
  - *A radius or polygon per place*: not taken — nearest point within a bound radius answers the
    question; a place's extent is additive later (§7).
- **Sources.** [R71][R72].

### D22 — Events: DEFERRED out of v1. The design is kept here for the day it returns.

- **Decision.** No `events` table. What happened on a day is that day's page (D5): its text says
  what, and its `[[links]]` say who and where — the days with `[[Ana]]` or at `[[Lakeside]]` are the
  day pages that link them (§6.3). A task is still a task; a reading is still a measurement.
- **Why.** The first real import, an Obsidian vault of daily notes, had a model write an event for
  every outing a note told ("we went to Lakeside", a haircut, a visit): each one a second copy of a
  sentence of that day's page, an event and a place of the same name, and nothing a question needed
  that the day page and its links did not already answer. Nothing else needed events yet: no source
  that delivers dated spans or timed sessions has been imported.
- **The deferred design** — additive later (a new table, two link kinds): `events(id, entity_type,
  name, start_day, end_day, start_at, end_at, place_id, note)` hanging off `entities` like `tasks`,
  with the round-trip CHECKs of §2.1 and `end ≥ start`; day-precise events first-class, instants
  optional; one place per event (`place_id`); `attended` (person → event); and an event's **kind** as
  an `is-a` link to the page naming it — [[Workout]], [[Sleep]] — never a column (free text splits a
  kind by spelling, a CHECK list closes a personal taxonomy, and a registry is a second namespace
  beside page titles). An importer would key its events like any entity (§6.21), and a moved event
  would update its row.
- **Alternatives.** *Keep the table and tell importers to write fewer events*: rejected — whatever
  is in the schema at the freeze stays for good (D13), and the table had no real row a day page could
  not hold. *An event as a page*: rejected — an event's name is a label, not a unique handle, and
  would collide (`Dentist`).
- **Reopen trigger.** A source that delivers dated spans or timed sessions — a phone's sleep and
  exercise sessions, a calendar, a location export's visits — or a question the day pages cannot
  answer: "how many workouts this year?", "where did I live in 2015?".

---

## 6. Query cookbook

Proof that the schema serves the product with plain SQL. `:named` are bind parameters.
All examples filter tombstones (`e.deleted_at IS NULL`). Every write transaction starts with
`BEGIN IMMEDIATE` (§2.6), and every block runs in `tests/`.

### 6.1 Capture: append to the day page (the universal insert convention)

Every entity insert is two statements in one transaction: `entities` first, `RETURNING id`, then the
domain row with that id, which the app keeps in a variable (below `:page_id`) and binds wherever the
page is meant (§2.2). Capture appends to today's page; the first capture of a day creates it.

```sql
BEGIN IMMEDIATE;
-- today's page, if the day has one (a day page's key is its title, §2.4): found, the app keeps its id
-- as :page_id and skips the two INSERTs; found tombstoned, it revives it as §6.13 step 2a does
SELECT p.id, e.deleted_at FROM pages p JOIN entities e ON e.id = p.id WHERE p.title_key = '2026-09-29';
INSERT INTO entities(entity_type, created_at, updated_at, tz, source)   -- tz: the capturing device's IANA zone right now
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'Europe/Berlin', 'ui')
RETURNING id;   -- the app keeps it as :page_id
INSERT INTO pages(id, title, title_key, day)
VALUES (:page_id, '2026-09-29', '2026-09-29', '2026-09-29');
-- the entry, after a blank line when the page already has text
UPDATE pages SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END
                        || 'Shipped the schema doc. Review pending. [[Lifelog]]'
 WHERE id = :page_id;
-- the body names [[Lifelog]]: the link sync of §6.13 runs here, inside this same transaction
-- optional mood, attached to the page it belongs to (D6):
INSERT INTO measurements(metric_id, day, value, source, captured_with_id, created_at)
SELECT id, '2026-09-29', 4, 'ui', :page_id, strftime('%Y-%m-%dT%H:%M:%fZ','now')
  FROM metrics WHERE name = 'mood';      -- (a timed reading also sets taken_at and tz)
COMMIT;
```

### 6.2 The day view

The day's page, other pages written that day, open tasks due by then, what was finished that **local**
day, and measurements (through `measurement_values`, so corrected readings never show).
`ORDER BY (at IS NOT NULL), at` puts undated items — the day page first — before the rest on purpose;
a bare `ORDER BY at` does it by accident.

```sql
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, p.body AS detail
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.title_key = :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.updated_at > e.created_at THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.day = :day AND p.title <> :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'task', NULL, t.name
    FROM tasks t JOIN entities e ON e.id = t.id
   WHERE e.deleted_at IS NULL AND t.completed_at IS NULL
     AND t.due_day <= :day
  UNION ALL
  SELECT 'done', t.completed_at, t.name
    FROM tasks t JOIN entities e ON e.id = t.id
   WHERE e.deleted_at IS NULL AND t.completed_day = :day
  UNION ALL
  SELECT m.name, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id
   WHERE me.day = :day
)
ORDER BY (at IS NOT NULL), at;
```

A page shows on the day it was *written* (a link target the app created has no day, D5), flagged if
edited since. Undated open tasks live in the task list, not in every day view. Completing a task sets
both columns: `UPDATE tasks SET completed_at = :now, completed_day = :day WHERE id = :task_id`.

### 6.3 The days that name someone or somewhere

"When did I see Ana?", "when was I at Lakeside?": the day pages that name the person, place or page,
newest first — by a `[[wikilink]]` in their text, or by an `about` link a writer added where the text
names them without brackets (an imported note). A day page is the page whose title is its day (§2.4).
For the days spent anywhere inside a place (Tokyo in Japan), walk `located-in` first (§6.18).

```sql
SELECT DISTINCT d.day, substr(d.body, 1, 60) AS start
  FROM links l
  JOIN pages d    ON d.id = l.from_id AND d.title = d.day
  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL
 WHERE l.to_id = :entity_id AND l.kind IN ('wikilink', 'about')
 ORDER BY d.day DESC;
```

### 6.4 Mood over time

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'
 ORDER BY me.day;
```

### 6.5 Backlinks to a page (or to anything)

```sql
SELECT l.kind, e.entity_type, l.from_id, COALESCE(pg.title, t.name) AS label
  FROM links l
  JOIN entities e ON e.id = l.from_id AND e.deleted_at IS NULL
  LEFT JOIN pages   pg ON pg.id = l.from_id
  LEFT JOIN tasks   t  ON t.id  = l.from_id
 WHERE l.to_id = :page_id
   AND l.kind <> 'redirect';   -- a rename stub is not a mention of its replacement (§2.4)
```

A person, place or holding is a page, so its title labels it. Symmetric kinds are mirrored (D8), so
this one direction suffices for them.

### 6.6 Everything about a person, place or holding (the ark query)

Asymmetric kinds put the entity on either end (`visited` is person → place, `about` is entity →
person), so query both directions. The pages that write `[[Bob Sample]]`, day pages included, link
to the person's own id (D20), so they are in the first leg; what the person's page body links to is in the
second.

```sql
SELECT l.kind, e.entity_type, l.from_id AS other_id, 'in' AS direction
  FROM links l JOIN entities e ON e.id = l.from_id
 WHERE l.to_id = :entity_id AND e.deleted_at IS NULL
UNION ALL
SELECT l.kind, e.entity_type, l.to_id, 'out'
  FROM links l JOIN entities e ON e.id = l.to_id
 WHERE l.from_id = :entity_id AND e.deleted_at IS NULL;
```

The legs are index-served by `links_to` and by the `UNIQUE(from_id, to_id, kind)` index.

### 6.7 Metric series, corrections applied (weight, last 90 days)

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN metrics m ON m.id = me.metric_id AND m.name = 'weight'
 WHERE me.day >= date(:day, '-90 day')
 ORDER BY me.day;
```

Two legitimate readings on one day are both returned; aggregate in the query if a daily value is wanted.

### 6.8 Full-text search

```sql
SELECT p.id, p.title,
       snippet(pages_fts, 1, '<b>', '</b>', '…', 24) AS ctx
  FROM pages_fts
  JOIN pages p    ON p.id = pages_fts.rowid
  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
 WHERE pages_fts MATCH :query
 ORDER BY rank;
```

### 6.9 A task from a day page (provenance preserved)

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('task', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :task_id
INSERT INTO tasks(id, name, due_day)
VALUES (:task_id, 'Book dentist appointment', :due_day);
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:task_id, :page_id, 'spawned', strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui');
COMMIT;
```

### 6.10 Correct a wrong measurement (append-only)

```sql
-- never UPDATE the value; supersede it:
INSERT INTO measurements(metric_id, day, taken_at, value, source, supersedes_id, created_at)
VALUES (:metric_id, :day, NULL, 71.4, 'ui', :wrong_row_id, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
-- rejected if :wrong_row_id belongs to a different metric, does not exist, or
-- was already corrected once (correct the correction instead)

-- a row that should never have existed (a mis-tap): RETRACT it — a correction with a NULL value.
-- measurement_values then hides both rows; to bring a value back, correct the retraction.
INSERT INTO measurements(metric_id, day, value, source, supersedes_id, created_at)
VALUES (:metric_id, :day, NULL, 'ui', :mistaken_row_id, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
```

### 6.11 Subtasks (recursive CTE over `links`)

```sql
WITH RECURSIVE subtree(root, id, depth) AS (
  SELECT :task_id, :task_id, 0
  UNION ALL
  SELECT subtree.root, l.from_id, subtree.depth + 1
    FROM links l JOIN subtree ON l.to_id = subtree.id
   WHERE l.kind = 'subtask' AND subtree.depth < 32     -- cycle guard: the walk always terminates
)
SELECT t.name, subtree.depth
  FROM subtree JOIN tasks t ON t.id = subtree.id
  JOIN entities e ON e.id = t.id AND e.deleted_at IS NULL
 WHERE subtree.id <> subtree.root OR subtree.depth = 0
 ORDER BY subtree.depth;
```

Cycle *prevention* is app-level: never link a task to its own ancestor. Without the depth cap, a
two-task cycle below the root never terminates (executed); with it, the query returns.

### 6.12 Ghost pages (the wikilink sweep, D5)

```sql
SELECT id, title, created_at FROM ghost_pages ORDER BY created_at;
```

The view (§3) lists empty plain pages nothing points at, 30 days old; the UI surfaces it as a cleanup
list. The sources are capture-time typos and a mention later edited out of a body (step 4 of §6.13
drops its link, the empty page stays).

### 6.13 Save a body with wikilinks (resolve or create each target, sync the links)

The app reads the body (§2.4) and gets a list of distinct valid targets, each with `:title` (first
spelling, NFC) and `:key` (`title_key(:title)`). Everything below is **one `BEGIN IMMEDIATE`
transaction**: two writers that save the same new link cannot both see "none found" — the second
waits, then finds the first one's page. Each target is its own `SAVEPOINT`, so a target that fails for
any reason is rolled back alone (no link, no orphan `entities` row) and the save carries on (D19).

```mermaid
%% diagram: save-flow
flowchart TD
    start(["save a page body"]) --> begin["BEGIN IMMEDIATE"]
    begin --> body["0. write the body<br/>INSERT (§6.1) or UPDATE pages SET body"]
    body --> more{"another distinct<br/>valid target?"}
    more -->|"yes"| sp["SAVEPOINT target"]
    sp --> resolve["1. resolve<br/>WHERE title_key = :key"]
    resolve --> found{"found?"}
    found -->|"no"| create["2b. INSERT entities and pages<br/>an empty page: no day, or a day page's own"]
    found -->|"yes, tombstoned"| revive["2a. entities.deleted_at = NULL"]
    found -->|"yes, live"| link
    create --> link["3. INSERT links wikilink<br/>ON CONFLICT DO NOTHING"]
    revive --> link
    link -->|"ok"| release["RELEASE target"]
    link -->|"any error in 1 to 3"| back["ROLLBACK TO target, RELEASE<br/>no link and no orphan row"]
    release --> more
    back --> more
    more -->|"no"| prune["4. DELETE the wikilink rows<br/>the body no longer names"]
    prune --> commit(["COMMIT"])
```

```sql
BEGIN IMMEDIATE;
-- 0) the body itself: the INSERT of §6.1, or  UPDATE pages SET body = :body WHERE id = :page_id;

-- for each target (skip a target whose :key is the page's own title_key):
SAVEPOINT target;
-- 1) resolve (a search on the unique index pages_title)
SELECT p.id, p.title, e.deleted_at
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.title_key = :key;

-- 2a) found, but tombstoned: revive it (the UI tells the owner the save revives a deleted page)
UPDATE entities SET deleted_at = NULL WHERE id = :found_id;
-- 2b) none found: create the empty page; no day (a link target is not something written today),
--     except a day page, whose day is its title (pages_day_page)
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
RETURNING id;   -- the app keeps it as :target_id
INSERT INTO pages(id, title, title_key, day)
VALUES (:target_id, :title, :key, CASE WHEN date(:title) IS :title THEN :title END);

-- 3) link it (:target_id is :found_id, or the id 2b returned)
INSERT INTO links(from_id, to_id, kind, created_at, source)
VALUES (:page_id, :target_id, 'wikilink', strftime('%Y-%m-%dT%H:%M:%fZ','now'), :source)
ON CONFLICT(from_id, to_id, kind) DO NOTHING;
RELEASE target;   -- on any error in 1-3:  ROLLBACK TO target;  RELEASE target;  and go on

-- 4) once, after the last target: drop the links the body no longer names
--    (:target_ids is a JSON array of the ids linked in step 3, '[]' when there are none)
DELETE FROM links
 WHERE from_id = :page_id AND kind = 'wikilink'
   AND to_id NOT IN (SELECT value FROM json_each(:target_ids));
COMMIT;
```

`[[café NOTES]]` and `[[Café notes]]` produce the same `:key`, so both resolve to the one page;
`title` keeps the spelling of whoever created it. A target may be a person, place or holding: it is a
page (D20). `:source` is the saving writer (§2.2).

### 6.14 Open a holding; record, correct and retract a balance (D18)

Amounts are **integer minor units** of the holding's currency: €12,345.67 is `1234567`. A holding is
a named entity (§6.19): its entity, its page (the title is its name), its `holdings` row.

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('holding', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :holding_id
INSERT INTO pages(id, entity_type, title, title_key) VALUES (:holding_id, 'holding', 'Main checking', 'main checking');
INSERT INTO holdings(id, side, currency, category, institution, opened_day)
VALUES (:holding_id, 'asset', 'EUR', 'cash', 'Bank A', '2019-03-01');
COMMIT;

-- what the statement says at the end of a LOCAL day
INSERT INTO balances(holding_id, day, amount, created_at, source, note)
VALUES (:holding_id, '2026-09-30', 1234567, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui', 'statement');

-- a wrong entry is never edited: write the right one for the same (holding, day) — the newest row wins
INSERT INTO balances(holding_id, day, amount, created_at, source, note)
VALUES (:holding_id, '2026-09-30', 1234576, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui', 'digits transposed');

-- an entry that should never have existed (wrong holding, wrong day): retract it with NULL
INSERT INTO balances(holding_id, day, amount, created_at, source, note)
VALUES (:holding_id, '2026-09-30', NULL, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui', 'belongs to the savings holding');

-- idempotent bulk import: ON CONFLICT ... DO NOTHING skips only the duplicate (§2.3)
INSERT INTO balances(holding_id, day, amount, created_at, source, import_key)
VALUES (:holding_id, :day, :amount, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:bank_csv', :row_key)
ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING;

-- reading one back as a number
SELECT b.day, b.amount * 1.0 / c.subunits AS whole_units, a.currency
  FROM balance_values b JOIN holdings a ON a.id = b.holding_id JOIN currencies c ON c.code = a.currency
 WHERE b.holding_id = :holding_id ORDER BY b.day;
```

A retraction only hides the day: the as-of rule of §6.15 falls back to the previous balance. Writing a
correct value after a retraction simply becomes the newest row again.

### 6.15 Net worth on a day, per holding and per currency

`:day` is the local day to value. Each holding's signed value is in its own currency's minor units.

```sql
WITH held AS (                                   -- open, live holdings on :day, and the day of the balance that counts
  SELECT a.id, p.title AS name, a.side, a.currency,
         (SELECT b.day FROM balance_values b
           WHERE b.holding_id = a.id AND b.day <= :day ORDER BY b.day DESC LIMIT 1) AS as_of
    FROM holdings a
    JOIN pages p    ON p.id = a.id
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
   WHERE coalesce(a.opened_day, '0000-01-01') <= :day
     AND coalesce(a.closed_day, '9999-12-31') >= :day
)
SELECT h.name, h.side, h.currency, b.amount, h.as_of,
       CAST(julianday(:day) - julianday(h.as_of) AS INTEGER) AS stale_days,        -- how old the number is
       (CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * b.amount AS net_minor       -- signed, minor units of h.currency
  FROM held h
  CROSS JOIN balance_values b ON b.holding_id = h.id AND b.day = h.as_of   -- CROSS JOIN pins the order: holdings first, then seek
 ORDER BY h.currency, h.side, h.name;
```

Net worth on `:day` is that query wrapped: `SELECT currency, sum(net_minor), count(*) FROM (…)
GROUP BY currency` — one row per currency, exact integers. **Never `sum(net_minor)` across
currencies** (D18). The rules, executed against an exact-arithmetic oracle: a holding **counts** on
`:day` if it is live, open (`opened_day <= :day <= closed_day`, both inclusive; NULL = unbounded) and
has a balance on or before `:day`; its value is the **latest effective balance on or before `:day`**,
carried forward (`stale_days` says how old); `side` decides the sign. `CROSS JOIN` pins the join order,
so SQLite seeks each holding's balance by index instead of scanning every balance (executed: the plan).

### 6.16 Net worth over time (month-ends)

```sql
WITH RECURSIVE month_ends(day) AS (
  SELECT date(:from_day, 'start of month', '+1 month', '-1 day')
  UNION ALL
  SELECT date(day, 'start of month', '+2 month', '-1 day') FROM month_ends
   WHERE day < date(:to_day, 'start of month', '+1 month', '-1 day')
),
held AS (                                        -- every open, live holding on every month-end, with its latest balance
  SELECT m.day, a.side, a.currency,
         (SELECT b.amount FROM balance_values b
           WHERE b.holding_id = a.id AND b.day <= m.day ORDER BY b.day DESC LIMIT 1) AS amount
    FROM month_ends m
    JOIN holdings a ON coalesce(a.opened_day, '0000-01-01') <= m.day
                   AND coalesce(a.closed_day, '9999-12-31') >= m.day
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
)
SELECT day, currency,
       sum((CASE side WHEN 'asset' THEN 1 ELSE -1 END) * amount) AS net_worth_minor,   -- minor units of that currency
       count(*)                                                  AS holdings           -- holdings of that currency with a balance by then
  FROM held
 WHERE amount IS NOT NULL
 GROUP BY day, currency
 ORDER BY currency, day;
```

One series per currency; a month-end at which no holding of a currency has a balance has no row for it.
The correlated scalar subquery makes each (month, holding) an index seek; joining `balance_values`
directly would scan all balances once per month.

**Backfilling history from a spreadsheet of monthly totals**: make one holding, `Legacy net worth`
(`side = 'asset'`, `category = 'aggregate'`), record the totals as its balances (liabilities already
netted, so amounts may be negative), and set its `closed_day` to the day the detailed holdings begin.

### 6.17 Holdings that need updating

```sql
SELECT p.title, max(b.day) AS last_balance,
       CAST(julianday(:day) - julianday(max(b.day)) AS INTEGER) AS stale_days
  FROM holdings a
  JOIN pages p    ON p.id = a.id
  JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
  LEFT JOIN balance_values b ON b.holding_id = a.id AND b.day <= :day
 WHERE a.closed_day IS NULL OR a.closed_day >= :day
 GROUP BY a.id
HAVING max(b.day) IS NULL OR max(b.day) < date(:day, '-35 day')
 ORDER BY stale_days DESC;
```

### 6.18 Everything inside a place (containment)

`located-in` (place → place: Tokyo → Kanto → Japan) is one-way and transitive. Walk it down with a
recursive CTE — `UNION`, not `UNION ALL`, so a mistaken cycle ends instead of looping (executed) —
then join what hangs off those places. "My days in Japan in 2019": the day pages that name a place
inside it (§6.3):

```sql
WITH RECURSIVE inside(id) AS (
  SELECT :place_id
  UNION
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id WHERE l.kind = 'located-in'
)
SELECT d.day, pl.title AS place
  FROM inside
  JOIN links l    ON l.to_id = inside.id AND l.kind = 'wikilink'
  JOIN pages d    ON d.id = l.from_id AND d.title = d.day
  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL
  JOIN pages pl   ON pl.id = inside.id
 WHERE d.day BETWEEN :from_day AND :to_day
 ORDER BY d.day, pl.title;
```

A day that names two places inside Japan is listed once per place.

### 6.19 A person, place or holding: create one, promote a ghost page (D20)

Step 0 is the resolve of §6.13. No row: create it (steps 1–3). A plain page (`entity_type = 'page'`,
e.g. a ghost an earlier `[[Bob Sample]]` made): promote it instead. Any other row: the handle is
taken; choose another (`Bob Sample (colleague)`). A place or a holding is the same with its own
type and domain row (§6.14).

```sql
-- 0. does the handle exist already?  :handle_key = title_key(:handle_title), §2.4
SELECT p.id, p.entity_type FROM pages p WHERE p.title_key = :handle_key;

-- create: entity, page, domain row — one id
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source)
VALUES ('person', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :person_id
INSERT INTO pages(id, entity_type, title, title_key) VALUES (:person_id, 'person', :handle_title, :handle_key);
INSERT INTO people(id, name) VALUES (:person_id, 'Bob Sample');
COMMIT;

-- promote: the plain page :ghost_id becomes a person; its links stay (the id does not change)
BEGIN IMMEDIATE;
UPDATE entities SET entity_type = 'person' WHERE id = :ghost_id AND entity_type = 'page';   -- cascades to pages.entity_type
INSERT INTO people(id, name) VALUES (:ghost_id, 'Ana Example');
COMMIT;
```

The day pages that already name the person (§6.3) keep their links: the id did not change. A promotion
cannot go wrong quietly: a page that is already named or gone makes the `UPDATE` change no row, so the
`people` insert fails on its key (executed); roll the transaction back.

### 6.20 Where was I? Record a fix, the fix at a moment, a day's track, the place it was (D21)

A fix is inserted once and never edited; an importer adds its `import_key` and `ON CONFLICT(source,
import_key) WHERE import_key IS NOT NULL DO NOTHING` (§2.8). "Where was I" is the last good fix at or
before the moment: show its `taken_at`, since the fix before a gap may be hours old. The place is the
nearest live place with a point within `:radius_m`. The app binds `:m_per_deg_lon = 111320 ·
cos(:lat in radians)`, so the query needs no math functions (D21) and works on every SQLite.

```sql
-- record a fix (here a manual pin)
INSERT INTO positions(taken_at, day, tz, lat, lon, accuracy_m, created_at, source)
VALUES (:taken_at, :day, 'Europe/Berlin', :lat, :lon, 12, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'ui')
RETURNING id;   -- the app keeps it as :position_id

-- where was I at :at? the last fix at or before it that is accurate enough (walks positions_time)
SELECT taken_at, lat, lon, accuracy_m
  FROM positions
 WHERE taken_at <= :at AND (accuracy_m IS NULL OR accuracy_m <= :max_accuracy_m)
 ORDER BY taken_at DESC
 LIMIT 1;

-- the track of a local day, in order (positions_day)
SELECT taken_at, lat, lon, accuracy_m
  FROM positions
 WHERE day = :day AND (accuracy_m IS NULL OR accuracy_m <= :max_accuracy_m)
 ORDER BY taken_at;

-- which place was the fix at :lat, :lon? d2 is the squared distance in metres
SELECT id, title, d2
  FROM (SELECT pl.id, pg.title,
               ((pl.lat - :lat) * 111320.0) * ((pl.lat - :lat) * 111320.0)
             + ((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2
          FROM places pl
          JOIN pages pg   ON pg.id = pl.id
          JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL
         WHERE pl.lat IS NOT NULL)
 WHERE d2 <= :radius_m * :radius_m
 ORDER BY d2
 LIMIT 1;
```

Give a place its point with `UPDATE places SET lat = …, lon = … WHERE id = :place_id`; both or neither
(`places_coords_pair`). No place within the radius is an answer too: the fix is shown as coordinates.

### 6.21 Import a row once: insert it, re-run it, update a changed one

**Who sets `import_key`:** every writer that may send the same row twice — an importer (re-run, or a
fresh export years later), a phone replaying its offline queue, an agent retrying after a timeout whose
first attempt did commit. It is the key the *sender* gives the row: the source's own id when there is
one (§2.8 step 3), else a UUID the client makes once and resends unchanged. A row typed on the hub itself
cannot arrive twice and has none. The same holds for measurements, balances and positions.

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source, import_key)
VALUES ('task', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:todo', :import_key)
ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING
RETURNING id;   -- the app keeps it as :task_id; no row back = imported before: skip the next INSERT
INSERT INTO tasks(id, name, due_day)
VALUES (:task_id, 'Renew passport', '2026-10-01');
COMMIT;

-- a later run finds the task moved: update the live row that has the key; a tombstoned one stays gone
UPDATE tasks
   SET due_day = '2026-10-15'
 WHERE id = (SELECT id FROM entities
              WHERE source = 'import:todo' AND import_key = :import_key AND deleted_at IS NULL);
```

A run that inserts nothing the second time is the check of §2.8 step 5. `import_key` never changes
(`entities_provenance_fixed`), so the key found on the next run is the key written on the first.

---

## 7. Explicit non-goals and deferred work

Researched, considered, and **deliberately cut** — recorded so they are not silently
re-added, each with the trigger that should reopen the question.

| Cut item | Why cut | Reopen when |
|---|---|---|
| Copies of the database that merge (sync, CRDTs) | Other devices are clients of the one writer (D3); a merge would force UUID keys and drop the checked FKs and the unique `title_key` [R75] | Never while the schema rests on those constraints |
| An entity `uid` (UUIDv7) | An integer id is never reused and a title is permanent, so references are already stable; `entities.import_key` covers an offline replay; the column is additive (D3, executed) | A reference that must survive a re-key or a merge |
| Agent CLI/API | Planned as a later layer over the same DB; single-writer rule (principle 3) extends to it naturally; `source` already tells its rows apart (§2.2) | After v1 UI exists |
| Binary files / `attachments` | Cut from v1 (D9): all-text DB stays megabyte-scale; design kept in D9 | The first real photo/PDF attachment need |
| Recurring tasks (and events) | A lifelog records what happened; the calendar does planning; birthdays, habits and reminders are covered without it; design kept in D15 | A recurring event wanted in this database, or a task to tick off per occurrence that a habit metric cannot hold |
| Database encryption | Deliberate plaintext (D17); protect the disk instead (full-disk encryption) | A legal/privacy requirement for at-rest encryption |
| Revision/history tables | Tombstones and append-only facts cover the need (D12); `sqlite-history` triggers are the documented fallback [R48] | Demonstrated need for intra-day history of structured rows |
| Raw wearable-import tier | health-mcp's two-tier mirror exists for provider quirks [R8]; premature with zero importers | A second data source appears, or re-import fidelity bites |
| LOINC / UCUM / reference ranges | Interop vocabulary, not storage need (D7) [R8] | FHIR export or clinical data exchange wanted |
| Multi-resolution rollups | ~5 GB/lifetime of sensor data queries fine raw (D7) [R45] | Query latency is ever noticeable |
| Text-valued measurements | `value REAL` keeps charting trivial (D7) | A real series needs non-numeric values (then: probably a note + link instead) |
| JSON columns / property bags | The core anti-decision (D2) | Never |
| Generic view system, AI generation | Cut from product scope by the owner | Product decision, not schema |
| Markdown export of the prose; nightly snapshots, restore and an off-box copy; a CSV dump; continuous replication | Out of scope while the schema is being made reliable; nothing in the schema depends on any of them (D4, D12). Until one exists there is **no second copy** of `life.db`, and the file itself is the only thing to leave with | Before the first real data enters a canonical `life.db` (the freeze, D13) at the latest |
| Trash UI with restore/expiry | Tombstones (D11) are already the data layer | UI work; zero schema change |
| `.sqlar` single-artifact packaging | `tar` covers "one file to email" | Frequent whole-archive portability need |
| An NFKC `title_key` (`NFKC(casefold(NFKC(title)))`) | Unicode's formal case-fold promise covers NFKC text [R74]; the NFC key is outside it only for titles with compatibility characters, rare in this life log. The key is derived, so switching is a recompute; two titles it merges (`Ｃａｆé`, `Café`) become a redirect | A rebuilt `title_key` differs from the stored one after a Unicode upgrade, or a look-alike duplicate page appears |
| Unicode collation for titles (ICU / app-registered) | A collation only one program registers makes the DB unwritable and un-integrity-checkable for everyone else; the app-computed `title_key` gives the same uniqueness (D5) | Never, unless SQLite ships Unicode folding in the core |
| Hard deletes / GDPR-style erasure | Tombstones keep everything (D11) | A legal/privacy need to truly destroy specific rows |
| Transaction ledger (income, spending, transfers), budgets, categories | A second product (splits, transfers, importers, categorisation); net worth needs only balances (D18). Additive later: `transactions` referencing `holdings`, `balances` as reconciliation points | The owner wants spending/savings-rate analysis, or a bank-feed importer exists |
| Per-holding quantity × price, cost basis / lots, dividends, returns (TWR/IRR) | Net worth needs the market *value* on a day, which the statement or app gives; quantity × price needs a `prices` table and non-currency units — the schema cannot hold a ticker (executed: `V`, `ZM`, `BRK.B` fail `currencies_code`). Additive later: a `securities` + `prices` pair and a nullable `holdings.security_id` | The owner wants automatic repricing, allocation by security or realised/unrealised gain |
| Currency conversion: exchange rates, one net-worth figure across currencies | Net worth is reported per currency (D18); with one currency it is already one figure | Holdings in more than one currency and you want a single total. Additive: an `fx_rates` table `(from_ccy, to_ccy, day, rate)` referencing `currencies`, and the conversion join in §6.15–6.16 |
| Co-ownership / shares of joint holdings, multiple owners | Single-user database; record your own share (D18) | A second person needs their own view |
| Storing account numbers, IBANs, credentials | A plaintext DB makes them a liability (§2.7, D17) | Never in `life.db`; use a password manager |
| Partial dates (`1870`, `1870-05`) for people | Nothing asked for one yet. The standard for them is EDTF, now ISO 8601-2 [R70]. The path is one migration: `DROP CONSTRAINT people_birth_day` and `ADD CONSTRAINT people_birth_day` with a CHECK that also accepts the EDTF forms wanted — executed for `YYYY` and `YYYY-MM` on a populated table; junk and month 13 stay rejected. Queries that do date arithmetic on `birth_day` must then skip partial values | The first ancestor or approximate date you want to record |
| Searching *inside* a CJK run | `unicode61`, kept, folds `é ü ș ț` but a CJK run is one token (`本語` does not find `日本語のノート`). The index is derived, so switching is one transaction — drop `pages_fts`, create it with `tokenize='trigram remove_diacritics 1'`, `rebuild` — and the sync triggers keep working (executed). Trigram finds 3+-character parts and still not two-character words (`京都`) | The first real CJK page you cannot find |
| A second name column for places and holdings | The page title is the name (D20); a separate `name` had to be unique a second time | A place or holding whose display name must differ from its permanent handle |
| Altitude, speed, heading, activity type on a fix | The question is where, not how (D21); the sources can record altitude, but no question needs it. Additive: nullable columns on `positions` | A question needs one |
| Correcting or retracting a fix | A fix is raw sensor output; a doubtful one is left out by `accuracy_m` (D21). Additive: a `retracted_positions(position_id)` table the reads exclude | A wrong fix that the accuracy filter keeps must be hidden |
| A place's extent (radius or polygon) | The nearest point within a bound radius answers "which place" (§6.20). Additive: a nullable `places.radius_m` | Matching picks the wrong one of two close places |
| Places across the ±180° meridian | The §6.20 distance treats 179.9° and −179.9° as 360° apart (D21) | A place or a trip near the antimeridian (Fiji, Chukotka) |
| A spatial index (R\*Tree) | `positions_time` and `positions_day` serve the questions asked; R\*Tree is a compile-time option [R73] | A query by area over the whole track is ever slow |
| Re-checking links when an entity changes type | `links_endpoint_types` checks a link at insert; a promotion (D20) can leave one its kind now refuses — a task spawned from the page [[Ana]], then Ana becomes a person (executed). Additive: a trigger on `UPDATE OF type ON entities` | The first such link found in real data |
| Events: appointments, trips, sessions, where you lived (a table of dated happenings) | A day page and its `[[links]]` record what happened, with whom and where (D22, §6.3); design kept in D22 | A source of dated spans or timed sessions (phone sleep and exercise, a calendar, a location export's visits), or a question the day pages cannot answer |

---

## 8. References

What each source contributed to the decisions above. Reference numbers are identifiers, not a count:
the gaps are intentional.

### SQLite durability, format, and features

- **[R1]** SQLite: *The SQLite Application File Format* (essay) —
  <https://www.sqlite.org/appfileformat.html>
  "Data lives longer than code"; the SQL schema as human-readable documentation of the
  format; additive schema change as the compatibility mechanism; Application ID. → D1,
  D2, D13.
- **[R2]** SQLite: *35% Faster Than the Filesystem* — internal-vs-external BLOB
  benchmarks — <https://www.sqlite.org/intern-v-extern-blob.html>
  Blobs < ~100 KB faster in-DB; larger blobs faster as files; page-size guidance. → D9.
- **[R3]** SQLite: *Faster Than The Filesystem* — <https://www.sqlite.org/fasterthanfs.html>
  Companion benchmark (10 KB blobs ~35% faster in DB, 20% less space). → D9.
- **[R4]** SQLite: *AUTOINCREMENT* — <https://www.sqlite.org/autoinc.html>
  `INTEGER PRIMARY KEY` = rowid alias; AUTOINCREMENT overhead, "usually not needed". → D3.
- **[R5]** SQLite: *Date And Time Functions* — <https://www.sqlite.org/lang_datefunc.html>
  `strftime('%Y-%m-%dT%H:%M:%fZ','now')`; 'now' is UTC. → §2.1, D10.
- **[R6]** SQLite: *Datatypes* — <https://www.sqlite.org/datatype3.html>
  Dates as TEXT ISO-8601 is the canonical representation. → D10.
- **[R10]** SQLite: *SQLite as a Library of Congress Recommended Storage Format* —
  <https://www.sqlite.org/locrsf.html> → D1.
- **[R11]** US Library of Congress: *Recommended Formats Statement — Data* —
  <https://www.loc.gov/preservation/resources/rfs/data.html>
  SQLite listed as a recommended storage format for datasets. → D1.
- **[R12]** SQLite: *SQLite Database File Format* (compatibility history) —
  <https://www.sqlite.org/formatchng.html>
  Backwards-compatible since 2004. → D1.
- **[R13]** SQLite: *Long Term Support* — <https://www.sqlite.org/lts.html>
  Commitment to support existing files through 2050. → D1.
- **[R14]** SQLite: *STRICT Tables* — <https://www.sqlite.org/stricttables.html>
  Per-table static typing since 3.37.0 (2021). → D2.
- **[R28]** allenap: *ISO-8601 and DATETIME in SQLite* —
  <https://allenap.me/posts/iso-8601-and-datetime-in-sqlite>
  DATETIME comparisons degrade to text; format discipline matters. → D10. (See also Igor
  Bubelov, *Don't Trust SQLite Timestamps*, <https://bubelov.com/blog/2020/sqlite-timestamps/>.)

### Prior art: real long-lived personal databases

- **[R9]** KrauseFx/FxLifeSheet — repository, `db/create_tables.sql` —
  <https://github.com/KrauseFx/FxLifeSheet>
  Verified actual schema: single `raw_data` table (unix `timestamp`, denormalized
  `yearmonth/yearweek/year/quarter/month/day/hour/minute`, `key`, `question`, `type`,
  `value` TEXT, timezone-corrected `matcheddate`, `source`, `importedat`, `importid`).
  380k data points, 6+ years. → D5, D7, D10.
- **[R42]** Felix Krause: *How I put my whole life into a single database* —
  <https://krausefx.com/blog/how-i-put-my-whole-life-into-a-single-database>
  Single self-owned DB; questions added/removed freely via config registry. → D5, D7.
- **[R7]** FxLifeSheet `tag_days` importer —
  <https://github.com/KrauseFx/FxLifeSheet/tree/master/ruby_importers/importers/tag_days>
  Local-date tagging with the timezone actually occupied — evidence that reconstructing
  local days after the fact is painful enough to need dedicated tooling. → D10.
- **[R43]** Connor Gallic: *Open Brane — annotated, 8 columns, 80-line write path, one
  SQLite file* — <https://dev.to/connor_gallic/open-brane-annotated-8-columns-80-line-write-path-one-sqlite-file-43n3>
  Append-only single table (ts, source, type, actor, payload_json, attachment_uri,
  ingested_at); idempotent writes via `INSERT OR IGNORE`; 942k rows / 3 GB; blobs
  outside the DB. → D7, D8, D9.
- **[R46]** Jamie Rubin: *ark, part 3* — <https://jamierubin.net/2026/06/09/ark-part-3/>
  Content-addressed flat store (sha256-named files, dedup for free) with SQLite as the
  index; typed edge tables doc↔doc, doc↔person, person↔person; the person-graph as the
  killer feature. → D8, D9.
- **[R8]** health-mcp: *DATA_MODEL.md* —
  <https://github.com/lukaisailovic/health-mcp/blob/main/docs/DATA_MODEL.md>
  Biomarkers with LOINC/UCUM/reference ranges (studied, then simplified away — D7);
  two-tier wearables (raw mirror + normalized — deferred); UTC ISO timestamps **plus
  denormalized local `YYYY-MM-DD`**; numbered forward-only migrations. → D7, D10, D13.
- **[R45]** Myome design paper — <https://scanlin.io/myome/paper.html>
  ~50 MB/year sensor data, ~5 GB/lifetime: scale is a non-issue; multi-resolution
  aggregation deferred. → D7, §7.
- **[R41]** mirat.dev: *Nine Years of Kaydet* —
  <https://mirat.dev/articles/nine-years-of-kaydet/>
  Plain-text entries + SQLite index; "plain text + SQLite beat every cloud note app"
  — 9 years of daily use. → D4, D5.

### The files-vs-database debate (D4 evidence)

- **[R34]** Logseq forum: *Why the database version and how it's going?* —
  <https://discuss.logseq.com/t/why-the-database-version-and-how-its-going/26744>
  Stated limits of markdown-canonical: block creation rewrites whole files; page rename
  updates all referencing files; files lack persistent IDs and timestamps. SQLite (via
  sqlite-wasm) chosen for the DB version.
- **[R35]** Logseq: *Big update — Logseq is splitting into two versions* —
  <https://logseq.io/page/b2ad9ce1-9cb7-4436-8083-54cb4516d324/df4dc09d-0a12-4c87-904e-22a9bf4c350a>
  Logseq OG (markdown files canonical) vs Logseq DB (SQLite canonical): the industry
  literally forked over this decision.
- **[R36]** *Logseq DB Unofficial FAQ* —
  <https://logseq.io/page/e87c7359-51f7-44fe-87b3-4a0cd9f2dee3/695feeec-88be-4c5b-8bf2-572513c2f730>
  In the DB version the database is canonical.
- **[R29]** morganholland/logseq-to-obsidian-migration —
  <https://github.com/morganholland/logseq-to-obsidian-migration>
  Dedicated tooling required between two "plain markdown" apps: journal filename
  conversion (`YYYY_MM_DD` → `YYYY-MM-DD`), frontmatter rewriting, property stripping,
  block-reference conversion, URL-decoded filenames.
- **[R30]** laughedelic/outbreak (Obsidian↔Logseq converter) —
  <https://github.com/laughedelic/outbreak>
  Syntax translation of tasks, highlights, wiki-links, embeds, callouts, frontmatter.
- **[R31]** msfjarvis: *The Obsidian Migration — One Week Later* —
  <https://msfjarvis.dev/posts/the-obsidian-migration--one-week-later/>
  First-person account of wikilink/tag/journal-date incompatibilities.
- **[R32]** laughedelic/obsidian-importer: Logseq assessment —
  <https://github.com/laughedelic/obsidian-importer/blob/feat/logseq-importer/docs/logseq-importer-assessment.md>
  Catalogue of Logseq-specific constructs an importer must translate.
### Schema-design evidence (D2, D3)

- **[R15]** Anton Zhiyanov: *JSON and virtual columns in SQLite* —
  <https://antonz.org/json-virtual-columns/>
  `json_extract()` parses text on every call; generated columns needed to make it fast.
- **[R16]** DelphiTools: *SQLite as a no-SQL database* —
  <https://www.delphitools.info/2021/06/17/sqlite-as-a-no-sql-database/>
  Measured: 169 ms unindexed scan vs 0.15 ms indexed column lookup; JSON paths worse.
- **[R17]** Simon Willison: *sqlite-tags-benchmark* —
  <https://github.com/simonw/research/tree/main/sqlite-tags-benchmark>
  Five strategies benchmarked on 100k rows: indexed relational fastest; `json_each()`
  scans much slower; FTS5 a close second (supports D14's search choice).
- **[R18]** Jason Graczyk (NWOS): *Postgres JSON Columns vs Proper Schemas* —
  <https://nwos.com/daily/postgres-json-columns-when-theyre-a-lifesaver-and-when-theyre-a-trap>
  Practitioner pattern: the "flexible" JSON column becomes "an unmapped wasteland of
  inconsistent keys" within ~6 months.
- **[R19]** Cybertec: *entity-attribute-value design — don't do it!* —
  <https://www.cybertec-postgresql.com/en/entity-attribute-value-eav-design-in-postgresql-dont-do-it/>
  EAV as documented anti-pattern once the attribute set is fixed. (See also: SQLBlog
  *What is so bad about EAV, anyway?*; cedanet.com.au EAV anti-pattern page; Red Gate
  *Avoiding the EAV of Destruction*.)
- **[R26]** lik.ai: *SQLite Primary Key Benchmarks (UUIDv7, UUIDv4, Snowflake, Integer)* —
  <https://lik.ai/blog/sqlite-primary-key-benchmarks/>
  Integer fastest overall; UUID variants close for query, worse for insert; UUIDv4 vs v7
  difference minimal. (Same benchmark: <https://engineered.at/articles/sqlite-primary-key-benchmarks-uuidv7-uuidv4-snowflake-integer>.)
- **[R27]** Production Hardening: *Integer Primary Keys for Embedded Writes* —
  <https://www.productionhardening.org/sqlite-architecture-production-hardening/schema-design-for-edge-devices/integer-primary-keys-for-embedded-writes/>
  Random TEXT UUID keys scatter inserts across the B-tree (page splits, dirty-block
  churn). → D3.
- **[R75]** vlcn.io: *cr-sqlite — Constraints* — <https://vlcn.io/docs/cr-sqlite/constraints> (code:
  <https://github.com/vlcn-io/cr-sqlite>, last release v0.16.3, January 2024) A merged ("CRR") table
  may not have checked foreign keys, unique constraints other than the primary key, or CHECKs that
  depend on other columns. → D3, §7.
- **[R47]** Microsoft Research TR-2006-45: *To BLOB or Not To BLOB* —
  <https://www.microsoft.com/en-us/research/wp-content/uploads/2006/04/tr-2006-45.pdf>
  Classic study; break-even a few hundred KB — small in DB, large on filesystem. → D9.

### Operations: migrations, audit (D12, D13)

- **[R20]** Ash: *Simple Migration System in SQLite* —
  <https://www.ash.dev/blog/simple-migration-system-in-sqlite/>
  `PRAGMA user_version` as the migration counter; no framework.
- **[R21]** Nhân: *Working with SQLite in Python without an ORM or migration framework* —
  <https://hi.imnhan.com/sqlite-python/>
  `mXXXX.sql` + user_version in < 100 lines.
- **[R22]** David Röthlisberger: *Simple declarative schema migration for SQLite* —
  <https://david.rothlis.net/declarative-schema-migration-for-sqlite/>
  Schema-in-one-file, auto-applied additions; the declarative extreme of the same idea.
- **[R48]** Simon Willison: *sqlite-history — tracking changes to SQLite tables using
  triggers* — <https://simonwillison.net/2023/Apr/15/sqlite-history/> (code:
  <https://github.com/simonw/sqlite-history>)
  The documented fallback audit mechanism if D12 ever proves insufficient.

### UI tooling (D14)

- **[R49]** Datasette — <https://datasette.io/> (code:
  <https://github.com/simonw/datasette>)
  Instant browsing/SQL/faceting/JSON-CSV export over any SQLite file; Datasette Lite runs
  in-browser.
- **[R50]** sqlite-web — <https://github.com/coleifer/sqlite-web> (**not used** — it edits rows, i.e. is a second writer; it does have `-r/--read-only`. D14)
  Web-based table browser with row insert/update/delete, CSV/JSON import-export.

### SQLite behaviour and money (D18)

- **[R53]** SQLite: *ON CONFLICT clause* — <https://www.sqlite.org/lang_conflict.html>
  "When the REPLACE conflict resolution strategy deletes rows in order to satisfy a
  constraint, delete triggers fire if and only if recursive triggers are enabled." The reason
  `PRAGMA recursive_triggers = ON` is mandatory (§2.6). (The page's wording on which
  constraints `IGNORE` skips is loose; what it does was executed.) → D18, §2.3, §2.6.
- **[R54]** SQLite: *PRAGMA statements* — <https://www.sqlite.org/pragma.html>
  `synchronous=NORMAL` in WAL mode: "A transaction committed in WAL mode with
  synchronous=NORMAL might roll back following a power loss or system crash";
  `recursive_triggers` is a per-connection setting, off by default. → §2.6, D12.
- **[R55]** SQLite: *ALTER TABLE* — <https://www.sqlite.org/lang_altertable.html>
  With the release notes, <https://www.sqlite.org/changes.html>: since 3.53.0 (2026-04-09)
  `ALTER TABLE` can add and remove NOT NULL and CHECK constraints. Its restriction to *named* constraints,
  and that `ADD CONSTRAINT` checks existing rows, were found by executing it on 3.53.4. → D13, D18.
- **[R56]** Beancount `balance` directive (via the `beancount_ex` library docs — the official
  syntax page URL tried returned 404): asserts an account's balance at the *beginning* of a
  date — the reason `balances.day` states "end of day" explicitly.
  <https://beancount-ex.hexdocs.pm/0.6.0/Beancount.Directives.Balance.html>. → D18.
- **[R57]** ISO 4217 (as tabulated at <https://en.wikipedia.org/wiki/ISO_4217>): the minor-unit
  exponent is a per-currency fact (JPY 0, KWD 3), and a few currencies (MRU, MGA) subdivide
  by 5 — why `currencies.subunits` is a per-row integer and not a global "cents" assumption. → D18.

### The wikilink save contract (D19)

- **[R58]** Microsoft Learn: *Naming Files, Paths, and Namespaces* —
  <https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file>
  Reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, and the superscript
  digits `COM¹ COM² COM³ LPT¹ LPT² LPT³`): "avoid these names followed immediately by an extension; for
  example, NUL.txt and NUL.tar.gz are both equivalent to NUL"; no trailing space or period. The
  DDL rejects the bare names, the names before an extension and the superscript names. → §2.4, D5.
- **[R59]** markdown-it-py 4.2.0, the Python port of markdown-it, a CommonMark-compliant parser —
  <https://github.com/executablebooks/markdown-it-py>; the specification it implements is
  <https://spec.commonmark.org/>. Used as the reference reader in `tests/wikilinks`: which text it hands
  back (code spans, fences, indented code, raw HTML and image alt text are not text; escapes and
  entities are decoded; `#Heading` without a space is not a heading) was executed, not read from
  the spec. → §2.4, D19.
- **[R63]** Unicode Technical Standard #39, *Unicode Security Mechanisms* —
  <https://www.unicode.org/reports/tr39/> (with UAX #31, *Identifiers*): default-ignorable and bidi
  characters are dropped or rejected before identifiers are compared, because they are invisible.
  → §2.4 (the invisible-character rule).
- **[R74]** Unicode: *Character Encoding Stability Policies* —
  <https://www.unicode.org/policies/stability_policy.html> Case folding stability (Unicode 5.2+): for a
  string of assigned characters, `toCasefold(toNFKC(S))` is the same under every later version — the
  formal promise covers NFKC text only. Normalization stability covers assigned characters. → §2.4 (`Cn`), §7.

### Reliability of the file (§2.5, §2.6, §2.8)

- **[R64]** SQLite: *The Checksum VFS Shim* — <https://www.sqlite.org/cksumvfs.html>
  An 8-byte checksum per page (reserve bytes = 8), `SQLITE_IOERR_DATA` on a mismatch; SQLite ≥ 3.32.
  Considered for in-value damage and not used (an extension in every writer); a checksumming
  filesystem does the same job below the file. → §2.5.
- **[R65]** SQLite: *Write-Ahead Logging* — <https://www.sqlite.org/wal.html>
  The WAL-reset bug (3.7.0 – 3.51.2, fixed in 3.51.3 and 3.53.0; backports 3.44.6 and 3.50.7): two or more
  connections, a write racing a checkpoint, a lost transaction. Also: checkpoint starvation by
  readers that never let go, "WAL does not work over a network filesystem", the conditions for
  read-only access. → §2.6, `lifelog_meta.sqlite`.
- **[R66]** SQLite: *How To Corrupt An SQLite Database File* —
  <https://www.sqlite.org/howtocorrupt.html> Network filesystems, files copied while open, broken
  POSIX locks, `immutable` on a changing file. → §2.6.
- **[R67]** T. S. Pillai et al., *All File Systems Are Not Created Equal: On the Complexity of
  Crafting Crash-Consistent Applications*, OSDI 2014 —
  <https://www.usenix.org/conference/osdi14/technical-sessions/presentation/pillai>
  SQLite among the studied applications: crash consistency depends on the filesystem's persistence
  properties, which is why power loss is listed as documented, not simulated. → §2.8.

### Time and dates (D7, D10, D18, §7)

- **[R68]** R. T. Snodgrass, *Developing Time-Oriented Database Applications in SQL*, Morgan
  Kaufmann 2000; SQL:2011's application-time and system-time periods are the same two clocks. Valid
  time vs transaction time — the two clocks of `measurements` and `balances`. → D7, D18.
- **[R69]** RFC 9557, *Date and Time on the Internet: Timestamps with Additional Information*
  (2024) — <https://datatracker.ietf.org/doc/html/rfc9557> An IANA zone in brackets after an RFC 3339
  instant. → D10.
- **[R70]** Library of Congress, *Extended Date/Time Format (EDTF)*, incorporated in ISO 8601-2:2019 —
  <https://www.loc.gov/standards/datetime/> Year and month precision, approximate (`~`) and uncertain
  (`?`) dates. → §7 (partial dates).

### Recurrence (D15)

- **[R51]** Stack Overflow: *Should I store dates or recurrence rules in my database when
  building a calendar app?* —
  <https://stackoverflow.com/questions/4239871/should-i-store-dates-or-recurrence-rules-in-my-database-when-building-a-calendar>
  The canonical store-the-rule-vs-materialize-the-instances discussion; the accepted
  answer separates "canonical" (the rule) from "serving" (generated dates) — exactly
  the template/expand-at-read split of D15's deferred design.
- **[R52]** Microsoft Learn: *dbo.sysschedules (Transact-SQL)* —
  <https://learn.microsoft.com/en-us/sql/relational-databases/system-tables/dbo-sysschedules-transact-sql>
  The structured-interval family in production since SQL Server 7: `freq_type` /
  `freq_interval` / `freq_recurrence_factor` / `freq_relative_interval` — the shape
  `repeat` / `repeat_weekdays` / `repeat_every` of D15's deferred design follows.

### Location (D21)

- **[R71]** RFC 7946, *The GeoJSON Format* (2016) — <https://datatracker.ietf.org/doc/html/rfc7946>
  Positions are WGS84 longitude and latitude in decimal degrees; geometry that crosses the
  antimeridian is cut in two (§3.1.9). → D21.
- **[R72]** SQLite: *Built-in Mathematical SQL Functions* — <https://sqlite.org/lang_mathfunc.html>
  `sin`, `cos`, `acos`, `radians` are active only in builds compiled with
  `-DSQLITE_ENABLE_MATH_FUNCTIONS`. → D21, §6.20.
- **[R73]** SQLite: *The SQLite R\*Tree Module* — <https://sqlite.org/rtree.html> A spatial index as a
  virtual table, present only in builds compiled with `SQLITE_ENABLE_RTREE`. → §7.

---

## Appendix A — Prior-art survey summary

The seven real systems surveyed during research, and exactly what was taken from each:

| System | Scale / longevity | Shape | Taken into this design | Rejected from this design |
|---|---|---|---|---|
| FxLifeSheet [R9][R42] | 380k points, 6+ yrs | One `raw_data` table; metric registry in config | measurements shape, `import_key` idempotency, denormalized time buckets (`day`), capture-friction philosophy | value-as-TEXT (we use REAL), Postgres, 8 separate time-bucket columns (one `day` suffices) |
| ark [R46] | 700k items, 125 GB store + 9 GB SQLite | Content-addressed files; SQLite index; typed edges | `media/` sha256 store, `links` as the one graph, "everything about a person" query | annotations layer, classification/quality subsystems |
| Open Brane [R43] | 942k rows, 3 GB | One append-only 8-column table; no FKs | append-only spirit for measurements, keyed idempotent writes (as `ON CONFLICT … DO NOTHING`), blobs-outside-DB | payload_json column (violates D2), no-FK design (violates D8), `INSERT OR IGNORE` (it also swallows CHECK and NOT NULL violations, §2.3) |
| health-mcp [R8] | Years of use | Typed biomarker tables; two-tier wearables; forward-only migrations | UTC+local-day convention, forward-only numbered migrations, metric registry concept | LOINC/UCUM/ref-ranges, raw mirror tier (both deferred, §7) |
| Myome [R45] | Design paper | TSDB + SQLite + object store | Scale calibration (~5 GB/lifetime → no rollups needed) | TSDB, FHIR machinery, multi-resolution storage |
| Kaydet [R41] | 9 yrs daily entries | Plain text + SQLite index | Evidence that boring survives; hybrid text+DB instinct (resolved as D4: the database is canonical) | Files-as-canonical (owner's writer-drift objection) |
| Logseq OG vs DB [R34]–[R36] | Product-scale split | Files-canonical vs SQLite-canonical | The decisive precedent for D4: a team that hit live-editing limits chose DB-canonical | Block-level datom model, collaboration machinery |
