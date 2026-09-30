# Lifelog — Database Schema v1

**Status:** v1.13 — frozen pending external review (review rounds 3, 3b, 3c applied; round 4 = an
independent review plus the finance tier D18; rounds 5–10 = its fixes: mechanical items, time zone / `completed_day` / one place per event, the text inconsistencies plus durability and write transactions, the wikilink save contract, and the last open items with a repeatable test suite in `tests/` — **no recorded finding is open**; what remains is two gates only the owner can close, the external review and the first real import, §8 round 10; round 11 then merged notes and wiki pages into one `page` kind, D5 addendum 5; round 12 narrowed the document to the schema and its reliability and withdrew the markdown export and the snapshot / restore / dump contract, §7, §8 #14). Not yet applied to any canonical database.
**Date:** 2026-09-30 (v1.0–v1.4: 2026-09-29; v1.5–v1.13: 2026-09-30)
**Scope of the project:** A lifetime personal database (journal/memos, pages (notes, wiki), events,
tasks, people, health metrics, personal finance — accounts, balances, net worth; file
attachments deferred — D9) in a single SQLite file,
plus a custom UI for data entry and daily use. Everything else (view generators, AI
features, sync, multi-device) is explicitly out of scope.

This document is self-contained: it records the full schema (DDL), every design decision
with its rationale, the alternatives that were considered and rejected, the conventions
that writers of the schema must follow, a query cookbook proving the schema serves its
use cases, and the sources the decisions are based on. It is written to be reviewed cold,
without access to the conversation that produced it.

---

## Table of contents

1. [Goals and design principles](#1-goals-and-design-principles)
2. [Storage contract (conventions)](#2-storage-contract-conventions)
3. [The schema (canonical DDL)](#3-the-schema-canonical-ddl)
4. [Entity model overview](#4-entity-model-overview)
5. [Decision log](#5-decision-log)
6. [Query cookbook](#6-query-cookbook)
7. [Explicit non-goals and deferred work](#7-explicit-non-goals-and-deferred-work)
8. [Validation records and review resolutions](#8-validation-records-and-review-resolutions)
9. [References](#9-references)

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
   silently re-added.
2. **The schema is the documentation.** Real tables, real column names, real types, real
   constraints. A stranger in 2075 should understand the database from
   `.schema` output alone. (SQLite's own "application file format" essay makes exactly
   this argument — see [R1].)
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

---

## 2. Storage contract (conventions)

These conventions are part of the schema's meaning. They are also embedded as comments in
the init DDL (§3; after the freeze, `0001_init.sql`).

### 2.1 Directory layout

```
life/
└── life.db                  # canonical: all structured data + all prose
```

`life.db`, `life.db-wal` and `life.db-shm` are never committed to git: binary churn, and git
history cannot be scrubbed of finance data (§2.10). Binary files (`media/`) are deliberately
deferred out of v1 (D9) — the layout grows a `media/` sibling when they return.

### 2.2 Time

- **Instants** (`*_at` columns): UTC, ISO-8601, millisecond precision,
  e.g. `2026-06-09T21:14:03.482Z`. Written by the app, never by SQLite defaults
  (SQLite's `CURRENT_TIMESTAMP` is second-precision and non-ISO; `strftime('%Y-%m-%dT%H:%M:%fZ','now')`
  is the in-DB form used by triggers) [R5][R6][R28].
- **Local days** (`*_day` columns): the *local calendar date where the thing happened or
  was captured*, TEXT `YYYY-MM-DD`, written at insert time from the writer's timezone.
  **Never derived from the UTC instant at query time.** This survives timezone changes,
  DST, and travel: "the day I graduated" is a local-date fact, not an instant [R7][R8].
  The pattern (UTC instant + denormalized local day) is taken from health-mcp [R8] and
  FxLifeSheet's `matcheddate` column [R9].
- Format and calendar validity are enforced in-DB with round-trip CHECKs:
  `date(x) IS x` for days, `strftime('%Y-%m-%dT%H:%M:%fZ', x) IS x` for instants.
  The `IS` operator, not `=`, matters: a CHECK passes when it evaluates to NULL, and
  `date()` returns NULL for malformed input, so `date(x) = x` silently *accepts*
  garbage like `2026-9-3` — empirically confirmed and fixed (see §8).
- **`entities.created_at` is when the row was written to `life.db`** — never back-dated, so it
  can serve as an audit trail (as `recorded_at` does on `measurements` and `balances`). When a
  thing *happened* is its own `day` / `*_at`. (An imported memo's original time of day has no
  column yet — open item R4-04, §8.)
- **Time zone.** `entities.tz` and `measurements.tz` store the writer's IANA zone at capture
  (`Europe/Berlin`; NULL = unknown), so a UTC instant can be read as local time. Only capture time
  can supply it — it cannot be reconstructed later. Events have no `tz` of their own (D10 addendum).
- An event may be day-precise only (`start_day`, `end_day`, no `*_at`). Date-level facts
  are first-class in a biography database.

### 2.3 Identity

- Every entity, fact and join table uses `INTEGER PRIMARY KEY` (rowid alias; no
  `AUTOINCREMENT` — the keyword adds overhead and is "usually not needed" [R4]). The four
  registries and reference tables keep their natural key instead: `lifelog_meta(key)`,
  `link_kinds(kind)`, `currencies(code)`, `fx_rates(from_ccy, to_ccy, day)`.
- The six *entity* types (`page`, `event`, `task`, `person`, `place`, `account`) share one ID
  space via the `entities` supertype table (D8, D16, D18). A domain row's `id` **equals** its
  `entities.id`; the app inserts the `entities` row first and reuses
  `last_insert_rowid()` in the same transaction (see §6.1). `UNIQUE(id, type)` on
  `entities` plus a composite FK in every domain table make the type↔table pairing
  structural, not conventional (verified — §8).
- `measurements`, `balances`, `metrics`, `currencies`, `fx_rates`, `links`, `link_kinds`,
  `lifelog_meta` are *not* entities (they are facts, joins, and registries). An `account` is an
  entity; its balances are facts.

### 2.4 Deletion

- **No hard deletes of entities.** Deletion sets `entities.deleted_at` (tombstone) — and since
  round 5 that is *enforced*: `BEFORE DELETE` triggers reject deleting an `entities` row or any
  domain row (`links` are the one hard-deleted table, D11). Every read path filters
  `deleted_at IS NULL`. In 20 years it should be possible to know what was erased and when (D11).
- Junk captured by accident (duplicate import, mis-tap) is tombstoned like everything
  else; the storage cost is irrelevant at this scale.
- `measurements` are **append-only and never deleted — enforced by triggers**, not
  convention: `UPDATE` and `DELETE` are rejected. Corrections insert a new row with
  `supersedes_id` pointing at the corrected row, at most one correction per row (D7). A
  correction whose `value` is NULL **retracts** the row it corrects (a mis-tap, a wrong metric).
  Importers use `INSERT … ON CONFLICT(source, import_id, metric_id) WHERE import_id IS NOT NULL
  DO NOTHING` — never `INSERT OR IGNORE` (it silently skips rows that violate a CHECK or NOT
  NULL) and never `OR REPLACE` (a delete, blocked only when `recursive_triggers=ON`, §2.9).
- `balances` are append-only in the same way: `UPDATE` and `DELETE` are rejected; a wrong
  value is corrected by inserting a newer row for the same `(account_id, day)`, and an entry
  that should never have existed is retracted with a NULL `amount` (D18).

### 2.5 Prose, wikilinks, and renames

- `pages.body` is CommonMark text. Wiki references are written inline as `[[Page Title]]`.
  A `#tag` is read as `[[tag]]`, so tags are just pages (D5). The app never rewrites
  the body: `#health` stays `#health` in the database.
- **The save contract (D19).** Saving a page body is one `BEGIN IMMEDIATE` transaction
  (§6.14): the body, then the page's `links(kind='wikilink')` rows, made **equal to the set of
  pages the body names** — missing rows added, rows the body no longer supports deleted — so a
  re-save changes nothing and every link can be rebuilt from the bodies alone. The `links`
  table is the source of truth for the graph; the body text is the source of truth for prose.
  Backlinks = `links WHERE to_id = ?`. The rules, all executed in §8 #10 against the vectors
  below:
  - *What is read.* The CommonMark **text** of `pages.body`, after NFC normalisation — not code
    spans, code blocks, raw HTML, link destinations or image alt text. Any CommonMark parser
    yields exactly this (tested with markdown-it-py [R59]), so nothing is hand-parsed. Only
    `pages.body` is read: tasks, events and people carry no wikilinks.
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
  - *An invalid target makes no link and never blocks a save.* A title the filename rules below
    reject (`[[Health/Diet]]`, `[[Re: plan]]`, the tag `#con`) is skipped. The app checks those
    rules before inserting — its predicate agreed with the DDL's own CHECKs on 43 361 strings —
    and creates each target inside its own `SAVEPOINT` (§6.14), so even a target the predicate
    wrongly let through is rolled back alone: the memo is saved and no orphan `entities` row is
    left. The UI reports skipped targets; nothing is stored about them — the text stays in the
    body, and the next save links it once it is valid.
  - *A page never links to itself* (`[[Diet]]` inside the page `Diet` is ignored), and *a
    tombstoned target is revived*, not duplicated (Lookups, below).
  - *Known limits.* A body that also defines a reference (`[Ref]: http://r`) turns `[[Ref]]` into
    a Markdown link, so it is not a wikilink; a `#` written as an entity (`&#35;x`) is decoded
    before the scan and counts as a tag; a wikilink resolves to a **page** only, so `[[Sam]]`
    never reaches the `people` row (R4-11 e, still open).

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
  | `[[Café]] [[CAFÉ]] [[Café]] [[cafe]]` | `Café`, `cafe` |
  | `[[Café notes]]` | `Café notes` |
  | `\[[escaped]]` | `escaped` |
  | `` text `[[code]]` text `` | — |
  | `a\n\n~~~\n[[fence]]\n~~~\n\nb` | — |
  | `[[Diet]](http://y)` | — |
  | `[[Health *Diet*]]` | — |
  | `[[Ref]]\n\n[Ref]: http://r` | — |
  | `[[CON]] [[nul]] [[Com1]] [[CONSOLE]] [[COM10]] [[LPT0]]` | `CONSOLE`, `COM10`, `LPT0` |
  | `[[CON.backup]] [[nul.txt]] [[COM\u00b9]] [[LPT\u00b2.x]] [[a.CON]] [[CONSOLE.txt]]` | `a.CON`, `CONSOLE.txt` |
  | `Feeling good #health today` | `health` |
  | `#Health and #health and #HEALTH` | `Health` |
  | `#tag. #tag2, (#paren) "#quoted" #end-` | `tag`, `tag2`, `paren`, `quoted`, `end` |
  | `#café #zürich` | `café`, `zürich` |
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
- **Renames are forbidden** (D5): renaming would silently repoint every `[[Old Title]]`
  in decades of prose, or leave ghosts if it didn't. Instead: create the new page, make
  the old page a one-line stub (`#REDIRECT [[New Title]]`), and add
  `links(kind='redirect', from=old, to=new)`. Consumers follow one hop; `redirect` links
  are excluded from backlink queries (§6.5), and the stub's own body is not read for
  wikilinks (contract above). Orphaned empty stubs are surfaced by the
  `ghost_pages` view for occasional sweeps (§6.13).
- **Titles are file-name-safe names — the schema keeps them safe, unique and permanent.**
  - *Safe.* A page title must be usable as a file name anywhere, so the DDL
    rejects titles that are unsafe as a filename on Linux, macOS and Windows: path
    separators and Windows-reserved characters (`/ \ : * ? " < > |`), control
    characters (incl. NUL), a leading or trailing `.` (hidden files, `..`, Windows), a
    Windows device name (`CON`, `NUL`, `COM1`…, and the superscript `COM¹ COM² COM³ LPT¹ LPT² LPT³`)
    **bare or before an extension** — `CON.backup` and `NUL.txt` are the device too [R58] — and
    leading/trailing spaces. Titles are 1–240 **bytes** (a filename limit is 255
    bytes, with headroom for an extension). `[[Health/Diet]]` and `[[Re: plan]]` are therefore not valid
    page names — use `[[Health - Diet]]` — and a wikilink to one makes no link (contract
    above). Memos have no title. Checked against the Windows documentation only (Windows itself
    is not executable here); a name with a space before its dot (`CON .txt`) is not covered
    because the documentation does not say it is reserved. The rule is the strict one on
    purpose: the title CHECKs are unnamed, so loosening one after the freeze is a table rebuild
    (D5 addenda 4 and 6).
  - *Permanent.* A title never changes (`pages_title_fixed`): a rename would silently
    repoint every `[[Old Title]]` in decades of prose (D5). To fix a title, create the
    new page and turn the old one into a `#REDIRECT` stub with `links(kind='redirect')`.
  - *Unique — on `title_key`, not on the title.* `title_key` is the title in normalised,
    case-folded form, **computed by the app** and stored beside it; `pages_title` is a
    `UNIQUE` index on it (every titled page, tombstoned rows included). So `Café`, `CAFÉ`, the
    decomposed `Cafe\u0301` and `Straße`/`STRASSE` are one page, on every filesystem.
    The function is fixed: `title_key = NFC(casefold(NFC(title)))` — in Python
    `unicodedata.normalize('NFC', unicodedata.normalize('NFC', t).casefold())`. A writer
    in another language must reproduce these vectors exactly:

    | title | `title_key` |
    |---|---|
    | `Café notes`, `Cafe\u0301 notes` (NFD), `CAFÉ NOTES` | `café notes` |
    | `Straße`, `STRASSE` | `strasse` |
    | `ΣΑΣ`, `σας` (final sigma) | `σασ` |
    | `Ǆ` | `ǆ` |
    | `ﬁle` (ligature) | `file` |
    | `İstanbul` | `i̇stanbul` (`i` + U+0307) |
    | `日本語 ノート`, `Diet` | `日本語 ノート`, `diet` |

    *Why a column and not a collation:* SQLite folds ASCII only (`NOCASE`), and a
    Unicode collation (ICU, or one the app registers) is rejected — a database whose
    index needs a collation only one program supplies can be read by anyone but **not
    written or integrity-checked** (`no such collation sequence`, verified with the
    `sqlite3` CLI) — see §7. *What the DB verifies:* a key exists iff there is a title;
    it is trimmed, non-empty and has no ASCII capitals; for a pure-ASCII title it must
    equal `lower(title)`. *What it cannot:* for a non-ASCII title, that the key is the
    *right* fold — a writer that computes it wrongly gets uniqueness wrong (and nothing
    else), so the key function lives in the one writing application (principle 3). The
    key is derived data: recompute it after changing the function; the unique index
    then reports any genuine collision at that moment.
  - *Lookups.* Resolve a `[[wikilink]]` with `WHERE title_key = :key`. The unique index is
    partial (`WHERE title_key IS NOT NULL`: memos have no key) and an equality on the key implies
    that predicate, so SQLite uses it (§6.14). The unique index covers tombstoned pages, so when a save resolves a title
    that belongs to a tombstoned page the app un-tombstones it rather than inserting a
    duplicate.

### 2.6 Files (deferred — see D9)

Binary files are cut from v1 (decision D9: `attachments` and `media/` deferred until the
first real photo/PDF need). The design — SHA-256 content-addressed `media/`, hash +
extension + mime + size in an `attachments` table, path derived never stored, dedup by
hash — is recorded in D9 so it is not reinvented. Reopen trigger: the first genuine
attachment need. Until then the database is all text and `life.db` stays megabyte-scale.

### 2.7 Migrations

- **Until the schema freeze there are no migrations.** §3 of this document is the single
  canonical init DDL, edited in place; test databases are created by applying §3 to a
  fresh file and thrown away (D13).
- After real data exists: numbered plain-SQL files, `db/migrations/0002_*.sql`, … applied
  in order; progress tracked with `PRAGMA user_version` (a 32-bit integer in the DB
  header — no bookkeeping table needed) [R20][R21][R22].
- No ORM, no migration framework. At single-user scale the runner is ~20 lines or, in the
  limit, `sqlite3 life.db < 000N_*.sql` by hand.
- The init DDL sets `PRAGMA application_id = 0x4C494645` (`'LIFE'`) so `file(1)` and
  future tools can recognize the database [R1].

### 2.8 Integrity checks

Three checks tell whether a file still obeys the schema. They read only the file, need no other
copy, and each catches what the others cannot. Run them before and after an import (§2.11) or a
migration (§2.7), and after any writer crashed. Every claim below was executed on the **live**
file (§8 #14), not on a copy.

```sql
PRAGMA integrity_check;      -- one row: ok
PRAGMA foreign_key_check;    -- no rows
SELECT id FROM entities WHERE id NOT IN (SELECT id FROM pages UNION SELECT id FROM events UNION SELECT id FROM tasks UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM accounts);   -- no rows
```

- **`integrity_check` — the file's structure.** It caught a zeroed table page, a file truncated by
  three pages, and an index entry that no longer matches its row (a flipped byte in a `title_key`
  inside `pages_title`: `row … missing from index pages_title`).
- **`foreign_key_check` — what the first cannot see.** A writer that forgot `PRAGMA foreign_keys=ON`
  (§2.9 — per connection) stored a balance for an account that does not exist, and
  `integrity_check` said `ok`.
- **The orphan query — the one check no constraint can express.** An `entities` row with no domain
  row (a writer that died between its two inserts; §8 round 5): both other checks are clean on it.
- **What none of them sees: a changed value.** A flipped byte inside a body passed
  `integrity_check` — SQLite keeps no page checksums — so damage inside a cell cannot be found from
  the file alone.

### 2.9 Connection setup (every writer, mandatory)

```sql
PRAGMA journal_mode = WAL;     -- persistent; set once by the init DDL (§3)
PRAGMA synchronous  = FULL;    -- per connection. NORMAL in WAL "might roll back following a power loss" [R54];
                               -- FULL costs about 1 ms per commit here (btrfs, §8 #9) — free for a journal
PRAGMA foreign_keys = ON;      -- MANDATORY per connection: SQLite's default is OFF and
                               -- STRICT does not enforce FKs (verified — see §8 record)
PRAGMA recursive_triggers = ON;  -- MANDATORY per connection: with OFF, INSERT OR REPLACE / REPLACE INTO
                               -- deletes the conflicting row WITHOUT firing the append-only DELETE
                               -- triggers (measurements, balances) — verified, §8 #6; ON blocks it
PRAGMA busy_timeout = 5000;    -- wait instead of failing instantly on SQLITE_BUSY
```

A writer should read these back at connect time and refuse to run if `foreign_keys` or
`recursive_triggers` is 0 or `synchronous` is not 2 (FULL, which is also SQLite's default) —
none of these is stored in the file, so the file alone cannot enforce them (and `PRAGMA
foreign_keys` is a silent no-op inside a transaction).

**Every write transaction starts with `BEGIN IMMEDIATE`.** A deferred `BEGIN` that reads first —
resolve a wikilink, then create the page (§6.14) — fails **at once** with `database is locked`
if another writer committed in between: `busy_timeout` does not apply to that lock upgrade
(executed, §8 #9). `BEGIN IMMEDIATE` takes the write lock up front, so a second writer waits
(up to `busy_timeout`) and then sees the first one's rows. Every write example in §6 does this;
keep such transactions short.

Readers need no setup but must be **read-only**: open the file with `?mode=ro` (SQLite then
refuses every write — `attempt to write a readonly database`, executed) or `sqlite3 -readonly`.
Datasette does this by itself — its connection is `mode=ro` and its SQL console accepts only
`SELECT` (executed on 0.65.5, §8 #9). Under WAL a reader sees the live file while the app writes
and never blocks it (executed). sqlite-web can insert, update and delete rows, which would make
it a second writer, so it is not used (D14 addendum).

"Single writing application" (principle 3, D3) does not mean a single OS process: the
app, its CLI, the API service, and local agents are all the same *writer* as long as
they go through the one application stack that owns the insert conventions (entity row
first, day written at insert, wikilinks re-extracted on save, measurements appended).

### 2.10 Money (D18)

- **Exact, integer, per-account currency.** An amount is an `INTEGER` count of *minor units*
  of the owning account's currency; `currencies.subunits` (100 for EUR, 1 for JPY) turns it
  into a number. Never `REAL`, never a `measurements` row: `0.1 + 0.2` in `REAL` is
  `0.30000000000000004` (executed, §8 #6), and a sum of balances must be exact.
- **A balance is a fact about a local day.** `balances.day` is the *local* date the figure
  describes (end of day) — the same rule as every `*_day` (§2.2). `recorded_at` is the UTC
  instant it was written down; the two differ whenever history is backfilled.
- **What an account is.** Anything with a balance or a value. Record **your own share** of
  joint items. `side` (`asset` | `liability`) and `currency` never change (a trigger);
  `category` is free taxonomy. One rule covers everything: *the balance is the value in the
  account's currency on that day, as the statement, the app or your own estimate says*.

  | You own | Make an account | Balance is |
  |---|---|---|
  | deposit, savings, cash | `category 'deposit'` / `'cash'`, its own currency | the statement balance |
  | stocks, funds, pension | one per broker or wrapper (`'brokerage'`, `'pension'`); one per currency if you hold several | the portfolio's market value |
  | crypto | one per exchange or wallet (`'crypto'`), valued in the fiat you track it in | its market value that day |
  | house, car, watch, art | `'property'`, `'vehicle'`, `'valuables'` | your estimate (`source = 'estimate'`) |
  | mortgage, loan, card | `side = 'liability'` | the amount owed, positive |

  Sell everything or dispose of it: record a final balance and set `closed_day`.
- **Net worth is derived.** It is computed on read from the latest balance of each open account
  as of a day, converted with `fx_rates` as of that day (§6.16–6.17); it is never written to a
  table, so it can be re-derived in any currency for any date.
- **Secrets.** Never store credentials, PINs or full account/card numbers in `life.db` or in a
  memo: the file is plaintext (D17). An account's `notes` may hold the last four digits. Finance
  data never goes into git — its history cannot be scrubbed (§2.1).

---


### 2.11 Threat model, the 2075 test, and imports (R4-19 c)

**What is protected, and from what.** The asset is `life.db`: prose, health and finance in one
plaintext file (D17 — the database is deliberately not encrypted). The threats worth a control,
the control, and what is left:

| Threat | Control | Residual |
|---|---|---|
| The file is damaged or lost | `synchronous=FULL` and WAL (§2.9); the integrity checks find damage (§2.8) | nothing recovers it: no second copy of the file is kept (§7); power loss is documented, not simulated; damage *inside a value* is found by no check |
| A buggy writer, importer or agent | one writing application; triggers for append-only facts, no hard deletes and fixed kinds and titles; `ON CONFLICT … DO NOTHING`; `BEGIN IMMEDIATE`; the foreign-key and orphan checks (§2.4, §2.9, §2.8) | the pragmas are per connection, so the application asserts them at connect |
| Another tool editing rows | exploration tools open the file read-only; Datasette was executed read-only (§2.9, D14) | anything with write access to the file bypasses every control |
| A stolen disk | the disk holding `life.db` is encrypted at rest (D17 addendum 2) | a stolen *unlocked* machine has everything |
| Finance or health data leaking through git | `life.db` and its `-wal`/`-shm` are never committed; no credentials or full account numbers, ever (§2.1, §2.10) | `notes` fields are free text — the owner's discipline |
| The data exposed on a network | Datasette on localhost only and read-only; nothing that runs arbitrary SQL is reachable from outside (D17 addendum 2) | a wrong bind address |
| A reader in fifty years without this document | the 2075 test, below | — |

Out of scope: a hostile local user, malware running as the owner, and legal compulsion — those
need the database itself encrypted (§7).

**The 2075 test.** A stranger holds `life.db` and nothing else — no `SCHEMA.md`, no application.
Every question below must be answerable from `.schema` and `SELECT * FROM lifelog_meta` (the
contract as data, D17). The table is executed (`tests/schema/r10probes.py`): each listed key must
exist in a fresh database and its text must contain each phrase in the last column, and **every key
of `lifelog_meta` must be used by some question** — so a contract rule cannot be added without a row
here, and a row cannot be dropped without the test noticing.

| # | Question | `lifelog_meta` key(s) | The answer says |
|---|---|---|---|
| 1 | What is this, and which design? | `schema` | `lifelog` |
| 2 | How is an instant stored? | `instants` | `UTC`, `ISO-8601` |
| 3 | What is a `*_day` column? | `days` | `LOCAL`, `never recomputed` |
| 4 | In which time zone was a row written? | `tz` | `IANA` |
| 5 | When was a row written, versus when did it happen? | `created_at` | `never back-dated` |
| 6 | Can anything be deleted? | `deletes` | `tombstone` |
| 7 | Are measurements kept? How is one corrected? | `measurements` | `append-only`, `retracts` |
| 8 | In what unit are amounts? How do I get net worth? | `money`, `net_worth` | `minor units`, `never stored` |
| 9 | Which balance row wins for a day? | `balances` | `newest row` |
| 10 | How are currencies converted? | `fx_rates` | `from_ccy < to_ccy` |
| 11 | Which link kinds exist, and who may link what? | `link_kinds` | `closed registry` |
| 12 | How do `[[wikilinks]]` and `#tags` become links? | `wikilinks` | `CommonMark`, `invalid target makes no link` |
| 13 | Why is a page never renamed? What makes a title valid? | `renames`, `titles`, `title_key` | `never`, `240`, `NFC` |
| 14 | Why do ids of different tables coincide? How are rows created? | `entities` | `supertype`, `one transaction` |
| 15 | Who may write, and with which settings? | `writers` | `BEGIN IMMEDIATE`, `read-only` |
| 16 | What is derived and can be rebuilt? | `pages_fts`, `title_key` | `rebuildable`, `derived` |
| 17 | How do imports avoid duplicates and bad rows? | `imports` | `DO NOTHING`, `OR IGNORE` |
| 18 | What does a repeating event or task mean? | `recurrence` | `templates` |
| 19 | How does the schema change after real data exists? | `evolution` | `additive`, `user_version` |
| 20 | What is a memo, what is a page, and can one become the other? | `pages_kind` | `untitled`, `never changes` |

**Imports** — the path for data that already exists elsewhere (a journal archive, a health export,
statements). Every step was executed (§8 #12) on 1 000 synthetic rows:

1. **Trial run first.** Rows are never deleted, so a bad import can only be retracted row by row
   (a NULL-value correction for a measurement or a balance, a tombstone for an entity). Do the
   first run of any new importer on a *copy*: `sqlite3 life.db "VACUUM INTO '/tmp/trial.db'"`.
2. **Load the rows into a scratch database, never into `life.db`** (`sqlite3 scratch.db ".import
   --csv weights.csv staging"`), then insert in one `BEGIN IMMEDIATE` transaction per batch:

```sql
ATTACH 'scratch.db' AS s;
BEGIN IMMEDIATE;
INSERT INTO measurements(metric_id, day, taken_at, tz, value, recorded_at, source, import_id)
SELECT (SELECT id FROM metrics WHERE name = 'weight'), day, NULLIF(taken_at, ''), NULLIF(tz, ''), value,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'scale-export', id
  FROM s.staging WHERE true
ON CONFLICT(source, import_id, metric_id) WHERE import_id IS NOT NULL DO NOTHING;
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
   `NULLIF(…, '')`; a `''` in `taken_at` fails its CHECK.
3. **Identity and time.** `source` names the importer, `import_id` is the source's own id, `day` /
   `taken_at` / `tz` say when it happened, `recorded_at` is when you imported it. `created_at` of an
   entity row is always the write time (§2.2) — never the date of the thing imported.
4. **What a failure does.** `ON CONFLICT … DO NOTHING` skips only a duplicate key: a malformed
   day, an impossible value or a dangling foreign key still raises and the **whole batch rolls back**
   (`OR IGNORE` would swallow them, §8 #6). Fix the data and run the batch again.
5. **Check afterwards:** the three checks of §2.8 (`integrity_check`, `foreign_key_check`, the orphan
   query), per-source counts (`SELECT source, count(*), min(day), max(day) FROM measurements
   GROUP BY source`), and **run the importer a second time — it must insert nothing.**
6. **Before the freeze**, run steps 1–5 once with a real export on a copy and record the result in
   §8: a real import is the one test this schema has never had.

## 3. The schema (canonical DDL)

This is the canonical init DDL. Until the freeze it is edited **in place** here — there
is no `0001_init.sql` file yet (D13); a test database is created by applying this block
to a fresh file (see the §8 validation records for the exact procedure).

```sql
-- ============================================================
-- Lifelog schema v1  (single init file — edited in place until the
-- freeze; numbered migrations begin only after real data exists)
--
-- Conventions (the whole contract — also queryable in lifelog_meta):
--   * All *_at columns: UTC ISO-8601 TEXT, millisecond precision
--     ('2026-06-09T21:14:03.482Z'), enforced by a CHECK round-trip
--   * All *_day columns: LOCAL calendar date TEXT ('YYYY-MM-DD'),
--     written at insert, never recomputed, enforced by date() round-trip
--   * Round-trip CHECKs use IS, not =: a CHECK passes when it
--     evaluates to NULL, and date('2026-9-3') is NULL — so
--     'date(x) = x' would ACCEPT malformed dates. 'date(x) IS x'
--     returns 0 for them. (Verified empirically; see §8.)
--   * Single writing APPLICATION (many processes/clients fine: app,
--     CLI, API, agents); nothing else writes life.db
--   * Importers use INSERT ... ON CONFLICT(...) DO NOTHING — never OR IGNORE
--     (it also skips CHECK / NOT NULL violations, silently) and never
--     OR REPLACE (a delete; §8 #6, #7)
--   * No binary files in v1: attachments/media are deferred (D9)
--   * No hard deletes of entities OR their domain rows: deleted_at is a
--     tombstone, ENFORCED by BEFORE DELETE triggers (links are the one
--     hard-deleted table, D11);
--     measurements are append-only, ENFORCED by triggers: no UPDATE, no
--     DELETE; corrections insert a row with supersedes_id (one per row); a
--     correction with a NULL value RETRACTS the row it corrects
--   * entities.created_at = when the row was written to life.db, never
--     back-dated; when a thing HAPPENED is the day / *_at of its own table
--   * entities.tz / measurements.tz = IANA zone of the writer at capture
--     (NULL = unknown): with the UTC instant it gives the local time of day,
--     which cannot be reconstructed later; only captured at write time
--   * link kinds are a CLOSED registry: links.kind must reference
--     link_kinds (FK); registering a kind is a deliberate INSERT, and a
--     kind's structure (symmetric flag, endpoint types) is fixed at
--     registration; a trigger checks every link's endpoint types
--   * page titles must be valid file names everywhere, so the DDL forbids
--     path-unsafe titles, never lets a title change (renames are forbidden,
--     D5), and enforces uniqueness on title_key — the title in NFC +
--     Unicode-casefolded form, computed by the app (SQLite cannot fold
--     Unicode); pages.kind never changes after insert
--   * Pages are never renamed: new page + links(kind='redirect')
--   * Repeating events/tasks are templates; occurrences expand at read
--   * Money is INTEGER minor units of the account's currency, never REAL:
--     amount / currencies.subunits = whole units (D18). Balances are
--     append-only snapshots of an account's VALUE on a day (a deposit, a
--     brokerage, a wallet, a house, a watch all look the same); net worth
--     is derived, never stored.
--   * Every enumerated CHECK is NAMED (entities_type, pages_kind,
--     tasks_status, events_repeat, tasks_repeat, *_repeat_position,
--     accounts_side) so a later ALTER TABLE ... DROP/ADD CONSTRAINT can
--     widen it; an unnamed CHECK can never be dropped (verified, §8 #6, #7)
--   * EVERY writer connection must set:  PRAGMA foreign_keys = ON;
--     PRAGMA recursive_triggers = ON;  PRAGMA busy_timeout = 5000;
--     PRAGMA synchronous = FULL;  and start every write transaction with
--     BEGIN IMMEDIATE (a deferred BEGIN that reads first fails at once with
--     'database is locked' when another writer commits in between; §2.9)
--     (SQLite defaults FKs OFF per connection; STRICT does not enforce
--     them; with recursive_triggers OFF a REPLACE deletes rows WITHOUT
--     firing the append-only DELETE triggers — verified, §8 #6)
-- ============================================================
PRAGMA application_id = 0x4C494645;   -- 'LIFE' — recognizable to file(1) and tools
PRAGMA user_version  = 1;
PRAGMA journal_mode  = WAL;           -- persistent; readers (Datasette) don't block the writer

-- ------------------------------------------------------------
-- The contract as data: time formats, derived indexes, rename and
-- recurrence semantics — readable with a SELECT, not just comments.
-- ------------------------------------------------------------
CREATE TABLE lifelog_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
INSERT INTO lifelog_meta(key, value) VALUES
  ('schema',     'lifelog v1'),
  ('instants',   'UTC ISO-8601 TEXT, ms precision, e.g. 2026-06-09T21:14:03.482Z'),
  ('days',       'LOCAL calendar date TEXT YYYY-MM-DD, written at insert, never recomputed'),
  ('renames',    'pages are never renamed; new page + links(kind=''redirect'')'),
  ('recurrence', 'rows with repeat <> ''none'' are templates; occurrences expand at read'),
  ('pages_fts',  'derived FTS5 index (unicode61: a CJK run is one token); pages_fts_* shadow tables are rebuildable, not data'),
  ('measurements','append-only (triggers reject UPDATE/DELETE); read through view measurement_values; one correction per row; a correction with NULL value retracts its row'),
  ('deletes',     'entities and their domain rows are never deleted (tombstone via entities.deleted_at, enforced by triggers); measurements and balances are never deleted; only links rows are hard-deleted (D11)'),
  ('created_at',  'entities.created_at = when the row was written to life.db, never back-dated; when a thing happened is its day / *_at fields'),
  ('tz',          'entities.tz and measurements.tz = IANA zone name of the writer when the row (measurements: taken_at) was captured, e.g. Europe/Berlin; NULL = unknown; with the UTC instant it gives the local time of day'),
  ('imports',     'INSERT ... ON CONFLICT(source, import_id ...) DO NOTHING; never OR IGNORE (skips CHECK/NOT NULL violations silently) or OR REPLACE (a delete)'),
  ('link_kinds',  'closed registry: links.kind references link_kinds; symmetric flag and endpoint types immutable and enforced; links rows are hard-deleted (D11)'),
  ('titles',      'page titles never change and are valid file names everywhere: <=240 bytes, no path/reserved characters or names (a device name like CON is reserved even before an extension); memos are untitled'),
  ('title_key',   'pages.title_key = NFC(casefold(NFC(title))), computed by the app; UNIQUE across pages (a memo has none); ASCII titles must equal lower(title); rebuildable'),
  ('pages_kind',  'pages.kind is memo (untitled: the capture stream and the inbox; always has a day) or page (titled, unique, linkable; its day is NULL when the app created it as a link target); it never changes after insert'),
  ('money',       'amounts are INTEGER minor units of accounts.currency; whole units = amount / currencies.subunits; never REAL, never in measurements'),
  ('balances',    'append-only snapshots of an account''s value on a local day (account, day, amount); the newest row per (account_id, day) wins; NULL amount retracts; read through balance_values'),
  ('net_worth',   'derived, never stored: per open account, latest balance on or before the day, converted with fx_rates, assets minus liabilities (section 6.16)'),
  ('fx_rates',    'reference data (mutable): 1 from_ccy = rate to_ccy in WHOLE units; one row per pair, stored with from_ccy < to_ccy; as-of lookup = newest day <= target'),
  ('entities',    'every page/event/task/person/place/account row has an entities row with the same id (supertype; composite FK (id, entity_type)); both are inserted in one transaction'),
  ('wikilinks',   'links(kind=wikilink) from a page always equal what its body names: [[Title]], [[Title|alias]] and #tag, read from the CommonMark text, never rewritten; rebuilt on every save; an invalid target makes no link'),
  ('writers',     'one writing application; every connection sets foreign_keys=ON, recursive_triggers=ON, synchronous=FULL, journal_mode=WAL and starts write transactions with BEGIN IMMEDIATE; every other tool opens the file read-only'),
  ('evolution',   'after the first real data: numbered forward-only SQL migrations, additive only, PRAGMA user_version; enumerated CHECKs are named so they can be widened with ALTER TABLE DROP/ADD CONSTRAINT');

-- ------------------------------------------------------------
-- Shared spine: one row per linkable thing. UNIQUE(id, type) plus
-- the composite FK in every domain table guarantees that a row's
-- type and its domain table always agree, and one id can never
-- live in two domain tables. deleted_at is the tombstone (D11).
-- ------------------------------------------------------------
CREATE TABLE entities (
  id         INTEGER PRIMARY KEY,
  type       TEXT NOT NULL CONSTRAINT entities_type
                  CHECK (type IN ('page','event','task','person','place','account')),
  created_at TEXT NOT NULL CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  updated_at TEXT NOT NULL CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at),
  deleted_at TEXT     CHECK (deleted_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at),
  tz         TEXT     CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),   -- IANA zone of the writer when the row was created ('Europe/Berlin'); NULL = unknown
  UNIQUE (id, type)
) STRICT;

-- ------------------------------------------------------------
-- All prose lives here: quick captures (memo: untitled, the journal
-- stream and the inbox) and titled, interlinked pages (page: an essay,
-- a reference page, a tag; one kind, D5 addendum 5). There is no
-- "journal" entity: the day page is a VIEW over the memo stream
-- (see query cookbook). memos double as an inbox: triaged_at NULL
-- = still in inbox. Mood is NOT a column: it is the 'mood' metric
-- in measurements, optionally pointed at its memo (D6).
-- ------------------------------------------------------------
CREATE TABLE pages (
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'page' CHECK (entity_type = 'page'),
  kind        TEXT NOT NULL CONSTRAINT pages_kind CHECK (kind IN ('memo','page')),
  title       TEXT,                       -- page: required, filename-safe, immutable; memo: NULL
  title_key   TEXT,                       -- page: NFC(casefold(NFC(title))), app-computed, unique; memo: NULL
  day         TEXT,                       -- local capture day; required for a memo; a page has one if written on purpose, NULL for a link target the app created
  triaged_at  TEXT CHECK (triaged_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', triaged_at) IS triaged_at),
  body        TEXT NOT NULL DEFAULT '',   -- CommonMark; [[Wiki Links]] inline
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),
  CHECK (kind = 'page' OR day IS NOT NULL),   -- a memo always has a day; a page may have none
  CHECK (kind = 'memo' OR title IS NOT NULL),
  CHECK (kind <> 'memo' OR title IS NULL),   -- memos are untitled: a titled memo would be unfindable
  CHECK ((title IS NULL) = (title_key IS NULL)),
  CHECK (title_key IS NULL OR (length(title_key) >= 1 AND title_key = trim(title_key)
                               AND title_key NOT GLOB '*[A-Z]*')),      -- a folded key has no ASCII capitals
  CHECK (title_key IS NULL OR title GLOB '*[^ -~]*' OR title_key = lower(title)),   -- pure-ASCII titles: the DB verifies the key
  CHECK (title IS NULL OR (title = trim(title) AND length(title) >= 1
                           AND length(CAST(title AS BLOB)) <= 240)),  -- bytes: a filename limit is 255 bytes
  CHECK (title IS NULL OR (                        -- a title must be a valid file name on Linux, macOS and Windows: keep it safe
         title NOT GLOB '*[/\:*?"<>|]*'            -- path separators and Windows-reserved characters
         AND title NOT GLOB ('*[' || char(1) || '-' || char(31) || char(127) || ']*')   -- control characters
         AND instr(title, char(0)) = 0
         AND substr(title, 1, 1) <> '.' AND substr(title, -1) <> '.'   -- no hidden files, '..', trailing dot
         AND upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)
             NOT IN ('CON','PRN','AUX','NUL',                -- Windows device names, bare or before an extension (CON.backup)
               'COM1','COM2','COM3','COM4','COM5','COM6','COM7','COM8','COM9','COM¹','COM²','COM³',
               'LPT1','LPT2','LPT3','LPT4','LPT5','LPT6','LPT7','LPT8','LPT9','LPT¹','LPT²','LPT³'))),
  CHECK (triaged_at IS NULL OR kind = 'memo'),
  CHECK (day IS NULL OR date(day) IS day)
) STRICT;
-- Uniqueness is on the key, not the title: 'Café' = 'CAFÉ' = NFD 'Café' = 'Diet' = 'diet' all collide.
-- Look pages up with  WHERE title_key = :key  (an equality implies the index's predicate, so SQLite uses it;
-- memos have no key and stay out of the index).
CREATE UNIQUE INDEX pages_title ON pages(title_key) WHERE title_key IS NOT NULL;
CREATE INDEX pages_day ON pages(day);
CREATE INDEX pages_inbox ON pages(day) WHERE kind = 'memo' AND triaged_at IS NULL;
-- Titles never change (D5: renames are forbidden — they would repoint every [[Old Title]]
-- in decades of prose). Fix a title by creating the new page and turning the old one into a
-- #REDIRECT stub (links.kind = 'redirect'). title_key is derived data and may be recomputed.
CREATE TRIGGER pages_title_fixed BEFORE UPDATE OF title ON pages
  WHEN NEW.title IS NOT OLD.title
BEGIN
  SELECT RAISE(ABORT, 'titles are immutable: create the new page and make this one a #REDIRECT stub');
END;
-- A page never changes kind: a memo that deserves to be a page becomes a NEW page
-- linked kind='spawned' (D5); flipping kind in place would silently change which
-- CHECKs and indexes govern the row.
CREATE TRIGGER pages_kind_fixed BEFORE UPDATE OF kind ON pages
  WHEN NEW.kind IS NOT OLD.kind
BEGIN
  SELECT RAISE(ABORT, 'pages.kind is fixed: create a new page and link it (kind=spawned) instead');
END;

-- Full-text search over prose. External-content FTS5 kept in sync
-- by triggers, so it can never drift and can be rebuilt with:
--   INSERT INTO pages_fts(pages_fts) VALUES('rebuild');
-- Tokenizer: the default unicode61 (folds accents: Zurich finds Zürich, stefan finds Ștefan)
-- — a CJK run is ONE token, so a part of it is not found. Decided, not forgotten: §7,
-- switching is drop + create with tokenize='trigram remove_diacritics 1' + rebuild.
CREATE VIRTUAL TABLE pages_fts USING fts5(
  title, body, content='pages', content_rowid='id'
);
CREATE TRIGGER pages_fts_ai AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;
CREATE TRIGGER pages_fts_ad AFTER DELETE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
END;
CREATE TRIGGER pages_fts_au AFTER UPDATE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', OLD.id, OLD.title, OLD.body);
  INSERT INTO pages_fts(rowid, title, body) VALUES (NEW.id, NEW.title, NEW.body);
END;

-- ------------------------------------------------------------
-- Named locations: a fifth entity type, linkable like everything
-- else (D16). events.place_id points here.
-- ------------------------------------------------------------
CREATE TABLE places (
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'place' CHECK (entity_type = 'place'),
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,   -- 'Berlin' = 'berlin'
  notes       TEXT,
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type)
) STRICT;

-- ------------------------------------------------------------
-- Happenings: appointments, trips, milestones. Date-level facts
-- are first-class; instants are optional extra precision.
-- Repeating rows are templates (D15): occurrences expand at read.
-- ------------------------------------------------------------
CREATE TABLE events (
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'event' CHECK (entity_type = 'event'),
  title       TEXT NOT NULL,
  start_day   TEXT NOT NULL CHECK (date(start_day) IS start_day),
  end_day     TEXT CHECK (end_day IS NULL OR date(end_day) IS end_day),
  start_at    TEXT CHECK (start_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', start_at) IS start_at),
  end_at      TEXT CHECK (end_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', end_at) IS end_at),
  place_id    INTEGER REFERENCES places(id),
  notes       TEXT,
  repeat        TEXT NOT NULL DEFAULT 'none'
                 CONSTRAINT events_repeat CHECK (repeat IN ('none','daily','weekly','monthly','yearly')),
  repeat_every  INTEGER,               -- NULL = 1; 13 w/ daily = every 13 days;
                                        -- 3 w/ monthly = quarterly
  repeat_weekdays TEXT,                -- weekly only: 'mo,we,fr'
  repeat_position TEXT,                -- monthly only, w/ repeat_weekday:
                                        -- 'first'..'last' → "last Friday"
  repeat_weekday  TEXT,                -- monthly only, w/ repeat_position: 'mo'..'su'
  repeat_until   TEXT CHECK (repeat_until IS NULL OR date(repeat_until) IS repeat_until),
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),
  CHECK (end_day IS NULL OR end_day >= start_day),
  CHECK (end_at IS NULL OR start_at IS NULL OR end_at >= start_at),
  CHECK (repeat_until IS NULL OR repeat_until >= start_day),
  CHECK (repeat_every IS NULL OR (repeat <> 'none' AND repeat_every >= 1)),
  CHECK (repeat_until IS NULL OR repeat <> 'none'),
  CHECK ((repeat = 'weekly') = (repeat_weekdays IS NOT NULL)),
  CHECK (repeat_weekdays IS NULL OR (              -- 'mo,we,fr': lowercase 2-letter tokens, comma-separated
         repeat_weekdays NOT GLOB '*[^a-z,]*'
         AND repeat_weekdays NOT GLOB ',*' AND repeat_weekdays NOT GLOB '*,'
         AND repeat_weekdays NOT GLOB '*,,*'
         AND length(repeat_weekdays) =
             3 * (length(repeat_weekdays) - length(replace(repeat_weekdays, ',', '')) + 1) - 1
         AND length(replace(replace(replace(replace(replace(replace(replace(replace(
               repeat_weekdays,'su',''),'mo',''),'tu',''),'we',''),'th',''),'fr',''),'sa',''),',','')) = 0)),
  CHECK ((repeat_position IS NULL) = (repeat_weekday IS NULL)),
  CONSTRAINT events_repeat_position CHECK (repeat_position IS NULL OR
         (repeat = 'monthly' AND repeat_position IN ('first','second','third','fourth','last')
          AND repeat_weekday IN ('mo','tu','we','th','fr','sa','su')))
) STRICT;
CREATE INDEX events_start ON events(start_day);

-- ------------------------------------------------------------
-- Tasks are fleeting: open | done. Abandoning a task is erasure
-- (a tombstone), not a recorded decision. A repeating task is a
-- template like a repeating event; completing it ends the series
-- (repeat_until = the completion day, by convention) (D15).
-- ------------------------------------------------------------
CREATE TABLE tasks (
  id           INTEGER PRIMARY KEY,
  entity_type  TEXT NOT NULL DEFAULT 'task' CHECK (entity_type = 'task'),
  title        TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'open' CONSTRAINT tasks_status CHECK (status IN ('open','done')),
  due_day      TEXT CHECK (due_day IS NULL OR date(due_day) IS due_day),
  completed_at TEXT CHECK (completed_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', completed_at) IS completed_at),
  completed_day TEXT CHECK (completed_day IS NULL OR date(completed_day) IS completed_day),   -- LOCAL day it was done (D10)
  repeat        TEXT NOT NULL DEFAULT 'none'
                 CONSTRAINT tasks_repeat CHECK (repeat IN ('none','daily','weekly','monthly','yearly')),
  repeat_every  INTEGER,
  repeat_weekdays TEXT,
  repeat_position TEXT,
  repeat_weekday  TEXT,
  repeat_until   TEXT CHECK (repeat_until IS NULL OR date(repeat_until) IS repeat_until),
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),
  CHECK ((status = 'done') = (completed_at IS NOT NULL)),
  CHECK ((status = 'done') = (completed_day IS NOT NULL)),
  CHECK (repeat = 'none' OR due_day IS NOT NULL),    -- a template needs an anchor day to expand from
  CHECK (repeat_every IS NULL OR (repeat <> 'none' AND repeat_every >= 1)),
  CHECK (repeat_until IS NULL OR repeat <> 'none'),
  CHECK ((repeat = 'weekly') = (repeat_weekdays IS NOT NULL)),
  CHECK (repeat_weekdays IS NULL OR (              -- 'mo,we,fr': lowercase 2-letter tokens, comma-separated
         repeat_weekdays NOT GLOB '*[^a-z,]*'
         AND repeat_weekdays NOT GLOB ',*' AND repeat_weekdays NOT GLOB '*,'
         AND repeat_weekdays NOT GLOB '*,,*'
         AND length(repeat_weekdays) =
             3 * (length(repeat_weekdays) - length(replace(repeat_weekdays, ',', '')) + 1) - 1
         AND length(replace(replace(replace(replace(replace(replace(replace(replace(
               repeat_weekdays,'su',''),'mo',''),'tu',''),'we',''),'th',''),'fr',''),'sa',''),',','')) = 0)),
  CHECK ((repeat_position IS NULL) = (repeat_weekday IS NULL)),
  CONSTRAINT tasks_repeat_position CHECK (repeat_position IS NULL OR
         (repeat = 'monthly' AND repeat_position IN ('first','second','third','fourth','last')
          AND repeat_weekday IN ('mo','tu','we','th','fr','sa','su')))
) STRICT;
CREATE INDEX tasks_open ON tasks(status, due_day);

-- ------------------------------------------------------------
CREATE TABLE people (
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'person' CHECK (entity_type = 'person'),
  name        TEXT NOT NULL,
  nickname    TEXT,
  birth_day   TEXT CHECK (birth_day IS NULL OR date(birth_day) IS birth_day),
  death_day   TEXT CHECK (death_day IS NULL OR date(death_day) IS death_day),
  notes       TEXT,
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),
  CHECK (death_day IS NULL OR birth_day IS NULL OR death_day >= birth_day)
) STRICT;

-- ------------------------------------------------------------
-- Metrics: a tiny registry that keeps time-series canonical.
-- 'weight' is one series forever, never 'weight'/'Weight'/'weight kg'
-- (hence COLLATE NOCASE). Seeded with 'mood' (D6); UI should make
-- create-on-the-fly painless (suggest + confirm).
-- ------------------------------------------------------------
CREATE TABLE metrics (
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL UNIQUE COLLATE NOCASE,  -- snake_case canonical: 'weight', 'mood'
  unit  TEXT NOT NULL DEFAULT '',             -- 'kg', 'bpm', 'h'; '' for 1-5 scales
  notes TEXT,
  CHECK (length(name) >= 1 AND name NOT GLOB '*[^a-z0-9_]*')   -- snake_case, as the comment says
) STRICT;
-- The unit gives every stored value its meaning; changing it would silently reinterpret the series.
CREATE TRIGGER metrics_unit_fixed BEFORE UPDATE OF unit ON metrics
  WHEN NEW.unit IS NOT OLD.unit
BEGIN
  SELECT RAISE(ABORT, 'metrics.unit is fixed: it defines what every stored value means; register a new metric instead');
END;
INSERT INTO metrics(name, unit, notes) VALUES
  ('mood', '', '1-5; attached to its memo via measurements.entity_id when posted');

-- ------------------------------------------------------------
-- Measurements: one row per data point (the FxLifeSheet shape,
-- which survived 380k rows / 6+ years). Append-only: never UPDATE
-- a value; corrections insert a new row with supersedes_id — and
-- triggers enforce it and reject cross-metric corrections. A correction whose
-- value is NULL RETRACTS the row it corrects (a mis-tap, a wrong metric).
-- import_id makes bulk re-imports idempotent:
--   INSERT ... ON CONFLICT(source, import_id, metric_id)
--     WHERE import_id IS NOT NULL DO NOTHING
-- (never OR IGNORE: it silently skips rows violating a CHECK or NOT NULL).
-- ------------------------------------------------------------
CREATE TABLE measurements (
  id            INTEGER PRIMARY KEY,
  metric_id     INTEGER NOT NULL REFERENCES metrics(id),
  day           TEXT NOT NULL CHECK (date(day) IS day),  -- local date the value refers to
  taken_at      TEXT CHECK (taken_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  tz            TEXT CHECK (tz IS NULL OR (length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),   -- IANA zone where taken_at was captured; NULL = unknown
  value         REAL,                        -- numeric only, by design (D7); NULL only on a correction: it RETRACTS the row it supersedes
  recorded_at   TEXT NOT NULL CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', recorded_at) IS recorded_at),  -- when it was written down (audit; taken_at is when it was measured)
  source        TEXT NOT NULL DEFAULT 'manual',
  import_id     TEXT,                        -- importer's dedup key, unique per (source, metric)
  entity_id     INTEGER REFERENCES entities(id),      -- provenance: captured with this memo/event
  supersedes_id INTEGER REFERENCES measurements(id),  -- optional: corrects an earlier row
  CHECK (supersedes_id IS NULL OR supersedes_id <> id),
  CHECK (value IS NOT NULL OR supersedes_id IS NOT NULL)   -- a first reading has a value; only a correction may retract
) STRICT;
CREATE INDEX measurements_series ON measurements(metric_id, day);
CREATE UNIQUE INDEX measurements_import
  ON measurements(source, import_id, metric_id) WHERE import_id IS NOT NULL;
-- Append-only, enforced: rows are never changed or removed. A correction
-- is a NEW row whose supersedes_id names the row it replaces; each row can
-- be corrected at most once (chain corrections: correct the correction).
-- Because supersedes_id can only be set at INSERT and must name an earlier
-- row, supersede chains can never form a cycle.
CREATE UNIQUE INDEX measurements_one_correction
  ON measurements(supersedes_id) WHERE supersedes_id IS NOT NULL;  -- also serves measurement_values
CREATE INDEX measurements_day ON measurements(day);                -- day view
CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are append-only: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_no_delete BEFORE DELETE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are never deleted: correct by inserting a row with supersedes_id');
END;
-- A correction must correct an EXISTING row of the SAME metric
-- (IS NOT, not <>: a dangling supersedes_id yields NULL, and NULL <> x is NULL = pass):
CREATE TRIGGER measurements_supersede_metric AFTER INSERT ON measurements
  WHEN NEW.supersedes_id IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'supersedes_id must reference a measurement of the same metric')
   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;
END;
-- The canonical read rule, as a view: rows nothing has corrected, minus retractions.
CREATE VIEW measurement_values AS
  SELECT me.*
    FROM measurements me
   WHERE me.value IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM measurements x WHERE x.supersedes_id = me.id);

-- ------------------------------------------------------------
-- Money (D18). Personal finance is a small, exact tier of its own — never
-- rows in measurements (REAL, one unit per metric, no retraction).
--   currencies : closed registry of the currencies you hold or value things
--                in; subunits = minor units per whole unit (EUR 100, JPY 1),
--                so a stranger can turn an INTEGER amount into a number
--                without the app. Immutable.
--   accounts   : anything with a balance or a value — bank, deposit,
--                brokerage, crypto wallet, pension, cash, property, vehicle,
--                valuables (watch, art), loan, mortgage, card. Stocks and
--                crypto are valued like everything else: the market value in
--                fiat that the statement or app shows on that day. A
--                sixth ENTITY type, so memos/events can link to it and it can
--                be tombstoned. side + currency define what every balance
--                means and never change. Record your OWN share of joint items.
--   balances   : append-only snapshots. The newest row per (account_id, day)
--                wins; a row with NULL amount RETRACTS that day. Wrong entry =
--                insert another row, never UPDATE/DELETE. Read through
--                balance_values. Net worth is DERIVED (see section 6.16).
--   fx_rates   : reference data (mutable, re-importable). Canonical direction
--                from_ccy < to_ccy, so one pair can never hold two rates.
-- Never store credentials or full account/card numbers anywhere in life.db.
-- ------------------------------------------------------------
CREATE TABLE currencies (
  code     TEXT PRIMARY KEY CHECK (length(code) BETWEEN 3 AND 10 AND code NOT GLOB '*[^A-Z0-9]*'),  -- ISO 4217 code: 'EUR', 'JPY' (a coin you hold by quantity may be registered too)
  name     TEXT NOT NULL,
  subunits INTEGER NOT NULL CONSTRAINT currencies_subunits CHECK (subunits BETWEEN 1 AND 1000000000),  -- minor units per 1 whole unit
  notes    TEXT
) STRICT;
INSERT INTO currencies(code, name, subunits) VALUES
  ('USD', 'US dollar',      100),
  ('EUR', 'Euro',           100),
  ('GBP', 'Pound sterling', 100),
  ('CHF', 'Swiss franc',    100),
  ('CAD', 'Canadian dollar',100),
  ('AUD', 'Australian dollar', 100),
  ('JPY', 'Japanese yen',   1);
-- Changing subunits would silently rescale every balance ever recorded in that currency.
CREATE TRIGGER currencies_subunits_fixed BEFORE UPDATE OF subunits ON currencies
  WHEN NEW.subunits IS NOT OLD.subunits
BEGIN
  SELECT RAISE(ABORT, 'currencies.subunits is fixed: it defines what every stored amount means; register a new currency code instead');
END;

CREATE TABLE accounts (
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'account' CHECK (entity_type = 'account'),
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,   -- 'Main checking', 'Flat (Berlin)'; unique like places.name
  side        TEXT NOT NULL CONSTRAINT accounts_side CHECK (side IN ('asset','liability')),
  currency    TEXT NOT NULL REFERENCES currencies(code),
  category    TEXT COLLATE NOCASE,                   -- free taxonomy: 'cash','deposit','brokerage','crypto','pension','property','valuables','loan'
  institution TEXT,
  opened_day  TEXT CHECK (opened_day IS NULL OR date(opened_day) IS opened_day),
  closed_day  TEXT CHECK (closed_day IS NULL OR date(closed_day) IS closed_day),   -- last day the account counts (inclusive)
  notes       TEXT,
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, type),
  CHECK (closed_day IS NULL OR opened_day IS NULL OR closed_day >= opened_day)
) STRICT;
-- side and currency give every balance its meaning; changing either would rewrite history.
-- (The WHEN clause keeps full-row ORM updates working.) To change: close it, open a new account.
CREATE TRIGGER accounts_meaning_fixed BEFORE UPDATE OF side, currency ON accounts
  WHEN NEW.side IS NOT OLD.side OR NEW.currency IS NOT OLD.currency
BEGIN
  SELECT RAISE(ABORT, 'an account''s side and currency are fixed: close it and open a new account instead');
END;

CREATE TABLE balances (
  id          INTEGER PRIMARY KEY,
  account_id  INTEGER NOT NULL REFERENCES accounts(id),
  day         TEXT NOT NULL CHECK (date(day) IS day),   -- LOCAL as-of date: the end-of-day balance / valuation
  amount      INTEGER,                                  -- minor units of accounts.currency, as the institution states it
                                                        -- (a mortgage of 200 000 is +20000000: side says it is owed);
                                                        -- NULL = retraction of this (account, day)
  recorded_at TEXT NOT NULL CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', recorded_at) IS recorded_at),  -- when it was written down
  source      TEXT NOT NULL DEFAULT 'manual',           -- 'manual','statement','estimate','import:<name>'
  import_id   TEXT,                                     -- importer's dedup key, unique per source
  note        TEXT                                      -- 'after selling the ETF', 'agent estimate'
) STRICT;
CREATE INDEX balances_series ON balances(account_id, day);   -- newest-per-day and as-of lookups (rowid is the last key part)
CREATE UNIQUE INDEX balances_import ON balances(source, import_id) WHERE import_id IS NOT NULL;
-- Append-only, enforced (needs PRAGMA recursive_triggers=ON for REPLACE, see header):
CREATE TRIGGER balances_no_update BEFORE UPDATE ON balances
BEGIN
  SELECT RAISE(ABORT, 'balances are append-only: correct by inserting a newer row for the same (account, day)');
END;
CREATE TRIGGER balances_no_delete BEFORE DELETE ON balances
BEGIN
  SELECT RAISE(ABORT, 'balances are never deleted: retract by inserting a row with NULL amount');
END;
-- The canonical read rule: the newest row per (account, day), unless that row is a retraction.
CREATE VIEW balance_values AS
  SELECT b.*
    FROM balances b
   WHERE b.amount IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM balances x
                      WHERE x.account_id = b.account_id AND x.day = b.day AND x.id > b.id);

CREATE TABLE fx_rates (
  from_ccy TEXT NOT NULL REFERENCES currencies(code),
  to_ccy   TEXT NOT NULL REFERENCES currencies(code),
  day      TEXT NOT NULL CHECK (date(day) IS day),
  rate     REAL NOT NULL CHECK (rate > 0 AND rate < 1e18),   -- 1 from_ccy = rate to_ccy, in WHOLE units (not minor units)
  source   TEXT NOT NULL DEFAULT 'manual',
  PRIMARY KEY (from_ccy, to_ccy, day),
  CHECK (from_ccy < to_ccy)          -- one canonical direction per pair; the inverse is 1/rate
) STRICT;

-- ------------------------------------------------------------
-- One graph for everything: wiki backlinks, person↔person
-- relationships, memo→task provenance, subtasks, attendance,
-- redirects. link_kinds is a CLOSED registry: a link's kind must be
-- registered first (FK), and a kind's structure — the symmetric flag
-- and the allowed endpoint entity types — is fixed at registration
-- and enforced (a trigger checks every link's endpoint types).
-- Registering a kind is a deliberate INSERT INTO link_kinds
-- (lowercase, [a-z0-9_-]) — a typo can no longer silently create a
-- new kind. from_types / to_types: NULL = any entity type, otherwise
-- a comma list drawn from entities.type ('task,page'). A misspelt type
-- token fails CLOSED: every link of that kind is rejected. The mirror
-- triggers keep symmetric kinds two-sided on insert AND delete, so a
-- half-edge can never exist and backlink queries need only to_id for
-- them. Cycles (e.g. subtask) are not prevented by the schema (D8).
-- ------------------------------------------------------------
CREATE TABLE link_kinds (
  kind       TEXT PRIMARY KEY CHECK (kind = lower(kind) AND length(kind) > 0 AND kind NOT GLOB '*[^a-z0-9_-]*'),
  symmetric  INTEGER NOT NULL DEFAULT 0 CHECK (symmetric IN (0,1)),
  from_types TEXT CHECK (from_types IS NULL OR (from_types NOT GLOB '*[^a-z,]*' AND from_types NOT GLOB ',*'
                         AND from_types NOT GLOB '*,' AND from_types NOT GLOB '*,,*')),
  to_types   TEXT CHECK (to_types   IS NULL OR (to_types   NOT GLOB '*[^a-z,]*' AND to_types   NOT GLOB ',*'
                         AND to_types   NOT GLOB '*,' AND to_types   NOT GLOB '*,,*')),
  note       TEXT,
  CHECK (symmetric = 0 OR from_types IS to_types)   -- a mirrored edge must be valid in both directions
) STRICT;
INSERT INTO link_kinds(kind, symmetric, from_types, to_types, note) VALUES
  ('wikilink', 0, 'page',      'page',         'extracted from [[body]] on save; body is the truth'),
  ('redirect', 0, 'page',      'page',         'old stub page → its replacement; renames, D5'),
  ('spawned',  0, 'task,page', 'page',         'task/page created from a memo during triage'),
  ('subtask',  0, 'task',      'task',         'child task → parent task'),
  ('attended', 0, 'person',    'event',        'person → event'),
  ('about',    0, NULL,        'person,place,account', 'entity → person/place/account it is about'),
  ('visited',  0, 'person',    'place',        'person → place; the place of an EVENT is events.place_id, never a link (D16 addendum)'),
  ('located-in', 0, 'place',   'place',        'containment: Tokyo → Japan; transitive — walk it with a recursive CTE (section 6.19)'),
  ('parent-of', 0, 'person',   'person',       'parent → child; ''family'' stays the symmetric catch-all'),
  ('friend',   1, 'person',    'person',       NULL),
  ('family',   1, 'person',    'person',       NULL),
  ('related',  1, NULL,        NULL,           'anything ↔ anything');
-- A kind's structure is fixed once registered: changing it would leave edges that
-- violate it or half-edges. To change structure, register a new kind.
CREATE TRIGGER link_kinds_structure_fixed BEFORE UPDATE OF symmetric, from_types, to_types ON link_kinds
  WHEN NEW.symmetric IS NOT OLD.symmetric OR NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types
BEGIN
  SELECT RAISE(ABORT, 'a link kind''s structure (symmetric, endpoint types) is fixed at registration; register a new kind instead');
END;

CREATE TABLE links (
  id         INTEGER PRIMARY KEY,
  from_id    INTEGER NOT NULL REFERENCES entities(id),
  to_id      INTEGER NOT NULL REFERENCES entities(id),
  kind       TEXT NOT NULL REFERENCES link_kinds(kind),  -- closed registry
  note       TEXT,
  created_at TEXT NOT NULL CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  UNIQUE (from_id, to_id, kind)
) STRICT;
CREATE INDEX links_to ON links(to_id);   -- backlinks query (from_id is served by the UNIQUE index)

-- Links are immutable: to change one, delete and re-insert.
CREATE TRIGGER links_immutable BEFORE UPDATE OF from_id, to_id, kind ON links
  WHEN NEW.from_id IS NOT OLD.from_id OR NEW.to_id IS NOT OLD.to_id OR NEW.kind IS NOT OLD.kind
BEGIN
  SELECT RAISE(ABORT, 'links are immutable: delete and re-insert');
END;
-- The registry is closed even if a connection forgot PRAGMA foreign_keys=ON: an
-- unregistered kind is rejected here as well as by the FK. Endpoint types must match
-- the kind; an unknown endpoint id is treated as type '?' and so rejected for typed kinds.
CREATE TRIGGER links_endpoint_types BEFORE INSERT ON links
BEGIN
  SELECT RAISE(ABORT, 'link kind is not registered in link_kinds')
   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);
  SELECT RAISE(ABORT, 'link endpoint type not allowed for this kind (see link_kinds.from_types / to_types)')
   WHERE EXISTS (SELECT 1 FROM link_kinds k
                  WHERE k.kind = NEW.kind
                    AND ((k.from_types IS NOT NULL
                          AND instr(',' || k.from_types || ',',
                                    ',' || coalesce((SELECT type FROM entities WHERE id = NEW.from_id), '?') || ',') = 0)
                      OR (k.to_types IS NOT NULL
                          AND instr(',' || k.to_types || ',',
                                    ',' || coalesce((SELECT type FROM entities WHERE id = NEW.to_id), '?') || ',') = 0)));
END;
-- Mirror symmetric edges on insert and remove the mirror on delete.
CREATE TRIGGER links_mirror_insert AFTER INSERT ON links
  WHEN NEW.from_id <> NEW.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = NEW.kind) = 1
BEGIN
  INSERT OR IGNORE INTO links(from_id, to_id, kind, note, created_at)
  VALUES (NEW.to_id, NEW.from_id, NEW.kind, NEW.note, NEW.created_at);
END;
CREATE TRIGGER links_mirror_delete AFTER DELETE ON links
  WHEN OLD.from_id <> OLD.to_id
   AND (SELECT symmetric FROM link_kinds WHERE kind = OLD.kind) = 1
BEGIN
  DELETE FROM links WHERE from_id = OLD.to_id AND to_id = OLD.from_id AND kind = OLD.kind;
END;

-- Ghost pages: empty pages nobody points at (a link target created by a
-- capture-time typo and never written; renames never create ghosts). The UI sweeps this list.
CREATE VIEW ghost_pages AS
  SELECT p.id, p.title, e.created_at
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.kind = 'page' AND p.body = '' AND e.deleted_at IS NULL
     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.from_id = p.id);

-- ------------------------------------------------------------
-- updated_at is maintained in-DB so ANY future writer (CLI,
-- agents, scripts) gets audit timestamps right without app help.
-- ------------------------------------------------------------
CREATE TRIGGER pages_touch AFTER UPDATE ON pages BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
CREATE TRIGGER events_touch AFTER UPDATE ON events BEGIN
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
CREATE TRIGGER accounts_touch AFTER UPDATE ON accounts BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;
-- Tombstoning and un-tombstoning are changes too (its own update of updated_at
-- does not re-fire: the trigger watches deleted_at only).
CREATE TRIGGER entities_touch AFTER UPDATE OF deleted_at ON entities
  WHEN NEW.deleted_at IS NOT OLD.deleted_at
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

-- No hard deletes (D11), enforced: an entity and its domain row are tombstoned, never removed.
-- (Under PRAGMA recursive_triggers=ON these also stop REPLACE from deleting a row.) links are
-- the one table whose rows are hard-deleted; measurements/balances have their own triggers.
CREATE TRIGGER entities_no_delete BEFORE DELETE ON entities
BEGIN SELECT RAISE(ABORT, 'entities are never deleted: set entities.deleted_at (tombstone)'); END;
CREATE TRIGGER pages_no_delete BEFORE DELETE ON pages
BEGIN SELECT RAISE(ABORT, 'pages are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER events_no_delete BEFORE DELETE ON events
BEGIN SELECT RAISE(ABORT, 'events are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER tasks_no_delete BEFORE DELETE ON tasks
BEGIN SELECT RAISE(ABORT, 'tasks are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER people_no_delete BEFORE DELETE ON people
BEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER places_no_delete BEFORE DELETE ON places
BEGIN SELECT RAISE(ABORT, 'places are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER accounts_no_delete BEFORE DELETE ON accounts
BEGIN SELECT RAISE(ABORT, 'accounts are never deleted: tombstone the entity (entities.deleted_at)'); END;
```

**Table count: 15 real tables (11 + `currencies`, `accounts`, `balances`, `fx_rates` — D18) + 1 FTS5
virtual table + 3 views** (`measurement_values`, `balance_values`, `ghost_pages`). That is the entire
system.

---

## 4. Entity model overview

```
entities (supertype: id, type, created_at, updated_at, deleted_at, tz)
   │ 1:1, shared PK, composite FK (id, entity_type) → entities(id, type)
   │    guarantees type and domain table always agree
   ├── pages      (memo | page)          ── prose, inbox state, capture day
   ├── events                             ── day-precise happenings, recurrence,
   │                                        optional place_id
   ├── tasks                              ── open | done; recurrence; subtasks via links
   ├── people                            ── name + birth/death days
   ├── places                            ── named locations for events and links
   └── accounts                          ── anything with a balance: side (asset|liability),
                                            currency, opened/closed day (D18)

links       (from_id → to_id, kind)        ── polymorphic graph over entities;
                                           │ symmetric kinds are mirrored by trigger
link_kinds  (kind, symmetric, from/to_types) ── CLOSED registry: links.kind must reference it;
                                           │ symmetry + endpoint types fixed and enforced
measurements (metric_id, day, value …)      ── append-only time series (optionally → entity,
                                           │ e.g. mood rows point at their memo)
metrics      (name, unit)                   ── registry keeping series canonical
balances     (account_id, day, amount …)    ── append-only INTEGER minor-unit snapshots; newest row per
                                           │ (account, day) wins, NULL amount retracts (D18)
currencies   (code, subunits)               ── closed registry: minor units per whole unit, immutable
fx_rates     (from_ccy, to_ccy, day, rate)  ── reference data, one canonical direction per pair
pages_fts    (FTS5 over pages)              ── derived, rebuildable
lifelog_meta (key, value)                   ── the storage contract as queryable data
```

How the pieces serve the product concepts:

| Product concept | Schema mechanism |
|---|---|
| Journaling | Posting `memo` pages during the day; the "day page" is a query (§6.2), not an entity |
| Inbox | `memo` rows with `triaged_at IS NULL` (§6.3) |
| Triage | Keep → set `triaged_at`; act → create task + `links(kind='spawned')`; junk → tombstone |
| Mood tracking | the `mood` metric in `measurements`, each row optionally pointing at its memo (§6.4) |
| Notes and wiki | `pages(kind='page')` — one kind: titled, unique, dated only if written on purpose — + `links(kind='wikilink')` kept equal to what the `[[body]]` text names (save contract, §2.5, D19) |
| Tags | same mechanism: `#health` is read as `[[health]]`, a page; the body is never rewritten (D5, D19) |
| Backlinks | `links WHERE to_id = ?` (§6.5); symmetric kinds are mirrored on insert and delete, so one direction suffices. Asymmetric kinds (`attended`, `subtask`, …) need both directions for "everything about X" (§6.6) |
| Life graph ("everything about my son") | `links` over `people`/`events`/`pages`/`places` (§6.6) |
| Subtasks | `links(kind='subtask', child → parent)`; recursive CTE for nesting (§6.11) |
| Recurring events & tasks | `repeat_*` columns; occurrences expanded at read with a recursive CTE (§6.12). A repeating *task* is a reminder: completing it ends the series (D15 addendum 2) |
| Birthdays | query over `people.birth_day` — deliberately not events |
| Biomarkers / quantified self / habits | `metrics` + `measurements` (§6.7) |
| Stocks, crypto, deposits, valuables, property, loans | one `account` each (§2.10 table): a balance is its value on a day |
| Net worth over time | derived: latest `balance_values` per open `account`, converted with `fx_rates` (§6.16–6.17) |
| "What was my flat / mortgage / pension worth in 2019?" | `balances` of that account, as of a day (§6.15–6.16) |
| Notes about an account or a money decision | `links(kind='about', memo/event → account)` |
| Which accounts are stale | §6.18 |
| Search | `pages_fts` (§6.8) |
| History of a row | none beyond `created_at` / `updated_at` / `deleted_at` and the append-only facts (D12) |

---

## 5. Decision log

Each decision: **context → decision → alternatives rejected → rationale → sources.**

### D1 — Container: a single SQLite file. *(Settled; not re-opened)*

- **Decision.** SQLite, one file (`life.db`). *(v1 addendum: binaries are deferred
  entirely — D9; when they return they live external in `media/`, by the design kept
  there.)*
- **Alternatives.** Postgres/server DB (rejected: operational burden for single user,
  no longevity benefit); plain files only (see D4); NoSQL embedded stores (rejected:
  weaker durability guarantees, no standard query language for future readers).
- **Rationale.** The US Library of Congress lists SQLite as a *Recommended Storage
  Format* for datasets — one of only four, alongside XML, JSON, CSV [R10][R11]. The file
  format has been backwards-compatible since 2004 and the developers commit to reading
  today's files "for decades into the future," planning through 2050 [R12][R13]. SQLite's
  application-file-format essay: "Data lives longer than code … an SQLite database remains
  readable long after all traces of the original application have been lost" [R1].
- **Consequence.** Portability story is strong; the remaining risk is meaning living in
  app code — which D2, D4 and the conventions in §2 address.

### D2 — Typed tables with real columns, `STRICT` mode; no JSON property bags, no EAV.

- **Context.** The predecessor design stored properties as JSON keyed by UUIDs, with
  definitions in TypeScript. The generic-object model existed to power a view-generation
  system that has been cut from scope.
- **Decision.** Per-domain typed tables; every column real, named, and typed; every table
  declared `STRICT` (SQLite ≥ 3.37): every value must be *losslessly* convertible to the
  declared column type or the statement fails — `'xyz'` into INTEGER is rejected
  (`datatype mismatch`), `123` into TEXT is accepted and stored as `'123'`. No
  lossy-conversion surprises [R14]. No JSON columns anywhere. No free-form key/value
  property tables.
- **Alternatives.**
  - *Single `objects` table + JSON properties*: rejected. `json_extract()` re-parses text
    on every call for every row; indexed JSON requires generated-column scaffolding
    [R15][R16]; benchmarks of tagging strategies show `json_each()` scans far slower than
    plain indexed tables [R17]. Practitioner reports converge on JSON columns becoming
    "an unmapped wasteland of inconsistent keys" within months [R18]. And with the field
    set fixed, the flexibility buys nothing.
  - *EAV (entity-attribute-value)*: rejected — the classic documented anti-pattern once
    attributes are known: no type integrity, no referential integrity, torturous queries
    [R19].
  - *Non-STRICT tables*: rejected — STRICT is one keyword per table and moves validation
    from app code into the durable artifact itself, which is exactly principle #2.
- **Long-tail fields** ("bike serial number") are added by additive migration, *not* by a
  JSON escape hatch. The migration ceremony is the feature: it forces an explicit
  decision that a field deserves to exist permanently.
- **Sources.** [R1][R14]–[R19].

### D3 — IDs: `INTEGER PRIMARY KEY`; UUIDs rejected.

- **Decision.** Every table keyed by `INTEGER PRIMARY KEY` (a rowid alias) *(the registries
  keep their natural key — addendum)*. No
  `AUTOINCREMENT` (extra CPU/IO/bookkeeping, "usually not needed" [R4]). No UUIDs.
- **Alternatives.** UUIDv7/v4 TEXT keys: benchmarked *slower* (random TEXT keys scatter
  inserts across the B-tree, causing page splits) and larger; their only real advantage —
  collision-free IDs for multi-device merge — buys nothing while sync is a non-goal
  (§7) [R26][R27].
- **Trade accepted.** If merging two databases or multi-device sync ever become
  real, integer IDs from two databases can collide. Mitigation if that day comes: SQLite
  makes re-keying a one-script job (`UPDATE … SET id = id + offset` in FK-off
  transaction), or add a nullable `uuid` column then. We do not pay for it now.
- **Multi-writer clarification (cross-review).** The owner's plan is multiple *clients*
  through one controlled service — a React app, an MCP layer for AI agents, a mobile
  client — all writing via the app's API. That is multiple processes or connections,
  not multiple divergent databases: WAL + `busy_timeout` serializes concurrent writers
  to one file safely, integer IDs stay correct, and the "single writer" rule
  (principle #3) is understood as *single writing application*, not single process.
  What remains out of scope: devices holding divergent local copies that merge
  (CRDT territory — cr-sqlite/vlcn-style tooling is currently beta-grade and its
  column-level machinery would violate "schema is the documentation"). If that day
  comes, nullable `uuid` + origin-device columns are the additive escape hatch; capture
  identity at write time is the only part that cannot be reconstructed later.
- **Sources.** [R4][R26][R27].

- **Addendum (round 7, registries).** "Every table" is not literally true: `lifelog_meta`,
  `link_kinds`, `currencies` and `fx_rates` are registries and reference data keyed by their
  natural key (`key`, `kind`, `code`, `(from_ccy, to_ccy, day)`) — an integer surrogate would
  only hide the name. The rule holds for every entity, fact and join table (§2.3).

### D4 — Text ownership: the database is canonical. *(Round 12: the markdown-export half is withdrawn, §7.)*

- **Context.** This was the hardest decision. *Files canonical*: any app, plugin, or future tool
  pointed at the folder becomes a legitimate writer of canonical data → dialect drift and
  corruption. Evidence: the Logseq↔Obsidian ecosystem needs dedicated conversion tools (journal
  filename formats `YYYY_MM_DD` vs `YYYY-MM-DD`, URL-encoded filenames, block-reference syntax,
  property formats, task statuses) [R29][R30][R31][R32]. *DB canonical*: the original Taskdesk
  fear — meaning trapped in the app.
- **Decision.** `pages.body` in SQLite is the single source of truth for prose. No folder of
  files holds canonical text, and nothing may write canonical data except this app (and later
  its CLI/API). The ability to leave rests on the file format itself (D1) and on the schema being
  its own documentation (principle 2).
- **Why not files-canonical with discipline (linters + git as recovery net)?** Git is a
  *recovery* net, not a *guard* — it requires noticing damage after the fact. The
  owner's own multi-app history (Obsidian, Logseq, Trilium, each leaving residue) is
  direct evidence that the discipline requirement fails in practice. Keeping the database
  canonical removes the trust requirement structurally.
- **Why not files-canonical with a single writer (only our app writes files)?** It keeps
  the open-format benefit, but inherits every engineering complaint Logseq documented
  when they *split their product in two* over this exact axis: live editing rewrites
  whole files per keystroke-batch; renaming a page must rewrite every referencing file;
  files lack persistent IDs and timestamps [R34][R35][R36]. Logseq DB (SQLite canonical)
  is the side chosen by a team that hit the live-editing requirement head-on [R34][R36].
- **Costs accepted.** Prose is edited only through this app's UI/CLI/API.
- **Withdrawn in round 12.** This decision used to include a nightly, derived, git-versioned
  markdown mirror of the prose. It is out of scope while the schema is made reliable (§7 says
  when to reopen); nothing in the schema depends on it.
- **Sources.** [R29]–[R32], [R34]–[R36].

### D5 — One `pages` table for all prose; journal dropped; memos = journal + inbox.

- **Decision.** A single text entity with `kind IN ('memo','note','wiki')` *(now `('memo','page')`: addendum 5)*. The owner's
  product intent: memos are a Twitter/Memos-style capture stream that serves *both* as
  the journal (the day's record) *and* as an inbox (capture now,
  triage later). There is **no journal entity and no daily-page row**: the day page is a
  query over `pages.day` (+ events/tasks/measurements for that day, §6.2). This is the
  "journal emerges from the stream" model. **Tags are the same mechanism**: `#health`
  is simply the wiki page `health` referenced as `[[health]]` — one graph, one syntax,
  no separate tag system. *(How a tag is recognised, and that the body is never rewritten
  to `[[health]]`: D19.)* `note` and `wiki` pages carry a capture `day` (local date,
  like memos) so "notes touched that day" is a first-class query, and note/wiki titles
  share one unique index (`pages_title`) so every page has an unambiguous
  name.
- **Inbox mechanism = one column.** `triaged_at TEXT NULL` on memos:
  - keep-as-memory → set `triaged_at`;
  - needs action → create task/note + `links(kind='spawned', from=task, to=memo)` + set
    `triaged_at` (provenance preserved);
  - junk → tombstone.
  Inbox view = `kind='memo' AND triaged_at IS NULL AND deleted_at IS NULL`.
- **Alternatives.**
  - *Separate `journal` kind / one daily page row*: rejected — a page you must not forget
    to create, and two capture paths. The day view (§6.2) reconstructs
    the classic journal page from the stream anyway.
  - *Separate `inbox` table or status workflow column*: rejected — a status machine is
    the 80% solution to a 20% problem; one nullable timestamp distinguishes
    untriaged/triaged and nothing else is needed until proven otherwise.
  - *Zero-column inbox ("recent memos are the inbox")*: rejected — it makes the inbox
    view either unbounded history or an arbitrary time window.
- **Sources.** Kaydet (9 years of daily entries as plain text + SQLite index) [R41];
  FxLifeSheet (capture-friction minimization) [R9][R42]; Memos-style capture is the
  owner's stated interface preference.

- **Addendum (review round 3, titles).** `note`/`wiki` titles are now unique
  case-insensitively (`pages_title` uses `COLLATE NOCASE`, matching `metrics.name`, and
  `places.name` likewise), because titles are file names and `Diet` /
  `diet` collide on case-insensitive filesystems. Titles must be trimmed and 1–200
  characters; a memo must **not** have a title (a titled memo is invisible to the
  title index and to §6.3). `pages.kind` remains mutable, but the CHECKs are re-evaluated
  on every UPDATE, so a kind change is only accepted if the row satisfies the new kind's
  rules. Path characters in titles were left to the app (§2.5).

- **Addendum 2 (review round 3b, filename-safe titles; `kind` is fixed).** The first
  addendum left path characters to the app and capped titles at 200 characters;
  both are superseded. The DDL now rejects path-unsafe titles itself (§2.5 lists the
  rules), and the length limit is 240 **bytes** (a 200-character title of 4-byte
  characters is 800 bytes — over the filesystem limit). Verified: every forbidden
  character, control characters 1/9/10/13/31/127, NUL, `.hidden`/`..`/trailing dot,
  `CON`/`nul`/`COM1`/`lpt9` rejected; `CONSOLE`, `com10`, `LPT0`, `Café notes`, `日本語 ノート`
  and 240-byte titles accepted; 243 bytes rejected. `pages.kind` **never changes after
  insert** (`pages_kind_fixed` trigger; a no-op `SET kind = kind` is allowed): a memo
  that deserves to be a note becomes a *new* note linked `kind='spawned'`, exactly as
  triage already works. This replaces the first addendum's "kind remains mutable" —
  the CHECKs alone let `note → wiki` silently drop the day requirement and `note → memo`
  leave an orphan title. **Known limit:** case-insensitive uniqueness is ASCII-only
  (§2.5). *(Superseded by addendum 3.)*

- **Addendum 3 (review round 3c, Unicode-proof uniqueness; titles are immutable).**
  Addenda 1–2 made uniqueness case-insensitive with `COLLATE NOCASE`, which folds only
  ASCII: `Café notes` and `CAFÉ NOTES` (and NFC vs NFD spellings of the same name) were
  distinct rows that collide as files on macOS/Windows. Now: `pages.title_key` holds
  `NFC(casefold(NFC(title)))` (function and test vectors in §2.5), `pages_title` is a
  unique index on it, and `NOCASE` no longer applies to titles. The DB verifies the
  parts it can (key present iff titled, trimmed, no ASCII capitals, `= lower(title)` for
  ASCII titles) and cannot verify a non-ASCII fold — that is the writing application's
  duty. Rejected: an ICU/custom collation (breaks writes and `integrity_check` for every
  reader lacking it — §7); ASCII-only titles (a life log has `日本語` and `Zürich` in it);
  leaving it to a check at the moment a file is written (a symptom fix). Titles are **immutable** (`pages_title_fixed`): D5's "renames are
  forbidden" was a convention and is now enforced; the sanctioned path is new page +
  `#REDIRECT` stub. `title_key` is derived and updatable, `title` is not. *Scope:*
  `places.name` and `metrics.name` remain ASCII-`NOCASE` — they are never filenames, and
  `Zürich`/`ZÜRICH` as two places is a data-quality issue, not a collision (§8 #5).

- **Addendum 4 (round 10, the titles stay; device names).** (1) The independent review (R4-19 a)
  argued that titles carry filename rules and immutability only so that *derived* file names stay
  simple, and that id-named files would free them. Reconsidered and **kept**: the rules are tested
  (D19, record #10), and the owner asked for them after round 3b.
  The cost is real and stated: the title CHECKs are unnamed, so loosening one after the freeze is a
  table rebuild, not an `ALTER` — a reason to settle it now, and it is settled. *Reopen only if* a
  title you actually want is forbidden (`Re: plan`) often enough to hurt. (2) R8-01 is fixed in the
  DDL: a Windows device name is rejected **before the first `.`** as well as bare, and the six
  superscript names (`COM¹ … LPT³`) are listed [R58]. Old vs new CHECK on 43 458 strings: exactly
  the 12 strings of that class flip from accepted to rejected, none flips the other way, none changes
  outside the class (§8 #12).

- **Addendum 5 (round 11, notes and wiki pages are one kind).** `note` and `wiki` differed in three
  things only: `day` (required for a note, optional for a wiki page), whether the page shows in the day
  view (§6.2), and whether the ghost sweep counts it. They already shared one title index, one set of
  title rules and one `[[link]]` namespace. The
  split also leaked: a `[[link]]` to a title that does not exist yet creates the page as `wiki` (§6.14),
  and `kind` and `title` are immutable — so anything linked before it was written became a wiki page
  whatever it was meant to be, and a wrong choice could be undone only by a new page and a redirect stub.
  Now `kind IN ('memo','page')`. *Dated is a property, not a type:* the app sets `day` on a page the owner
  creates on purpose and leaves it NULL on a page it creates as a link target, so the day view (§6.2)
  shows what was **written** that day, not what was **mentioned**. A ghost that is written later keeps
  `day = NULL`, as a wiki page did (`*_day` is written at insert, never recomputed, §2.2). `pages_title`
  is now `UNIQUE … WHERE title_key IS NOT NULL` (memos have no key) and the `kind` predicate is gone from
  every lookup — an equality on the key implies the index's predicate (executed, §8 #13); the `SCAN` that
  round 3 saw was for a predicate on `kind`, which a lookup by key does not imply. Memos stay separate: untitled
  (capture without friction), the inbox column, never the target of a
  `[[link]]`. A category of pages (essay, reference) is a tag, and a tag is a page. One behaviour is new:
  an empty page created on purpose, with a day, that nothing links to, is listed by `ghost_pages` after 30
  days, as a link target always was — a note never was; the view only lists, tombstoning stays the owner's
  act. *Cost:* no data
  (no canonical database exists); after real data exists, merging two kinds would mean rewriting `kind`
  on every row against `pages_kind_fixed` — this is the cheap moment. *Reopen only if* a need appears
  that a tag or `day IS NOT NULL` cannot serve.

- **Addendum 6 (round 12, the title rules stay without the export).** The filename rules of addenda
  2–4 were introduced so that a title could name a file, and the code that writes those files is out of
  scope for now (§7). The rules stay unchanged, for the reasons already recorded: they are tested (D19,
  record #10); loosening one later is a table rebuild because the title CHECKs are unnamed, while a title
  that is valid everywhere never has to be renamed (D5) if files return. *Reopen only if* a title you
  actually want is forbidden (`Re: plan`) often enough to hurt — the trigger of addendum 4.

### D6 — Mood: the `mood` metric in `measurements`, not a column on `pages`.

- **Decision.** Mood is a time series like any other: a seeded metric
  `('mood', '', '1-5')` whose rows are appended to `measurements`. When a mood is
  attached to a memo, the measurement row carries `entity_id` = the memo's id, so
  the provenance link costs nothing (the column already existed). "Mood over time"
  is one query over one table (§6.4).
- **History.** The first draft of this document proposed
  `pages.mood INTEGER CHECK (mood BETWEEN 1 AND 5)`. The cross-review (§8) reopened
  it; the owner chose the metric home so a standalone mood tap (no memo text) needs
  no second mechanism, and mood charts uniformly with every other series. The 1–5
  range is now app-level validation on one metric row, not a schema CHECK.
- **Alternatives.**
  - *`pages.mood` column*: rejected — splits the concept across two tables the
    moment a text-less mood tap happens, and forces a join for series queries.
  - *Both (column + metric)*: rejected — two homes for one concept is the classic
    drift failure; pick one.
- **Rule.** One home per concept, forever: mood lives in `measurements` and nowhere
  else.

### D7 — Measurements: one FxLifeSheet-shaped table + tiny metric registry; append-only.

- **Decision.**
  - `metrics(id, name UNIQUE, unit, notes)` — a registry whose only job is keeping series
    canonical ('weight' is one series forever). Seeded rows, extensible; UI should make
    create-on-the-fly painless (suggest + confirm).
  - `measurements(metric_id, day, taken_at?, value REAL, source, import_id?, entity_id?,
    supersedes_id?)` — one row per data point. **Append-only**: values are never UPDATEd;
    a correction is a new row with `supersedes_id` → the old row. Reads take
    "latest non-superseded per (metric, day)" — now codified as the
    `measurement_values` VIEW so every future reader uses the same rule (cross-review,
    open question 5). `import_id` + partial UNIQUE index make bulk re-imports
    idempotent via `INSERT OR IGNORE` *(now `ON CONFLICT … DO NOTHING` — addendum 2)*. A trigger (added in cross-review) rejects a
    correction whose `supersedes_id` points at a row of a *different* metric — the one
    supersede invariant that could silently corrupt a series. `entity_id` is documented
    as provenance ("captured with this memo/event"), not "about this person": the owner
    is the only subject of measurements. `metrics.name` is UNIQUE with COLLATE NOCASE
    so 'Weight' and 'weight' cannot become two series.
- **This is the most battle-tested part of the design.** FxLifeSheet's actual schema
  (verified from `db/create_tables.sql` in the repo) is a single `raw_data` table —
  `timestamp, yearmonth/yearweek/year/quarter/month/day/hour/minute, key, question,
  type, value TEXT, matcheddate, source, importedat, importid` — carrying 380k data
  points over 6+ years with zero schema drama [R9][R42]. Open Brane independently runs
  one append-only table with `(source, actor)`-keyed idempotent writes at 942k rows [R43].
  We keep three of their devices: `import_id` idempotency, denormalized local `day`,
  `source` provenance.
- **Deliberate simplifications vs. prior art** (all listed in §7 as deferred):
  - `value REAL` only — no text-valued measurements. Charting stays trivial; prose
    observations belong in memos/notes with a link.
  - No LOINC/UCUM/reference-range columns (health-mcp has them [R8]) — a personal
    registry with free-text `unit` is the 20%; standards matter for export/interop, not
    local storage.
  - No raw/normalized two-tier wearable mirror (health-mcp [R8]) — build the raw tier
    only when a second data source actually exists.
  - No multi-resolution rollups (Myome's minute/hour/day aggregates [R45]) — at ~50 MB/yr
    of sensor data and ~5 GB/lifetime, raw queries are fine for decades; SQLite computes monthly
    aggregates in milliseconds at this volume.
- **Sources.** [R8][R9][R42][R43][R45].

- **Addendum (review round 3, append-only is now enforced; one correction per row).**
  D7 said measurements are append-only and that reads take "latest non-superseded per
  (metric, day)". Both were conventions the DDL did not enforce, and the view does not
  literally deduplicate per day. Corrected: (1) triggers reject every `UPDATE` and
  `DELETE` on `measurements`; (2) a partial `UNIQUE` index on `supersedes_id` allows at
  most one correction per row (chain corrections by correcting the correction), and
  because `supersedes_id` can only be set at insert and must name an existing row,
  chains cannot form cycles; (3) `measurement_values` means exactly "rows nothing has
  corrected" — two independent readings on one day are both legitimate and both
  returned (average or pick in the query, not the view); (4) the same-metric trigger uses
  `IS NOT` so a dangling `supersedes_id` is rejected even with `foreign_keys=OFF`;
  (5) the unique index doubles as the index the view's `NOT EXISTS` needs — without it
  the view was quadratic (measured: 14 s at 20 000 rows; 0.004 s with the index). Also
  added: `measurements(day)` for the day view. Accepted cost: a wrong `source` or
  `entity_id` cannot be edited in place either — correct by superseding.

- **Addendum 2 (round 5: retraction, audit time, import key).** Executed findings R4-02/03/06/10
  (§8): (1) a correction whose `value` is NULL **retracts** the row it corrects —
  `value` is nullable, `CHECK (value IS NOT NULL OR supersedes_id IS NOT NULL)` keeps a first
  reading honest, and `measurement_values` hides retractions (re-entry = correct the
  retraction); (2) `recorded_at` (NOT NULL) says when a row was written, apart from `taken_at`;
  (3) the import key is `(source, import_id, metric_id)` — two importers can no longer collide —
  and importers use `ON CONFLICT … DO NOTHING`, superseding the `INSERT OR IGNORE` above.

### D8 — One `entities` supertype + one polymorphic `links` graph; symmetry in-DB.

- **Decision.** The five linkable types share one ID space through `entities`; all
  relationships of every kind live in a single `links(from_id, to_id, kind)` table with
  real foreign keys. `kind` is free text — unconstrained in `links` itself, but
  auto-registered in the `link_kinds` registry table on first use. The registry's one
  structural job: a `symmetric` flag. An AFTER INSERT trigger mirrors any row whose
  kind is symmetric (`A→B` also stores `B→A`), so a half-edge can never exist —
  whatever the writer (app, CLI, API, agent). Backlink queries stay trivial
  (`WHERE to_id = ?`) for symmetric kinds because the mirror row exists.
- **Alternatives.**
  - *No supertype; discriminator pairs* (`from_kind TEXT, from_id INT`): rejected — no
    foreign keys, so edges can dangle silently forever. The whole point of putting the
    graph in the DB is integrity.
  - *Per-relationship tables* (`friendships`, `attendance`, `page_links`, …): rejected —
    N tables and N code paths for one concept ("these two things are related"), and
    "everything about X" becomes a union over an open-ended set.
  - *CHECK-list on `links.kind`*: rejected — relationship taxonomy is personal and grows
    unpredictably ('godmother', 'college-roommate'); each addition would need a
    migration. Structural enums (`entities.type`, `pages.kind`, `tasks.status`) ARE
    constrained, because those are architecture, not taxonomy. The line: **constrain
    structure, leave taxonomy free.**
  - *Symmetry as discipline (store one edge, query with `from_id = ? OR to_id = ?`)*:
    rejected — every graph query must remember the OR (miss once, lose half the
    graph), and nothing stops two writers storing the same pair in both directions.
  - *Symmetry as double-write in the app (mirror rows, no trigger)*: rejected — with
    multiple writers planned (API, CLI, agents), any future writer can silently
    create a half-edge. The trigger makes the invariant structural, exactly like the
    `updated_at` triggers.
  - *Separate `pending_links` table for unresolved wikilinks (no auto-create)*:
    rejected — splits the graph across two tables and creates a second source of
    truth for one concept; see D5's ghost-page policy instead.
- **Rationale.** The graph is where a life database earns its keep: ark's entire "killer
  feature" is its SQLite edge tables (doc↔doc, doc↔person, person↔person) answering
  "everything about my son" in one query [R46]. One `links` table serves all product
  needs with one index: wiki backlinks (`kind='wikilink'`, asymmetric), life
  relationships (`kind='family'/'friend'/…`, symmetric), provenance (`kind='spawned'`
  from memo triage, `kind='attended'` for events, `kind='subtask'`, `kind='redirect'`
  for renames). `UNIQUE(from_id, to_id, kind)` allows multiple relationship kinds
  between the same pair but forbids duplicate edges (and makes the trigger's mirror
  insert idempotent).
- **Operational note.** If a kind's symmetry flag is flipped 0→1 after edges exist,
  run the documented backfill (`INSERT OR IGNORE` the mirrors for that kind); the
  trigger only fires on new inserts.
- **Sources.** [R46][R43].

- **Addendum (review round 3, the registry is closed; mirrors on delete).**
  D8 said `kind` is "auto-registered on first use". That let a typo (`Friend`) register
  a permanent asymmetric kind, and a kind later flagged symmetric never got its missing
  mirror rows. Now: `links.kind` is a foreign key to `link_kinds`, so an unregistered
  kind is rejected; registering a kind is a deliberate `INSERT INTO link_kinds`
  (a data row, not a schema change — the taxonomy is still open-ended, just never
  implicit); kind names must be lowercase `[a-z0-9_-]`; `symmetric` is immutable
  (`BEFORE UPDATE` trigger) — to change a kind's symmetry, register a new kind. This
  supersedes the "Operational note" above (backfill on flag flip) — flips are now
  impossible. `links` rows are immutable in `from_id`/`to_id`/`kind` (delete and
  re-insert); deleting one side of a symmetric edge deletes its mirror. Both mirror
  triggers terminate under `PRAGMA recursive_triggers=ON` (verified §8 #3): the insert
  mirror uses `INSERT OR IGNORE`, so its re-fire finds the row present and stops; the
  delete mirror finds nothing left to delete. **(Superseded by addendum 2 below: endpoint
  types are now constrained.)** *Originally not constrained:* endpoint types
  (`subtask` between two pages is accepted); this is knowingly left to the app and
  reopens if a bad edge is ever found in real data. Cycle prevention for `subtask`
  stays app-level, so §6.11 caps its depth. Seeded `lives-in` and `visited` (D16
  used them).

- **Addendum 2 (review round 3b, endpoint types are now constrained; closed even
  with foreign keys off).** The first addendum left endpoint types to the app and is
  superseded on that point. `link_kinds` gained `from_types` / `to_types` (NULL = any
  entity type, else a comma list from `entities.type`), and a `BEFORE INSERT` trigger on
  `links` checks both endpoints — including the mirror rows of symmetric kinds, which
  must therefore be valid in both directions (`CHECK (symmetric = 0 OR from_types IS
  to_types)`). Seeded: `wikilink`/`redirect` page→page; `spawned` task|page→page;
  `subtask` task→task; `attended` person→event; `about` any→person|place;
  `lives-in` person→place; `visited` person|event→place; `friend`/`family`
  person↔person; `related` any↔any. A kind's structure (symmetry *and* endpoint types)
  is immutable once registered (`link_kinds_structure_fixed`); to change it, register a
  new kind. A misspelt type token fails **closed** (every link of that kind is
  rejected), and an unknown endpoint id counts as type `'?'`. The same trigger also
  rejects an **unregistered kind** itself — the FK alone let one through on a connection
  with `foreign_keys=OFF` (executed in autocommit; `PRAGMA foreign_keys` is a no-op inside
  a transaction). What still depends on `foreign_keys=ON` (mandatory, §2.9): dangling
  endpoint ids of *untyped* kinds (`related`, `about`'s source) and every other FK.
  Cycles remain unconstrained by the schema (§6.11 caps its walk).

- **Addendum 3 (round 4, a sixth entity type).** `entities.type` gains `'account'` (D18), so
  the shared ID space now holds six types; the constraint is named `entities_type` (see D18,
  "why now"). The seeded `about` kind now targets `person,place,account`, so a memo or an event
  can be *about* an account.

- **Addendum 4 (round 5, small changes).** Seeded `located-in` (place → place, so "everything in
  Japan" is answerable, §6.19) and `parent-of` (person → person, direction kept; `family` stays
  the symmetric catch-all). `links_immutable` and `link_kinds_structure_fixed` gained a `WHEN`
  guard, so a full-row `UPDATE` that changes only `note` passes (R4-17).

- **Addendum 5 (round 6, seeds).** `lives-in` is removed and `visited` is person → place only
  (D16 addendum): an event's place is `events.place_id`, never a link.

### D9 — Binary files: DEFERRED out of v1. The design is kept here for the day it returns.

- **Decision (v1, cross-review).** No `attachments` table, no `media/` directory, no
  binary files in the system at all. Media was a second thought for the owner, and the
  "if it needs to be outside, it's not part of this system" framing made the cut the
  honest one: the entire design below is **perfectly additive later** (a future
  `attachments` migration touches nothing else, needs no backfill, and nothing in v1
  references it). Until then, a memo that needs a file references it in prose.
- **The deferred design (unchanged, for the record).** Binary files live in `media/`,
  named by SHA-256; `attachments` rows carry `(entity_id, sha256, ext, mime, size)`;
  path = `media/<sha256[0:2]>/<sha256><ext>`, derived, never stored. Dedup is
  automatic (same bytes → same file). No inline BLOBs: SQLite's own benchmarks put
  the break-even around 100 KB [R2][R3]; Microsoft Research's "To BLOB or Not To
  BLOB" agrees [R47]. Keeping bytes out keeps `life.db` megabyte-scale for decades.
  ark (700k items: files out, sha256-named, metadata in) and Open Brane both
  converged here [R46][R43].
- **The cut we are explicitly NOT making elsewhere.** No GC ever: orphans accumulate,
  tombstoned entities keep their files, and a documented one-query sweep
  (hashes in DB vs directory listing) exists for the day it matters.
- **Reopen trigger.** An actual attachment need appears (photos in journal entries,
  scanned documents, PDFs on events). Then: additive migration, the design above
  verbatim, plus `CHECK (length(sha256)=64)` and a fixed `ext` convention.
- **Sources.** [R2][R3][R43][R46][R47].

### D10 — Time model: UTC instants + denormalized local days, both TEXT.

- **Decision.** See §2.2. ISO-8601 UTC TEXT for instants; local `YYYY-MM-DD` TEXT for
  days, written at insert, never recomputed.
- **Alternatives.**
  - *Instants only, local day computed at query time*: rejected — a timezone move or DST
    rule silently rewrites history ("June 3" becomes "June 2" for anything stored near
    midnight). This is exactly why FxLifeSheet needed a dedicated `tag_days` importer to
    reconstruct local dates after the fact [R7], and why health-mcp denormalizes the
    local date at write time [R8].
  - *Unix epoch / Julian day integers*: faster and smaller, but unreadable in the file
    and in ad-hoc queries — violates principle #2. SQLite's own docs list TEXT ISO-8601
    as the canonical date representation [R5][R6].
  - *SQLite `CURRENT_TIMESTAMP` defaults*: rejected — second precision, non-ISO format
    (space separator, no Z); a documented footgun [R28].
- **Sources.** [R5][R6][R7][R8][R28].

- **Addendum (round 6, zone and completion day).** (1) `entities.tz` and `measurements.tz` hold
  the IANA zone name of the writer when the row — for measurements, `taken_at` — was captured
  (`NULL` = unknown). A UTC instant alone cannot say whether `22:30Z` was 14:30, 22:30 or 07:30 the
  next morning (R4-04), and only capture time can supply the zone, which is why it could not wait
  for a migration. The DB checks the shape (1–64 characters of `A-Za-z0-9_/+-`), not that the name
  is a real zone; readers convert with a tz database. `events` deliberately have no `tz` (owner
  decision, KISS): an event's `start_at` is read in the creating writer's `entities.tz`, a guess
  for a trip planned from elsewhere, and a *recurring timed* event still has no defined local time
  across DST — a known limit. (2) `tasks.completed_day` (local; paired with `status = 'done'` and
  `completed_at`) makes "what I finished on day X" a plain lookup instead of the query-time
  UTC-to-local derivation this decision forbids; §6.2 gained a `'done'` row.

### D11 — Deletion: tombstones, never hard deletes.

- **Decision.** Deleting an entity sets `entities.deleted_at`. Reads filter
  `deleted_at IS NULL`. No row is ever physically removed by the app.
- **Rationale.** In a biography database, *erasure is itself biographical*: in 20 years
  it should be possible to see what the 2027 version of the owner deleted, and when.
  Hard deletes also break the `links` graph (FK violations or silently dangling
  relationships) and undermine the audit story of D12. Storage cost of keeping everything
  is irrelevant at this scale.
- **Alternatives.** Hard delete + `ON DELETE CASCADE` (destroys evidence, cascades
  surprises); trash-with-expiry (a policy layer — can be added later *on top of*
  tombstones without schema change; the data layer is already there).
- **Exception.** `measurements` are never deleted at all — not even tombstoned. They are
  corrected via `supersedes_id` (D7). This is how medical records think, and it makes the
  health history tamper-evident for free *(overstated — D11 addendum 3)*.
- **Task semantics (cross-review).** Tasks are *fleeting* entities: `status` is
  `open | done` — there is no `dropped` status. Abandoning a task is erasure (a
  tombstone), not a recorded decision, and a completed task stays completed forever.
  `CHECK ((status = 'done') = (completed_at IS NOT NULL))` keeps the two columns honest.
- **Known asymmetry (owner-settled, Option A).** `links` rows are hard-deleted — no
  tombstone, no audit. Wikilink removal is even *required* (the body is the truth,
  §2.5: re-extraction on save must delete dangling rows or the table fights its own
  source of truth). Authored links (`friend`, `family`, …) are deleted with them, which
  was weighed explicitly against a split policy (derived rows hard-deleted, authored
  rows tombstoned, policy encoded in `link_kinds`) and **rejected**: the owner does not
  need "who was in my life when" as a structured query — the *evidence* (memos,
  `attended` events) survives anyway, and relationship links are a summary over that
  evidence. Consequences, accepted knowingly: relationship removal is invisible — the
  row is gone and nothing records that it existed. The
  split policy remains additive-later (a `deleted_at` column + partial unique index +
  one `derived` flag in `link_kinds`), and `sqlite-history` triggers [R48] are the
  documented retrofit if relationship erasure ever needs to be auditable. `attachments`,
  if D9 ever returns, inherits this same rule.

- **Addendum (review round 3).** The `measurements` exception above is now enforced by
  triggers (D7 addendum). The known `links` hard-delete asymmetry stands (owner-settled);
  note that deleting a symmetric edge now removes both directions.

- **Addendum 2 (review round 3b).** Tombstoning and un-tombstoning now bump
  `entities.updated_at` (`entities_touch`, watching `deleted_at` only, so it cannot
  re-fire itself — verified with `recursive_triggers=ON`): "what changed most recently"
  is one column again, and `updated_at` equals `deleted_at` for a fresh tombstone.

- **Addendum 3 (round 5, deletes are enforced).** "No row is ever physically removed by the
  app" was a convention for entities (executed: `DELETE FROM pages` then `DELETE FROM entities`
  worked, and a freed newest id was reused). Now `BEFORE DELETE` triggers on `entities` and the six
  domain tables reject it; the `entities` trigger also holds on a connection with
  `foreign_keys=OFF` and for orphan entity rows. "Tamper-evident for free" above overstated it:
  the triggers guard against mistakes, not against a writer that drops them. An `entities` row
  with no domain row (a writer bug) is still insertable — see R4-07 in §8.

### D12 — Audit trail: no revision tables. *(Round 12: git-over-export and nightly snapshots are withdrawn, §7.)*

- **Decision.** The schema contains **no revision or history tables**. The only in-DB temporal
  metadata is row-level: `entities.created_at` (written by the app, never back-dated, §2.2) and
  `updated_at` (trigger-maintained); the `deleted_at` tombstone (D11); and `recorded_at` on the
  append-only `measurements` and `balances` (measurements also `taken_at` and `tz`), whose
  corrections are new rows, not overwrites (D7, D18).
- **Alternatives.**
  - *Full revision snapshots per edit* (the predecessor design): rejected — an app-level
    versioning system is significant code to build and maintain, for a history nobody has asked to
    query.
  - *Trigger-based history tables* (e.g., Simon Willison's `sqlite-history` pattern —
    triggers log every INSERT/UPDATE/DELETE with JSON diffs into a companion table):
    rejected **for now**, but this is the documented fallback: it retrofits onto the
    current schema with no redesign if a real need appears (e.g., wanting intra-day
    history of structured rows) [R48].
- **Cost accepted.** An `UPDATE` to a mutable row (a page body, a task, a person) overwrites the
  old value, and nothing recovers it.
- **Addendum (round 7, durability, write transactions).** A commit must survive power loss, so
  connections use `synchronous = FULL` (§2.9). SQLite documents that with `NORMAL` in WAL "a
  transaction committed … might roll back following a power loss" [R54]; measured here, `FULL` is ~1 ms
  per commit against ~0.1 ms (btrfs, 500 memo commits, §8 #9) — invisible for a journal. Every write
  transaction starts with `BEGIN IMMEDIATE` (§2.9).
- **Withdrawn in round 12.** This decision used to add prose history by git over a nightly markdown
  export, and database history by nightly `VACUUM INTO` snapshots with retention, verification, an
  off-box copy, a restore procedure and a CSV dump (§2.8, D12 addendum 2). All of it is out of scope
  while the schema is made reliable; §7 says when to reopen, and §2.8 keeps only the checks that
  read the file itself.
- **Sources.** [R48][R54].

### D13 — Migrations: numbered plain SQL + `PRAGMA user_version`; freeze-and-migrate.

- **Decision.** See §2.7. No ORM, no migration framework, no down-migrations. Forward
  only. **Until the freeze, there are no migrations at all:** the v1 schema is edited in
  place (§3 is the canonical DDL; at freeze it becomes `db/migrations/0001_init.sql`)
  and any test database is
  recreated from scratch; `user_version` stays 1. Numbered files and the additive-only
  policy begin **after** the first real data is imported. **After the freeze, all changes
  are additive** (new tables,
  new columns, new indexes); column renames via `ALTER TABLE … RENAME COLUMN` are allowed
  and must be recorded in the migration file with a comment explaining the rename (the
  migration history is the dictionary of meaning changes).
- **Rationale.** This is the convergent simplest practice for SQLite projects: a
  `user_version` pragma in the file header, an array of `.sql` files applied in order —
  multiple independent write-ups implement it in under 100 lines and report no need for
  more [R20][R21][R22]. SQLite's compatibility essay explicitly blesses additive change
  as the mechanism by which schemas evolve without breaking old meaning [R1].
- **Down-migrations** are rejected as a category. A migration is applied to a *copy* of the file
  first (`VACUUM INTO`, as for an importer, §2.11) and the three checks of §2.8 must pass on the
  copy before it touches `life.db`; a migration that fails on the copy is fixed, never reversed.

- **Addendum (round 5, named CHECKs make "additive" true).** Widening an enum after the
  freeze (a new `pages.kind`, entity type, `repeat` value) is a two-statement transactional
  migration — `ALTER TABLE … DROP CONSTRAINT <name>; … ADD CONSTRAINT <name> CHECK (…)` — **only
  because every enumerated CHECK is now named**; verified for all eight on a populated database with
  integrity and foreign-key checks clean (§8 #7), on SQLite 3.53.4. The migration runner must use
  such a SQLite; the older claim of a "one-line migration" (§8, question 9) was false for the
  unnamed DDL.

### D14 — UI: thin custom app for capture/browse; off-the-shelf tools for exploration.

- **Decision.** Build only what the product needs: a Memos/Twitter-style capture
  composer (with mood — a `mood` measurement in the same transaction, D6), a day view,
  an inbox triage view, simple metric charts
  (sparklines/line charts from `measurements`), forms for events/tasks/people/places, a
  search box over `pages_fts`, and a backlinks panel. For ad-hoc exploration, browsing raw
  tables, and running SQL: **Datasette** pointed at `life.db` (instant table browsing,
  faceting, SQL console, JSON/CSV export, zero code) [R49], optionally `sqlite-web` when
  direct row editing is wanted [R50] *(dropped — it is a second writer; addendum)*.
- **Rejected.** Building a generic object-browser/admin UI — Datasette already is one,
  is maintained by someone else, and reads any SQLite file this schema produces. This is
  the single biggest UI-side Pareto cut: the custom surface shrinks to data entry and
  the day view.
- **Corollary (schema consequence):** because a third-party tool will read the file, the
  schema must stay readable without the app — reinforcing D2 (real column names) and the
  conventions header embedded in §3's init DDL.
- **Sources.** [R49][R50].

- **Addendum (round 7, one writer).** `sqlite-web` "when direct row editing is wanted" is
  dropped: it can insert, update and delete rows — a second writer that bypasses the entity-row-first
  and `title_key` conventions, against principle 3 and D3. Exploration tools open the file
  read-only (§2.9); Datasette does so by default and its SQL console accepts only `SELECT`
  (executed on 0.65.5, §8 #9). Editing goes through the app. D14's list of what the app builds
  (composer, day view, inbox, forms) already covers row editing.

### D15 — Recurrence: structured `repeat_*` columns, occurrences expanded at read.

- **Decision.** `events` and `tasks` carry six readable recurrence columns:
  `repeat` (`none|daily|weekly|monthly|yearly`), `repeat_every` (NULL = 1; 13 with
  `daily` = every 13 days, 3 with `monthly` = quarterly), `repeat_weekdays` (weekly
  only, e.g. `'mo,we,fr'`), `repeat_position` + `repeat_weekday` (monthly only:
  `'last'` + `'fr'` = last Friday of the month), `repeat_until` (inclusive local day,
  NULL = open-ended). A repeating row is a *template*; occurrences are computed at
  read time with a recursive CTE (§6.12) — never materialized. Birthdays are *not*
  recurrence: they are a one-line query over `people.birth_day`. Recurring *habits*
  (daily meditation) are *not* events: they are 0/1 `measurements` on a habit metric —
  the FxLifeSheet pattern, charts for free.
- **Task recurrence semantics.** Completing a repeating task ends the series
  (`completed_at` set, `repeat_until` = the completion day by convention); the app may
  spawn the next-occurrence task via `links(kind='spawned')`. "Edit only this
  occurrence" is deliberately unsupported — split the series instead (end the first
  with `repeat_until`, create a second).
- **Alternatives.**
  - *RFC-5545 RRULE text column*: rejected — a cryptic string in a column is exactly
    the "meaning lives in app code" failure this schema exists to prevent; unreadable
    cold in 2075, needs a parser to answer "does this occur on June 3?".
  - *Materialized occurrence rows* (Google Calendar's shape): rejected at this scale —
    drags in the three-way edit problem (this instance / this and future / all) and a
    regeneration job, for a calendar that is mostly non-recurring.
  - *A separate `recurrences` table*: rejected — six columns used by exactly two tables
    don't justify a join and a second ID space.
- **Research basis.** The calendar literature is unanimous on the fork (store the rule
  and expand on read vs materialize instances) [R51][R52]; the structured-interval
  shape follows the classic practitioner designs (e.g. SQL Server Agent's
  `freq_type/freq_interval` family) [R52].
- **Sources.** [R51][R52].

- **Addendum (review round 3, recurrence hardening and the expander).**
  - *Payload CHECKs.* `repeat_every` only with a repeating kind and ≥ 1;
    `repeat_until` only on a repeating row (and ≥ `start_day` for events);
    `repeat = 'weekly'` **iff** `repeat_weekdays` is present, and the value must be
    a strictly formatted list of lowercase two-letter tokens (`'mo,we,fr'` — anything
    else used to insert silently and then never recur); a repeating **task** must have
    a `due_day` (its expansion anchor; `due_day` plays `start_day`'s role).
  - *Week semantics.* "Every Nth week" counts **calendar weeks (Mon–Sun)** from the
    week containing `start_day`. The first draft counted 7-day blocks from `start_day`,
    which put a Wednesday-start series' following Monday in week 0.
  - *Month arithmetic.* SQLite's `'+N month'`/`'+N year'` **normalise** rather than
    clamp (`date('2026-01-31','+1 month')` = `2026-03-03`; `date('2024-02-29','+1 year')` =
    `2025-03-01`), so "the 31st" and Feb-29 series are computed by matching
    `min(start day-of-month, last day of that month)` (clamping: Jan 31, Feb 28, Mar 31).
  - *Duration.* A recurring event with `end_day` keeps its duration per occurrence
    (§6.12 returns `occ_start` and `occ_end`).
  - *One expander.* The four per-kind CTEs of the first draft are replaced by a single
    window-bounded query (§6.12). The first draft's weekly/monthly/daily CTEs had five
    independent defects (unbounded walk to year 9999, occurrences before `start_day`,
    `repeat_until`/`:end_day` not applied, `repeat_every` ignored for monthly, no
    monthly-same-day or yearly expansion at all) — none were caught because the
    validation records checked that queries ran, not that they were right.

- **Addendum 2 (round 7, repeating tasks are reminders).** Two statements above did not fit the
  rest of the schema. (1) *"The app may spawn the next-occurrence task via `links(kind='spawned')`"*:
  D8 addendum 2 makes `spawned` task|page → page only (a task or note born from a memo), so a
  task → task `spawned` link is rejected (executed). The next occurrence is simply a new task;
  if a link is wanted, use the symmetric `related` (any ↔ any). (2) *Completing a repeating task
  ends the series* — so a repeating task cannot record "done this month, not that month": it is
  a **reminder** (`repeat_until` = the `completed_day` by convention), not a checklist. For "did I
  do it each month" use a 0/1 habit measurement (this decision, first paragraph), whose history
  charts for free; for a one-off follow-up create a normal task.

### D16 — Places: a fifth entity type.

- **Decision.** `places(id, name UNIQUE, notes)` joins `entities` as a linkable type;
  `events.place_id` references it; links can connect anything to a place
  (`kind='lives-in'`, `kind='visited'`). Promoted from the v1 cut list after the
  owner confirmed place-centric queries ("everything in Japan 2019", "days spent in
  Berlin") are likely — free-text `place` would make those impossible and backfilling
  10 years of free text is the painful path.
- **Alternatives.** Free-text `events.place` (rejected — the queries above fail); a
  `places` table *outside* the entity supertype (rejected — places deserve graph
  links and tombstones like everything else; that is what the supertype is for).
- **Sources.** [R46] (ark's place nodes).

- **Addendum (round 6, one home for an event's place).** An event has exactly one place,
  `events.place_id`. The `visited` link kind is person → place only (it was person|event) and
  `lives-in` is removed — it was undated, so "where did I live in 2015" was unanswerable; a dated
  event (start and end day, `place_id`) answers it, and §6.19 rolls cities up into countries.

### D17 — The contract as data: `lifelog_meta`, date GLOBs, and in-DB semantic guards.

- **Decision.** A tiny `lifelog_meta(key, value)` table, seeded at init, carries the
  storage contract (time formats, rebuildability of FTS, rename convention, recurrence
  semantics) as *queryable data* rather than comments — comments are invisible to
  `SELECT *` and stripped by some tooling. Alongside it, the contract is enforced where
  CHECK constraints can reach: every `*_day` column must pass
  `date(col) = col` (format *and* calendar validity — '2026-13-45' and '2026-9-3' are
  rejected), every `*_at` column a GLOB on the ISO-8601 shape. The supertype gains
  `UNIQUE(id, type)` and every domain table a constant `entity_type` column + composite
  FK, so a row's type and its domain table can never disagree and one id can never live
  in two domain tables (the cross-review's B2/B3 findings — now impossible).
- **Rationale.** Principle #2 ("the schema is the documentation") deserves mechanism,
  not prose. These are one-time costs, zero per-write cost, and they make the 2075 test
  (`.schema` + the data itself, cold) actually pass.
- **Alternatives.** Comments only (status quo ante — invisible to queries); a
  documentation wiki (lives outside the artifact, rots).

- **Addendum (review round 3).** Two statements above are corrected: (1) day columns
  are checked with `date(col) IS col`, not `date(col) = col` (`=` silently accepts
  malformed dates — §2.2, §8), and instants with the `strftime(...) IS col` round-trip,
  not a GLOB; the only GLOBs in the DDL are the `repeat_weekdays` format guard and the
  `link_kinds.kind` character guard. (2) The contract table now also records the
  measurement, link-kind and title rules (`lifelog_meta`).
- **Sources.** none needed — all claims here were executed (§8 #3).

- **Addendum 2 (round 4, finance raises the stakes of plaintext).** With D18 the file holds a
  wealth history, not only prose. The decision stands (no database encryption) but the
  threat model it rested on is restated: (1) the finance tables never enter git, whose history
  cannot be scrubbed (§2.1, §2.10); (2) the disk holding `life.db` must be encrypted at rest
  (full-disk encryption), because the file itself will not be; (3) Datasette listens on localhost only and opens the
  file read-only (`?mode=ro`, its default for a mutable database — verified, §8 #9); nothing that can
  run arbitrary SQL from a browser is exposed to a network; (4) no credentials or full account numbers, ever (§2.10).

- **Addendum 3 (round 10, the 2075 test is now a test).** The contract as data claimed to let a stranger
  understand the file, but nothing checked it. §2.11 lists 20 questions and the `lifelog_meta` keys that
  must answer them; `tests/schema/r10probes.py` runs the table. It found four things a stranger could not
  learn from `.schema` — how an entity row relates to its domain row, how wikilinks and tags become links,
  who may write and with what settings, and how the schema evolves — and they are now keys (`entities`,
  `wikilinks`, `writers`, `evolution`; 23 rows in all). A new contract rule now needs a row in the table
  and a key in the DDL.

- **Addendum 4 (round 12, two keys withdrawn).** Round 10 added six keys; two of them — `export` and
  `backups`, with questions 17 and 18 — described the export folder and the snapshot / restore contract,
  and went with them (§7, §8 #14). The rule and its test are unchanged: every key answers some question,
  every question is answered from `lifelog_meta` alone.

### D18 — Money: accounts, balances, currencies, FX. Net worth is derived. *(Round 4)*

- **Context.** The owner wants financial data in the same lifelong database — first of all
  *net worth over time* and entries like it (what an account, a house, a loan was worth on a
  day). Money has three properties the rest of the schema does not: it must be **exact**, it
  has a **currency** (several, over 50 years — and currencies are redenominated: the DEM became
  the EUR at a fixed rate), and its history is **audited** — a wrong balance must be corrected
  visibly, not overwritten.
- **Decision.**
  - **Exact integers.** An amount is an `INTEGER` count of minor units of the owning account's
    currency. `currencies(code, name, subunits)` is a closed registry (`subunits` = minor units
    per whole unit: EUR 100, JPY 1, BTC 10⁸; any integer, so a 1/5 subunit like MRU works) and
    `subunits` is immutable (`currencies_subunits_fixed`) — changing it would silently rescale
    every stored amount. The seed holds eight common codes; adding one is a deliberate `INSERT`.
  - **`accounts` — a sixth entity type.** Anything with a balance or value: bank account,
    brokerage, pension, cash, property, vehicle, loan, mortgage, card. Columns: `name` (unique,
    NOCASE, like `places`), `side` (`asset|liability`), `currency`, free `category`,
    `institution`, `opened_day`, `closed_day` (inclusive), `notes`. `side` and `currency` are
    immutable (`accounts_meaning_fixed`, with a `WHEN` clause so ORMs' full-row updates work).
    Being an entity gives accounts the graph (`about` links from memos and events), the tombstone
    and `updated_at` for free. Record your **own share** of joint holdings.
  - **`balances` — append-only snapshots.** `(account_id, day, amount, recorded_at, source,
    import_id, note)`. `day` is the local as-of date (end of day; note Beancount asserts at the
    *start* of a date [R56]); `recorded_at` is when it was written. `UPDATE`/`DELETE` are
    rejected. **The newest row per `(account_id, day)` wins**, and a row with a NULL `amount`
    **retracts** that day — the two operations that `measurements` cannot do (§8 R4-06). Read
    through `balance_values`. Import idempotency is `UNIQUE(source, import_id)` used with
    `ON CONFLICT … DO NOTHING`.
  - **`fx_rates` — reference data.** `(from_ccy, to_ccy, day, rate)` with `from_ccy < to_ccy`
    enforced, so one pair can never carry two contradictory rates; `rate` is `REAL` because a
    rate is a ratio, not money (canonical amounts stay integers; converted figures are derived).
    Mutable and re-importable — it is public reference data, not a personal fact.
  - **Net worth is derived, never stored** (§6.16–6.17): per live, open account the latest
    balance on or before the day, × the rate *as of the reporting day*, assets minus
    liabilities. The reporting currency is a query parameter, so the history can be re-stated
    in any currency; a missing rate yields an explicit NULL/`unconverted` count instead of a
    silent gap.
- **Why now (before the freeze).** Adding `'account'` to `entities.type` later is a table
  rebuild unless the CHECK is *named*: SQLite 3.53.4 supports `ALTER TABLE … DROP CONSTRAINT` /
  `ADD CONSTRAINT`, but only for a constraint that has a name — an unnamed inline `CHECK`
  reports `no such constraint`, and adding a looser second CHECK does not relax the first (both
  apply). Verified, including that a named constraint can be dropped and re-added on a populated
  database with the foreign keys and `integrity_check` intact (§8 #6). So D18 names
  `entities_type`, `accounts_side` and `currencies_subunits`; naming every other enum CHECK is
  finding R4-08.
- **Alternatives rejected.**
  - *Money as `measurements`* (one metric per account, `value REAL`): rejected. `REAL` drifts
    (`0.1 + 0.2` executed: `0.30000000000000004`); the unit lives on the metric, not the row, so a
    currency cannot vary or be looked up per account; there is no retraction (R4-06); an account is
    not linkable; and `INSERT OR REPLACE` can rewrite it (R4-01). Storing integer-valued floats
    would be exact up to 2⁵³, but nothing in the file says so — principle 2 fails.
  - *Decimal as TEXT / `NUMERIC`*: rejected — SQLite has no decimal type. Executed: TEXT
    decimals sort as strings (`'10.25'` before `'9.5'`) and their `SUM` is a float
    (`0.30000000000000004`); a `NUMERIC` column stores every decimal as REAL
    (`'1234567890123456.78'` came back as `1234567890123456.8`).
  - *One signed column and no `side`*: rejected — a loan typed positive by mistake flips the
    sign of net worth silently; `side` makes the meaning explicit and immutable, amounts stay as
    statements print them.
  - *Store net-worth totals directly*: rejected — loses the drivers and cannot be re-stated in
    another currency. The one legitimate use, importing a spreadsheet of totals, is a
    pseudo-account closed when detailed accounts begin (§6.17).
  - *`supersedes_id` chains for balances*: rejected — `balances` has a natural key
    `(account, day)` (measurements do not: several readings a day are valid), so "newest row
    wins" is simpler and cannot be ambiguous. Two correction shapes exist for two shapes of data.
  - *Full double-entry ledger now* (`transactions` + `postings`, hledger/GnuCash shape):
    deferred, §7 — it is a second product (categories, transfers, splits, importers) and it is
    additive later: a `transactions` table would reference `accounts`, and `balances` would become
    its reconciliation points.
  - *Accounts outside the entity supertype*: rejected — no links, no tombstone, and (see "why
    now") no additive way to change that after freeze.
- **Costs accepted.** A balance carries forward until replaced, so a stale account keeps
  counting (`stale_days`, §6.18, exist to expose that); FX rates must be maintained by hand or
  by an importer, per pair, for the reporting currency you use (no triangulation through a pivot);
  there is no return/attribution analysis (needs flows — §7); joint holdings are
  recorded as your share, not modelled as co-ownership.
- **Sources.** [R53][R54][R55][R56][R57].

- **Addendum 1 (round 5: stocks, crypto, deposits, valuables — KISS).** The owner asked for net
  worth to include these and *nothing more* (no ledger). One rule covers all of them: **an account's
  balance is its value in its own currency on a day** — a deposit's statement balance, a
  portfolio's or a crypto wallet's market value, an estimate for a house or a watch (table in §2.10).
  No schema was added for this; only the `category` vocabulary and guidance, and the BTC seed row
  was dropped from `currencies` (crypto is valued in fiat like everything else).
  *Considered and deferred:* tracking quantity × price per holding. Tested against v1.5, it does not
  fit `currencies`/`fx_rates`: the code CHECK rejects `V`, `ZM`, `BRK.B`, `VWCE.DE`; a price must be
  stored as its reciprocal whenever the ticker sorts after the reporting currency (`from_ccy <
  to_ccy`); a ticker-to-ticker "rate" is accepted; and a US-listed stock priced in USD cannot be
  valued in EUR without a second hop. Doing it properly means a units registry, a `prices` table and
  longer queries — real weight for a number the statement already prints. Reopen when the owner
  wants automatic repricing or allocation by security; the additive path is a `securities` +
  `prices` pair and a nullable `accounts.security_id`, and nothing in v1.6 blocks it.

### D19 — The wikilink save contract: one transaction, links follow the body, a bad target never blocks a save. *(Round 8)*

- **Decision.** Saving a page body is one `BEGIN IMMEDIATE` transaction that writes the body and
  makes the page's `links(kind='wikilink')` rows equal to the pages the body names — adding,
  and deleting, rows — with each target resolved or created inside its own `SAVEPOINT` (§6.14).
  What is read (CommonMark text only), what a wikilink and a tag are, stubs, self-links,
  tombstones and the known limits are in §2.5, with test vectors. **An invalid target makes no link
  and never blocks a save.** The schema did not change: the DDL of §3 is byte-identical to v1.8.
- **Why now.** R4-12, executed on v1.8: the auto-created page for `[[Health/Diet]]`, `[[Re: plan]]`
  and `[[Target|alias]]` is rejected by the filename CHECK; a writer that swallows the error and
  commits leaves an orphan `entities` row, and one that does not loses the memo. Four neighbouring
  gaps were found while writing it down: no cookbook block wrote a `wikilink` row (the §6.1 memo
  mentioned `[[Lifelog]]` and linked nothing); "upserts" left a link behind after the body dropped
  it; read literally, the tag rule turns a stub's `#REDIRECT` into a page called `REDIRECT`; and
  §6.5 listed `redirect` rows although §2.5 said backlink queries exclude them.
- **Alternatives.**
  - *A `pending_links` table for targets that do not resolve*: rejected in D8's alternatives, and
    not needed — the body is the record of an unresolved mention.
  - *Make the database skip a bad target* (a trigger that swallows the page insert, or a title
    CHECK loose enough for any `[[text]]`): a CHECK cannot skip a row, and a title the filesystem
    cannot hold is exactly what the CHECK exists to stop (D5 addendum 2).
  - *A regular expression over the raw body*: rejected — it cannot tell code, URLs and raw HTML
    from prose without re-implementing a CommonMark parser. Reading the parser's text nodes gives
    all the exclusions with no grammar to maintain.
  - *Expand `#health` into `[[health]]` in the body* (what §2.5 used to say): rejected — it rewrites
    the owner's text, cannot be told from a hand-written `[[health]]`, and adds nothing the
    extraction does not already do.
  - *Obsidian-style `[[Page#Heading]]` / `^block` targets*: rejected — `#` is legal in a title
    (`[[C#]]`); `|alias` is kept because `|` can never be in a
    title, so it is unambiguous.
  - *Keep a stub's wikilink and exempt only its tag*: rejected — the stub would show up as a
    backlink of its own replacement, duplicating the `redirect` edge.
- **Costs accepted.** The contract lives in the one writing application (principle 3). The database
  checks what it can — endpoints are pages (`links_endpoint_types`), titles are filename-safe and
  unique by key — but not that `links` matches the bodies. That drift is detectable and repairable:
  a rebuild from the bodies gives the same links as 400 incremental random edits (§8 #10). A skipped
  target is not remembered anywhere except in the body's own text.
- **Sources.** [R58] [R59]; R4-12 (§8).

---

## 6. Query cookbook

Proof that the schema serves the product with plain SQL. `:named` are bind parameters.
All examples assume the reader filters tombstones (`e.deleted_at IS NULL`). Every write
example starts its transaction with `BEGIN IMMEDIATE` (§2.9).

### 6.1 Capture a memo (the universal insert convention)

Every entity insert is two statements in one transaction: `entities` first, then the

domain row reusing the id. The domain row's `entity_type` is constant per table (the
composite FK relies on it).

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(type, created_at, updated_at, tz)      -- tz: the writer's IANA zone right now
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'Europe/Berlin');
INSERT INTO pages(id, kind, day, body)
VALUES (last_insert_rowid(), 'memo', '2026-09-29',
        'Shipped the schema doc. Review pending. [[Lifelog]]');
-- the body names [[Lifelog]]: the link sync of §6.14 runs here, inside this same transaction
-- optional mood, attached to the memo it belongs to (D6):
INSERT INTO measurements(metric_id, day, value, source, entity_id, recorded_at)
SELECT id, '2026-09-29', 4, 'manual', last_insert_rowid(), strftime('%Y-%m-%dT%H:%M:%fZ','now')
  FROM metrics WHERE name = 'mood';      -- (a timed reading also sets taken_at and tz)
COMMIT;
```

### 6.2 The day view ("journal page" — it's a query, not a table)

Memos for the day, pages written that day, events occurring that day, open due tasks,
and measurements (read through `measurement_values`, so corrected readings never show).
Undated items (all-day events, tasks, untimed readings) sort first, then everything
chronologically — an explicit `ORDER BY (at IS NOT NULL), at`, because a bare
`ORDER BY at` sorts NULLs first *by accident* in SQLite.

```sql
SELECT what, at, detail FROM (
  SELECT 'memo' AS what, e.created_at AS at, p.body AS detail
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.day = :day AND p.kind = 'memo' AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.updated_at > e.created_at THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.day = :day AND p.kind = 'page' AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'event', ev.start_at, ev.title
    FROM events ev JOIN entities e ON e.id = ev.id
   WHERE e.deleted_at IS NULL AND ev.repeat = 'none'
     AND ev.start_day <= :day AND coalesce(ev.end_day, ev.start_day) >= :day
  UNION ALL
  SELECT 'task', NULL, t.title
    FROM tasks t JOIN entities e ON e.id = t.id
   WHERE e.deleted_at IS NULL AND t.status = 'open' AND t.repeat = 'none'
     AND t.due_day <= :day
  UNION ALL
  SELECT 'done', t.completed_at, t.title                     -- what was finished that LOCAL day
    FROM tasks t JOIN entities e ON e.id = t.id
   WHERE e.deleted_at IS NULL AND t.completed_day = :day
  UNION ALL
  SELECT m.name, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id
   WHERE me.day = :day
)
ORDER BY (at IS NOT NULL), at;   -- undated items (all-day events, tasks, untimed readings) first, then chronological
```

**Recurring events** join through the §6.12 expander. A recurring multi-day event that
began before `:day` must still show, so expand a window that starts early enough and
keep occurrences still running on `:day`:

```sql
-- :start_day = :day minus the longest recurring-event duration; :end_day = :day
--   SELECT date(:day, '-' || coalesce((SELECT max(julianday(end_day) - julianday(start_day))
--                                        FROM events WHERE repeat <> 'none' AND end_day IS NOT NULL), 0) || ' days')
-- run §6.12 with those two bounds, then keep rows WHERE occ_end >= :day
```

"Pages touched that day" = pages *written* that day (a page has a `day`; a link target the app created has none, D5
addendum 5), flagged if edited since;
deriving an *updated*-day from the UTC instant at query time is deliberately not done
— D10. Undated open tasks deliberately do NOT appear in every day view — they live in
the task list, not the journal. Recurring tasks are expanded the same way as events,
with `due_day` as the anchor (§6.12). Completing a task sets `status`, `completed_at` and the
**local** `completed_day` together (`UPDATE tasks SET status = 'done', completed_at = …,
completed_day = :day WHERE id = :task_id`); the `'done'` row above is "what I finished today"
without any UTC-to-local conversion at read time (D10).

### 6.3 The inbox

```sql
SELECT p.id, e.created_at, p.body
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.kind = 'memo' AND p.triaged_at IS NULL AND e.deleted_at IS NULL
 ORDER BY e.created_at DESC;
```

### 6.4 Mood over time

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'
 ORDER BY me.day;
```

(`measurement_values` returns every row nothing has corrected; with no corrections on
mood rows it is simply the series. Several mood taps on one day are all returned.)

### 6.5 Backlinks to a page

```sql
SELECT l.kind, e.type, l.from_id,
       COALESCE(pg.title, ev.title, t.title, pe.name, pl.name,
                substr(pg.body, 1, 40)) AS label   -- memos: first line of body
  FROM links l
  JOIN entities e ON e.id = l.from_id AND e.deleted_at IS NULL
  LEFT JOIN pages   pg ON pg.id = l.from_id
  LEFT JOIN events  ev ON ev.id = l.from_id
  LEFT JOIN tasks   t  ON t.id  = l.from_id
  LEFT JOIN people  pe ON pe.id = l.from_id
  LEFT JOIN places  pl ON pl.id = l.from_id
 WHERE l.to_id = :page_id
   AND l.kind <> 'redirect';   -- a rename stub is not a mention of its replacement (§2.5)
```

For symmetric kinds the mirror row (D8) makes this one direction sufficient.

### 6.6 Everything about a person (the ark query)

Symmetric kinds are mirrored, so `to_id` finds them; asymmetric kinds put the person
on either end (`attended` is person → event, `about` is entity → person), so query
both directions:

```sql
SELECT l.kind, e.type, l.from_id AS other_id, 'in' AS direction
  FROM links l JOIN entities e ON e.id = l.from_id
 WHERE l.to_id = :person_id AND e.deleted_at IS NULL
UNION ALL
SELECT l.kind, e.type, l.to_id, 'out'
  FROM links l JOIN entities e ON e.id = l.to_id
 WHERE l.from_id = :person_id AND e.deleted_at IS NULL;
```

Both directions are index-served: `links_to` for `to_id`, the `UNIQUE(from_id,to_id,kind)`
index for `from_id`.

### 6.7 Metric series, corrections applied (weight, last 90 days)

```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN metrics m ON m.id = me.metric_id AND m.name = 'weight'
 WHERE me.day >= date(:day, '-90 day')
 ORDER BY me.day;
```

(`measurement_values` is the in-schema view: every row that no later row corrects —
every reader gets the same rule, §5 D7. Two legitimate readings on one day are both
returned; aggregate in the query if a daily value is wanted.)

### 6.8 Full-text search

```sql
SELECT p.id, p.kind, p.title,
       snippet(pages_fts, 1, '<b>', '</b>', '…', 24) AS ctx
  FROM pages_fts
  JOIN pages p    ON p.id = pages_fts.rowid
  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
 WHERE pages_fts MATCH :query
 ORDER BY rank;
```

### 6.9 Triage: memo → task (provenance preserved)

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(type, created_at, updated_at)
VALUES ('task', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'));
INSERT INTO tasks(id, title, due_day)
VALUES (last_insert_rowid(), 'Book dentist appointment', :due_day);
-- last_insert_rowid() is still the new task's id: no statement has intervened
INSERT INTO links(from_id, to_id, kind, created_at)
VALUES (last_insert_rowid(), :memo_id, 'spawned', strftime('%Y-%m-%dT%H:%M:%fZ','now'));
UPDATE pages SET triaged_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = :memo_id;
COMMIT;
```

### 6.10 Correct a wrong measurement (append-only)

```sql
-- never UPDATE the value; supersede it:
INSERT INTO measurements(metric_id, day, taken_at, value, source, supersedes_id, recorded_at)
VALUES (:metric_id, :day, NULL, 71.4, 'manual', :wrong_row_id, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
-- rejected if :wrong_row_id belongs to a different metric, does not exist, or
-- was already corrected once (correct the correction instead) — D7 addendum

-- a row that should never have existed (a mis-tap): RETRACT it — a correction with a NULL value.
-- measurement_values then hides both rows; to bring a value back, correct the retraction.
INSERT INTO measurements(metric_id, day, value, source, supersedes_id, recorded_at)
VALUES (:metric_id, :day, NULL, 'manual', :mistaken_row_id, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
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
SELECT t.title, subtree.depth
  FROM subtree JOIN tasks t ON t.id = subtree.id
  JOIN entities e ON e.id = t.id AND e.deleted_at IS NULL
 WHERE subtree.id <> subtree.root OR subtree.depth = 0
 ORDER BY subtree.depth;
```

Endpoint types are enforced (`subtask` is task→task only — D8 addendum 2), but cycle
*prevention* is app-level: never link a task to its own ancestor. Without the depth cap, a two-task
cycle below the root never terminated (verified); with it, a cycle repeats at most 32
levels and the query still returns.

### 6.12 Occurrences of recurring events and tasks (one expander, D15)

The `repeat_*` columns carry enough *readable* information to answer "what occurs in
this window?" with date arithmetic — no RRULE parser, no materialized rows. One
window-bounded query covers every repeat kind: it walks the days of
`[:start_day, :end_day]` (never further) and joins each day to the recurring events
whose rule matches it. Each occurrence returns `occ_start` and `occ_end` (a multi-day
event keeps its duration). Tasks use the same query with `tasks`, `due_day` as the
anchor and no `end_day`/duration columns.

```sql
WITH RECURSIVE days(day) AS (
  SELECT :start_day
  UNION ALL
  SELECT date(day, '+1 day') FROM days WHERE day < :end_day
)
SELECT e.id, e.title, d.day AS occ_start,
       date(d.day, '+' || CAST(julianday(coalesce(e.end_day, e.start_day)) - julianday(e.start_day) AS INTEGER) || ' days') AS occ_end
  FROM days d JOIN events e
    ON e.repeat <> 'none'
   AND d.day >= e.start_day
   AND d.day <= coalesce(e.repeat_until, :end_day)
   AND (
        -- daily: every N days from start_day
        (e.repeat = 'daily'
         AND CAST(julianday(d.day) - julianday(e.start_day) AS INTEGER) % coalesce(e.repeat_every, 1) = 0)
        -- weekly: chosen weekdays, every Nth CALENDAR week (Mon-Sun) counted from start_day's week
     OR (e.repeat = 'weekly'
         AND (',' || e.repeat_weekdays || ',') LIKE
             '%,' || substr('su,mo,tu,we,th,fr,sa', CAST(strftime('%w', d.day) AS INTEGER) * 3 + 1, 2) || ',%'
         AND CAST((julianday(d.day) - julianday(date(e.start_day, '-' || ((CAST(strftime('%w', e.start_day) AS INTEGER) + 6) % 7) || ' days'))) / 7 AS INTEGER)
             % coalesce(e.repeat_every, 1) = 0)
        -- monthly, same day of month (clamped to the month's last day)
     OR (e.repeat = 'monthly' AND e.repeat_position IS NULL
         AND ((CAST(strftime('%Y', d.day) AS INTEGER) * 12 + CAST(strftime('%m', d.day) AS INTEGER))
            - (CAST(strftime('%Y', e.start_day) AS INTEGER) * 12 + CAST(strftime('%m', e.start_day) AS INTEGER)))
             % coalesce(e.repeat_every, 1) = 0
         AND CAST(strftime('%d', d.day) AS INTEGER) =
             min(CAST(strftime('%d', e.start_day) AS INTEGER),
                 CAST(strftime('%d', date(d.day, 'start of month', '+1 month', '-1 day')) AS INTEGER)))
        -- monthly, Nth weekday ('first'..'fourth', 'last')
     OR (e.repeat = 'monthly' AND e.repeat_position IS NOT NULL
         AND ((CAST(strftime('%Y', d.day) AS INTEGER) * 12 + CAST(strftime('%m', d.day) AS INTEGER))
            - (CAST(strftime('%Y', e.start_day) AS INTEGER) * 12 + CAST(strftime('%m', e.start_day) AS INTEGER)))
             % coalesce(e.repeat_every, 1) = 0
         AND d.day = CASE e.repeat_position
             WHEN 'last' THEN date(d.day, 'start of month', '+1 month', '-1 day',
                  '-' || ((CAST(strftime('%w', date(d.day, 'start of month', '+1 month', '-1 day')) AS INTEGER)
                           - (instr('su,mo,tu,we,th,fr,sa', e.repeat_weekday) / 3) + 7) % 7) || ' days')
             ELSE date(date(d.day, 'start of month', 'weekday ' || (instr('su,mo,tu,we,th,fr,sa', e.repeat_weekday) / 3)),
                  '+' || (7 * (CASE e.repeat_position WHEN 'first' THEN 0 WHEN 'second' THEN 1
                                                      WHEN 'third' THEN 2 ELSE 3 END)) || ' days')
             END)
        -- yearly (Feb 29 clamps to Feb 28 in non-leap years)
     OR (e.repeat = 'yearly'
         AND (CAST(strftime('%Y', d.day) AS INTEGER) - CAST(strftime('%Y', e.start_day) AS INTEGER))
             % coalesce(e.repeat_every, 1) = 0
         AND strftime('%m', d.day) = strftime('%m', e.start_day)
         AND CAST(strftime('%d', d.day) AS INTEGER) =
             min(CAST(strftime('%d', e.start_day) AS INTEGER),
                 CAST(strftime('%d', date(d.day, 'start of month', '+1 month', '-1 day')) AS INTEGER)))
   )
 ORDER BY d.day, e.id
```

Semantics, all executed and cross-checked (§8 #3):

- `daily` / `repeat_every`: every N days from `start_day`.
- `weekly`: the listed weekdays, every Nth **calendar week (Mon–Sun)** counted from the
  week containing `start_day`; the first occurrence is never before `start_day`.
- `monthly` without a position: the same day of the month, **clamped** to the month's
  last day (the 31st → Jan 31, Feb 28, Mar 31, Apr 30); `repeat_every=3` = quarterly.
- `monthly` with `repeat_position` + `repeat_weekday`: `first`…`fourth` or `last`
  weekday. "Last" steps *backward* from the month's last day by
  `(last_day_w − target_w + 7) % 7` — SQLite's `'weekday N'` only steps forward. Months
  are also filtered by `repeat_every`.
- `yearly`: same month and day, clamped (a Feb 29 start returns Feb 28 in common years).
- `repeat_until` is inclusive and applied to the *occurrence*, not the month.
- SQLite traps this query is written around: `strftime` has **no `%a`** (weekday names come
  from a lookup string indexed by `%w`); `'+N month'`/`'+N year'` normalise instead of
  clamping; `||` binds **tighter** than `*`, `/` and `+`, so every arithmetic
  sub-expression next to a `||` is fully parenthesised — dropping a paren fails
  silently (`'a' || 7/3` is `0`), not loudly.
- A window costs O(days in window × recurring events): a one-month window over 16
  recurring events ran in 0.0005 s.

### 6.13 Ghost pages (the wikilink sweep, D5)

```sql
SELECT p.id, p.title, e.created_at
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.kind = 'page' AND p.body = '' AND e.deleted_at IS NULL
   AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
   AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')
   AND NOT EXISTS (SELECT 1 FROM links l WHERE l.from_id = p.id);
```

Live in the schema as the `ghost_pages` view; the UI surfaces it as a cleanup list.
Renaming never creates ghosts (D5: redirects), so the sources are capture-time typos and a
mention later edited out of a body (step 4 of §6.14 drops its link, the empty page stays).

### 6.14 Save a body with wikilinks (resolve or create each target, sync the links)

The app reads the body (§2.5) and gets a list of distinct valid targets, each with `:title` (first
spelling, NFC) and `:key` (`title_key(:title)`). Everything below — the body write and the link
sync — is **one `BEGIN IMMEDIATE` transaction** (§2.9): two writers that save the same new link
cannot both see "none found" — the second waits, then finds the first one's page. Each target is
its own `SAVEPOINT`, so a target that fails for any reason is rolled back alone (no link, no orphan
`entities` row) and the save carries on (D19).

```sql
BEGIN IMMEDIATE;
-- 0) the body itself: the INSERT of §6.1, or  UPDATE pages SET body = :body WHERE id = :page_id;

-- for each target (skip a target whose :key is the page's own title_key):
SAVEPOINT target;
-- 1) resolve (an equality on title_key implies the partial unique index's predicate, so it is used)
SELECT p.id, p.title, e.deleted_at
  FROM pages p JOIN entities e ON e.id = p.id
 WHERE p.title_key = :key;

-- 2a) found, but tombstoned: revive it
UPDATE entities SET deleted_at = NULL WHERE id = :found_id;
-- 2b) none found: create the empty page (no day: a link target is not something written today)
INSERT INTO entities(type, created_at, updated_at)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'));
INSERT INTO pages(id, kind, title, title_key) VALUES (last_insert_rowid(), 'page', :title, :key);

-- 3) link it (:target_id is :found_id, or last_insert_rowid() after 2b)
INSERT INTO links(from_id, to_id, kind, created_at)
VALUES (:page_id, :target_id, 'wikilink', strftime('%Y-%m-%dT%H:%M:%fZ','now'))
ON CONFLICT(from_id, to_id, kind) DO NOTHING;
RELEASE target;   -- on any error in 1-3:  ROLLBACK TO target;  RELEASE target;  and go on

-- 4) once, after the last target: drop the links the body no longer names
--    (:target_ids is a JSON array of the ids linked in step 3, '[]' when there are none)
DELETE FROM links
 WHERE from_id = :page_id AND kind = 'wikilink'
   AND to_id NOT IN (SELECT value FROM json_each(:target_ids));
COMMIT;
```

`[[cafe\u0301 NOTES]]` and `[[Café notes]]` produce the same `:key`, so both resolve to the one page;
`title` keeps the spelling of whoever created it (in NFC). Without step 4 a link outlives the text
that made it, and without the `SAVEPOINT` a rejected target leaves its `entities` row behind — the
two defects of v1.8 (R4-12, §8 round 8).

### 6.15 Open an account; record, correct and retract a balance (D18)

Amounts are **integer minor units** of the account's currency (`currencies.subunits` per
whole unit): €12,345.67 is `1234567`. Opening an account is the universal two-statement
entity insert (§6.1); recording a balance is one row. Nothing is ever edited or deleted.

```sql
BEGIN IMMEDIATE;
INSERT INTO entities(type, created_at, updated_at)
VALUES ('account', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'));
INSERT INTO accounts(id, name, side, currency, category, institution, opened_day)
VALUES (last_insert_rowid(), 'Main checking', 'asset', 'EUR', 'cash', 'Bank A', '2019-03-01');
COMMIT;

-- what the statement says at the end of a LOCAL day
INSERT INTO balances(account_id, day, amount, recorded_at, source)
VALUES (:account_id, '2026-09-30', 1234567, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'statement');

-- a wrong entry is never edited: write the right one for the same (account, day) — the newest row wins
INSERT INTO balances(account_id, day, amount, recorded_at, note)
VALUES (:account_id, '2026-09-30', 1234576, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'digits transposed');

-- an entry that should never have existed (wrong account, wrong day): retract it with NULL
INSERT INTO balances(account_id, day, amount, recorded_at, note)
VALUES (:account_id, '2026-09-30', NULL, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'belongs to the savings account');

-- idempotent bulk import: ON CONFLICT ... DO NOTHING skips only the duplicate. Not INSERT OR IGNORE,
-- which would also skip a row with a malformed day or a NULL where one is required — silently (§8 #6).
INSERT INTO balances(account_id, day, amount, recorded_at, source, import_id)
VALUES (:account_id, :day, :amount, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'bank_csv', :row_key)
ON CONFLICT(source, import_id) WHERE import_id IS NOT NULL DO NOTHING;

-- reading one back as a number
SELECT b.day, b.amount * 1.0 / c.subunits AS whole_units, a.currency
  FROM balance_values b JOIN accounts a ON a.id = b.account_id JOIN currencies c ON c.code = a.currency
 WHERE b.account_id = :account_id ORDER BY b.day;
```

A retraction only hides the day: the earlier days remain, so the as-of rule of §6.16 falls
back to the previous balance. Writing a correct value after a retraction simply becomes the
newest row again.

### 6.16 Net worth on a day, per account and in a reporting currency

`:day` is the local day to value, `:base` the reporting currency code (`'EUR'`). Net worth is
derived — never stored — so it can be re-computed in any currency, on any day.

```sql
WITH held AS (                                   -- open, live accounts on :day, and the day of the balance that counts
  SELECT a.id, a.name, a.side, a.currency,
         (SELECT b.day FROM balance_values b
           WHERE b.account_id = a.id AND b.day <= :day ORDER BY b.day DESC LIMIT 1) AS as_of
    FROM accounts a
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
   WHERE coalesce(a.opened_day, '0000-01-01') <= :day
     AND coalesce(a.closed_day, '9999-12-31') >= :day
)
SELECT h.name, h.side, h.currency, b.amount, h.as_of,
       CAST(julianday(:day) - julianday(h.as_of) AS INTEGER) AS stale_days,        -- how old the number is
       CAST(round((CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * b.amount
            * CASE WHEN h.currency = :base THEN 1.0
                   WHEN h.currency < :base THEN (SELECT r.rate FROM fx_rates r
                        WHERE r.from_ccy = h.currency AND r.to_ccy = :base AND r.day <= :day ORDER BY r.day DESC LIMIT 1)
                   ELSE 1.0 / (SELECT r.rate FROM fx_rates r
                        WHERE r.from_ccy = :base AND r.to_ccy = h.currency AND r.day <= :day ORDER BY r.day DESC LIMIT 1)
              END
            * (SELECT subunits FROM currencies WHERE code = :base) * 1.0
            / (SELECT subunits FROM currencies WHERE code = h.currency)) AS INTEGER) AS net_base_minor  -- NULL = no FX rate
  FROM held h
  CROSS JOIN balance_values b ON b.account_id = h.id AND b.day = h.as_of   -- CROSS JOIN pins the order: accounts first, then seek
 ORDER BY h.side, h.name;
```

The rules, all executed against an exact-arithmetic oracle (§8 #6):

- An account **counts** on `:day` if it is live (not tombstoned), open (`opened_day <= :day <=
  closed_day`, both inclusive; NULL = unbounded) and has at least one balance on or before
  `:day`. Its value is the **latest effective balance on or before `:day`** (carried forward);
  `stale_days` says how old that number is — a balance from 2019 silently counts in 2026 unless
  someone looks (§6.18 lists the accounts that need updating).
- `side` decides the sign: assets add, liabilities subtract. Balances are stored as the
  institution states them (a mortgage is a positive amount owed).
- Conversion uses the `fx_rates` row **as of `:day`**, not as of the balance's own day, so a
  2019 balance is worth what it would fetch on the reporting day. A pair is stored in one
  direction (`from_ccy < to_ccy`): the inverse is `1/rate`. `net_base_minor` is in **minor units of
  `:base`**. If no rate exists it is NULL, and a `SUM` would silently skip the row — so always
  check for NULLs (the series query counts them).
- Each row is rounded to a whole minor unit, so the sum of the rows can differ by a minor
  unit or so from the series total in §6.17, which rounds the sum.
- `CROSS JOIN` pins the join order (accounts first, then an index seek per account). Without
  it SQLite scans every balance row (measured at 36 500 rows: 0.043 s without the `CROSS
  JOIN`, 0.000 s with it).

### 6.17 Net worth over time (month-ends)

```sql
WITH RECURSIVE month_ends(day) AS (
  SELECT date(:from_day, 'start of month', '+1 month', '-1 day')
  UNION ALL
  SELECT date(day, 'start of month', '+2 month', '-1 day') FROM month_ends
   WHERE day < date(:to_day, 'start of month', '+1 month', '-1 day')
),
held AS (                                        -- every open, live account on every month-end, with its latest balance
  SELECT m.day, a.side, a.currency,
         (SELECT b.amount FROM balance_values b
           WHERE b.account_id = a.id AND b.day <= m.day ORDER BY b.day DESC LIMIT 1) AS amount
    FROM month_ends m
    JOIN accounts a ON coalesce(a.opened_day, '0000-01-01') <= m.day
                   AND coalesce(a.closed_day, '9999-12-31') >= m.day
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
)
SELECT day,
       CAST(round(sum(net)) AS INTEGER) AS net_worth_minor,   -- reporting currency, minor units
       count(*)                          AS accounts,          -- accounts that have a balance by then
       sum(net IS NULL)                  AS unconverted        -- > 0: an FX rate is missing and the total understates
  FROM (
    SELECT h.day,
           (CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * h.amount
           * CASE WHEN h.currency = :base THEN 1.0
                  WHEN h.currency < :base THEN (SELECT r.rate FROM fx_rates r
                       WHERE r.from_ccy = h.currency AND r.to_ccy = :base AND r.day <= h.day ORDER BY r.day DESC LIMIT 1)
                  ELSE 1.0 / (SELECT r.rate FROM fx_rates r
                       WHERE r.from_ccy = :base AND r.to_ccy = h.currency AND r.day <= h.day ORDER BY r.day DESC LIMIT 1)
             END
           * (SELECT subunits FROM currencies WHERE code = :base) * 1.0
           / (SELECT subunits FROM currencies WHERE code = h.currency) AS net
      FROM held h
     WHERE h.amount IS NOT NULL
  )
 GROUP BY day
 ORDER BY day;
```

`accounts` is how many accounts had a balance by that month-end; `unconverted > 0` means an FX
rate was missing for at least one of them, so that month understates (never a silent
zero). Measured over 31 years of month-ends with 40 accounts and 438 000 balance rows: 0.085 s.
The first draft of this query joined `balance_values` directly and SQLite scanned all
balances once per month — 17.3 s at 36 500 rows; the correlated scalar subquery in `held`
turns that into an index seek per (month, account).

Net worth for one day is §6.16 with a final `SELECT sum(net_base_minor), count(*) …`;
"net worth by category" is the same query grouped on `a.category`.

**Backfilling history from a spreadsheet of monthly totals** (no per-account detail): make one
account, `'Legacy net worth'` (`side = 'asset'`, `category = 'aggregate'`), record the totals as
its balances (liabilities already netted, so amounts may be negative), and set its
`closed_day` to the day the detailed accounts begin. Nothing is double-counted and the series is
continuous.

### 6.18 Accounts that need updating

```sql
SELECT a.name, max(b.day) AS last_balance,
       CAST(julianday(:day) - julianday(max(b.day)) AS INTEGER) AS stale_days
  FROM accounts a
  JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
  LEFT JOIN balance_values b ON b.account_id = a.id AND b.day <= :day
 WHERE a.closed_day IS NULL OR a.closed_day >= :day
 GROUP BY a.id
HAVING max(b.day) IS NULL OR max(b.day) < date(:day, '-35 day')
 ORDER BY stale_days DESC;
```

### 6.19 Everything inside a place (containment)

`located-in` (place → place: Tokyo → Kanto → Japan) is one-way and transitive. Walk it down
with a recursive CTE — `UNION`, not `UNION ALL`, so a mistaken cycle ends instead of looping —
then join whatever hangs off any of those places. This is D16's motivating query, "everything
in Japan 2019", for events (via `events.place_id`):

```sql
WITH RECURSIVE inside(id) AS (
  SELECT :place_id
  UNION
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id WHERE l.kind = 'located-in'
)
SELECT ev.title, ev.start_day, pl.name AS place
  FROM events ev
  JOIN entities e ON e.id = ev.id AND e.deleted_at IS NULL
  JOIN inside ON inside.id = ev.place_id
  JOIN places pl ON pl.id = ev.place_id
 WHERE ev.start_day BETWEEN :from_day AND :to_day
 ORDER BY ev.start_day;
```

An event has exactly one place, `events.place_id` — there is no second, link-based home
(D16 addendum). A trip through several cities is one event per leg, or a country-level event
plus city-level ones: this query finds all of them because containment rolls the cities up
into the country. Where someone *lived* is a dated event (start and end day, `place_id`), not an
undated link.

---

## 7. Explicit non-goals and deferred work

Researched, considered, and **deliberately cut** — recorded so they are not silently
re-added, each with the trigger that should reopen the question.

| Cut item | Why cut | Reopen when |
|---|---|---|
| Multi-device sync / CRDTs | Not a goal; would force UUIDs (D3), change-tracking columns, conflict resolution | A second device must write canonical data |
| Agent CLI/API | Planned as a later layer over the same DB; single-writer rule (§1.3) extends to it naturally | After v1 UI exists |
| Binary files / `attachments` | Cut from v1 (D9): all-text DB stays megabyte-scale; design kept in D9 | The first real photo/PDF attachment need |
| Database encryption | Deliberate plaintext (D17); protect the disk instead (full-disk encryption) | A legal/privacy requirement for at-rest encryption |
| Revision/history tables | Tombstones and append-only facts cover the need (D12); `sqlite-history` triggers are the documented fallback [R48] | Demonstrated need for intra-day history of structured rows |
| Raw wearable-import tier | health-mcp's two-tier mirror exists for provider quirks [R8]; premature with zero importers | A second data source appears, or re-import fidelity bites |
| LOINC / UCUM / reference ranges | Interop vocabulary, not storage need (D7) [R8] | FHIR export or clinical data exchange wanted |
| Multi-resolution rollups | ~5 GB/lifetime of sensor data queries fine raw (D7) [R45] | Query latency is ever noticeable |
| Text-valued measurements | `value REAL` keeps charting trivial (D7) | A real series needs non-numeric values (then: probably a note + link instead) |
| JSON columns / property bags | The core anti-decision (D2) | Never |
| Generic view system, AI generation | Cut from product scope by the owner | Product decision, not schema |
| Markdown export of the prose; nightly snapshots, restore and an off-box copy; a CSV dump; continuous replication | Withdrawn in round 12 to focus on the schema and its reliability; nothing in the schema depends on any of them (D4, D12). Until one exists there is **no second copy** of `life.db`, and the file itself is the only thing to leave with | Before the first real data enters a canonical `life.db` (the freeze, D13) at the latest; the earlier design is in git history (§8 #14) |
| Trash UI with restore/expiry | Tombstones (D11) are already the data layer | UI work; zero schema change |
| `.sqlar` single-artifact packaging | `tar` covers "one file to email" | Frequent whole-archive portability need |
| Unicode collation for titles (ICU / app-registered) | A collation only one program registers makes the DB unwritable and un-integrity-checkable for everyone else (`no such collation sequence`); the app-computed `title_key` gives the same uniqueness (D5 addendum 3) | Never, unless SQLite ships Unicode folding in the core |
| Unicode-aware uniqueness for `places.name` / `metrics.name` | Not filenames; ASCII-`NOCASE` is enough for a personal registry today | A real duplicate like `Zürich`/`ZÜRICH` appears (then: a `name_key`, same pattern) |
| Hard deletes / GDPR-style erasure | Tombstones keep everything (D11) | A legal/privacy need to truly destroy specific rows |
| Transaction ledger (income, spending, transfers), budgets, categories | A second product (splits, transfers, importers, categorisation); net worth needs only balances (D18). Additive later: `transactions` referencing `accounts`, `balances` as reconciliation points | The owner wants spending/savings-rate analysis, or a bank-feed importer exists |
| Per-holding quantity × price, cost basis / lots, dividends, returns (TWR/IRR) | Net worth needs the market *value* on a day, which the statement or app gives; quantity × price needs a `prices` table and non-currency units — v1.5 cannot even hold a ticker (executed: `V`, `ZM`, `BRK.B` fail the code CHECK; a price must be entered as a reciprocal when the ticker sorts after the reporting currency). Additive later: a `securities` + `prices` pair and a nullable `accounts.security_id` (D18 addendum 1) | The owner wants automatic repricing, allocation by security or realised/unrealised gain |
| Cross rates through a pivot currency; automatic FX import | Store the pairs you report in (D18); an importer can fill `fx_rates` | A second reporting currency, or backfilling decades of rates by hand hurts |
| Co-ownership / shares of joint accounts, multiple owners | Single-user database; record your own share (D18) | A second person needs their own view |
| Storing account numbers, IBANs, credentials | A plaintext DB makes them a liability (§2.10, D17 addendum 2) | Never in `life.db`; use a password manager |
| Partial dates (`1870`, `1870-05`) for people and events (R4-18) | Nothing asked for one yet. `birth_day`'s CHECK is unnamed, so it cannot be loosened in place (`DROP CONSTRAINT` finds no name — executed); the additive path is a nullable `birth_approx` TEXT column with a GLOB CHECK, which works on a populated STRICT table and leaves the triggers alone (executed, §8 #12) | The first ancestor or approximate date you want to record |
| Searching *inside* a CJK run (R4-15) | `unicode61`, kept, folds `é ü ș ț` but a CJK run is one token (`本語` does not find `日本語のノート`). The index is derived, so switching is one transaction — drop `pages_fts`, create it with `tokenize='trigram remove_diacritics 1'`, `rebuild` — and the sync triggers keep working (executed). Trigram finds 3+-character parts and still not two-character words (`京都`) | The first real CJK memo you cannot find |
| Typing a person's name instead of picking them (`[[Sam]]`, `@Sam`; R4-11 e) | A wikilink resolves to a page; people are linked with `about` from a picker (§6.6). A text mention needs a rule for two people called Sam. The additive path is one `link_kinds` row (`mention`, page→person) and a line in D19 — executed: accepted for a person, rejected for a place | Picking a person becomes the slow part of capture |
| The local wall-clock time of a recurring *timed* event across DST (D10 addendum) | Occurrences expand by local day; a timed recurrence is a UTC instant, so its local hour shifts across a DST change; `entities.tz` records where it was set but the expander does not use it | A recurring timed event where the hour matters |

---

## 8. Validation records and review resolutions

*Records #1–#13 describe the document as it stood when each was written, and are kept unedited (AGENTS.md).
§2.8 was then the backup contract, D4 and D12 also specified a markdown export and nightly snapshots, §2.1 had
`export/`, `backups/` and `dump/`, and `tests/backups/` ran the scripts. Round 12 withdrew all of that (#14);
read the older records with that in mind.*

### Validation record (2026-09-29, one author)

§3 was applied verbatim with the `sqlite3` CLI on **SQLite 3.53.4** (Linux):

- All tables, indexes, and triggers create cleanly; `user_version=1` and
  `application_id='LIFE'` round-trip; external-content FTS5 over a STRICT content table
  and the partial UNIQUE indexes parse and work.
- Cookbook queries §6.1–§6.9 executed against seeded data; FTS5 sync triggers kept
  `pages_fts` correct on insert and update; `supersedes_id` filtering returned only the
  corrected value.
- `updated_at` triggers verified. Cosmetic caveat: timestamps have millisecond
  precision, so an insert and an immediate update can share the same millisecond —
  timing-sensitive tests need ≥ 1 ms separation. No schema impact.
- CHECK constraints reject as designed: `memo` without `day`; `mood=7`.
- STRICT semantics confirmed as *lossless conversion*, not blanket rejection (D2
  wording corrected accordingly).
- **FK gotcha confirmed:** `PRAGMA foreign_keys` defaults to OFF per connection and
  STRICT does not enable it. With FKs off, a `links` row referencing nonexistent entity
  9999 persisted silently; with `foreign_keys=ON` it was rejected
  (`FOREIGN KEY constraint failed`). This drove the new §2.9 and the DDL header note.

### Validation record #2 (2026-09-29, independent cross-review)

The amended §3 was applied to a fresh file (SQLite 3.53.4, python `sqlite3` and CLI) and
**52/52 adversarial + positive tests passed**, covering: all B-series rejections (NULL
title wiki, type↔table mismatch, id in two domain tables, done-without-completed_at,
end<start, malformed days/instants including `2026-02-31` / `2026-9-3` / `banana` /
space-separated and second-precision instants, NOCASE metric duplicates, note/memo
without day, duplicate note titles, cross-metric supersede, recurrence CHECK matrix),
plus positives: symmetric mirroring, auto-registered new link kinds, redirect
non-mirroring, subtask links, places FK, FTS sync on insert/update, `ghost_pages`
behavior, `measurement_values`, `pages_touch`, STRICT rejections, import idempotency.

Three real bugs were found in the *reviewer's own first draft* of the amended DDL and
fixed before landing — all are of a kind that only empirical testing catches:

1. **The CHECK NULL hole.** `CHECK (date(x) = x)` evaluates to NULL (not 0) when
   `date(x)` is NULL, and **a CHECK passes on NULL** — so `date(x) = x` silently accepts
   `2026-9-3` and `2026-13-01`. The fix is `date(x) IS x` (`IS` yields 0/1, never NULL).
   Same for the instant round-trip: `strftime(...) IS x`. All round-trip CHECKs in §3
   now use `IS`; the original v1 draft's day-format constraints had the identical hole
   via `LIKE`-free text columns.
2. **`strftime` has no `%a`.** SQLite's `strftime` does not support weekday-name
   formats; `strftime('%a', d)` returns an empty string, silently. The weekly-recurrence
   cookbook query therefore uses `substr('su,mo,tu,we,th,fr,sa', strftime('%w', d)*3+1, 2)`.
3. **`'weekday N'` steps forward only.** Computing "last Friday" by walking *forward*
   from month-end to Sunday then back 2 days is wrong for months ending Tue–Thu
   (it lands outside the month). Correct: step *backward* from the last day by
   `(last_day_w − target_w + 7) % 7` days. All recurrence CTEs in §6.12 were executed
   and cross-checked against Python's `calendar`, including both Februaries — exact
   match.

Also verified: `date('2026-02-31') = '2026-03-03'` (SQLite rolls over rather than
rejecting), which the `IS` round-trip catches correctly.

### Questions — resolution after cross-review

The original reviewer questions were settled as follows (details in the decision log):

1. **Re-validation** — done; see record #2 above.
2. **Supertype FK delete behavior** — composite FKs `(id, entity_type) → entities(id,
   type)` now make the pairing structural; delete behavior is moot under tombstones.
3. **Mood** — removed from `pages`; the `mood` metric in `measurements`, each row
   optionally pointed at its memo (D6).
4. **`links.kind` registry** — adopted: `link_kinds` table with a `symmetric` flag and
   auto-registration trigger (D8).
5. **Measurement read rule** — added the `measurement_values` view (D7).
6. **`attachments` files table** — moot: attachments deferred out of v1 (D9).
7. **Index set** — `entities(type)` index cut; inbox partial index added; titles unique
   partial index added (D5).
8. **FTS triggers vs. nightly rebuild** — kept triggers (search always correct; rebuild
   remains available via the documented `INSERT INTO pages_fts(pages_fts)
   VALUES('rebuild')`).
9. **`pages.kind` list** — kept enumerated ('memo','note','wiki'); new kinds are a
   one-line migration after freeze, per the additive-only policy.
10. **Over-engineering pass** — cut: `entities.type` index, `tasks.status='dropped'`,
    `attachments` (whole table, deferred), `places` free-text column, UUIDs reaffirmed.
    The remaining flagged asymmetry is below.

### Owner veto received (2026-09-29, post-cross-review)

1. **`links` hard-delete asymmetry (B11) — settled: Option A.** Blanket hard-delete of
   `links` stays. A split policy (tombstone authored kinds, hard-delete derived kinds,
   encoded in `link_kinds`) was proposed and rejected: "who mattered, when" is not a
   query the owner needs; the evidence in memos and events survives regardless.
   Consequences recorded in D11 (relationship removal invisible after snapshot
   retention; split policy remains additive-later if ever wanted).

No open questions remain. The schema is freeze-ready.

### Review round 3 (2026-09-29, two independent reviewers + author)

Two further reviews ran after the owner veto above. Every finding below was executed
against a fresh database (SQLite **3.53.4**); findings that did not reproduce are listed
as refuted so they are not raised again. Changes are recorded in the decision addenda
(D5, D7, D8, D11, D12, D15, D17) and in §3/§6.

**Resolved (DDL changed):** `measurement_values` was quadratic and did not mean what D7
said (unique partial index on `supersedes_id`; wording corrected); append-only was a
comment (triggers reject `UPDATE`/`DELETE`; `supersedes_id` immutable, so no cycles;
`IS NOT` closes the dangling-reference hole); symmetric links left a half-edge on delete
(mirror-delete trigger); the link registry auto-registered typos and late-flagged
symmetric kinds (closed registry, owner decision: FK to `link_kinds`, immutable
`symmetric`, immutable links); `repeat_weekdays` accepted garbage and weekly rules with no
weekdays never recurred (format CHECK, weekly ⇔ weekdays); repeating tasks without an
anchor day expanded to nothing; case-colliding and blank note/wiki titles, titled memos;
`repeat_every`/`repeat_until` on non-repeating rows; `measurements(day)` index.
**Resolved (cookbook changed):** §6.2 read superseded values and sorted NULLs by
accident; §6.6 missed asymmetric kinds; §6.9 used `ORDER BY id DESC LIMIT 1`; §6.11
did not terminate on a cycle; §6.12's four CTEs replaced by one expander (bounded
walk, `start_day`, `repeat_until`, `repeat_every`, clamping, yearly, monthly-same-day,
duration, calendar weeks).
**Refuted (schema was right):** `pages_day` index exists (line in §3; plan uses it);
`links(from_id)` is served by the `UNIQUE(from_id,to_id,kind)` index (plan:
`SEARCH … sqlite_autoindex_links_1 (from_id=?)`); `pages_fts` does declare
`content_rowid='id'`; the mirror trigger terminates under `recursive_triggers=ON`; the
end-of-`repeat_until` month is *not* dropped by the monthly CTE (the opposite defect was
real: the occurrence itself was never compared with `repeat_until`).
**Deliberately not adopted at the time, adopted afterwards (round 3b below):** an
`updated_at` bump on tombstoning, restricting path characters in titles, link endpoint
types, and a `pages.kind` transition rule.
**Note on record #1:** its "`mood=7`" line refers to the first-draft `pages.mood` column,
superseded by D6; earlier records are not edited.

### Validation record #3 (2026-09-29, review round 3)

§3 was extracted from this document and applied to a fresh file; all DDL statements
create cleanly (49 schema objects). Unlike records #1–#2, every check below has an
**expected value stated in advance**; a run that merely completes is not a pass.

- **Regression probes: 52/52 expectations met** — each adversarial insert/update was
  declared "must reject" or "must accept" before running: case-duplicate title,
  titled memo, blank/padded/201-char title, `Berlin`/`berlin` places; nine malformed
  `repeat_weekdays` values (`Mon,Wed`, `friday`, `xyz`, `mo we fr`, `mo,,fr`, `,mo`,
  `mo,`, `mo,mo,zz`, `MO`), weekly without weekdays, daily with weekdays,
  `none`+`repeat_every=-5`, `none`+`repeat_until`, `every 0`, `until` before start,
  recurring task without `due_day`; a second correction of one measurement, a
  correction chain (100←102←104 leaves exactly {101,104} visible), cross-metric and
  dangling `supersedes_id` (with `foreign_keys=OFF`), `UPDATE` of value / `supersedes_id`
  / `entity_id`, `DELETE`, `INSERT OR IGNORE` import idempotency; unregistered and
  mis-cased link kinds, illegal kind names, immutable `symmetric`, immutable links,
  mirror on insert (2 edges) and on delete (0 edges), self-link stored once, and both
  mirror triggers terminating under `recursive_triggers=ON`.
- **Performance:** `measurement_values` over 120 000 rows: **0.009 s**; plan
  `SEARCH x USING COVERING INDEX measurements_one_correction (supersedes_id=?)`
  (before: `SCAN x` per row — 14 s at 20 000 rows, killed after 300 s at 200 000).
  `measurements` day lookup uses `measurements_day`.
- **§6.12 expander vs an independent oracle:** 16 recurring events (daily, every 13
  days, weekly, biweekly starting on a Wednesday, biweekly Sunday, last Friday with a
  start after that month's Friday, second and fourth weekday, first Monday with
  `repeat_until` mid-month, monthly-on-the-31st, quarterly same-day and quarterly
  last-Friday, Feb-29 yearly, every-2-years, an ended series, a 3-day yearly
  conference) over four windows (15 months, 5 years, October 2026, February 2026):
  **SQL and oracle agree on every (title, occ_start, occ_end) triple in all 4 windows**
  (706, 1959, 60 and 36 occurrences). The oracle is plain Python (`calendar`,
  `datetime.date.weekday`, no SQL date arithmetic). It and the SQL share one *reading
  of the rules* — calendar weeks, clamping — which is now written down in D15 instead
  of implied. Spot checks: biweekly Wed-start `2026-09-30, 10-02, 10-12, 10-14, 10-16,
  10-26…`; last Friday from a `2026-10-31` start first returns `2026-11-27`; monthly on
  the 31st `01-31, 02-28, 03-31, 04-30, 05-31`; quarterly `01-15, 04-15, 07-15, 10-15`;
  Feb-29 yearly `2026-02-28, 2027-02-28`; first Monday with `until 2026-12-04` stops
  after `2026-11-02`.
  One month over 16 recurring events: 60 occurrences in 0.0005 s (the first-draft
  daily CTE materialised 224 014 rows for one open-ended event).
- **Task variant** (same query, `tasks`, `due_day` anchor): biweekly Tue/Thu from
  `2026-03-03` → `03-03, 03-05, 03-17, 03-19, 03-31, 04-02`; monthly on the 30th
  clamps; daily-every-3 correct.
- **Day view (§6.2):** superseded reading absent; undated rows first, then
  chronological; the multi-day recurring conference (`05-05`→`05-07`) shows on `05-06`
  through the lookback window and not on `05-08`.
- **Cookbook:** every SQL block in §6 was extracted from this document's text and
  executed against a seeded database (13 blocks, 0 failures). §6.9 produced exactly one
  `spawned` edge from the new task's id to the memo and set `triaged_at`; §6.10 accepted
  a first correction.
- **SQLite sharp edges executed and now recorded** (D15 addendum, §6.12): `date()`
  normalises `+N month`/`+N year`; `||` binds tighter than `*`, `/`, `+`; a trigger takes
  exactly one event (`AFTER INSERT, UPDATE` is a syntax error); `GLOB` negation is
  `[^…]` with no alternation; a partial `UNIQUE` index serves a correlated `NOT EXISTS`;
  `NULL <> x` is NULL (so a CHECK/trigger guard needs `IS NOT`).

### Review round 3b (2026-09-29, owner follow-up: the four "not adopted" items)

The owner asked for the four items round 3 had deliberately left out. All are now in §3
(decisions: D5 addendum 2, D8 addendum 2, D12 addendum 2):

1. **Filename-safe titles** — path/Windows-reserved characters, control characters and
   NUL, leading/trailing `.`, Windows device names, ≤ 240 **bytes**.
2. **Link endpoint types** — `link_kinds.from_types/to_types`, enforced by a trigger;
   kind structure immutable; the registry stays closed even with `foreign_keys=OFF`.
3. **`pages.kind` is fixed** after insert.
4. **Tombstone / un-tombstone bumps `updated_at`.**

### Validation record #4 (2026-09-29, review round 3b)

§3 was extracted from this document and applied to fresh files (SQLite **3.53.4**, 52
schema objects). Expected outcomes were declared before each probe ran.

- **Round-3 regression suite re-run: 53/53** (the title-length probe was updated from
  "201 characters rejected" to "241 bytes rejected, 240 bytes accepted"; a garbled
  `links_immutable` probe that never ran was removed — it is covered below).
- **New probes: 93/93** — 30 path-safety rejections (each of `/ \ : * ? " < > |`,
  control characters 1/9/10/13/31/127, NUL, `.hidden`, `..`, `.`, `trail.`, `a.`,
  `CON`/`con`/`Nul`/`prn`/`AUX`/`COM1`/`lpt9`) and 9 acceptances (`CONSOLE`, `com10`,
  `LPT0`, `Lifelog v1.2`, `Café notes`, `日本語 ノート`, a punctuation-heavy title, 240 ASCII
  bytes, 80 three-byte characters = 240 bytes) with 81 three-byte characters (243 bytes)
  rejected; `kind` changes memo→note, note→wiki and wiki→memo rejected while a no-op
  `SET kind = kind` and ordinary body/triage edits are accepted; tombstone and
  un-tombstone each bump `updated_at` (equal to `deleted_at` for a tombstone) and
  terminate under `recursive_triggers=ON`; **26 endpoint-type cases** (each seeded kind
  accepted with valid endpoints and rejected with a wrong source, wrong target or both,
  incl. `subtask` page→page, `attended` event→person, `friend` person→event,
  `wikilink` page→person, `spawned` task→task) with symmetric mirrors intact under typing
  (`friend` = 2 edges, `related` = 2); registry structure (types accepted, symmetric
  kind with differing types rejected, malformed type lists `Person` / `page,,task` /
  `page,` rejected, a misspelt token `persno` fails closed, `symmetric`/`from_types`/
  `to_types` immutable, `note` still editable).
- **Correction to record #3.** Record #3 listed "dangling `supersedes_id`, with
  `foreign_keys=OFF`" as verified, but that probe ran inside an open transaction, where
  `PRAGMA foreign_keys` is a **no-op** — the FK was actually ON. The claim happens to be
  true, and was re-verified here on a genuinely FK-OFF autocommit connection
  (`PRAGMA foreign_keys` read back as 0): the dangling `supersedes_id` is rejected by the
  trigger's `IS NOT`; a typed link to a nonexistent endpoint is rejected by the endpoint
  trigger; **an unregistered link kind was accepted** (the FK was the only guard) — the
  reason the endpoint trigger now rejects unregistered kinds itself; a link of an *untyped*
  kind to a nonexistent endpoint is still accepted with FKs off — that is what
  `foreign_keys=ON` (mandatory, §2.9) is for.
- **Known limit, executed:** `NOCASE` folds ASCII only. `health`/`HEALTH` collide as
  intended, but `Café notes`/`CAFÉ NOTES` are both accepted; the exporter must
  disambiguate (§2.5). SQLite has no Unicode case folding or normalisation without ICU.
- **Still passing after the changes:** `measurement_values` over 120 000 rows 0.010 s;
  expander vs oracle **4/4 windows exact**; all **13** §6 blocks executed from this
  document's text with 0 failures (§6.6 returned the person's `attended` edge, which
  the old `to_id`-only query would have missed).
- **Not done, on purpose:** titles remain editable (D5 forbids renames by convention —
  `[[Old Title]]` links in prose would silently repoint — but the schema does not enforce
  it; a title-immutability trigger is a one-line follow-up if wanted).

### Review round 3c (2026-09-29, owner follow-up: immutable titles, Unicode collisions)

Both items the owner asked for after round 3b are in §3 (D5 addendum 3; §2.5 rewritten;
§6.14 and two §7 rows added):

1. **Titles are immutable** — `pages_title_fixed`; the sanctioned fix is new page +
   `#REDIRECT` stub. This closes the "not done, on purpose" item of record #4.
2. **Unicode-proof uniqueness** — `pages.title_key` = `NFC(casefold(NFC(title)))`,
   app-computed, with `pages_title` now a `UNIQUE` index on it (replacing the
   ASCII-only `NOCASE` index). This supersedes the "known limit" of record #4.

### Validation record #5 (2026-09-29, review round 3c)

§3 was extracted from this document and applied to fresh files (SQLite **3.53.4**, 53
schema objects). Expected outcomes were declared before each probe ran.

- **Title-key probes: 38/38.** Collisions rejected with `UNIQUE constraint failed:
  pages.title_key`: `CAFÉ NOTES`, NFD `Cafe\u0301 notes`, `café NOTES` and NFD-upper
  against `Café notes`; `STRASSE` against `Straße`; `σας` against `ΣΑΣ` (final sigma);
  `ǆ` against `Ǆ`; `DIET` against `Diet` (also across kinds: a wiki page against a
  note); a new page colliding with a **tombstoned** title. Distinct titles accepted:
  `Cafe notes` (no accent), `日本語` and `日本語 2`. The DB verifies what it can: an ASCII
  title with a wrong key (`Diet2`/`dyet2`) rejected, a key with a capital rejected, a
  note without a key and a memo with a key rejected, an empty key and a key with
  surrounding spaces rejected; a correct ASCII key and a plausible non-ASCII key
  (`Über`/`über`) accepted. **UNIQUE also holds on the UPDATE path** (setting `Öl`'s key
  to `Ärger`'s key rejected). *Note on an earlier draft of this probe:* "recompute
  `title_key` to a colliding value" used an ASCII title and was rejected by the
  ASCII-consistency CHECK, not the index — the non-ASCII probe above is the real test.
- **Titles immutable:** `UPDATE title` rejected; `SET title = title` (no-op), body edits
  and `title_key` recomputation accepted (recomputation still passes the CHECKs and the
  index); the sanctioned rename — new page, old page's body replaced with
  `#REDIRECT [[Diet plan]]`, `links(kind='redirect')` — accepted.
- **Wikilink resolution:** the §6.14 query resolves `[[CAFE\u0301 NOTES]]` to the stored
  `Café notes`; plan `SEARCH pages USING INDEX pages_title (title_key=?)`. Without the
  `kind IN ('note','wiki')` predicate the same lookup is `SCAN pages` (a partial index
  is used only when the query implies its predicate) — hence §2.5's instruction.
- **Vector table:** all 12 titles in the §2.5 table were parsed from this document and
  compared with the reference function: each title's key is the one shown in its row.
- **Why not a collation (executed):** an app-registered `UNICODE_CI` unique index rejected
  `Café`/`CAFÉ` as wanted, but the `sqlite3` CLI without the collation could `SELECT`
  yet failed every `INSERT` and `PRAGMA integrity_check` with `no such collation
  sequence: UNICODE_CI`. That is §7's reason for rejecting it.
- **Still passing after the change:** round-3 regression suite 53/53 (its inserts now
  supply `title_key`); round-3b probes 93/93 (the `CAFÉ NOTES` probe flipped from
  "known limit: accepted" to "rejected"); expander vs oracle 4/4 windows exact;
  `measurement_values` over 120 000 rows 0.009 s; all **14** §6 blocks (incl. §6.14)
  executed from this document's text with 0 failures.
- **Known limits, stated plainly:**
  1. SQLite cannot check a **non-ASCII** key: a writer sending a wrong key (`Ünï2` with
     key `x1`) is accepted — verified. Consequence: uniqueness (only) is as good as the
     app's fold function; the vectors in §2.5 exist so every writer can be tested.
  2. Casefold + NFC is stricter than most filesystems (`ﬁle` = `file`), never looser
     for canonical equivalence and case, but it was **not run on a macOS or Windows
     filesystem** here; the exporter's write-time collision check stays as a backstop.
  3. If a future Unicode version adds case-fold pairs, recompute `title_key` (allowed);
     the unique index will then surface any real collision at that moment.
  4. `places.name` and `metrics.name` are still ASCII-`NOCASE` (§7, D5 addendum 3).


### Review round 4 (2026-09-30, independent review + owner request: finance)

An independent review of the goals and of §3 as it stood at v1.4, run **empirically** (the
AGENTS.md rule): every finding below was reproduced on a fresh database (SQLite **3.53.4**)
before it was written down; suspicions that did not reproduce are not listed. In parallel the
owner asked for financial data (net worth over time and similar entries) — that is D18 and
`§6.15–6.18`. The "freeze-ready" line in *Owner veto received* above predates this round and no
longer holds for the items marked **decide before first data**.

Status: **APPLIED** = already in v1.5; **OPEN** = recorded with a proposed fix, awaiting the owner
(nothing else in §3 was changed — round 4 touched only what D18 needs). *Statuses are as of round 4;
round 5 below resolves R4-02, 03, 06, 07, 08, 09, 10, 17 and seeds for R4-11 a/c; round 6 resolves R4-04, 05 and R4-11 b/d; round 7 resolves R4-13, R4-16 and the `backups/` label of R4-14; round 8 resolves R4-12; round 9 resolves R4-14; round 10 resolves R4-11 e, R4-15, R4-18, R4-19 and R8-01 (§8, round 10 has the final status of every finding).*

*Suggested triage.* **Decide before the first real row** (impossible or costly to retrofit): R4-04,
R4-08, R4-10, R4-11 b/d, R4-05. **Correctness, fix any time**: R4-02, R4-03, R4-06, R4-07, R4-09.
The rest is documentation or low-risk polish.

**High**

- **R4-01 — `REPLACE` bypasses the append-only triggers. APPLIED (§2.9).** With SQLite's default
  `recursive_triggers=OFF`, `INSERT OR REPLACE` / `REPLACE INTO` on `measurements` silently
  rewrote a row (70 → 99 via the unique `import_id`; 70 → 123 via the primary key) — the
  `measurements_no_delete` trigger never fired. §2.4, D7 and D11 say append-only is "enforced by
  triggers, not convention"; for REPLACE it was convention. With `recursive_triggers=ON` the same
  statements fail (`measurements are never deleted`) [R53]. Fix: the pragma is now mandatory
  (§2.9, DDL header) and a writer must read it back at connect. Residual: a per-connection
  setting, like `foreign_keys` — the file cannot enforce it.
- **R4-02 — `INSERT OR IGNORE`, the documented importer idiom, silently drops bad rows.** APPLIED
  for `balances` (§6.15); **OPEN** for the `measurements` wording (§2.4, D7). Executed: an
  `INSERT OR IGNORE` with day `'2026-9-3'` returned OK with 0 rows and no error; likewise a NULL
  `value`. (Foreign-key violations and `RAISE(ABORT)` triggers are *not* swallowed.) A 50 000-row
  import with one date-format bug would report success. `INSERT … ON CONFLICT(<target>) DO
  NOTHING` skips only the duplicate — the same bad day still raised `CHECK constraint failed`.
- **R4-03 — `measurements_import` has no `source`. OPEN.** The index is `(import_id, metric_id)`:
  two importers that both number rows `1001` collide and the second is dropped silently
  (executed: 1 row kept, expected 2). Open Brane, cited in D7, keys on `(source, actor)`. Fix:
  `(source, import_id, metric_id)`. (`balances` already uses `(source, import_id)`.)
- **R4-04 — No timezone is captured anywhere. OPEN — decide before first data.** An instant plus a
  local `day` cannot say what the local *time* was: `22:30Z` is 14:30, 22:30 or 07:30 next day
  depending on where you were; no table has a `tz`/offset column (executed), and the DB accepts a
  `taken_at` and `day` three days apart. Recurring events with a `start_at` have no defined local
  time across DST. Only capture time can supply the zone — it cannot be backfilled, which is the
  argument D3 makes about identity. Options: `entities.tz` (IANA name of the writer at creation),
  `events.tz`, `measurements.tz`; or an integer offset. With an offset, `day` becomes checkable
  (`date(taken_at, offset) IS day`).

**Medium**

- **R4-05 — Tasks have no local completion day. OPEN — decide before first data.** Only
  `completed_at` (UTC) exists, so "what I finished on 2026-06-09" needs the query-time
  UTC→local derivation D10 forbids, and §6.2's day view never selects completed tasks (executed:
  no `_day` column but `due_day`; §6.2 has no completed branch). Fix: `completed_day`
  (`(status='done') = (completed_day IS NOT NULL)`) and a day-view row.
- **R4-06 — A measurement cannot be retracted. OPEN.** Superseding with a NULL value fails
  (`NOT NULL constraint failed: measurements.value`) and superseding a row of another metric
  fails (same-metric trigger), so a value logged on the wrong metric, or a mistaken mood tap, is
  immortal in the series. Fix: let a correction carry NULL and exclude it in
  `measurement_values` — the shape `balances` has (D18).
- **R4-07 — "No hard deletes" is a convention for entities. OPEN.** Executed: `DELETE FROM pages`
  then `DELETE FROM entities` both succeed; an `entities` row of type `page` with no `pages` row
  is accepted; and because there is no `AUTOINCREMENT`, deleting the newest row lets the id be
  reused (ids `1` and `1`), silently repointing anything that remembers it (export filenames,
  ids in prose). Fix: `BEFORE DELETE` triggers on `entities` and the six domain tables — with
  `recursive_triggers=ON` they also stop REPLACE.
- **R4-08 — Enumerated CHECKs are unnamed, so they cannot be widened. APPLIED for
  `entities.type` and the new tables; OPEN for the rest.** Executed on 3.53.4: `ALTER TABLE …
  DROP CONSTRAINT` reports `no such constraint` for an unnamed inline CHECK, and adding a looser
  second CHECK does not relax the first (both apply); a *named* constraint drops and re-adds in a
  transaction on a populated database with `foreign_key_check` and `integrity_check` clean, a
  violating `ADD` is rejected, and `ROLLBACK` restores it. So the resolution of question 9
  ("new kinds are a one-line migration after freeze") was false for this DDL and true only with
  names — and only where the migration runner's SQLite supports it (tested: 3.53.4; `ALTER
  COLUMN … NOT NULL` is documented as 3.53.0, 2026-04-09 [R55]). Fix: name `pages_kind`,
  `tasks_status`, the two `repeat` CHECKs, `link_kinds.symmetric` — free now, a table rebuild later.
- **R4-09 — `metrics.unit` and `name` are mutable. OPEN.** `UPDATE metrics SET unit='lb'`
  succeeded, silently reinterpreting every stored value; `' Blood Pressure (sys) '` is an accepted
  name. `link_kinds` protects its structure; `metrics` does not. Fix: a `metrics_meaning_fixed`
  trigger (as `accounts_meaning_fixed`) and a name CHECK like `link_kinds.kind`.
- **R4-10 — "Happened" and "recorded" are not separated. OPEN — decide before first data.**
  `measurements` has no `recorded_at`, so when a value was entered — or corrected, D11's own "what was
  erased and when" — is unknowable; `entities.created_at` doubles as "authored" and "imported"
  for backfilled memos (the day view then shows 2026 timestamps on 2015 days). Fix:
  `measurements.recorded_at`; say what `created_at` means for imports. (`balances` has it.)
- **R4-11 — Graph gaps. OPEN (b and d: decide before first data; a and c are data-only seeds).**
  (a) No place-containment kind: the only kinds that can join place→place are `about` and
  `related` (executed), so D16's motivating query "everything in Japan 2019" is unanswerable —
  seed `located-in` (place→place). (b) `events.place_id` and `links(kind='visited', event→place)`
  are two homes for one fact, against D6's own rule, and `place_id` cannot hold a trip's two
  cities. (c) `family` is symmetric, so "my son" (D8's own example) cannot be told from "my
  father" — seed `parent-of`. (d) `lives-in` carries no dates: "where did I live in 2015" is
  unanswerable, and `links` have no valid-time at all. (e) Wikilinks resolve only to pages, so
  `[[Sam]]` never reaches the `people` row — the "everything about a person" query (§6.6) sees only
  hand-made `about` links.
- **R4-12 — The wikilink save contract is unspecified. OPEN (spec).** Executed: the auto-created
  wiki page for `[[Health/Diet]]`, `[[Re: plan]]` and `[[Target|alias]]` is rejected by the
  filename CHECK (`[[C#]]` is fine). If auto-create shares the memo's transaction, saving a memo
  that mentions one fails. Unspecified: `|alias`, `#anchor`, code spans/fences, and how a `#tag`
  is told from a Markdown heading, a URL fragment or `C#`. Rule to add: an invalid target makes no
  link and never blocks a save.
- **R4-13 — Concurrency and durability defaults. OPEN (doc).** A deferred `BEGIN`, a read, another
  writer's commit, then a write fails at once with `database is locked` — after 0.0 s, despite
  `busy_timeout=5000` (executed); `BEGIN IMMEDIATE` succeeds. Wikilink resolve-then-create
  (§6.14) is exactly that shape, and D3 plans several writers. Also `synchronous=NORMAL` in WAL
  "might roll back following a power loss" [R54] — for a low-write, high-value database `FULL`
  costs milliseconds (from SQLite's documentation; a power cut is not executable here). Fix:
  §2.9 — write transactions are `BEGIN IMMEDIATE`; `synchronous=FULL`.
- **R4-14 — Backups, export, and ability-to-leave. OPEN.** `backups/` is labelled DERIVED, yet it is
  the only history of structured data (D12), off-box is optional, and there is no retention rule,
  no `integrity_check`/`foreign_key_check` of a snapshot and no restore drill. `export/` mirrors
  **prose only**: §2.6's "export/ mirrors 100% of the data" is false — events, tasks, people,
  places, measurements, balances and links have no text mirror — so D4's answer to *file over app*
  holds for prose. Fix: relabel snapshots irreplaceable and make an off-box copy mandatory; verify
  each snapshot; add a nightly per-table CSV dump (a Library-of-Congress format, D1), with an
  explicit decision on whether finance tables may enter git (history cannot be scrubbed, D17
  addendum 2).
- **R4-15 — Search cannot see inside CJK text. OPEN (low urgency: the index is derived).**
  Default `unicode61` treats a CJK run as one token: in `日本語のノートを書く` the queries `本語` and
  `ノート` get 0 hits, only the whole run matches (executed). `é/ü/ș/ț` fold (`cafe`, `zurich`,
  `stefan`, `tara` hit), `ß`→`ss` and `ł`→`l` do not, even with `remove_diacritics 2`. `trigram
  remove_diacritics 1` finds `ノート`/`日本語` and folds accents but misses 2-character words
  (`京都`, `本語`) and queries shorter than three characters. The doc's own examples (`日本語`,
  `Zürich`) make this a stated requirement; record the tokenizer decision.

**Low**

- **R4-16 — Contradictions and overclaims in the text. OPEN.** D14 offers sqlite-web "when direct
  row editing is wanted" — a second, unguarded writer against principle 3/D3. D15 says the app
  may spawn the next occurrence via `links(kind='spawned')` task→task, which D8 addendum 2
  rejects (executed). D11's "tamper-evident for free" — any writer can drop the triggers, and
  REPLACE bypassed them (R4-01): "guards against mistakes" is what is true. And completing a
  repeating task ends its series, so per-occurrence tracking ("pay rent" monthly) is impossible:
  it is a reminder, not a task.
- **R4-17 — Immutability triggers reject no-op writes. OPEN.** `links_immutable` and
  `link_kinds_structure_fixed` fire on `SET kind = kind` (executed): a full-row ORM `UPDATE` of a
  link raises even when only `note` changed. `pages_title_fixed` and D18's triggers use a `WHEN`
  guard.
- **R4-18 — Partial dates. OPEN (additive later).** `people.birth_day` (and `events.start_day`)
  reject `1870` and `1870-05` (executed): ancestors and approximate dates cannot be stored.
- **R4-19 — Goals and process. OPEN.** (a) *Pareto vs machinery*: canonical titles are forbidden
  `/ : ? |` and made immutable forever so that *derived* export filenames stay simple — principle 5
  inverted; an exporter naming files `<id>-<slug>.md` would free the titles. (b) *Single writer*
  rests on three per-connection pragmas the file cannot enforce (R4-01): assert them at connect.
  (c) *Missing goals*: a threat model (finance makes it real — D17 addendum 2), an explicit "2075
  test" (the questions a stranger must answer from `.schema` and `lifelog_meta` alone) and an import
  / data-quality path. (d) The validation scripts are not in the repository, so records #1–#6 cannot
  be re-run; keeping them (a `tests/` folder is not a migration) is the owner's call.

**Confirmed sound (executed, so not repeated as findings):** §3 applies cleanly; the
recurrence expander agrees with a fresh independent oracle; `measurement_values` is fast at
120 000 rows; FTS stays in sync on update and passes `integrity-check`; all §6 blocks run; the
NFC/casefold title uniqueness, the endpoint-typed link registry and the CHECK matrices behave
as the earlier records say (§8 #6, regression).

### Validation record #6 (2026-09-30, round 4 — finance and the review probes)

§3 was **extracted from this document's text** and applied to fresh files (SQLite **3.53.4**,
**68** schema objects: 53 + 4 tables (`currencies`, `accounts`, `balances`, `fx_rates`), 5 indexes,
5 triggers and the `balance_values` view; `integrity_check` ok, `foreign_key_check` clean). Expected outcomes were
declared before each probe ran.

- **Round-4 review probes** (R4-01…R4-18): each finding above is an executed probe with the
  result quoted in its entry. Four of my own first probes were wrong and were fixed before
  anything was concluded: the "cross-metric `OR IGNORE`" probe was masked by the
  one-correction unique index; the first REPLACE-with-triggers probe ran after the row's
  `import_id` had been overwritten; the first `BEGIN IMMEDIATE` scenario made the second writer
  block, which is correct behaviour, not a failure; and one cookbook run passed a `title` and
  `title_key` that disagree (the CHECK rightly rejected it).
- **Finance probes: 89/89** — `currencies` (lower-case, 2- and 11-character codes, duplicate,
  `subunits` 0 and 10⁹+1 rejected, 10⁹ and a non-decimal 5 accepted, `subunits` immutable while
  a no-op `SET subunits = subunits` and a name change pass); `accounts` (type/table mismatch,
  no entity row, id also in `places`, bad `side`, NULL side, unregistered currency, NOCASE
  duplicate name, `closed < opened`, malformed day, `side`/`currency` immutable, full-row no-op
  update accepted, `updated_at` bumped, tombstone recorded, `entities.type = 'foo'` rejected);
  `balances` (integer accepted, `12.5` and `'12x'` rejected, `12.0` stored as integer, bad day and
  instant, missing `recorded_at`, dangling account, `UPDATE`/`DELETE` rejected, negative and
  int64-size amounts, newest row per day wins, a retraction hides the day and the as-of value
  falls back, re-entry after retraction wins, all rows retained, idempotent import with
  `ON CONFLICT`, the same `import_id` from another source accepted, `ON CONFLICT` still raises on a
  bad day while `OR IGNORE` swallows it — recorded as a hole); `REPLACE` **rewrites history with
  `recursive_triggers=OFF` and is blocked with `ON`** (also `REPLACE INTO … id`), and the
  symmetric-link mirror triggers still terminate under `ON`; `fx_rates` (inverse direction, same
  currency, rate 0 / negative / +inf / NULL, unknown currency, duplicate day, bad day rejected;
  in-place correction and a DEM→EUR fixed-rate row accepted); links to accounts (`about` from a
  memo and an event accepted, `about` a page and `wikilink`/`subtask` to an account rejected);
  exactness (REAL `0.1 + 0.2 = 0.30000000000000004`, integer minor units exact); widening
  `entities.type` by drop/add of the named constraint.
- **The probe suite can fail.** Mutation test: eight deliberate breakages of the DDL (canonical-order
  CHECK removed, `accounts_meaning_fixed` neutered, balances `DELETE` allowed, `balance_values`
  ignoring retractions, `account` dropped from `entities_type`, dropped from `about`, `subunits`
  CHECK removed, `entities_type` left unnamed) were each caught by 2–4 probes (or by the suite
  refusing to run). A first version of one mutation was invalid SQL and was redone.
- **Net worth vs an exact oracle.** A random book — 14 accounts in EUR/USD/JPY/BTC/GBP and a
  5-subunit currency, assets and liabilities, corrections, retractions, gaps, closed accounts, one
  tombstoned account, GBP with **no** rate at all, one currency whose rates start in 2016 — was
  valued with §6.17 for **155 month-ends** and compared with a Python oracle using exact
  rational arithmetic: every total within rounding (worst 0.49 of a minor unit, none off by one),
  and the `accounts` and `unconverted` counts identical in every month. §6.16's breakdown ties to
  the series (per-row rounding differs by one minor unit at most). A hand-checkable case
  (€1 200.00 + $50 000 at 1.10 + ¥300 000 at 160 − €200 000 mortgage) gives −151 470.45;
  a missing rate yields NULL / `unconverted = 1`, never a silent zero.
- **Performance.** `measurement_values` over 120 000 rows: 0.011 s. Net worth over 438 000 balance
  rows and 40 accounts: as-of breakdown 0.000 s, 372 month-ends 0.085 s. The first draft of §6.17
  took 17.3 s at 36 500 rows (planner scanned all balances per month); §6.16 without `CROSS JOIN`
  scanned every balance row. Both are rewritten (§6.16–6.17).
- **Regression of v1.4 behaviour: 139/139** on the v1.5 DDL (titles and their Unicode keys,
  path safety, kind/title immutability, day/instant round-trips, recurrence CHECK matrix,
  measurement append-only and supersede rules, link registry and endpoint types, mirror
  insert/delete, tombstone `updated_at`, FTS sync) — and the **identical suite gives the same 139/139 on
  the untouched v1.4 DDL**. Earlier exploratory probes produce byte-identical output on both DDLs.
- **§6 cookbook:** all **19** SQL blocks extracted from this text (15 + §6.15–6.18) executed, 0
  failures. **Expander vs a fresh oracle** (written independently of the one in record #3): 600
  random rules × 5 windows = 212 290 occurrences, 0 mismatches.
- **Known limits, stated plainly.** (1) R4-01's fix is a per-connection pragma the file cannot
  enforce. (2) `ALTER … ADD/DROP CONSTRAINT` was verified on 3.53.4 only. (3) `synchronous`
  durability is from SQLite's documentation; a power cut was not simulated. (4) Net-worth
  conversion uses REAL rates and rounds at the end — exact to a minor unit in the tests, not a
  guarantee for every pathological rate. (5) Nothing here has run against real data.

### Review round 5 (2026-09-30, owner: apply the round-4 mechanical fixes; net worth to include stocks, crypto, deposits and valuables — "keep it KISS and Pareto")

**Applied (v1.6)** — each is a probe in validation record #7:

| Finding | Change |
|---|---|
| R4-02 importer idiom | `ON CONFLICT(…) DO NOTHING` everywhere (header, §2.4, D7 addendum 2, `lifelog_meta.imports`) |
| R4-03 import key | `measurements_import` is `(source, import_id, metric_id)` |
| R4-06 retraction | `measurements.value` nullable; a NULL-value *correction* retracts; `CHECK` keeps a first reading honest; `measurement_values` hides retractions (§6.10) |
| R4-07 hard deletes | `BEFORE DELETE` triggers on `entities` and the 6 domain tables |
| R4-08 named CHECKs | all 8 enumerated CHECKs named (`entities_type`, `pages_kind`, `tasks_status`, `events_repeat`, `tasks_repeat`, `events_repeat_position`, `tasks_repeat_position`, `accounts_side`); each proven to drop and re-add widened. Left unnamed on purpose: `symmetric IN (0,1)` (a boolean) and the Windows-device-name blacklist (not a set of allowed values) |
| R4-09 metrics | `metrics.unit` immutable (`metrics_unit_fixed`); `name` must be snake_case |
| R4-10 audit time | `measurements.recorded_at` NOT NULL; `created_at` = write time, never back-dated (§2.2) |
| R4-11 a, c | seeds `located-in` (place→place) and `parent-of` (person→person); §6.19 |
| R4-17 no-op writes | `WHEN` guards on `links_immutable` and `link_kinds_structure_fixed` |

**Finance scope (D18 addendum 1).** Stocks, crypto, deposits and other valuables are all accounts
whose balance is their value in their own currency on a day (§2.10 table). No table was added; the
BTC seed row was dropped. Tracking quantity × price per holding was tested, found not to fit
v1.5, and deferred (§7) — the owner's "KISS and Pareto" applies: a net-worth number needs the
value a statement already prints.

**Still OPEN as of round 5** (round 6 below closes R4-04, R4-05 and R4-11 b/d) — decisions for the owner, none blocks the finance tier: **R4-04** timezone,
**R4-05** `completed_day`, **R4-11 b/d/e** (place_id vs `visited`, dated `lives-in`, wikilinks to
people), **R4-12** wikilink save contract, **R4-13** `BEGIN IMMEDIATE` / `synchronous`,
**R4-14** backups and a CSV mirror, **R4-15** CJK search, **R4-16** text contradictions,
**R4-18** partial dates, **R4-19** goals and process. R4-07's residue: an `entities` row with no
domain row can still be inserted (a writer bug, not a deletion); detect it with

```sql
SELECT id, type FROM entities
 WHERE id NOT IN (SELECT id FROM pages UNION SELECT id FROM events UNION SELECT id FROM tasks
                  UNION SELECT id FROM people UNION SELECT id FROM places UNION SELECT id FROM accounts);
```

### Validation record #7 (2026-09-30, round 5)

§3 was **extracted from this document's text** (633 lines) and applied to fresh files (SQLite
**3.53.4**, **76** schema objects; `integrity_check` ok, `foreign_key_check` clean). Expected outcomes
were declared before each probe ran.

- **Round-5 probes: 112/112.** R4-03: the same `import_id` from two sources is kept, a repeat from
  one source is a no-op. R4-02: `ON CONFLICT … DO NOTHING` still raises on a malformed day while
  `OR IGNORE` swallows it (recorded as the hole it is). R4-06: a first reading with NULL rejected;
  a mis-tap retracted and hidden together with its retraction; a cross-metric retraction rejected;
  a retracted row cannot be corrected twice; correcting the retraction restores a value; nothing is
  ever removed; `UPDATE` still rejected. R4-10: `recorded_at` required and validated. R4-07:
  `DELETE` rejected on `entities` and on all six domain tables, for an orphan `entities` row,
  and on a connection with `foreign_keys=OFF` (the orphan-row query above returns nothing on a healthy
  database and finds an inserted orphan); `REPLACE INTO pages` rejected under
  `recursive_triggers=ON` and the body survived; tombstoning, hard-deleting a link, and the
  measurement `DELETE` rejection unchanged. R4-08: for **each of the eight** named constraints,
  the new value is rejected before, `DROP CONSTRAINT` then `ADD CONSTRAINT` (widened) succeed in a
  transaction, the new value is accepted after, and integrity/FK checks are clean. R4-09:
  `UPDATE unit` rejected, a no-op `SET unit = unit` and a rename accepted, seven bad names
  (`Blood Pressure`, `bp sys`, `bp-sys`, empty, `Weight`, `x(y)`, `ünï`) rejected, four good
  ones accepted. R4-17: a full-row `UPDATE` changing only `note` passes on `links` and `link_kinds`;
  changing a kind, an endpoint, `symmetric` or `to_types` is rejected. R4-11: `located-in` is
  place→place only and one-way; `parent-of` is person→person only and keeps direction; §6.19 finds
  the Tokyo trip inside Japan through Kanto and terminates on a cycle.
- **The suite can fail.** Eleven deliberate breakages (import key without `source`, `value` NOT
  NULL again, the view showing retractions, each of three delete triggers neutered, `pages_kind`
  unnamed, `metrics_unit_fixed` neutered, `recorded_at` optional, the `WHEN` guard removed, the
  `located-in` seed removed) were each caught. One of my first mutations was invalid (it only
  changed a message, so the trigger still aborted); and neutering `entities_no_delete` at first went
  unnoticed because the foreign key already blocked deleting an entity that has a domain row — the
  orphan-row and `foreign_keys=OFF` probes were added and now catch it.
- **Before the KISS decision** the plan was to generalise `currencies` into units with a `prices`
  table; testing v1.5 with tickers (D18 addendum 1) showed why that is real weight, and the
  owner's KISS/Pareto instruction ended it. What v1.6 adds for finance is guidance, not tables.
- **Unchanged and re-run on the v1.6 DDL:** regression of v1.4 behaviour **139/139**; finance probes
  **88/88** (the BTC seed row and one now-moot "unnamed CHECK" probe removed); net worth vs the exact
  oracle **155/155 month-ends** within rounding (worst 0.49 of a minor unit; `accounts` and
  `unconverted` counts identical); expander vs an independent oracle **212 290** occurrences,
  0 mismatches; **all 20** §6 SQL blocks extracted from this text (§6.19 is new) ran with 0
  failures; `measurement_values` over 120 000 rows 0.008 s.
- **Known limits.** Named CHECKs widen only on a SQLite that supports `ALTER … DROP/ADD CONSTRAINT`
  (tested on 3.53.4). Everything in this record ran on synthetic data.

### Review round 6 (2026-09-30, owner: "yes" to the three recommendations)

**Applied (v1.7)** — each is a probe in validation record #8:

| Finding | Change |
|---|---|
| R4-04 timezone | `entities.tz` and `measurements.tz`: IANA zone of the writer at capture, NULL = unknown, shape-checked (`A-Za-z0-9_/+-`, 1–64); `events` deliberately have none (D10 addendum); §2.2, §6.1, `lifelog_meta.tz` |
| R4-05 local completion day | `tasks.completed_day`, paired with `status = 'done'` like `completed_at`; §6.2 day view lists what was finished that local day |
| R4-11 b, d | `events.place_id` is an event's only place: `visited` is person→place only, `lives-in` removed (a dated event answers "where did I live in 2015"); §6.19 text |

**Still OPEN** (none blocks anything): **R4-11 e** (wikilinks to people), **R4-12** wikilink save
contract, **R4-13** `BEGIN IMMEDIATE` and `synchronous`, **R4-14** backups and a CSV mirror,
**R4-15** CJK search, **R4-16** text contradictions, **R4-18** partial dates, **R4-19** goals and
process. Known limit of R4-04: a *recurring timed* event still has no defined local time across DST.

### Validation record #8 (2026-09-30, round 6)

§3 was **extracted from this document's text** and applied to fresh files (SQLite **3.53.4**, 76
schema objects, `integrity_check` ok, `foreign_key_check` clean). Expected outcomes were declared
before each probe ran.

- **Round-6 probes: 49/49.** `tz` on both tables: `Europe/Berlin`, `UTC`,
  `America/Argentina/Buenos_Aires`, `Etc/GMT+5`, `Asia/Kolkata`, `America/Port-au-Prince`, a
  64-character name and NULL accepted; empty, `Europe Berlin`, 65 characters, a trailing newline,
  `ünï/x`, `a;b` and a trailing space rejected; the §6.1 example from this text stores its zone.
  `completed_day`: done without it, done without `completed_at`, open with it, malformed and
  impossible days rejected; the documented completion `UPDATE` works; un-completing without
  clearing the day is rejected; §6.2 run from this text lists the task done on local 2026-06-10
  (its UTC instant is 2026-06-09 22:30) and not on 06-09. Places: `lives-in` is no longer a kind;
  `visited` accepts person→place and rejects event→place and place→person; a dated event answers
  "where did I live on 2015-06-01" and is empty after it ended.
- **The suite can fail.** Eight deliberate breakages (each `tz` CHECK removed, the length limit
  removed, the `completed_day` pairing and format CHECKs removed, `lives-in` seeded again,
  `visited` allowing events again) were each caught (1–7 failing probes); one mutation text was
  malformed and was redone.
- **Regression:** v1.4 behaviour **139/139** (four link probes updated for the removed kinds — an
  expected change, not a regression); round-5 probes **112/112** (one `lives-in` link replaced by
  `visited`); finance probes **88/88**; net worth vs the exact oracle **155/155** month-ends;
  expander vs an independent oracle **212 290** occurrences, 0 mismatches; every §6 SQL block
  extracted from this text ran with 0 failures.
- **Known limits.** The DB checks a zone's *shape*, not that it exists. Synthetic data only.

### Review round 7 (2026-09-30, owner: fix the doc inconsistencies and R4-13)

**R4-13 — durability and write transactions (applied).** `synchronous = FULL` replaces `NORMAL` in
§2.9 (SQLite documents that a WAL commit under `NORMAL` "might roll back following a power loss"
[R54]; the measured price of `FULL` is ~1 ms per commit, §8 #9). Every write transaction starts with
`BEGIN IMMEDIATE` (§2.9, the DDL header, §6.1/6.9/6.14/6.15), and §6.14 now resolves and creates
inside one transaction, so two writers saving the same new link cannot both see "none found".
A writer asserts `synchronous` alongside `foreign_keys` and `recursive_triggers` at connect time.

**R4-16 — contradictions and overclaims (applied)**, plus five more found by re-scanning the whole
text for the same kind of defect. Live text (§1–§7, the DDL header) was corrected in place;
decision text keeps its history and gains an addendum or a pointer (AGENTS.md):

| Where | Was | Now |
|---|---|---|
| D14 | "optionally `sqlite-web` when direct row editing is wanted" — a second, unguarded writer | dropped; tools open the file read-only; editing goes through the app (D14 addendum, §2.9, §9 [R50]) |
| D15 | next occurrence via `links(kind='spawned')` task→task — rejected by D8 addendum 2 | a new task, optionally `related`; **repeating tasks are reminders** (completing ends the series); per-occurrence tracking = a 0/1 habit measurement (D15 addendum 2, §4) |
| D11 | "tamper-evident for free" | pointer to D11 addendum 3 ("guards against mistakes") |
| D17 addendum 2 | "Datasette/sqlite-web … open read-only" | Datasette only, with the verified default |
| §1 principle 5, §2.1, DDL header *(new)* | "Only `life.db` is irreplaceable"; `backups/` "DERIVED" | snapshots are irreplaceable — the only history of structured data (D12) |
| §2.3, D3 *(new)* | "Every table uses `INTEGER PRIMARY KEY`" | every entity/fact/join table does; `lifelog_meta`, `link_kinds`, `currencies`, `fx_rates` keep their natural key (D3 addendum) |
| §2.6, §7 *(new)* | "`export/` mirrors 100% of the data" | mirrors all the prose; structured tables live only in `life.db` and snapshots |
| D12 *(new)* | "`created_at`/`updated_at` (trigger-maintained) is the only temporal metadata" | `created_at` is app-written, `updated_at` trigger-maintained, `recorded_at`/`taken_at`/`tz` exist (D12 addendum) |
| D7 *(new)* | importers use `INSERT OR IGNORE` | pointer to addendum 2 (`ON CONFLICT … DO NOTHING`) |
| §2.9 readers *(new)* | "Datasette, sqlite-web … need no setup" | readers must be read-only (`?mode=ro`, `sqlite3 -readonly`) |

**Still OPEN** (none blocks anything): **R4-11 e** (wikilinks to people), **R4-12** the wikilink save
contract, **R4-14** (retention, snapshot verification, an off-box copy, a CSV mirror — the `backups/`
label is fixed), **R4-15** CJK search, **R4-18** partial dates, **R4-19** goals and process.

### Validation record #9 (2026-09-30, round 7)

§3 was **extracted from this document's text** (644 lines; only header comments changed since #8)
and applied to fresh files (SQLite **3.53.4**, 76 objects, `integrity_check` ok, `foreign_key_check`
clean). Expected outcomes were declared before each probe ran.

- **Round-7 probes: 36/36.** *The race (real connections and threads):* with a deferred `BEGIN`,
  a read, and another writer committing in between, the write fails at once with `database is locked`
  and one page exists; with `BEGIN IMMEDIATE`, the first writer creates the page and the second waits
  for the lock (0.33 s in the run) and then **finds** it — no error, one page. The §6.14 block from this
  text opens with `BEGIN IMMEDIATE`, resolves inside the transaction, runs, and creates the page; no
  bare `BEGIN` remains in §6. *Pragmas:* SQLite's default `synchronous` is FULL (2), so the setting
  records intent; §2.9 and the DDL header state `FULL` and the `BEGIN IMMEDIATE` rule. *Readers:* a
  `mode=ro` connection is not blocked by an open write transaction and sees the commit; `INSERT`,
  `DELETE`, `UPDATE` and `DROP` all fail with `attempt to write a readonly database`; the writer is
  not slowed by a connected reader; `sqlite3 -readonly` refuses writes. *Text:* no "mirrors 100%",
  no "DERIVED" backups, principle 5 names the snapshots, §2.3 no longer says "every table" and
  the tables without a single integer primary key are exactly the four it now names, the readers
  paragraph no longer lists sqlite-web, D14/D17 carry the pointer; the advice in D15 works
  (`spawned` task→task stays rejected, `related` links a next occurrence with two edges, a 0/1
  habit metric records per-occurrence outcomes); `entities.created_at` has no default and cannot be
  omitted (app-written).
- **The probes can fail.** Run against the document as it stood *before* this round, **14 of the 36
  fail** — every text and cookbook fix is detected — while the concurrency and read-only probes pass
  on both, as they should: that SQLite behaviour never changed, only the documentation did. (The
  first version of that comparison crashed at the first missing string instead of listing failures;
  the probes were made robust.)
- **Datasette 0.65.5, installed in a scratch venv: 9/9.** Its connection to a mutable database
  is `mode=ro` (from its own source: `qs = "?mode=ro"`; `?immutable=1` only for an immutable
  database); reading works; `INSERT`, `DELETE` and `DROP` are refused with `attempt to write a
  readonly database`; its SQL console answers `400 Statement must be a SELECT` to `INSERT` and
  `DELETE` and runs `SELECT`; table browsing works; the file is unchanged afterwards. The Datasette
  documentation page fetched did not state the default, so this is from the executed version, not
  the docs. sqlite-web's own documentation lists row editing by default and a `-r/--read-only` flag.
- **What `synchronous = FULL` costs, measured.** 500 two-row memo commits (`BEGIN IMMEDIATE`,
  entity + page, `COMMIT`) on this machine's btrfs: **`NORMAL` 0.12 ms per commit, `FULL` 0.97 ms**.
  A first run in `/tmp` (tmpfs, where `fsync` is free) showed no difference and was discarded as
  meaningless. That `NORMAL` can lose a committed transaction on power loss is SQLite's own
  documentation [R54]; a power cut was not simulated.
- **Regression, unchanged:** v1.4 behaviour **139/139**; finance probes **88/88**; round-5 probes
  **112/112**; round-6 probes **49/49**; net worth vs the exact oracle **155/155** month-ends;
  expander vs an independent oracle **212 290** occurrences, 0 mismatches; all **20** §6 SQL blocks
  extracted from this text ran with 0 failures.
- **Known limits.** Durability under power loss is documented, not demonstrated here. The read-only
  behaviour of Datasette is that of 0.65.5. Synthetic data only.

### Review round 8 (2026-09-30, owner: "Let's continue" — R4-12, the wikilink save contract)

**R4-12 — applied (D19; §2.5, §6.14).** Reproduced on v1.8 before anything was written (record #10:
9 of 9 expectations held). The contract is prose plus one cookbook block; **the DDL did not change**
(byte-identical to v1.8), so every choice below is reversible by editing §2.5 and D19:

| Where | Was | Now |
|---|---|---|
| §2.5 tags | "`#tag` is sugar for `[[tag]]`: the app *expands* hashtags to wikilinks" — a rewrite of the body, with no grammar | a tag is *read*, never expanded; the grammar is in §2.5 (not `C#`, `a#b`, `http://x/#frag`, `#12`; not inside a wikilink) |
| §2.5 sync | "upserts `links(kind='wikilink')` rows" — a link outlived the text that made it | the page's wikilinks **equal** the set its body names: rows added and deleted; a re-save changes nothing; everything is rebuildable from the bodies |
| §2.5 scan | unspecified — `\|alias`, `#anchor`, code spans and fences, raw HTML, escapes | the CommonMark *text* of the body only (a parser's text nodes); `\|alias` ignored by the DB, no `#anchor`, no escape (code span instead) |
| invalid target | the auto-created page for `[[Health/Diet]]` fails the filename CHECK and either loses the memo or leaves an orphan `entities` row | **no link, never blocks a save**: a predicate mirroring the CHECKs, and each target in its own `SAVEPOINT` |
| §6.14 | resolve and create only — no cookbook block wrote a `wikilink` row, and §6.1's memo named `[[Lifelog]]` and linked nothing | the whole save in one transaction: savepoint per target, revive a tombstone, link with `ON CONFLICT … DO NOTHING`, drop stale links |
| rename stubs | `#REDIRECT [[New]]` would become the tag page `REDIRECT` and a backlink of its own replacement | a body starting `#REDIRECT [[` is not scanned; its one edge is the `redirect` link |
| §6.5 | listed `redirect` rows although §2.5 said backlink queries exclude them | `AND l.kind <> 'redirect'` |
| §6.13 | ghosts come only from capture-time typos | also from a mention edited out of a body |
| §2.5 *Safe* | "rejects titles unsafe as a filename on any mainstream filesystem" | narrower wording, and R8-01 |

The four choices an owner might veto, each decided here for the smallest rule that works: `|alias` is
supported and `#anchor` is not (`|` can never be in a title, `#` can); a tag is read and the text is
never rewritten; a stub is skipped wholesale rather than only its tag; "what counts as text" is
delegated to a CommonMark parser instead of a hand-written grammar.

**R8-01 — a Windows device name followed by an extension passes the title CHECK. OPEN (low).**
Found while fuzzing the filename predicate. The DDL rejects the bare names (`CON`, `COM1`) but
accepts `CON.backup`, `NUL.txt`, `COM¹` and `LPT²` (executed). Microsoft documents `NUL.txt` as
equivalent to `NUL` and `COM¹` as reserved [R58] (from the documentation; Windows is not executable
here). It harms only a Windows copy of `export/`, and the exporter already owns a write-time
backstop. Fix if ever wanted: test the part of the title before the first `.`, and add the six
superscript names — a `CHECK` rewrite, not a migration, until real data exists (D13).

**R4-19 d, evidence.** The scratch scripts of rounds 5–7 (the 139, 88, 112, 49, 36 and 9 probes, the
net-worth and recurrence oracles) were lost when `/tmp` was cleared between sessions. This round had
to rebuild its harness and could re-run none of them; its regression therefore rests on the DDL being
byte-identical (record #10, D1). That is the cost R4-19 d warned about. Keeping the scripts in a
`tests/` folder remains the owner's call.

**Still OPEN** (none blocks anything): **R4-11 e** (wikilinks to *people* — a wikilink resolves to a
page only), **R4-14** (retention, snapshot verification, an off-box copy, a CSV mirror), **R4-15**
CJK search, **R4-18** partial dates, **R4-19** goals and process, **R8-01** device names with an
extension.

### Validation record #10 (2026-09-30, round 8)

§3 was **extracted from this document's text** and is **byte-identical to v1.8's** (644 lines,
sha-256 `be71f4ab…`; 76 objects, `integrity_check` ok, `foreign_key_check` clean, SQLite **3.53.4**).
Expected outcomes were declared before each probe ran. The reference implementation of the contract is
Python with markdown-it-py 4.2.0 [R59]; it is a test instrument, not a deliverable.

- **Baseline on v1.8: 9/9 held.** Creating the page for `[[Health/Diet]]` and for `[[Target|alias]]`
  fails on the filename CHECK; a writer that swallows the error and commits leaves one orphan
  `entities` row each; a literal reading of the tag rule turns a stub's `#REDIRECT` into a tag; an
  upsert-only sync leaves the link after the body drops it; no §6 block inserts a `wikilink` row; the
  DDL accepts a page linking to itself; §6.5 has no `redirect` exclusion.
- **What the parser hands back** (executed before the rules were written): code spans, fenced and
  indented code, raw HTML, comments and image alt text never reach the scan; `\[[x]]` and `&#35;x`
  arrive already decoded (so there is no escape, and an entity can make a tag); text between inline
  tags is one run each; `#Heading` without a space is a paragraph; `[[Diet]](url)` and a
  `[[Ref]]` with a `[Ref]:` definition are Markdown links, not text.
- **54 extraction vectors: 54/54** (29 are printed in §2.5; the rest cover 240/241-byte and 80/81-CJK
  title limits, raw HTML, image alt text, link labels, nesting, newlines, dots and devices). The first
  run had one failure that was **my expectation's**: the order of `[[Project #alpha]] #beta [[#gamma]]`
  is appearance order (`beta` before `#gamma`); the vector was corrected, not the code.
- **The filename predicate against the DDL: 43 361 distinct strings** (random over an alphabet of
  every hazard — separators, control characters, dots, spaces, NBSP and ideographic space, device
  names in every case, boundary lengths built from 1- to 4-byte characters). The database accepted
  6 234; **zero disagreements in either direction** — the app never offers a title the CHECK rejects.
- **The save procedure against the real DDL: 25/25.** A memo naming `[[Health/Diet]]`, `[[Re: plan]]`,
  `[[Target|alias]]`, `[[Good page]]`, `#health`, `#con`, `#C` is saved; links go to `C`, `Good page`,
  `Target`, `health`; the three invalid targets are reported and stored nowhere; no orphan. With the
  predicate **switched off** the save still commits (the savepoint rolls the bad target back alone).
  Edit sequences: add, drop, re-save (no new rows), empty body (all links gone); a page never links
  to itself; a stub loses its old wikilinks, makes none, and no `REDIRECT` page exists; a tombstoned
  target is revived, not duplicated; **400 random edits over six pages leave the same links as a
  rebuild from the final bodies**; four real threads saving memos about the same new target and tag
  end with no error, one page each and eight links.
- **The probes can fail: 13/13 mutants caught** — no CommonMark parser (raw scan), no validation,
  no savepoint, alias kept in the title, tags read inside wikilinks, tag without the "not glued"
  rule, numeric tags, no stub rule, self-links, no stale-link delete, no tombstone revival, an
  ASCII-only word class for tags, no NFC. One result is worth keeping: with the predicate alone off
  only the extraction vectors fail — the savepoint keeps the save alive on its own; with the
  savepoint alone off, the save raises. The two layers are independent.
- **The document text: 26/26.** The 29 vectors parsed back out of §2.5's table reproduce; §6.14's SQL,
  split into statements and **executed literally from the text** (only extraction, validation and
  the key are the app's), gives the vector result for all 54 vectors on 54 fresh databases, leaves
  no orphan, and after 400 random edits equals the reference implementation; `last_insert_rowid()`
  after `INSERT INTO pages` is the new page's id (step 3 relies on it); §6.5 from the text drops a
  stub's `redirect` row and keeps the memo's `wikilink`; §6.1 runs; all 39 statements of the 20 §6
  blocks prepare; the stale phrases are gone from §1–§7. **Against the v1.8 text 16 of the 21 outcomes
  recorded fail** (the table and §6.14 sections cannot even run there); seven deliberately broken copies of this text (no stale-link delete, no savepoint, a
  wrong vector, the old filename claim back, a changed CHECK, no `redirect` exclusion, no `ON CONFLICT`)
  were each caught.
- **R8-01, executed:** the DDL accepts `CON.backup`, `NUL.txt`, `COM¹`, `LPT²`, `Com10` and rejects
  `CON`, `COM1`, `CON ` and `con.`.
- **Mistakes of mine, fixed before any conclusion:** the vector order above; one probe's expected
  list omitted `#C` (a valid one-letter tag); the first document harness reused one database for all
  vectors (a later vector then linked to an earlier page spelled differently — which is the
  documented "first spelling wins") and did not bind parameters; one check had an `or True`.
- **Regression.** The DDL is byte-identical, so the database behaviour recorded in #6–#9 is
  unchanged; those suites (139, 88, 112, 49, 36, 9 probes, the net-worth and recurrence oracles) were
  **not re-run** — their scripts no longer exist (R4-19 d). What did change is §6.1 (a comment),
  §6.5, §6.13 (prose) and §6.14, all executed above.
- **Known limits.** The reader is one parser (markdown-it-py 4.2.0); another could differ on an
  unlisted edge, which is what the vectors are for. Windows behaviour is from Microsoft's
  documentation. Concurrency was four threads against a local file. The NFC step means a title typed
  decomposed is *stored* composed. Synthetic data only.

### Review round 9 (2026-09-30, owner: "let's continue" — R4-14, backups, verification and the way out)

**R4-14 — applied (D12 addendum 2; §2.8, §2.1, §2.9).** The DDL did not change (byte-identical to
v1.8) and no new decision number was needed: this is the contract D12 left open. Every claim in §2.8
that the old text took from documentation was executed first, and three of my own predictions were
wrong (below). What the experiments turned up, in the order they turned up:

| # | Finding (executed) | Consequence |
|---|---|---|
| a | `.backup` is consistent but **starves**: on a 50 MB file a writer doing 19 commits/s kept it from finishing in 30 s (5/s was fine; in tmpfs on 10 MB the break came at ~230/s) | the snapshot is `VACUUM INTO`, one read transaction: 0.26–0.29 s at 19 and 155 commits/s |
| b | "never `cp` a live database" was from the docs: a byte copy was damaged in **95 of 150** tries at constant checkpoints, 5 of 150 at the default; on btrfs a reflinked copy hid it | claim confirmed, with the filesystem caveat |
| c | a `VACUUM INTO` copy is **not WAL** (rollback-journal header) though it keeps `application_id`, `user_version`, all 76 objects and FTS5 | restore switches it back to WAL |
| d | restoring over a database that still has its `-wal`/`-shm`: the snapshot silently came back as the newer data (3 426 pages, not 2 626), or an older snapshot was corrupted | restore moves the old file **and its own sidecars** aside together, never leaves a stale `-wal` |
| e | `integrity_check` says `ok` for an orphan row (foreign keys are per connection) and for a flipped byte inside a value (no page checksums) | `foreign_key_check` too, plus a recorded SHA-256; the off-box tool verifies what it stores |
| f | nothing said `backups/` must stay out of git; `life/` is a git repository and the snapshots hold the finance tables | `.gitignore` (§2.1) |

The rest of the R4-14 list, as decided: retention is the newest 30 plus the first of each month,
forever; an off-box copy is mandatory and the night fails without it (append-only — restic, or
rsync **without** `--delete`, which mirrors accidents); the restore is a script and is drilled from
the off-box copy; `dump/` holds one CSV per table as the way out. **The finance-in-git question is
answered no**: `backups/` and `dump/` are ignored, and `export/` never had finance rows (D17
addendum 2).

**Still OPEN** (none blocks anything): **R4-11 e** (wikilinks to *people*), **R4-15** CJK search,
**R4-18** partial dates, **R4-19** goals and process, **R8-01** device names with an extension.
Not executed here because the tool is not installed: the recommended off-box path, restic (append-only
backups, `check --read-data`) — only rsync was run.

### Validation record #11 (2026-09-30, round 9)

§3 was **extracted from this document's text** and is **byte-identical to v1.8's** (644 lines; the
round-8 document checks still pass, below). The scripts of §2.8 and the `.gitignore` of §2.1 were
**extracted from this text and run**. Expected outcomes were declared before each experiment.
Numbers: a synthetic lifelog (1 year = 1 126 pages, 62 780 measurements, ~10 MB; 5 years = 5 628
pages, 313 900 measurements, 50 MB), SQLite **3.53.4**, Linux, GNU coreutils; timings on btrfs unless
said otherwise.

- **Does the snapshot finish and is it consistent while another process writes?** A second process
  commits an entity + page + measurement per transaction, tagged, so a torn snapshot shows as a
  mismatch. *50 MB, btrfs:* `.backup` 0.17 s and 0.20 s at 1 and 5 commits/s, **did not finish in
  30 s at 19 and 46**; `VACUUM INTO` 0.29 s at 19 and 0.26 s at 155. *10 MB, tmpfs:* `.backup` 0.02 s up
  to 21/s, 0.58 s at 93/s, did not finish in 40 s at 229/s; `VACUUM INTO` 0.03 s at 21, 219 and 13 238.
  Every snapshot that finished was consistent (pages = measurements, `integrity_check` ok, no orphan).
  My prediction was "every `.backup` is clean" — true of the ones that finished, blind to the ones that
  did not. The first run, with an unthrottled writer, grew the file to 2.7 GB and ran one `.backup`
  for 4½ minutes; it was stopped and discarded, and redone with a bounded writer.
- **`cp` of a live file.** `cp --reflink=never` while the writer ran: constant checkpoints — 55 ok,
  65 `integrity_check` failures, 30 unreadable of 150; default checkpoints 145 ok, 5 unreadable of
  150; main file then `-wal`: 99 ok, 35 + 16 bad of 150; `-wal` then main: 83 ok, 46 + 21 bad. The
  first version of this experiment was **invalid**: on btrfs `shutil.copy` makes a reflink clone and
  was clean in 360 tries; plain `cp` failed 2 of 100.
- **Identity of a snapshot.** `.backup` and `VACUUM INTO` both keep `application_id` (0x4C494645),
  `user_version` 1, all 76 objects and a working FTS5 (779 hits for `coffee`); `.backup` keeps WAL
  (header bytes 2, 2) and `VACUUM INTO` is `delete` mode (1, 1); 10 268 vs 9 916 KiB; 0.07 s vs 0.09 s.
- **What verification sees.** An orphan balance written with `foreign_keys=OFF`: `integrity_check`
  `ok`, `foreign_key_check` one row. A zeroed page, a truncated file: detected. A flipped byte in the
  indexed `title_key`: detected (`row … missing from index pages_title`). A flipped byte inside a text
  cell: **not detected** — `integrity_check` ok while the `pages` table differs; only the SHA-256
  differs. On a damaged *live* file: a flipped index key — `VACUUM INTO` succeeds, the copy carries the
  damage and fails `integrity_check` (I predicted `VACUUM INTO` would rebuild the index and hide it —
  wrong); a zeroed table page — `VACUUM INTO` itself fails; a flipped content byte — copied silently.
  `integrity_check` takes 0.55 s and `quick_check` 0.20 s on 50 MB.
- **Stale WAL on restore** (a crash image: `life.db` + `-wal` + `-shm` holding newer frames).
  Snapshot copied over with both sidecars kept: 3 426 pages, not the snapshot's 2 626; `-shm` removed,
  `-wal` kept: the same; both removed: 2 626; an **older** snapshot next to a newer `-wal`:
  `integrity_check` errors (`btreeInitPage() returns error code 11`).
- **The CSV dump.** 15 base tables, FTS shadow tables excluded, 0.64–0.80 s and 33 MB; record counts
  equal row counts except the empty `fx_rates`, which is a 0-byte file with no header. NULL is an
  empty field and `''` is `""`. I predicted REAL values would lose digits past 15 — wrong on 3.53.4:
  `0.30000000000000004`, `1.0e-07`, `123456789.12345679`, `4.9406564584124654e-324` all round-trip.
  Newlines, CRLF, quotes, commas, emoji, CJK, `\N` and `NULL` inside text survive a real CSV parser.
  `-nullvalue '\N'` works in CSV mode but leaves text `\N` indistinguishable and puts `\N` into numeric
  cells, so it is not used.
- **The two scripts: 25/25 probes**, run from the drafts and again from the text of §2.8 and §2.1.
  On the 50 MB file with a writer active `nightly.sh` exits 0 in 1.8 s, and the snapshot is
  consistent, hashed (`sha256sum -c` ok), with 15 CSVs, an off-box copy and no `.tmp`. A live file
  with an orphan balance: non-zero exit, no snapshot for the night, the earlier snapshot
  byte-identical, `dump/` untouched, nothing copied off-box. A live file with a damaged index:
  `VACUUM INTO` succeeds, step 2 rejects the copy, exit non-zero, no snapshot and no `dump/`. Off-box
  command unset, empty or failing: non-zero exit with the local snapshot kept. Pruning 1 200 fake
  names (with gaps, a `.FAILED` file and an unrelated file): the kept set equals an independent oracle
  (newest 30 ∪ first of each month, 69 files), `.sha256` files go with their snapshot. Restore over a
  crash image: exit 0, 5 628 pages, WAL mode, `integrity_check` ok, and the old file with its `-wal` and
  `-shm` kept as `life.db.broken-<time>` still opens with the newer 6 328 pages; restore refuses a
  snapshot with flipped content (hash), a zeroed page (`integrity_check`) and an orphan balance
  (`foreign_key_check`), each time leaving `life.db` untouched; the drill from the off-box copy into an
  empty directory works. With §2.1's `.gitignore`, `git add -A` stages only `export/` and the
  `.gitignore`. `rsync -a` keeps a file deleted locally; `rsync -a --delete` removes it from the copy.
- **The probes can fail: 13/13 mutants of the scripts caught, from the drafts and again from the text of §2.8** — `.backup` instead of `VACUUM INTO`,
  no `foreign_key_check`, no `integrity_check`, verification after the rename, no temporary name,
  keeping 15 instead of 30, no monthly keep, an optional off-box step, no hash check, the stale
  `-wal` left behind, no switch back to WAL, no `foreign_key_check` on restore, the old file deleted
  instead of kept (two of them, `.backup` and the missing temporary name, were caught by the night failing outright rather than by a probe of their own). One mutant (`no_integrity`) **escaped** the first 24 probes — none of them made that
  line decisive; the damaged-index night was added and catches it.
- **The document text, re-checked:** the round-8 checks (DDL byte-identical, 76 objects, the 29
  vectors, §6.14 run literally, all §6 statements prepare) — all 26 pass on the final text.
- **Mistakes of mine, fixed before any conclusion:** the writer flood above; the `cp` experiment that
  measured btrfs reflinks; a `pkill -f` that matched its own shell (twice); an `E5b` check that looked
  for the wrong title and then called the CLI helper with `-csv` as the SQL and hung on stdin; the
  generator swapping a metric's name and id; one sentence of the first draft of §2.8 said "clean in
  480 tries" where the record says 360 (corrected), and "0.26–0.29 s at 155/s" where it was two rates.
- **Regression.** The DDL is byte-identical, so the behaviour recorded in #6–#10 is unchanged; the
  older probe suites (#6–#9) were **not re-run** — their scripts no longer exist (R4-19 d).
- **Known limits.** Synthetic data, one machine, one filesystem (btrfs) and tmpfs; the thresholds move
  with size and disk. **Power loss** is not simulated — durability is `synchronous=FULL` plus SQLite's
  documentation. **restic** is not installed: the off-box step was run with rsync to a local directory.
  The scripts were run with GNU coreutils and `sqlite3` 3.53.4 (`timeout`, `sha256sum`; macOS lacks
  both by default). The off-box copy's own corruption is out of reach of these scripts.

### Review round 10 (2026-09-30, owner: "fix everything in one go")

Every item left by round 9 is resolved, or deferred with a path that was **executed**, or recorded as an
accepted limit. The DDL changed this time (one CHECK, six `lifelog_meta` keys, a comment), so a regression
was needed — and possible: the validation suites of rounds 5–7, lost with the scratch directory (R4-19 d),
were **recovered from the session transcript** (the commands that wrote them replayed in order; the first
replay reproduced the recorded counts exactly: 139, 88, 112, 49) and are now kept in `tests/` with a
runner (`python3 tests/run_all.py`).

| Item | Resolution | Where |
|---|---|---|
| **R4-19 d** validation scripts not in the repo | recovered and kept: `tests/`, `run_all.py`, a README that maps each suite to its record; AGENTS.md now says to run it | `tests/README.md` |
| **R8-01** device names before an extension | **fixed in the DDL**: the part of the title before the first `.` is tested, and the six superscript names are listed; 12 strings flip from accepted to rejected, none the other way | D5 addendum 4, §2.5 |
| **R4-19 a** filename-safe immutable titles | **kept**, with the cost (unnamed CHECKs cannot be loosened after the freeze) and the reopen condition written down | D5 addendum 4 |
| **R4-19 b** pragmas per connection | already asserted at connect since round 7 — **closed** | §2.9 |
| **R4-19 c** threat model, 2075 test, import path | written: §2.11. The 2075 test is *executed* and found six keys missing from `lifelog_meta`: `entities`, `wikilinks`, `writers`, `export`, `backups`, `evolution` (now 25 rows); the import path is run on 1 000 rows and found three traps (below) | §2.11, D17 addendum 3 |
| **R4-15** CJK search | **decided**: keep `unicode61`; the switch to trigram (drop, create, rebuild, one transaction) was executed | §7, DDL comment |
| **R4-18** partial dates | **deferred**; executed: the unnamed `birth_day` CHECK cannot be dropped in place, a nullable `birth_approx` column with a CHECK can be added to a populated STRICT table | §7 |
| **R4-11 e** a person named in text | **deferred**; executed: one `link_kinds` row (`mention`, page→person) works and the endpoint types are enforced | §7 |
| DST residual (recurring timed events) | recorded as a deferred row with its trigger | §7 |
| orphan `entities` rows | now checked: a third verification in `nightly.sh`, with a test and a mutant | §2.8 |

**Three traps the import example would have taught, found by running it:** `CAST(x AS REAL)` turns
`'abc'` and `''` into `0.0` and `'12.5kg'` into `12.5`, silently (a plain insert into the STRICT column
converts `'12.5'` and rejects the others); `INSERT … SELECT … FROM … ON CONFLICT` needs `WHERE true`
(SQLite reads the `ON` as a join's); the conflict target must repeat the partial index's
`WHERE import_id IS NOT NULL`. And a CSV empty field is `''`, not NULL, so `taken_at` needs `NULLIF`.

**The final status of every recorded finding** (the round-4 list keeps its old "OPEN" headings as history):

| Finding | Status | Round |
|---|---|---|
| R4-01 `REPLACE` bypasses triggers | applied — `recursive_triggers=ON` | 4 |
| R4-02, 03, 06, 07, 08, 09, 10, 17 | applied (imports, retractions, no-delete triggers, named CHECKs, `recorded_at`, immutable units, no-op-safe triggers) | 5 |
| R4-04 timezone, R4-05 `completed_day` | applied | 6 |
| R4-11 graph gaps | a, c seeds (5); b, d one place per event (6); **e deferred with a tested path (10)** | 5, 6, 10 |
| R4-12 wikilink save contract | applied — D19 | 8 |
| R4-13 concurrency, durability | applied — `BEGIN IMMEDIATE`, `synchronous=FULL` | 7 |
| R4-14 backups and the way out | applied — §2.8, D12 addendum 2 | 7, 9 |
| R4-15 CJK search | **decided, switch tested** | 10 |
| R4-16 contradictions | applied | 7 |
| R4-18 partial dates | **deferred with a tested path** | 10 |
| R4-19 goals and process | a kept, b closed, c written, d done | 10 |
| R8-01 device names | **fixed** | 10 |

**Nothing recorded is open.** What is left is not a defect: **three gates only the owner can close** — the
external review the status line waits for; an off-box destination and the first restore drill from it
(§2.8); and the first real import (§2.11 step 6), the one test this schema has never had. **Accepted
limits**, unchanged: power loss is documented not simulated; restic, Windows itself and real data were
never run; widening a named CHECK is verified on SQLite 3.53.4 only; the per-connection pragmas cannot be
enforced by the file. **Deferred, each with its reopen trigger** (§7): partial dates, CJK-inside search, a
typed person mention, the local hour of recurring timed events across DST.

### Validation record #12 (2026-09-30, round 10)

§3 was extracted from this document's text (654 lines, 76 objects, `integrity_check` ok,
`foreign_key_check` clean; `lifelog_meta` 25 rows). Expected outcomes were declared before each probe ran.

- **Recovery of the lost suites.** Replaying only the file-writing commands of the transcript (heredocs,
  in-place patches) into an empty directory and running the result on the v1.8 DDL gave the recorded
  counts exactly: `regress` **139/139**, finance **88/88**, round 5 **112/112**, round 6 **49/49**, and
  the recurrence expander **212 290** occurrences, 0 mismatches, net worth **155/155** month-ends. Round 7
  (**36/36**) and the cookbook runner needed one change each, for a legitimate reason: both executed the
  §6.14 block that round 8 rewrote (they now bind its new parameters / skip its link-sync statements,
  which `tests/wikilinks` covers). The recovery is faithful because the old counts came back, not because
  it looked right.
- **The DDL change, old against new.** The same 43 458 strings (random over an alphabet of every filename
  hazard, plus device names in every case, with and without extensions and superscripts) were offered to
  the v1.8 and the new `pages` CHECK: **12 flip from accepted to rejected — all of them device names before
  an extension or superscript names — none flips the other way, none changes outside that class.**
  Independently, the app-side title predicate and the new CHECK agree on 43 309 strings (6 163 accepted),
  zero disagreements either way; the 55 extraction vectors, including the new device-name row, pass.
- **All suites on the new DDL:** `regress` 139/139, finance 88/88, round 5 112/112, round 6 49/49, round 7
  36/36, expander 212 290 / 0, net worth 155/155, 20 cookbook blocks 0 failures; wikilink vectors 55/55,
  save procedure 25/25, document checks 30/30; nightly/restore harness **26/26** (S1–S8, now including the orphan-entities night). `python3 tests/run_all.py
  --slow --mutants --datasette` runs them from a clean checkout: **16/16 suites** — the 13 fast suites in about
  12 s, the backup harness in 39 s, the 14 broken scripts in 524 s.
- **Round-10 probes: 116/116** (`tests/schema/r10probes.py`). *Titles:* 11 names accepted (`CONSOLE`,
  `CONSOLE.txt`, `a.CON`, `x.NUL`, `COM10`, `com10.x`, `LPT0`…), 20 rejected (`CON`, `con.txt`, `CON.backup`,
  `NUL.tar.gz`, `lpt9.a.b`, the six superscript names, with extensions too, `CON.`, `.CON`, `CON `, ` CON`).
  *The 2075 test:* 22 questions, each key present and each phrase in the answer, every `lifelog_meta` key
  used, numbering without gaps. *R4-18:* `birth_day` rejects `1870` and `1870-05`; `ALTER TABLE … DROP
  CONSTRAINT birth_day` fails (nothing to name); `ADD COLUMN birth_approx … CHECK` works on the populated
  STRICT table, stores `1870` and `1870-05`, rejects `abc`, `1870-13`, `1870-5`, `18700`, leaves the
  `people` trigger working. *R4-15:* before, `unicode61` finds the whole run and `zurich` but not `本語` or
  `ノート`; after drop + create + `rebuild` with `trigram remove_diacritics 1`, `本語の`, `ノート`, `日本語` and
  `zurich` are found and the two-character `本語` is still not (the recorded limit); insert and update stay in
  sync through the triggers; the FTS integrity-check passes. *R4-11 e:* a page→person link is only possible
  as `about` today; registering `mention` is one `INSERT`; a memo may then mention a person and may not
  mention a place. *Orphans:* the query finds an `entities` row without a domain row and is silent on a clean
  database. *Imports,* run from the document's SQL: 1 000 rows load, 200 empty `taken_at`/`tz` cells become
  NULL; running it again inserts nothing; ten duplicates plus ten new rows insert exactly ten; a batch with
  `'abc'`, with an empty value or with a malformed day (`CHECK` — not swallowed by `DO NOTHING`) is
  rejected as a whole; without `NULLIF` an empty `taken_at` fails its CHECK; without `WHERE true` and
  without the index's `WHERE` the statement is rejected; `CAST` stores the bad value as `0.0`;
  afterwards `integrity_check`, `foreign_key_check` and the orphan query are clean.
- **The orphan check in `nightly.sh`:** a live file with an `entities` row and no domain row — passes
  `integrity_check` and `foreign_key_check` — now stops the night with no snapshot and no `dump/`.
- **The probes can fail.** Ten breakages of the DDL or the document, each caught: the old device-name
  CHECK back (11 failures), the superscript names removed (6), the `wikilinks`, `entities` and `backups`
  keys damaged or removed, a non-partial import index, the orphan query removed from the document, a
  broken FTS insert trigger, and a row cut from the 2075 table. **The last one escaped at first**: the
  probe only asked for "at least 15 questions". It now also requires every `lifelog_meta` key to be used
  by a question and the numbering to be gap-free (the table gained question 22, `pages_kind`, which the
  coverage rule exposed). The scripts' harness gained a mutant for the orphan check; **all 14 mutants of `nightly.sh` and `restore.sh` are caught** (the 13 of record #11 and `no_orphan_check`, which S2c catches).
- **Mistakes of mine, fixed before any conclusion:** my replay's first run of round 7 and the cookbook
  runner failed on the rewritten §6.14 (expected, above); the first sentence I wrote about `WHERE true`
  said "a syntax error" where SQLite says "a JOIN clause is required before ON" (the probe caught the
  wording); a stray line in the import probes; and the 2075 table that could be shortened unnoticed.
- **Known limits.** Synthetic data only; one machine and one filesystem; Windows behaviour is from
  Microsoft's documentation (a name with a space before its dot is not covered because the documentation
  does not say it is reserved); `tests/experiments/` holds the one-off scripts behind #11 and #12 as
  evidence, not as portable tests; Datasette 0.65.5, executed in `tests/.venv`, 9/9 (its behaviour is that of that version).

### Review round 11 (2026-09-30, owner: "do we need notes and wiki pages, and treat everything like a wiki page?" — "Yes")

The question was answered from the DDL, not from memory: `note` and `wiki` differed in three things only
(the `day` rule, the day view, the ghost sweep), and already shared one title index, one set of title rules
and one `[[link]]` namespace; their two export folders were one filename space. The split also leaked — a
link to a missing title creates a **wiki** page, and `kind` and `title` are immutable, so anything linked
before it was written became a wiki page whatever it was meant to be. They are now one kind, `page`; memos
stay separate. (The owner also asked whether rows carry `created_at` and `updated_at`: yes, on `entities`,
`created_at` written by the app and `updated_at` by triggers — unchanged, §3, D12.)

| Item | Resolution | Where |
|---|---|---|
| `note` and `wiki` | one kind: `kind IN ('memo','page')`; "dated" is the `day` column, not a type | D5 addendum 5, §3 |
| `day` | required for a memo only; the app sets it on a page created on purpose and leaves it NULL on a link target | §3, §6.2, §6.14 |
| `pages_title` | `UNIQUE … WHERE title_key IS NOT NULL`; no `kind` predicate in any lookup | §2.5, §3, §6.14 |
| export | one folder, `export/pages/<Title>.md` | §2.1, D4 |
| ghost sweep and day view | `kind = 'page'`; the day view labels them `page` | §3, §6.2, §6.13 |
| 2075 test | row 22 asks what a memo and a page are; the `pages_kind` text says `untitled` and `never changes` | §2.11, `lifelog_meta` |

### Validation record #13 (2026-09-30, round 11)

§3 was extracted from this document's text (656 lines, 76 objects, `integrity_check` ok, `foreign_key_check`
clean; `lifelog_meta` 25 rows, unchanged). Expected outcomes were declared in each probe's label before it ran.

- **SQLite claims, executed first (3.53.4).** A `UNIQUE … WHERE title_key IS NOT NULL` index accepts two NULL
  keys and one real key and rejects a duplicate real key. `WHERE title_key = ?` and `= :key` plan as
  `SEARCH … USING INDEX` — an equality implies `IS NOT NULL` — and so does the §6.14 statement, taken from this
  document, on a database of 500 memos and 4 pages; a lookup by `lower(title)` is a `SCAN` (the control).
  `INDEXED BY` on the partial index answers `WHERE title_key = 'diet'` and refuses `SELECT id … WHERE title_key
  IS NULL` with `no query solution` (the index holds no memo); on a full index that query works.
  `SELECT count(*) … INDEXED BY` ignores the hint and scans the table (52 of 52 rows), so it cannot tell the
  two indexes apart — my first probe used it.
- **Round-11 probes: 43/43** (`tests/schema/r11probes.py`). *Kinds:* `memo` and `page` accepted; `note`,
  `wiki`, `image` and `''` rejected; the named CHECK lists exactly the two. *Day:* a memo without a day
  rejected; a page with and without a day accepted; a page without a title or key, a titled memo and a keyed
  memo rejected. *One namespace:* `DIET` (no day) and `diet` (another day) collide with `Diet`; NFD `Café`
  collides with NFC; 200 memos with NULL keys are accepted; a tombstoned page still holds its title.
  *Lookups:* the §6.14 resolve statement has no `kind`, is a `SEARCH` on `pages_title`, finds `Zürich` by
  `zürich`; the index is partial and its SQL has no `kind`. *Link first, write later:* `#japan-trip` in a memo
  creates an empty `page` with no day; writing it needs no new page or redirect; the memo still links to it;
  `Japan-Trip` is refused. *Ghosts and the day view, from the document's own SQL:* `ghost_pages` and §6.13
  list exactly the empty, unlinked, live pages older than 30 days (with or without a day) and not a written
  page, a linked one, a young one, a tombstoned one or an empty memo; §6.2 shows the page written that day
  once, labelled `page` and `page (edited)` after an edit, and not a link target or another day's page.
  *Fixed kind:* `page → memo` and `memo → page` are refused **by `pages_kind_fixed`** (its message is checked,
  so a CHECK that happens to fail does not count), a no-op `SET kind = kind` is accepted, a title still cannot
  change.
- **The probes can fail: 12 broken copies of this document, all noticed** (`tests/schema/r11_mutants.py`,
  1 to 20 probes failing each): the old kinds back (20), every page needing a day (7), no day required at all
  (1), the index predicate on `kind` (3), no predicate so memos are indexed (2), a non-unique index (6), the
  old `wiki` in the view (1) and in §6.13 (1), the old `note` in §6.2 (2), the old `kind` predicate in §6.14
  (2), and each of the two fixed-kind / fixed-title triggers disabled (2, 1). **My first runner counted a crash
  as "noticed" and passed 12/12 with four of them tracebacks** (a probe dereferencing a row the broken schema
  had not created). The probes are now None-safe and a crash counts as a miss.
- **Suites changed because the document legitimately changed** (in the same edit; nothing loosened):
  `regress.py` — the kind literals, and the two expectations that encoded the old day rule (*note without a
  day → ERR* is now *page without a day → OK*; the count stays 139); `r5probes.py` (the widened
  `pages_kind` text), `r7probes.py` and `wikisave.py` (the resolve statement without a `kind` predicate),
  `probes.py`, `cookbook_doc.py`, `finprobes.py`, `probes1.py`, `r10probes.py`, `title_fuzz.py`,
  `backups/mkdb.py` (kind literals); `docchecks.py` — the kind literals and a new section D9 (12 stale phrases
  absent from §1–§7, one folder in §2.1, addendum 5 present; 30 → 44 checks). `tests/experiments/r10_diff.py`
  keeps `'wiki'`: it is the frozen evidence of record #12.
- **All suites on the new DDL:** `regress` 139/139, finance 88/88, round 5 112/112, round 6 49/49, round 7
  36/36, round 10 116/116, round 11 43/43 and 12/12, expander 212 290 occurrences / 0 mismatches, net worth
  155/155 month-ends (worst difference 0.49), 20 cookbook blocks 0 failures, vectors 55/55, title fuzz 43 309
  strings (6 163 accepted; the app predicate and the CHECK disagree on none), save procedure 25/25, document
  checks 44/44; nightly/restore harness 26/26; all 14 script mutants noticed. `python3 tests/run_all.py
  --slow --mutants`: **17/17 suites** (the 15 fast suites in about 10 s, the backup harness in 22 s, the
  script mutants in 275 s).
- **Mistakes of mine, fixed before any conclusion:** the `count(*) … INDEXED BY` probe (above); a probe for
  "a second page with that title" that used a different key (`Japan Trip` against the tag `japan-trip`); a
  ghost comparison sorted by `id` instead of `title`; the runner that counted crashes (above); and a search
  for old kind literals that excluded `tests/backups/`. **The last one the fast suites could not see:** they
  passed 15/15 while `backups/mkdb.py` still inserted `kind='wiki'`; the `--slow` run crashed on
  `CHECK constraint failed: pages_kind`, which is why AGENTS.md asks for `--slow --mutants` when the DDL
  changes.
- **Known limits.** A ghost that is written later keeps `day = NULL` and so never appears in a day view (as a
  wiki page never did; `*_day` is never recomputed). An empty, unlinked page created on purpose is swept like
  a ghost after 30 days (D5 addendum 5). The title CHECKs are unchanged and still unnamed (D5 addendum 4).
  Synthetic data only; no canonical database exists, so no row was migrated.

### Review round 12 (2026-09-30, owner: "simplify for now the schema.md document and remove anything mentioning markdown export or backup or dumps, for now we just focus on the schema and its reliability")

A scope cut, not a redesign: the DDL keeps every table, column, CHECK, index, view and trigger. What went is
everything about *copies* of the data — the markdown export (`export/`), the snapshot / retention / off-box /
restore contract with its two scripts (§2.8), the CSV dump, the `backups/` and `dump/` layout, and the tests of
those scripts. What stayed is everything about *the file itself*: WAL, `synchronous=FULL`, `BEGIN IMMEDIATE`
(§2.9), the integrity checks (now §2.8, executed on the live file), the import path and the read-only tools.

| Item | Resolution | Where |
|---|---|---|
| §2.8 Backups | replaced by *Integrity checks*: `integrity_check`, `foreign_key_check` and the orphan query, each with what it does and does not see | §2.8 |
| §2.1 layout, `.gitignore` | `life.db` alone; the file and its `-wal` / `-shm` are never committed | §2.1 |
| D4 | "the database is canonical" stays with its two arguments against files-canonical; the mirror half is withdrawn | D4 |
| D12 | "no revision tables" stays, its metadata text made exact; git-over-export and snapshots (incl. addendum 2) withdrawn | D12 |
| D13 | down-migrations: a migration runs on a copy first and the three checks must pass (was: "restore the last snapshot") | D13 |
| `lifelog_meta` | keys `export` and `backups` removed (25 → 23 rows); `titles` no longer says "export filenames" | §3 |
| 2075 test | questions 17 (readable copies, history) and 18 (restore) removed: 20 questions | §2.11, D17 add. 4 |
| threat model | "file damaged or lost" now says no second copy is kept; the off-box and mirror rows are reduced to the disk and git | §2.11 |
| title rules | **kept unchanged** — they were introduced for export filenames, but loosening one later is a table rebuild (unnamed CHECKs) and the strict rule needs no exporter | D5 add. 6 |
| import step 1 | "snapshot first" became "trial run on a copy first"; a bad import is retracted row by row (rows are never deleted) | §2.11 |
| §7 | one row for what was withdrawn and when to reopen it (before the first real data); the Litestream row is folded into it | §7 |
| references | R23–R25, R33, R37–R40 and R60–R62 dropped (cited only by withdrawn text); numbers are not reused | §9 |
| owner gates | three (round 10) are now two: the external review and the first real import | status line |

### Validation record #14 (2026-09-30, round 12)

Baseline first: all 15 suites passed on v1.12 before the first edit. Then §3 was extracted from this document's text
(652 lines, 76 objects, `integrity_check` ok, `foreign_key_check` clean; `lifelog_meta` 23 rows, was 25). The DDL differs from
v1.12 in comments and in the two removed and one reworded `lifelog_meta` rows only. Expected outcomes were declared in
each probe's label before it ran.

- **SQLite claims, executed on the live file (3.53.4; 3 000 pages of prose, 1.8 MB, no snapshot involved).** Before, the
  checks had only been run on `VACUUM INTO` copies (#11). *Clean file:* `integrity_check` ok, `foreign_key_check` and the
  orphan query empty. *Zeroed table page:* `integrity_check` reports errors. *Truncated by three pages:* the file is
  reported malformed. *Flipped byte in a `title_key` inside `pages_title`:* `row … missing from index pages_title`.
  *Flipped byte inside a body value:* the text changed and `integrity_check` still says `ok`. *Orphan balance written
  with `foreign_keys=OFF`:* `integrity_check` ok, `foreign_key_check` reports `balances|1|accounts`, orphan query empty.
  *`entities` row without a domain row:* both PRAGMAs clean, the orphan query returns exactly that id. 7 of 7 as declared.
- **Round-12 probes: 33/33** (`tests/schema/r12probes.py`): the same seven, taken from the block printed in §2.8 (three
  statements, executed literally, on a database that holds a row of every domain type so a query that forgets one table
  reports a false orphan); `lifelog_meta` has 23 rows and no `export` / `backups`; the 2075 table has 20 questions numbered
  1–20; the live text (§1–§7) holds none of `export/`, `dump/`, `backups/`, `nightly.sh`, `restore.sh`, `OFFBOX`, `restic`,
  `rsync`, `.sha256`, `exporter`, `Litestream`, `off-box` only where the withdrawal is recorded, and every remaining line that
  says "export" is a tool feature, a standards remark or that record; the DDL text names no exporter, folder or nightly job;
  `tests/` holds no backup harness.
- **The probes can fail: nine broken copies of this document, all noticed** (`tests/schema/r12_mutants.py`, 1 to 6 probes
  failing each): the `export` key back (6), the `backups` key back (5), a `backups/` folder back in the layout (1), the orphan
  query without `accounts` (3), the `foreign_key_check` line gone from §2.8 (6), the imports step running `nightly.sh` again
  (1), the `titles` row saying "export filenames" again (1), a 2075 question dropped (1), and the off-box copy mandatory again
  in the threat model (2). The **unchanged v1.12 document** passes 5 of the 33.
- **Suites changed because the document legitimately changed** (same edit; nothing loosened): `r7probes.py` D2 and D3 guarded
  round-7 wording about `backups/` being "derived" and principle 5 naming the snapshots — rewritten to the new layout and
  principle 5 (count unchanged, 36); `r10probes.py` labels only (116 → 112: two 2075 questions × two checks); `docchecks.py` D9b
  (the layout is `life.db` alone; 44 → 44); `run_all.py` and `tests/README.md` (`--slow`, `--mutants`, the backup harness and its 14
  script mutants removed; the two r12 suites added). **Removed with the contract they tested:** `tests/backups/` (26 harness
  checks, 14 mutants) and the one-off `tests/experiments/r9_*` scripts behind #11 — in git history. `experiments/r10_diff.py`
  stays: it is the evidence of #12.
- **All suites on the new DDL:** `regress` 139/139, finance 88/88, round 5 112/112, round 6 49/49, round 7 36/36, round 10
  112/112, round 11 43/43 and 12/12, round 12 33/33 and 9/9, expander 212 290 occurrences / 0 mismatches, net worth 155/155
  month-ends (worst difference 0.49), 20 cookbook blocks 0 failures, vectors 55/55, title fuzz 43 309 strings (6 163 accepted; the
  app predicate and the CHECK disagree on none), save procedure 25/25, document checks 44/44. `python3 tests/run_all.py`:
  **17/17 suites**, about 15 s.
- **Mistakes of mine, fixed before any conclusion:** my first index-corruption probe asked for the `dbstat` virtual table,
  which this `sqlite3` build does not have — the error surfaced as a *parse* error and crashed the script, not as a failed
  probe — so it now locates the index cell by scanning the file for the key's bytes; the first `r12probes` run failed four
  of my own stale-phrase patterns (`export/interop` in D7, `append-only snapshots` for balances, `a mirrored edge` for
  symmetric links, `health export` in the imports text — all legitimate) and one insert of a currency that is already seeded;
  `r7probes` D2 tripped on `export/interop` the same way. The patterns are narrowed, and the "remaining `export` lines"
  check whitelists three literal fragments of the withdrawal record — it will need touching if that text is re-wrapped.
- **Known limits.** **There is no second copy of `life.db` in this design any more** (§7 row; the reopen trigger is the first
  real data). Records #1–#13, the *Document history* footer and the older addenda of D5, D13 and D17 still name the withdrawn
  contract in the past tense: they are history and were not edited (AGENTS.md); git holds the full earlier text. The title
  rules keep their filename form although no code writes files (D5 addendum 6). The integrity checks find damage to the
  file's structure and to constraints, not a changed value (E5); one machine, one filesystem, synthetic data, no power-loss
  test; restic, Windows and real data were never run.

---

---

## 9. References

What each source contributed to the decisions above. Reference numbers are never reused: R23–R25,
R33, R37–R40 and R60–R62 were dropped in round 12 with the text that cited them.

### SQLite durability, format, and features

- **[R1]** SQLite: *The SQLite Application File Format* (essay) —
  <https://www.sqlite.org/appfileformat.html>
  "Data lives longer than code"; the SQL schema as human-readable documentation of the
  format; additive schema change as the compatibility mechanism; Application ID. → D1,
  D2, D13, §2.7.
- **[R2]** SQLite: *35% Faster Than the Filesystem* — internal-vs-external BLOB
  benchmarks — <https://www.sqlite.org/intern-v-extern-blob.html>
  Blobs < ~100 KB faster in-DB; larger blobs faster as files; page-size guidance. → D9.
- **[R3]** SQLite: *Faster Than The Filesystem* — <https://www.sqlite.org/fasterthanfs.html>
  Companion benchmark (10 KB blobs ~35% faster in DB, 20% less space). → D9.
- **[R4]** SQLite: *AUTOINCREMENT* — <https://www.sqlite.org/autoinc.html>
  `INTEGER PRIMARY KEY` = rowid alias; AUTOINCREMENT overhead, "usually not needed". → D3.
- **[R5]** SQLite: *Date And Time Functions* — <https://www.sqlite.org/lang_datefunc.html>
  `strftime('%Y-%m-%dT%H:%M:%fZ','now')`; 'now' is UTC. → §2.2, D10.
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
- **[R50]** sqlite-web — <https://github.com/coleifer/sqlite-web> (**not used** — it edits rows, i.e. is a second writer; it does have `-r/--read-only`. D14 addendum)
  Web-based table browser with row insert/update/delete, CSV/JSON import-export.

### Round 4 — SQLite behaviour and money (D18, §8 #6)

- **[R53]** SQLite: *ON CONFLICT clause* — <https://www.sqlite.org/lang_conflict.html>
  "When the REPLACE conflict resolution strategy deletes rows in order to satisfy a
  constraint, delete triggers fire if and only if recursive triggers are enabled." The reason
  `PRAGMA recursive_triggers = ON` is mandatory (§2.9). (The page's wording on which
  constraints `IGNORE` skips is loose; what it does was executed — §8 #6.) → D18, §2.4, §2.9.
- **[R54]** SQLite: *PRAGMA statements* — <https://www.sqlite.org/pragma.html>
  `synchronous=NORMAL` in WAL mode: "A transaction committed in WAL mode with
  synchronous=NORMAL might roll back following a power loss or system crash" (R4-13);
  `recursive_triggers` is a per-connection setting, off by default. → §2.9, §8 round 4.
- **[R55]** SQLite: *ALTER TABLE* — <https://www.sqlite.org/lang_altertable.html>
  `ALTER COLUMN … SET/DROP NOT NULL` arrived in 3.53.0 (2026-04-09). The page as fetched does
  not describe `ADD/DROP CONSTRAINT` for CHECK; that behaviour, and its restriction to *named*
  constraints, was found by executing it on 3.53.4 (§8 #6). → D18, R4-08.
- **[R56]** Beancount `balance` directive (via the `beancount_ex` library docs — the official
  syntax page URL tried returned 404): asserts an account's balance at the *beginning* of a
  date — the reason `balances.day` states "end of day" explicitly.
  <https://beancount-ex.hexdocs.pm/0.6.0/Beancount.Directives.Balance.html>. → D18.
- **[R57]** ISO 4217 (as tabulated at <https://en.wikipedia.org/wiki/ISO_4217>): the minor-unit
  exponent is a per-currency fact (JPY 0, KWD 3), and a few currencies (MRU, MGA) subdivide
  by 5 — why `currencies.subunits` is a per-row integer and not a global "cents" assumption. → D18.

### Round 8 — the wikilink save contract (D19)

- **[R58]** Microsoft Learn: *Naming Files, Paths, and Namespaces* —
  <https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file>
  Reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, and the superscript
  digits `COM¹ COM² COM³ LPT¹ LPT² LPT³`): "avoid these names followed immediately by an extension; for
  example, NUL.txt and NUL.tar.gz are both equivalent to NUL"; no trailing space or period. The
  DDL covers the bare names only (R8-01). → §2.5.
- **[R59]** markdown-it-py 4.2.0, the Python port of markdown-it, a CommonMark-compliant parser —
  <https://github.com/executablebooks/markdown-it-py>; the specification it implements is
  <https://spec.commonmark.org/>. Used as the reference reader in §8 #10: which text it hands
  back (code spans, fences, indented code, raw HTML and image alt text are not text; escapes and
  entities are decoded; `#Heading` without a space is not a heading) was executed, not read from
  the spec. → §2.5, D19.

### Recurrence (D15)

- **[R51]** Stack Overflow: *Should I store dates or recurrence rules in my database when
  building a calendar app?* —
  <https://stackoverflow.com/questions/4239871/should-i-store-dates-or-recurrence-rules-in-my-database-when-building-a-calendar>
  The canonical store-the-rule-vs-materialize-the-instances discussion; the accepted
  answer separates "canonical" (the rule) from "serving" (generated dates) — exactly
  D15's template/expand-at-read split.
- **[R52]** Microsoft Learn: *dbo.sysschedules (Transact-SQL)* —
  <https://learn.microsoft.com/en-us/sql/relational-databases/system-tables/dbo-sysschedules-transact-sql>
  The structured-interval family in production since SQL Server 7: `freq_type` /
  `freq_interval` / `freq_recurrence_factor` / `freq_relative_interval` — the shape
  `repeat` / `repeat_weekdays` / `repeat_every` / `repeat_position` follows.

---

## Appendix A — Prior-art survey summary

The seven real systems surveyed during research, and exactly what was taken from each:

| System | Scale / longevity | Shape | Taken into this design | Rejected from this design |
|---|---|---|---|---|
| FxLifeSheet [R9][R42] | 380k points, 6+ yrs | One `raw_data` table; metric registry in config | measurements shape, `import_id` idempotency, denormalized time buckets (`day`), capture-friction philosophy | value-as-TEXT (we use REAL), Postgres, 8 separate time-bucket columns (one `day` suffices) |
| ark [R46] | 700k items, 125 GB store + 9 GB SQLite | Content-addressed files; SQLite index; typed edges | `media/` sha256 store, `links` as the one graph, "everything about a person" query | annotations layer, classification/quality subsystems |
| Open Brane [R43] | 942k rows, 3 GB | One append-only 8-column table; no FKs | append-only spirit for measurements, `INSERT OR IGNORE` idempotency, blobs-outside-DB | payload_json column (violates D2), no-FK design (violates D8) |
| health-mcp [R8] | Years of use | Typed biomarker tables; two-tier wearables; forward-only migrations | UTC+local-day convention, forward-only numbered migrations, metric registry concept | LOINC/UCUM/ref-ranges, raw mirror tier (both deferred, §7) |
| Myome [R45] | Design paper | TSDB + SQLite + object store | Scale calibration (~5 GB/lifetime → no rollups needed) | TSDB, FHIR machinery, multi-resolution storage |
| Kaydet [R41] | 9 yrs daily entries | Plain text + SQLite index | Evidence that boring survives; hybrid text+DB instinct (resolved as D4: the database is canonical) | Files-as-canonical (owner's writer-drift objection) |
| Logseq OG vs DB [R34]–[R36] | Product-scale split | Files-canonical vs SQLite-canonical | The decisive precedent for D4: a team that hit live-editing limits chose DB-canonical | Block-level datom model, collaboration machinery |

---

*Document history: v1.13, 2026-09-30 — round 12 (§8 #14): the document narrowed to the schema and its reliability — the markdown export and the snapshot / restore / dump contract (§2.8, D12 addendum 2) and their tests are withdrawn (§7); D4 and D12 keep their decisions (the database is canonical; no revision tables); §2.8 is now the three integrity checks, executed on the live file; the `export` and `backups` keys and two 2075 questions are gone from the DDL; validation record #14. v1.11, 2026-09-30 — round 10 (§8): every remaining finding closed or deferred with an executed path — R8-01 fixed in the DDL (device names before an extension, superscript names), six `lifelog_meta` keys added by the now-executed 2075 test, §2.11 (threat model, 2075 test, import path), D5 addendum 4 and D17 addendum 3, four deferred rows in §7, the orphan-entities check in `nightly.sh`; the lost validation suites recovered and kept in `tests/` with `run_all.py` (R4-19 d); validation record #12. v1.10, 2026-09-30 — round 9 (§8): the backup contract of R4-14 (D12 addendum 2; §2.8 rewritten with a tested nightly script and restore script, §2.1 `.gitignore` and `dump/`): `VACUUM INTO` instead of `.backup`/`cp`, verification, retention, mandatory off-box copy, restore rules, CSV dump never in git — the DDL is unchanged; validation record #11. v1.9, 2026-09-30 — round 8 (§8): the wikilink save contract of R4-12 (D19; §2.5 rules and test vectors, §6.14 rewritten as the full save) — the DDL is unchanged; `#tag` is read, not expanded; a stub is not scanned; §6.5 excludes `redirect`; validation record #10. v1.8, 2026-09-30 — round 7 (§8): the text inconsistencies of R4-16 and five more found in a re-scan (snapshots are not derived, natural-key registries, "export mirrors 100%", readers must be read-only, D12 audit metadata); `synchronous = FULL` and `BEGIN IMMEDIATE` for every write transaction (R4-13), §6.14 resolves inside one transaction; validation record #9. v1.7, 2026-09-30 — round 6 (§8): `entities.tz` / `measurements.tz` (IANA zone at capture), `tasks.completed_day` and a `'done'` row in the day view, `events.place_id` as an event's only place (`visited` person→place only, `lives-in` removed); validation record #8. v1.6, 2026-09-30 — round 5 (§8): the mechanical round-4 fixes (`ON CONFLICT` imports, `(source, import_id, metric_id)` key, retractable measurements, `recorded_at`, enforced no-delete triggers, all enumerated CHECKs named, immutable `metrics.unit`, `located-in`/`parent-of`, no-op-safe immutability triggers); finance scope settled as value-snapshot accounts (stocks, crypto, deposits, valuables — D18 addendum 1); validation record #7. v1.5, 2026-09-30 — round 4 (§8): independent review with 19 recorded findings (R4-01…R4-19; R4-01 applied: `recursive_triggers=ON` mandatory) and the finance tier D18 (`currencies`, `accounts` as a sixth entity type, append-only `balances`, `fx_rates`; net worth derived, §6.15–6.18; validation record #6). v1 draft, 2026-09-29 — initial synthesis of two research rounds and
the owner's decisions (scope narrowed to DB + UI; Model B chosen after the writer-drift
objection; journal merged into memos-as-inbox). v1.1, same day — DDL mechanically
validated on SQLite 3.53.4 (§8 validation record); §2.9 connection pragmas added after
FK enforcement was found OFF by default; D2 wording corrected on STRICT semantics.
v1.4, same day — round 3c (§8 #5): titles immutable; `title_key` (NFC + casefold, app-computed) makes title uniqueness Unicode-proof; §6.14. v1.3, same day — round 3b (§8 #4): filename-safe titles, endpoint-typed links (registry closed even without the FK), `pages.kind` fixed, tombstones bump `updated_at`. v1.2, same day — review round 3 (§8 #3): measurements made genuinely append-only and
the read view indexed, link registry closed with mirror-on-delete, recurrence CHECKs
and one oracle-verified expander, title rules, cookbook fixes. Frozen pending external review; on approval, §3 becomes `db/migrations/0001_init.sql`
verbatim and the schema enters additive-only mode.*
