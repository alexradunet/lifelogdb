# Plan 028: The MCP tools take what a model sends — large ids, numbers, objects — and every action has its own name

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/mcp internal/api/siren.go internal/api/handler.go internal/api/import.go internal/client/client.go`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P1 — the import is driven over MCP by a small local model
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The MCP server (`lifelog mcp`) is how a local model (LM Studio) runs the import and captures. Four defects, each
verified by reading and the first by execution:

1. Every argument is turned into a form value with `fmt.Sprint`. JSON numbers decode as `float64`, and
   `fmt.Sprint(float64(1000000))` is `"1e+06"`; the API parses ids with `strconv.ParseInt`, so **every id of
   1,000,000 or more is "no such id"** (404). At the planned scale (a million health readings) `correct` and
   `retract` stop working for every newer reading. An object argument (e.g. `facts` sent as a JSON object) becomes
   Go's `map[...]` text.
2. Two actions are named `find` when the server runs with an import workspace (the page lookup and the import's name
   search). `AddTool` replaces a tool of the same name, so the model silently loses one; the CLI's `lifelog do find`
   always gets the first.
3. The `check_in` tool's schema says `{"type": "number", "enum": ["1", "0"]}` — strings in a number enum — which no
   value satisfies; a validating host or a strict model refuses every call.
4. A panic in a handler kills the MCP process mid-session: the in-process transport does not recover.

## Current state

- `internal/mcp/mcp.go:126-140`:
  ```go
  // arguments flattens the tool arguments to the form values the API takes.
  func arguments(req *mcp.CallToolRequest) (map[string]string, error) {
  	var raw map[string]any
  	if len(req.Params.Arguments) > 0 {
  		if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
  			return nil, err
  		}
  	}
  	out := map[string]string{}
  	for k, v := range raw {
  		if v != nil {
  			out[k] = fmt.Sprint(v)
  		}
  	}
  	return out, nil
  }
  ```
- `internal/mcp/mcp.go:86-114` — `schema(fields)`: a `number` field gets `"type": "number"`, and
  `if len(f.Options) > 0 { p["enum"] = f.Options }` copies the string options as they are.
- `internal/api/siren.go:108-109` — the check-in field: `{Name: "done", Type: "number", Title: "Done", Required: true, Options: []string{"1", "0"}}`.
- `internal/api/siren.go:114-115` — `{"find", "Find page", "Open the page with this title (any case or normalisation).", "GET", "/pages", []Field{req("title", "text", "Title")}, false},`
- `internal/api/import.go:24-25` — `{"find", "Find", "Look a name up before writing it: …", "GET", "/import/find", []Field{req("text", "text", "Name")}, false},`
  The import guide (`docs/guides/importing.md:189`) names this operation *find*, so the **import** action keeps
  the name `find`; the **page** lookup is renamed `find-page`.
- `internal/api/handler.go:254` — the root entity offers `action("find", nil, nil)` (the page lookup).
- `internal/api/siren.go:120-127` — `specOf(name)` returns the first spec with that name and panics if none.
- `cmd/lifelog/main.go:265-275` — `doAction` takes the first catalog action with the name. The CLI's own
  `lifelog page TITLE` calls `GET /pages?title=` directly (`main.go:233-234`) and needs no change.
- `internal/client/client.go:33-41`:
  ```go
  func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
  	rec := httptest.NewRecorder()
  	t.h.ServeHTTP(rec, r)
  	res := rec.Result()
  	res.Request = r
  	return res, nil
  }
  ```
- `internal/mcp` has **no tests**. The go-sdk v1.8.0 offers `mcp.NewInMemoryTransports()`,
  `mcp.NewClient(impl, nil)`, `(*Client).Connect(ctx, transport, nil)`, `(*Server).Connect(ctx, transport, nil)`,
  `(*ClientSession).ListTools(ctx, nil)` and `(*ClientSession).CallTool(ctx, &mcp.CallToolParams{Name, Arguments})`.
- Test helpers to copy: `internal/api/api_test.go:17-30` (`fresh`: `db.Init` + `db.Open` in `t.TempDir()`,
  `api.New(&core.Store{DB: d}, nil)`, `client.InProcess(h, "cli")`).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Package tests | `go test ./internal/mcp ./internal/api ./internal/client` | ok (client has no tests: `[no test files]`) |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/mcp/mcp.go`, `internal/mcp/mcp_test.go` (create), `internal/api/siren.go` (the `find` spec
name only), `internal/api/handler.go` (line 254 only), `internal/api/api_test.go` (one new test),
`internal/client/client.go` (`RoundTrip` only).

**Out of scope**: `internal/api/import.go` (keeps `find`); `docs/` (the guide already calls the import operation
*find*); the CLI's `page` shortcut; tool descriptions other than the renamed one.

## Git workflow

Branch `advisor/028-mcp-arguments`; one commit, e.g. `mcp: numbers and objects pass as written; find-page and the
import's find are two tools; check_in's schema is satisfiable; a handler panic is a 500`, body + `Ran: …` + trailer.

## Steps

### Step 1: A pure argument flattener, tested first

In `internal/mcp/mcp.go`, split `arguments` into the request part and a pure function:

```go
// flatten turns tool arguments into the form values the API takes: strings as they are, numbers in plain
// decimal (never exponent form, so an id of 1234567 stays 1234567), booleans as true/false, objects and arrays
// as their JSON text.
func flatten(raw json.RawMessage) (map[string]string, error)
```

