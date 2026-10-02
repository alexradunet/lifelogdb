# Plan 027: `lifelog serve` answers only the owner's own browser and tools on this machine

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- cmd/lifelog/main.go internal/api/handler.go internal/api/guard.go internal/api/guard_test.go internal/client/client.go README.md`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P1 — security
- **Effort**: S–M
- **Risk**: LOW (the in-process CLI and MCP never pass through the new guard; the browser's same-origin forms keep working)
- **Depends on**: none. Independent of 026 (which closes the SQL side); land both.
- **Category**: security
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

`lifelog serve` mounts the whole API on `127.0.0.1:7777` with no authentication, on the premise that only the owner
can reach it (README: "`serve` binds 127.0.0.1 … the API is for this machine"). That premise does not hold for a
browser. **Any web page the owner has open** can submit an HTML form to `http://127.0.0.1:7777/...`: a
form-urlencoded POST is a "simple" request, needs no CORS preflight, and the handler accepts it — verified with
`httptest` at `9ac130f`: a POST carrying a foreign `Origin` and `Sec-Fetch-Site: cross-site` got 200 for `create-page`
and for the owner-only `register-metric` (a browser without the `Lifelog-Source` header writes as `ui`, which
`ownerOnly` lets through). And because the `Host` header is never checked, a DNS-rebinding page becomes same-origin
with the server and can **read** every resource, `POST /query` included. Finally `--addr 0.0.0.0:7777` is accepted
silently, which puts all of it on the network. After this plan: a cross-site browser write is refused (403), a
request whose `Host` is not this server's loopback name is refused, `serve` refuses a non-loopback address, and the
HTML responses carry headers that stop framing, sniffing and caching.

## Current state

- `cmd/lifelog/main.go:184-193` — the server:
  ```go
  case "serve":
  	if o.url != "" {
  		return errors.New("serve opens the file itself: drop --url")
  	}
  	srv := &http.Server{Addr: o.addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
  	go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
  	fmt.Fprintf(os.Stderr, "lifelog: serving %s on http://%s\n", o.db, o.addr)
  	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
  		return err
  	}
  	return nil
  ```
  `o.addr` defaults to `"127.0.0.1:7777"` (`main.go:74`) and is taken verbatim from `--addr`.
- `internal/api/handler.go:30-82` — `api.New(s, ws) http.Handler` builds the mux. The **same handler** is used
  in-process by the CLI and MCP (`internal/client/client.go:24-41`, `handlerTransport` calls `h.ServeHTTP`
  directly, with base URL `http://lifelog.local`). So the guard must **not** go inside `api.New` — it wraps the handler
  only in `serve`.
- `internal/api/handler.go:148-161` — `write` sets only `Content-Type`.
- `internal/api/handler.go:181-205` — `form` decodes JSON bodies with no size limit (`json.NewDecoder(r.Body)`);
  urlencoded bodies are capped at Go's default 10 MB by `ParseForm`.
- `internal/client/client.go:29-31` — `Remote` uses `http.DefaultClient`, which follows redirects to any host and
  re-sends the form body and the `Lifelog-Source` header on 307/308.
- The HTML template (`internal/api/html.go:14-54`) has an inline `<style>` and **no** scripts, images or external
  resources; forms post to relative paths. A strict CSP fits it.
- Go version: `go.mod` says `go 1.27.1`. `net/http` has `http.NewCrossOriginProtection()` (Go ≥ 1.25): it rejects
  non-GET/HEAD/OPTIONS browser requests that are cross-origin by `Sec-Fetch-Site`, or by `Origin` host ≠ `Host`;
  requests with neither header (the CLI, MCP, curl) are allowed. Its `Handler(h)` method wraps a handler.
- README.md:80-81 (the decision this plan makes true):
  ```
  - **`serve` binds 127.0.0.1** and has no authentication: `life.db` holds health data, and the API is for this
    machine.
  ```
- Test conventions: `internal/api/api_test.go` (package `api_test`, `fresh(t)` returns a client and the handler,
  `httptest.NewRequest` + `httptest.NewRecorder` as in `TestBrowserGetsHTMLAndSourceUI`, lines 104-121).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| API tests | `go test ./internal/api` | ok |
| Build | `go build -o NUL ./cmd/lifelog` (Windows) / `go build -o /dev/null ./cmd/lifelog` | exit 0 |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**:
- `internal/api/guard.go` (create) — the wrapper
- `internal/api/guard_test.go` (create)
- `cmd/lifelog/main.go` — the `serve` case only
- `internal/client/client.go` — `Remote` only
- `README.md` — the `serve` decision bullet only

**Out of scope**:
- `internal/api/handler.go` routes and `api.New` — the in-process surfaces must not change behaviour.
- `docs/` — writer-neutral contract; no change.
- Changing a successful HTML POST to a 303 redirect (considered and rejected: the browser already asks before
  re-posting, and a redirect would drop the Result panel that shows what a save skipped).
- Authentication tokens — not needed for a loopback-only, same-origin-only server.

## Git workflow

Branch `advisor/027-serve-guard`; one commit, e.g. `serve: refuses cross-site writes, foreign Host names and
non-loopback addresses; hardening headers`, body + `Ran: …` + trailer.

## Steps

### Step 1: Tests first

Create `internal/api/guard_test.go` (package `api_test`) with a helper that builds `g := api.Guard(h, "127.0.0.1:7777")`
from `fresh(t)`'s handler, and these cases (each an `httptest.NewRequest` → `g.ServeHTTP(rec, req)`):

