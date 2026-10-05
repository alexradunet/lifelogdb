# 070 — Plan an owner-run capture pilot without freezing the canonical file

> Executor: this is an owner-gated direction brief, not implementation or real-data access authorization. A hosted agent may prepare synthetic scenarios and this plan only. The owner chooses/runs the local pilot. Do not point any tool at the canonical database.
>
> Drift check: `git diff --stat 0460379..HEAD -- README.md docs/process.md docs/decisions/D13-migrations-and-freeze.md internal/api/handler.go internal/mcp/mcp.go cmd/lifelog/main.go`. Read changed behavior before updating the brief; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P3. **Effort:** S for the brief; owner pilot duration is an explicit choice.
- **Risk:** HIGH if a wrong database is selected; LOW for a synthetic disposable pilot.
- **Status:** BLOCKED (owner chooses pilot, disposable path and local MCP client).
- **Depends on:** acceptance of 050, 055, 057–061, 063–065 for their affected capture paths; import use additionally needs the import-safety sequence in the index.
- **Category:** direction. **Direction option:** 2. **Evidence:** grounded in existing capture surfaces; no usability claims measured.

## Why

The product already has browser, CLI and MCP capture paths. A short trial can expose everyday friction more usefully than another hypothetical schema review. But [D13](../decisions/D13-migrations-and-freeze.md) defines the first unreplayable canonical write as the freeze: a seemingly harmless real capture is not just a demo.

## Current state and conventions

The application [README](../../README.md) documents:

```text
lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog serve
lifelog mcp --agent lmstudio
```

Those abbreviated examples may use an environment-selected database. **Do not use them unqualified for this pilot.** Every pilot command must explicitly name the owner-approved absolute disposable path. `internal/api` is the shared handler, and `internal/mcp` discovers its tools from the action catalog. The [process checklist](../process.md#before-the-freeze) remains the authority for any later canonical transition.

## Scope and privacy

Modify only this plan and `docs/plans/README.md` to record sanitized decisions/results. Other paths in the drift command are read-only references. No source change, new feature, build, real database access, import workspace inspection, canonical rebuild/freeze, migration runner, whole-library import or task/reminder feature. Do not record personal prose, readings, pictures, filenames or credentials in plans.

The owner may later choose a personal local trial, but that is separate authorization; no hosted model sees it. Start with synthetic content in a fresh disposable file, never a copy of private data.

## Preparation and verification commands

- Source gate: `go test -mod=readonly -count=1 ./internal/api ./internal/mcp ./internal/core ./cmd/lifelog` → all pass on the chosen reviewed commit.
- Plan gate: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/document$' && git diff --check` → exit 0.
- Scope gate: `git diff --name-only` → only this plan/index for preparation.
- Owner commands below use `ABS_DISPOSABLE_DB` as a placeholder, never a default or an actual committed private path. Use the reviewed installed binary; do not build or launch it as part of advisory planning.

## Steps and owner-gated test plan

1. Ask the owner to choose a short fixed pilot window, browser versus local MCP route (both if useful), and an absolute disposable path outside Git that is physically distinct from every canonical/workspace file. Record only that the path was checked, not the private path itself. Confirm the local client cannot fall back to `LIFELOG_DB` or an existing MCP server's database.
   **Verify:** plan gate and scope gate → pass; status stays BLOCKED until the owner explicitly confirms these choices.
2. The owner initializes a fresh synthetic file: `lifelog init --db ABS_DISPOSABLE_DB`, then `lifelog get integrity --db ABS_DISPOSABLE_DB`. Expected: exit 0 and all four checks clean. For the browser, run `lifelog serve --db ABS_DISPOSABLE_DB`; for MCP, configure `lifelog mcp --db ABS_DISPOSABLE_DB --agent pilot` in the chosen local client, replacing the placeholder explicitly. Never reuse an already-running canonical server.
   **Verify:** owner confirms the selected file is disposable and initial day view is empty; if not, stop before any capture.
3. Exercise synthetic capture on a fixed date: text with a person/place link and tag, a mood, a habit check-in after owner registration, an edited body with a stale-version refusal, a tombstone/revival, and one optional synthetic selected-photo keep. Observe returned links, visible browser feedback, numeric MCP calls and cancellation. Repeat only operations whose existing contract says they are idempotent; capture append itself is not a replay key.
   **Verify:** `lifelog get days/2031-04-11 --db ABS_DISPOSABLE_DB` and `lifelog get integrity --db ABS_DISPOSABLE_DB` → expected synthetic contents/actions and clean checks. Owner records counts/friction categories only.
4. Record a small sanitized outcome table in this plan: route, attempted scenario, pass/fail, and issue/plan reference for any real defect. Recommend proceed, fix-and-repeat, or defer. Remove no files automatically. A later first canonical write requires the owner to complete the freeze checklist and explicitly authorize it; successful trial does not do so.
   **Verify:** plan gate and scope gate → pass; canonical access/freeze remains explicitly unperformed.

## Done criteria

- [ ] Owner choices and physical separation check are recorded without private paths/data.
- [ ] Synthetic pilot outcomes and four clean checks are reported by the owner, or the brief remains BLOCKED.
- [ ] Concrete friction produces bounded follow-up issues/plans, not speculative schema features.
- [ ] No canonical file, real export or hosted real-data tool access occurred; index records the actual gated state.

## STOP conditions

Stop if any path/server/client might be canonical, a capture cannot be routed explicitly, or the owner asks a hosted agent to inspect real data contrary to the privacy boundary. Do not infer freeze approval from pilot approval. Stop on prerequisite failures or unexplained drift.

## Git workflow and maintenance

No branch/commit/push or code execution is authorized by writing this brief. If the owner later requests a plan-only commit, use `plans: record capture pilot decisions (plan 070)` and list validation. Keep canonical freeze decisions in their existing process/decision homes; this is a dated experiment record only.
