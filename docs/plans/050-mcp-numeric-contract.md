# 050 — Preserve MCP numeric schema types and integer identifiers

> Executor: exercise real SDK tool listing/calls, not only helper functions. Use synthetic stores; set plan/index IN REVIEW (owner) after all gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/mcp/mcp.go internal/mcp/mcp_test.go`. Compare live symbols before editing; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** S. **Risk:** MED (all generated MCP tools share this adapter).
- **Status:** IN REVIEW (owner). **Depends on:** none; retain plan 036's name/owner-tool guards.
- **Category:** bug. **Deep-audit finding:** 4. **Confidence:** HIGH, source-confirmed; new SDK regressions not executed.

## Why

Numeric catalog fields receive string enums, so `check_in.done` advertises a number constrained to string values: no argument can satisfy both. Numeric identifiers decoded through `float64` also become scientific notation at ordinary million-scale values and can lose precision at large values. The API expects exact decimal identifiers.

## Current state and conventions

`internal/mcp/mcp.go:113–127`:

```go
case "number":
    p["type"] = "number"
// ...
p["enum"] = f.Options
```

`arguments` at line 148 unmarshals into `map[string]any`, then uses `fmt.Sprint(v)`. `call` forwards those strings to the shared hypermedia client. Catalog metadata in `internal/api/siren.go` is the authority; do not duplicate the action list. Model tests after `TestFindToolsWithWorkspace`: `mcpsdk.NewInMemoryTransports`, `ListTools`, `CallTool`, and `mcpTestClient` already exercise the real transport.

## Scope

Only `internal/mcp/mcp.go`, `internal/mcp/mcp_test.go`, plan/index. No Go SDK upgrade, API/schema redesign, authentication change, or manual per-tool registry. Context propagation is plan 057, to run after this adapter change.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/mcp -run 'TestMCPNumeric|TestFindToolsWithWorkspace|TestValidateToolNames'`.
- Package: `go test -mod=readonly -count=1 ./internal/mcp ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestMCPNumericSchema` and `TestMCPNumericArguments`. Through SDK listing/calls, check numeric enum values, successful habit check-ins for both 0 and 1, and rejection of other values. Capture forwarded paths for IDs 999999, 1000000 and 9007199254740993; use a recording handler rather than creating millions of pages. Include decimal readings and string enums as controls.
   **Verify:** focus command → baseline enum/identifier assertions fail; existing tool-name tests pass.
2. Convert enum options to JSON values matching each field's advertised type. Invalid numeric catalog options must produce a construction error, not an impossible schema or dropped constraint. Keep required fields, file-field omission and owner-only exclusions unchanged.
   **Verify:** focus command → SDK accepts legal check-ins and rejects invalid arguments; all enum types match their property types.
3. Decode JSON numbers without an intermediate float. Preserve exact tokens for ordinary numeric values; for integer identifier fields produce exact base-10 int64 text, including mathematically integral exponent forms if accepted by the schema. Reject fractional/out-of-range IDs before dispatch. Do not use float conversion to expand exponents or quietly round IDs. Preserve string handling/null omission; unsupported composite arguments must not turn into Go formatting strings.
   **Verify:** package and final commands → exit 0; tests prove large IDs and integral exponent forms dispatch exactly, while invalid IDs do not dispatch.

## Done criteria

- [x] `check_in` has satisfiable numeric enums and works through an SDK session.
- [x] Integer IDs retain exact digits; fractional/overflow IDs refuse without API writes.
- [x] Decimal measurements, string options, required fields, unique names and owner exclusions remain correct.
- [x] All gates pass; only scoped files changed; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): real in-memory MCP SDK sessions cover typed numeric enums, precise large IDs and invalid values. Focus/package gates and integrated generation/vet/uncached full tests passed; cancellation integration retained these tests.

## STOP conditions

Stop if field semantics cannot distinguish integer identifiers from real-valued readings without extending catalog metadata; ask to expand scope rather than guessing for every number. Stop on unexplained drift or two failed gates. Do not expose owner-only actions to simplify fixtures; set up habits through the existing owner/core test path.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `mcp: preserve numeric tool contracts (plan 050)`, listing checks. Catalog changes need schema-value compatibility tests, not just a tool-count assertion.