1. Cross-site write refused: `POST /pages` body `title=X`, `Content-Type: application/x-www-form-urlencoded`,
   `Host: 127.0.0.1:7777`, `Origin: http://example.test`, `Sec-Fetch-Site: cross-site` → status **403**; afterwards
   `GET /pages?title=X` through the plain handler is 404 (nothing written).
2. Same-origin browser write allowed: same request with `Origin: http://127.0.0.1:7777`,
   `Sec-Fetch-Site: same-origin`, `Accept: text/html` → 200.
3. Tool write allowed: same request with **no** `Origin`/`Sec-Fetch-Site` → 200 (this is the CLI with `--url`).
4. Foreign Host refused: `GET /` with `Host: rebind.example.test:7777` → **421** (Misdirected Request), body does not
   contain `Lifelog`.
5. Loopback names allowed: `GET /` with `Host` `127.0.0.1:7777`, `localhost:7777` and `[::1]:7777` → 200 each.
6. Headers: any 200 HTML response has `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
   `Cache-Control: no-store`, and a `Content-Security-Policy` containing `frame-ancestors 'none'` and
   `form-action 'self'`.
7. Body cap: a `POST /query` with `Content-Type: application/json` and a 17 MiB body → status 400 or 413 (not 200).

Set the host with `req.Host = "127.0.0.1:7777"` (a `Host` entry in `req.Header` is ignored by `net/http`);
`httptest.NewRequest` defaults it to `example.com`.

**Verify**: `go test ./internal/api -run Guard` → fails to compile (`api.Guard` undefined). Expected.

### Step 2: `api.Guard`

Create `internal/api/guard.go`:

```go
// Guard wraps the API for a socket (lifelog serve): only this machine's loopback names, no cross-site browser
// writes, a bounded body, and headers that keep a browser from framing, sniffing or caching the owner's data.
// The in-process surfaces (CLI, MCP) call the handler directly and never pass through it.
func Guard(h http.Handler, addr string) http.Handler
```

Behaviour, in this order:
1. Compute the allowed hosts from `addr`'s port (`net.SplitHostPort`): `127.0.0.1:<port>`, `localhost:<port>`,
   `[::1]:<port>`. If `r.Host` (lower-cased) is not one of them → `http.Error(w, "misdirected request", 421)`.
2. Set the four headers of test 6 on every response, with
   `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`.
3. `r.Body = http.MaxBytesReader(w, r.Body, 16<<20)`.
4. Delegate to `http.NewCrossOriginProtection().Handler(h)` (build it once, outside the returned func).

**Verify**: `go test ./internal/api -run Guard -v` → all 7 cases pass. If case 7 returns 200, check that
`form()`'s JSON decode error surfaces as 400 (it does: `&core.Error{Status: 400, ...}`).

### Step 3: `serve` uses it and refuses a non-loopback address

In `cmd/lifelog/main.go`, in `case "serve":` before building the server:

```go
host, _, err := net.SplitHostPort(o.addr)
if err != nil {
	return fmt.Errorf("--addr %q: %v", o.addr, err)
}
if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
	return fmt.Errorf("serve binds a loopback address only (127.0.0.1, ::1, localhost), not %q: life.db holds health data and the API has no authentication", host)
}
```

and use `Handler: api.Guard(handler, o.addr)`, plus `ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute`
(no `WriteTimeout`: a replay through a running server can take minutes). Add `"net"` to the imports.

**Verify**: build the binary to a temp path, then `<bin> serve --db x --addr 0.0.0.0:7777` → exits 1 with the
"loopback address only" message (the database check may run first: if it does, point `--db` at a database made by
`<bin> init --db <tmp>/t.db`). `<bin> serve --addr :7777 --db <tmp>/t.db` → refused too (empty host).

### Step 4: The remote client never follows a redirect to another host

In `internal/client/client.go`, `Remote`: replace `http.DefaultClient` with

```go
&http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("refusing a redirect to %s", req.URL.Host)
	}
	return nil
}}
```

**Verify**: `go vet ./internal/client` → exit 0.

### Step 5: README decision

Replace the README bullet with:

```
- **`serve` binds a loopback address only** and has no authentication: `life.db` holds health data, and the API is
  for this machine. It answers only its loopback host names (a DNS-rebinding page gets 421), refuses cross-site browser
  writes (`http.CrossOriginProtection`), and sends no-store, no-frame and no-sniff headers.
```

### Step 6: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

`internal/api/guard_test.go`, cases 1–7 above, modelled on `TestBrowserGetsHTMLAndSourceUI`
(`internal/api/api_test.go:104-121`). No change to existing tests.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0; `go test ./internal/api -run Guard -v` shows 7 passing cases
- [ ] `grep -n "api.Guard" cmd/lifelog/main.go` → one match
- [ ] `grep -n "DefaultClient" internal/client/client.go` → no match
- [ ] The binary refuses `--addr 0.0.0.0:7777` (Step 3 verification)
- [ ] `git status --short` lists only in-scope files; plan 027's status row updated

## STOP conditions

- `http.NewCrossOriginProtection` is missing from the Go toolchain in use (`go version` < 1.25) — report.
- A same-origin browser POST (case 2) is refused by the protection — report the headers; do not add bypass patterns.
- Any existing test in `internal/api` or `tests/` fails — the guard must not be inside `api.New`; report.

## Maintenance notes

- A new route needs nothing: the guard wraps the whole mux. A route that changes state must never be a GET (the
  protection allows GET by design).
- If `serve` ever needs to be reachable from another device, that is a new decision (authentication, TLS) — not a flag
  that skips the loopback check.
- `docs/contract/threat-model.md`'s "data exposed on a network" row names only Datasette; if the owner wants the
  contract to require this of every writer with an HTTP surface, that goes through an issue ([process](../process.md)).
