# Plan 040 (design + prototype): The model reads its import procedure from the server, copied from the guide

> **Executor instructions**: This is a design-and-prototype plan. Build the prototype exactly as scoped, record the
> answers to the open questions in the plan's "Findings" section (append it at the end of this file — a plan is a
> dated record), and stop. If anything in the "STOP conditions" section occurs, stop and report. When done, update the
> status row for this plan in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- docs/guides/importing.md internal/mcp tools`
> Plan 028 adds `internal/mcp/mcp_test.go` — expected. If the guide's headings `## The procedure for the model` or
> `## An Obsidian vault` changed, re-read them before Step 2.

## Status

- **Priority**: P2 (direction)
- **Effort**: M (coarse)
- **Risk**: LOW (additive: a prompt and a read-only tool in `--workspace` mode)
- **Depends on**: 028 (MCP tests exist to extend)
- **Category**: direction
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The owner imports with a **local** model (LM Studio) driving `lifelog mcp --workspace … --agent lmstudio` (README), for
privacy: no life data goes to a hosted provider. The model's instructions are already written — `docs/guides/importing.md`,
section "The procedure for the model" ("These are the model's instructions, written for a small local model: one file
per turn, fixed tables, no judgement left implicit"), plus "An Obsidian vault" for a vault. But the MCP server sends only a
four-line generic `Instructions` string (`internal/mcp/mcp.go:33-38`), so today the owner pastes the procedure into LM
Studio by hand, and the pasted copy drifts from the guide. The repo already solves the same problem for the schema:
`tools/copyschema` copies `docs/schema/schema.sql` into `internal/db` on `go generate`, and a test fails when the copy is
stale. Doing the same for the procedure keeps one home (the guide) and gives every import session the same rules.

## Current state

- `internal/mcp/mcp.go:28-75` — `New(c, version)` builds the server: one tool per catalog action (owner actions skipped),
  plus `get_day` and `get`; `Instructions` is a fixed string.
- `cmd/lifelog/main.go` — `lifelog mcp [--agent NAME] [--workspace DIR]`; with a workspace the import actions are in the
  catalog (`internal/api/import.go`), so `New` sees them.
- go-sdk v1.8.0 has `(*mcp.Server).AddPrompt(p *mcp.Prompt, h mcp.PromptHandler)` (`server.go:278`) and
  `AddResource` (`server.go:619`).
- `tools/copyschema/main.go` — the pattern: `//go:generate go run ../../tools/copyschema ../../docs/schema/schema.sql schema.sql`
  in `internal/db/db.go:5`; `internal/db/db_test.go:12-20` — `TestSchemaCopyIsCurrent`.
- `docs/guides/importing.md` — sections: `## The procedure for the model` (line ~244) through the line before
  `## An Obsidian vault` (~328), and `## An Obsidian vault` through the line before `## Trial, then the real run` (~356).
- Repo rule (AGENTS.md): `docs/` never names the app; a rule has one home. The guide stays the home; the binary carries a
  generated copy.

## Open questions (answer them in "Findings")

1. Does LM Studio's MCP client show **prompts** to the user or the model? (The owner can check: LM Studio → the
   `lifelog` server → prompts list.) If not, does it pass the server's `Instructions` to the model?
2. Is a read-only **tool** (`import_procedure`, returning the text) the most portable channel for a tools-only client?
3. How long is the procedure in tokens (≈ characters / 4)? Is it small enough for the owner's local model's context
   together with the tool list? If not, split per step.
4. Should the per-source part (Obsidian) be a separate prompt/tool (`import_procedure_obsidian`), matching the owner's
   "one generic skill plus a short per-source skill" design?

## Scope

**In scope (prototype)**: `tools/copysection/main.go` (create: copy a `## ` section range of a markdown file),
`internal/mcp/procedure.go` (create: `//go:embed` of the generated files and the registration), the generated
`internal/mcp/procedure.md` and `internal/mcp/procedure-obsidian.md`, `internal/mcp/mcp.go` (call the registration in
workspace mode only), `internal/mcp/mcp_test.go` (staleness + presence tests), this plan file (Findings).

**Out of scope**: editing the guide's text; any change to the tools' behaviour; `docs/`.

## Steps

### Step 1: The section copier

Create `tools/copysection/main.go`: `go run ./tools/copysection <src.md> "<start heading>" "<stop heading>" <dst>` copies
the lines from the line equal to `<start heading>` up to (not including) the line equal to `<stop heading>`, and writes
them with a first line `<!-- generated from docs/guides/importing.md by go generate; edit the guide, not this file -->`.
Exit non-zero if either heading is missing. Model it on `tools/copyschema/main.go`.

**Verify**: `go vet ./tools/...` → exit 0.

### Step 2: The generated copies and the staleness test

In `internal/mcp/procedure.go` add two `//go:generate` lines:
`//go:generate go run ../../tools/copysection ../../docs/guides/importing.md "## The procedure for the model" "## An Obsidian vault" procedure.md`
and the same for `"## An Obsidian vault"` → `"## Trial, then the real run"` → `procedure-obsidian.md`; embed both with
`//go:embed`. Run `go generate ./...`. In `mcp_test.go` add `TestProcedureCopiesAreCurrent`: re-extract both sections from
`../../docs/guides/importing.md` with the same function (export it from a small internal package, or duplicate the 15
lines in the test) and compare with the embedded strings.

**Verify**: `go generate ./... && go test ./internal/mcp` → ok; editing one word of the guide's procedure makes the test
fail until `go generate` runs again (try it, then revert).

### Step 3: Register prompt and tool in workspace mode

In `New`, when the catalog contains the action `import-status` (that is how `New` knows a workspace is mounted), add:
- a prompt `import_procedure` (description: "The model's procedure for an import, from the import guide") whose handler
  returns the generic text as one user message, and `import_procedure_obsidian` for the vault part;
- a read-only tool `import_procedure` with an optional `part` (`"generic"` default, or `"obsidian"`) returning the text.
Change nothing when no workspace is mounted.

Test: with a workspace (fixture from plan 028's `TestToolsOverInMemoryTransport`), `ListPrompts` lists both prompts and
`CallTool("import_procedure", {})` returns text starting with the generated-comment line; without a workspace neither
exists.

**Verify**: `go test ./internal/mcp -v` → PASS.

### Step 4: Findings

Append `## Findings (YYYY-MM-DD)` to this file: answers to the four open questions (question 1 needs the owner — write
"owner to check in LM Studio" if you cannot), the token estimate (`len/4` of each text), and a recommendation:
keep prompt + tool, or drop one.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `TestProcedureCopiesAreCurrent` exists and fails on a stale copy (shown in Step 2)
- [ ] in workspace mode the prompt and the tool exist; without a workspace they do not
- [ ] "Findings" appended; status row for 040 updated (IN REVIEW (owner) until question 1 is answered)

## STOP conditions

- The section headings are not unique in the guide — report; do not change the guide.
- The SDK's prompt API differs from `AddPrompt(*Prompt, PromptHandler)` in the pinned version — report what exists.

## Maintenance notes

- The guide stays the only place to edit the procedure; `go generate` and the staleness test keep the binary in step
  (the same contract as `schema.sql`).
- If the owner adds a per-source procedure for another source type (a health export), add a section and a `part` value.
