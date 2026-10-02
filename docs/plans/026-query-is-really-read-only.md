# Plan 026: `POST /query` (and the MCP `query` tool) runs one read and nothing else

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/core/read.go internal/core/core_test.go internal/db/db.go internal/db/db_test.go internal/api/handler.go`
> If any in-scope file changed since `cad659b`, compare the "Current state" excerpts with the live code; on a
> mismatch, STOP.

## Status

- **Priority**: P1 — security, confirmed by execution
- **Effort**: M
- **Risk**: LOW (legitimate SELECTs keep working)
- **Depends on**: none (025 first only for the index)
- **Category**: security
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

`life.db` holds health data and private notes. `POST /query` — also the MCP tool `query`, offered to every local
model with `ReadOnlyHint: true` — is documented as "one statement on a `mode=ro`, `query_only` connection" (README,
"Read-only queries"). It is not read-only. Executed at `9ac130f` on a throwaway database, three calls of
`Store.Query` — first `PRAGMA query_only=0`, then `ATTACH` of the same `life.db` file under a second name, then an
`UPDATE` of `pages.body` through that name — changed a page body, read back by the writer. `mode=ro` covers only the
main database, `query_only` can be switched off by the statement text, the change persists on the pooled
connection, and `VACUUM INTO` can copy the whole file to any path. So any agent (or any caller of the API) can write
rows that skip `core.Tx`, the foreign keys, provenance and the owner-only gates, and can copy the data anywhere.
After this plan, the query path accepts exactly one `SELECT`/`WITH`/`VALUES`/`EXPLAIN` statement, on a connection
that cannot attach a database, with its output capped.

## Current state

- `internal/core/read.go:347-389` — the ad-hoc query (all callers go through it):
  ```go
  // Result is the answer to an ad-hoc read-only query.
  type Result struct {
  	Columns   []string `json:"columns"`
  	Rows      [][]any  `json:"rows"`
  	Truncated bool     `json:"truncated"`
  }

  // Query runs one statement on the read-only pool (mode=ro, query_only): any SQL may be sent, none can write.
  func (s *Store) Query(ctx context.Context, q string, maxRows int) (*Result, error) {
  	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
  	defer cancel()
  	rows, err := s.DB.R.QueryContext(ctx, q)
  	if err != nil {
  		return nil, invalid("%v", err)
  	}
  	defer rows.Close()
  	cols, _ := rows.Columns()
  	res := &Result{Columns: cols, Rows: [][]any{}}
  	for rows.Next() {
  		if len(res.Rows) == maxRows {
  			res.Truncated = true
  			break
  		}
  		... scan into vals; []byte → string ...
  		res.Rows = append(res.Rows, vals)
  	}
  	if err := rows.Err(); err != nil {
  		return nil, invalid("%v", err)
  	}
  	return res, nil
  }
  ```
  `modernc.org/sqlite` executes **every** statement of a multi-statement string here and returns the last one's rows.
- `internal/db/db.go:91-105` — the reader DSN:
  ```go
  func dsn(path string, readOnly bool) string {
  	q := url.Values{}
  	if readOnly {
  		q.Set("mode", "ro")
  		q.Add("_pragma", "query_only(1)")
  		q.Add("_pragma", "trusted_schema(0)")
  	} else {
  		q.Set("_defensive", "1")
  		...
  ```
- Callers of `Store.Query` (all keep working — each sends one `SELECT`):
  - `internal/api/handler.go:775` — `POST /query`, `h.s.Query(r.Context(), v.Get("sql"), 500)`.
  - `internal/api/handler.go:377-392` — `linkKinds` builds SQL by **string concatenation** of the page type:
    ```go
    res, err := h.s.Query(ctx, `SELECT kind FROM link_kinds WHERE kind NOT IN ('wikilink', 'redirect')
                                 AND (from_types IS NULL OR instr(','||from_types||',', ','||'`+typ+`'||',') > 0) ORDER BY kind`, 100)
    ```
    (`typ` is a stored `entity_type`, CHECKed to page/person/place, so not exploitable today — but it is SQL built
    from data, on the same path.)
  - `internal/api/import.go:90` — `SELECT file FROM pragma_database_list WHERE name = 'main'`.
  - `internal/importer/status.go:193` and `:206` — two `SELECT`s (plan 037 replaces them; do not touch them here).
- `internal/mcp/mcp.go:79-84` — `annotations`: `query` is marked `ReadOnlyHint: true`. After this plan that is true;
  leave it.
- The modernc driver exposes `sqlite.Limit(c *sql.Conn, id int, newVal int) (int, error)`
  (`modernc.org/sqlite@v1.60.1/sqlite.go:1339`), and the limit id constant is `sqlite3.SQLITE_LIMIT_ATTACHED` in
  package `modernc.org/sqlite/lib`. `internal/core/core.go:15-16` already imports both:
  `"modernc.org/sqlite"` and `sqlite3 "modernc.org/sqlite/lib"`.
- Error convention: core returns `*core.Error` with an HTTP-shaped status; use the helpers in
  `internal/core/core.go:33-35` (`invalid(...)` → 422). Match them.
- Test convention: `internal/core/core_test.go` (package `core`, helper `fresh(t) *Store`, `ctx`, `status(err)`).
  The existing test to extend is `TestQueryCannotWrite` at `internal/core/core_test.go:203-212`:
  ```go
  func TestQueryCannotWrite(t *testing.T) {
  	s := fresh(t)
  	if _, err := s.Query(ctx, "DELETE FROM link_kinds WHERE kind = 'related'", 10); status(err) != 422 {
  		t.Errorf("a write through Query: %v", err)
  	}
  	r, err := s.Query(ctx, "SELECT kind FROM link_kinds ORDER BY kind", 3)
  	if err != nil || len(r.Rows) != 3 || !r.Truncated {
  		t.Errorf("rows %v truncated %v err %v", r, r != nil && r.Truncated, err)
  	}
  }
  ```

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Unit tests | `go test ./internal/core ./internal/db ./internal/api` | `ok` ×3 |
| Vet | `go vet ./...` | exit 0 |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**:
- `internal/core/read.go` (Query, a new statement check, a new `LinkKindsFrom` read)
- `internal/core/core_test.go` (tests)
- `internal/db/db.go` (reader DSN: `_defensive=1`)
- `internal/db/db_test.go` (one assertion)
- `internal/api/handler.go` (`linkKinds` only: call the new core read)

**Out of scope**:
- `internal/importer/status.go` — its two queries are replaced in plan 037.
- `internal/mcp/mcp.go` — the annotation is correct once this lands.
- `docs/` — the docs never name the app; the README's "Read-only queries" decision is already what this plan makes true.
- Any change to `POST /query`'s response shape (`columns`, `rows`, `truncated`).

## Git workflow

- Branch `advisor/026-query-read-only`; one commit, e.g.
  `core: an ad-hoc query is one read statement on a connection that cannot attach` with a body naming the finding
  and `Ran: go generate ./... && go vet ./... && go test ./...`.

## Steps

### Step 1: Write the failing test first

In `internal/core/core_test.go`, add `TestQueryIsOneReadStatement`. Build it as a table of statements that **must be
refused with status 422**, each followed by a check that nothing changed:

- `PRAGMA query_only = 0`
- `SELECT 1; PRAGMA query_only = 0` (a second statement)
- `ATTACH ':memory:' AS x`
- `VACUUM INTO '<t.TempDir()>/copy.db'` (build the path with `filepath.Join(t.TempDir(), "copy.db")` and
  `filepath.ToSlash`), and assert afterwards that the file does **not** exist (`os.Stat` returns an error)
- `DETACH x`, `CREATE TEMP TABLE t(a)`, `BEGIN`, `REINDEX`, `ANALYZE`

and a table of statements that **must succeed**:

- `SELECT kind FROM link_kinds`
- `  -- a comment\n  with x(a) as (select 1) select a from x`
- `VALUES (1), (2)`
- `EXPLAIN QUERY PLAN SELECT * FROM pages`
- `SELECT ';' AS semi, 'it''s; fine' AS q FROM link_kinds LIMIT 1` (a `;` inside literals is not a separator)
- `SELECT 1;` and `SELECT 1;   -- trailing comment` (one statement, trailing `;`)
- `SELECT * FROM pragma_table_info('pages')` (how a pragma is read)

After the refused table, assert the pool is still read-only: `s.Query(ctx, "SELECT * FROM pragma_query_only", 1)`
returns one row with value `int64(1)` (run it 4 times, since the pool has several connections).

Also add `TestQueryCapsItsOutput`: `s.Query(ctx, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n LIMIT 10) SELECT printf('%.*c', 1000000, 'x') FROM n", 500)`
returns `Truncated == true` and fewer than 10 rows (the byte cap of Step 3 is 4 MiB).

**Verify**: `go test ./internal/core -run 'TestQueryIsOneReadStatement|TestQueryCapsItsOutput'` → FAIL (several
refused statements succeed today). This proves the test bites.

### Step 2: The statement check

In `internal/core/read.go`, add an unexported function:

```go
// oneRead refuses anything but a single SELECT, WITH, VALUES or EXPLAIN statement: the ad-hoc query is a read,
// and its text must not change the connection (PRAGMA), reach another file (ATTACH, VACUUM INTO) or run twice.
func oneRead(q string) error
```

Implement it as a small scanner over the string (no regexp over the whole SQL):
- Skip whitespace, `-- …` to end of line, and `/* … */` comments.
- Skip literals and quoted identifiers without looking inside: `'…'` (a doubled `''` is an escaped quote), `"…"`
  (doubled `""`), `` `…` `` and `[…]`.
- The first word (letters only, case-insensitive) must be one of `SELECT`, `WITH`, `VALUES`, `EXPLAIN`;
  otherwise return `invalid("only one SELECT, WITH, VALUES or EXPLAIN statement may be sent (read a pragma as SELECT * FROM pragma_table_info('pages'))")`.
- A `;` outside literals and comments may be followed only by whitespace, comments and further `;`; any other
  character after it → `invalid("one statement at a time")`.
- An unterminated literal or comment is **not** an error here (SQLite reports it when it prepares).

Call it first thing in `Query`: `if err := oneRead(q); err != nil { return nil, err }`.

**Verify**: `go test ./internal/core -run TestQueryIsOneReadStatement` → PASS.

### Step 3: A connection that cannot attach, and a byte cap

Still in `Query`, run the statement on one dedicated connection instead of the pool:

```go
conn, err := s.DB.R.Conn(ctx)
if err != nil {
	return nil, err
}
defer conn.Close()
if _, err := sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0); err != nil {
	return nil, err
}
rows, err := conn.QueryContext(ctx, q)
```

(add the imports `"modernc.org/sqlite"` and `sqlite3 "modernc.org/sqlite/lib"` to `read.go` — the same aliases as
`core.go`). Then cap the total size: keep a running `size` (add `len(s)` for every string value after the `[]byte →
string` conversion, and 8 for any other value); when `size > 4<<20` set `res.Truncated = true` and stop, exactly like
the row cap.

Add `TestQueryConnectionCannotAttach` (package `core`, so it may reach unexported code): take
`conn, _ := s.DB.R.Conn(ctx)`, call `sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0)`, then
`conn.ExecContext(ctx, "ATTACH ':memory:' AS x")` must return an error. This pins the driver behaviour this plan
relies on.

**Verify**: `go test ./internal/core` → ok.

### Step 4: Readers run in defensive mode

In `internal/db/db.go`, `dsn`, add `q.Set("_defensive", "1")` to the read-only branch too (before the pragmas).
In `internal/db/db_test.go`, `TestPragmasAndReaders`, add: `if !strings.Contains(dsn("x.db", true), "_defensive=1")
{ t.Error("readers are not defensive") }`.

**Verify**: `go test ./internal/db` → ok.

### Step 5: `linkKinds` stops building SQL from data

In `internal/core/read.go`, add:

```go
// LinkKindsFrom are the kinds a page of this entity type may start (link_kinds.from_types; NULL = any type).
func (s *Store) LinkKindsFrom(ctx context.Context, typ string) ([]string, error)
```

with the same SQL as today's `linkKinds`, but `typ` bound as a parameter (`… ','||?||',' …`), reading through
`s.DB.R.QueryContext`. In `internal/api/handler.go:377-392`, make `linkKinds` call `h.s.LinkKindsFrom(ctx, typ)`
and keep its filtering of `at` for non-day pages unchanged.

**Verify**: `go test ./internal/api` → ok (`TestActionsAreOfferedOnlyWhereLegal` covers the link-kind options).

### Step 6: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0. `grep -n "'\`+typ" internal/api/handler.go` → no match.

## Test plan

- `TestQueryIsOneReadStatement` (refused and accepted tables, the pool stays `query_only`, no file created).
- `TestQueryCapsItsOutput` (byte cap).
- `TestQueryConnectionCannotAttach` (the driver limit holds).
- Existing `TestQueryCannotWrite` keeps passing unchanged (a `DELETE` is now refused by `oneRead`, still 422).
- `db_test.go`: the reader DSN is defensive.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] The three new core tests exist and pass; Step 1 was seen failing before Step 2
- [ ] `grep -n "R.QueryContext(ctx, q)" internal/core/read.go` → no match (Query uses its own connection)
- [ ] `grep -n "_defensive" internal/db/db.go` → two matches (writer and reader)
- [ ] `git status --short` lists only in-scope files; status row for 026 updated in `docs/plans/README.md`

## STOP conditions

- `sqlite.Limit` or `SQLITE_LIMIT_ATTACHED` does not exist in the module version in `go.mod`, or the ATTACH test
  of Step 3 still attaches — report; do not replace it with a string check alone.
- Adding `_defensive=1` to readers makes any existing test fail — report which (modernc refuses some DSN
  combinations under `_defensive`; it must not be worked around by dropping `mode=ro`).
- An existing caller's SQL is refused by `oneRead` (e.g. a caller sends a `PRAGMA`) — report the caller; do not
  widen the allowlist.

## Maintenance notes

- Any new internal read should be a typed method on `Store`, not a call of `Query` with built SQL (plan 037 moves the
  importer's two remaining ones).
- If a future SQLite adds a writing statement that can start with `WITH` and reach another file, the attach limit and
  `mode=ro` remain; review `oneRead`'s allowlist on SQLite upgrades.
- A reviewer should check that the scanner treats `[a;b]` and `` `a;b` `` as one identifier each (add
  `SELECT 1 AS [a;b]` to the accepted table).