Decode with a `json.Decoder` and `UseNumber()` so numbers arrive as `json.Number`, and emit `n.String()` for them —
that keeps the digits exactly as the model sent them. For `bool` use `strconv.FormatBool`; for `map[string]any` and
`[]any` use `json.Marshal`; skip `nil`. `arguments(req)` becomes `return flatten(req.Params.Arguments)`.

Create `internal/mcp/mcp_test.go` (package `mcp`) with `TestFlatten`: input
`{"id": 1234567, "big": 1000000, "value": 48.5, "done": 1, "ok": true, "facts": {"file": "a.md", "writes": []}, "none": null, "s": "x"}`
must give `id=1234567`, `big=1000000`, `value=48.5`, `done=1`, `ok=true`, `facts={"file":"a.md","writes":[]}`,
`s=x`, and no `none` key.

**Verify**: `go test ./internal/mcp -run TestFlatten -v` → PASS.

### Step 2: Number enums are numbers

In `schema(fields)`: when `f.Type == "number"` and `len(f.Options) > 0`, build the enum as `[]any` of `float64`
parsed from each option with `strconv.ParseFloat` (an option that does not parse: keep the field's type as
`"string"` and the options as strings). Leave string fields unchanged.

Add `TestNumberEnumsAreNumbers` (package `mcp`): `schema([]api.Field{{Name: "done", Type: "number", Required: true, Options: []string{"1", "0"}}})`
→ `props["done"]["enum"]` is `[]any{1.0, 0.0}` and `type` is `"number"`.

**Verify**: `go test ./internal/mcp -run TestNumberEnums -v` → PASS.

### Step 3: One name per action

In `internal/api/siren.go:114` rename the page lookup's spec name `"find"` → `"find-page"` (keep its title "Find
page"). In `internal/api/handler.go:254` change `action("find", nil, nil)` → `action("find-page", nil, nil)`.

Add to `internal/api/api_test.go` a test `TestActionNamesAreUnique`: start an API **with** a workspace is not needed
— fetch `GET /actions` from `fresh(t)`'s client, collect names, and fail on a duplicate. Because `importCatalog` is
only listed with a workspace, also add the check in `internal/mcp/mcp_test.go` (Step 4), which can build a workspace.

**Verify**: `grep -rn '"find"' internal/api/siren.go internal/api/handler.go` → no match;
`go test ./internal/api` → ok.

### Step 4: An end-to-end MCP test with a workspace

In `internal/mcp/mcp_test.go` add `TestToolsOverInMemoryTransport`:
- Build a database as `api_test.fresh` does, an import workspace with `importer.Open`/setup as
  `internal/importer/importer_test.go` does (read that file's helper and copy the minimal setup; if building a
  workspace needs more than ~15 lines, skip the workspace and test only the base catalog — say so in the commit).
- `h := api.New(store, ws)`, `c := client.InProcess(h, "agent:test")`, `srv, _ := New(c, "test")`.
- `st, ct := mcp.NewInMemoryTransports()`; `srv.Connect(ctx, st, nil)`; `cs, _ := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)`.
- `cs.ListTools(ctx, nil)`: assert tool names are unique; no tool is named `register_metric` or `replay`; both
  `find` and `find_page` exist (with a workspace).
- `cs.CallTool(ctx, &mcp.CallToolParams{Name: "capture", Arguments: map[string]any{"day": "2026-09-29", "text": "x", "mood": 4}})`
  → `IsError == false`.

**Verify**: `go test ./internal/mcp -v` → all pass.

### Step 5: A handler panic is a 500, not a crash

In `internal/client/client.go`, `RoundTrip`: wrap the call in a deferred `recover()`; on a panic, write a Siren error
entity with status 500 and message `fmt.Sprintf("internal error: %v", p)` to the recorder (JSON:
`{"class":["error"],"title":"Error","properties":{"status":500,"message":"…"}}`, `Content-Type:
application/vnd.siren+json`) and return that response.

Add a test in `internal/mcp/mcp_test.go` or a new `internal/client/client_test.go`: an `http.HandlerFunc` that
panics, wrapped by `client.InProcess(h, "cli")`; `c.Get("/")` returns a `*client.Error` with `Status == 500` and
the process keeps running.

**Verify**: `go test ./internal/client ./internal/mcp` → ok.

### Step 6: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

`TestFlatten`, `TestNumberEnumsAreNumbers`, `TestToolsOverInMemoryTransport` (unique names, no owner tools, a real
call), the panic test, `TestActionNamesAreUnique` in `internal/api`.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "fmt.Sprint(v)" internal/mcp/mcp.go` → no match
- [ ] `go test ./internal/mcp -v` lists the new tests as PASS
- [ ] `git status --short` lists only in-scope files; status row for 028 updated

## STOP conditions

- `req.Params.Arguments` is not a `json.RawMessage`/`[]byte` in the go-sdk version in `go.mod` — report the type.
- Renaming `find` → `find-page` breaks a test in `tests/` (the suites should not depend on the app's action names) —
  report the test.
- `ListTools` shows an owner action as a tool — that is a security bug outside this plan's fix; report it at once.

## Maintenance notes

- A new action name must be unique across `catalog` and `importCatalog`; the two tests catch a clash.
- If a field ever takes a JSON object (e.g. a future `write-facts` accepting structured facts), `flatten` already
  passes it as JSON text; the API side must parse it (`api.form` handles a JSON body, but MCP sends form values).
