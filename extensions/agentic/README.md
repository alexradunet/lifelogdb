# lifelog-agentic-init

`lifelog-agentic-init` makes the folder of the `lifelog` program ready for a coding agent that is opened in it
([plan 090](../../docs/plans/090-agentic-init.md)). It is the extension behind `lifelog agentic-init`.

It is a separate Go module, `lifelog-agentic`. It uses the standard library only. It imports nothing from lifelog. It
opens no database and calls no API: it reads two variables that `lifelog` sets for an extension
([extensions/README.md](../README.md)), `LIFELOG_EXE` and `LIFELOG_DB`, and writes files beside `LIFELOG_EXE`.

## What it writes

The texts are templates embedded in the program (`agentic/templates`). They name the paths of the program and of the
database.

| file | for |
|---|---|
| `AGENTS.md`, `CLAUDE.md` | what is in the folder, the rules for the agent; `CLAUDE.md` imports `AGENTS.md` |
| `.agents/skills/*/SKILL.md`, `.claude/skills/*/SKILL.md` | the skills: import notes, keep photos, build an extension |
| `.mcp.json`, `.pi/mcp.json`, `opencode.json`, `.codex/config.toml` | the MCP config of Claude Code, Pi, OpenCode and Codex. Each starts `lifelog mcp --agent NAME --db FILE`. |

A file that does not exist is written. A file with the same text is `unchanged`. A file with another text is `kept`,
unless `--force`, which `replaces` it. Every write goes to a temporary file first, then a rename.

## Build

```
go build -o lifelog-agentic-init ./cmd/lifelog-agentic-init
```

From the repository root: `go build -C extensions/agentic -o ../../lifelog-agentic-init ./cmd/lifelog-agentic-init`.
Put `lifelog-agentic-init` beside `lifelog`.

## Run

```
lifelog agentic-init [--force]
```

`lifelog` finds `lifelog-agentic-init` beside itself and runs it. Run alone, without `LIFELOG_EXE` and `LIFELOG_DB`,
it refuses and says so.

## Tests

`go vet ./... && go test ./...` in this folder. The tests of `agentic/` run on a temporary folder. `e2e_test.go` builds
the real `lifelog`, `lifelog-mcp` and `lifelog-agentic-init`, copies them into a folder of its own for each test, and
holds: the files and the database beside the program, the MCP configs (each line is run, and the server lists its
tools), the texts against the built `lifelog help` and `lifelog actions`, a failure and a kept file. It needs the `go`
command and the sources of the repository.
