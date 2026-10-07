# 0035 — Validation fixtures can open the wrong file or credit an unrelated error

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** graph fuzz seeds and inspection of the defensive-connection test on `90c062e`.

## What happened

The schema test helper interpolated filesystem paths into a SQLite URI without encoding them. `#` truncated the filename as a URI fragment; separate fuzz seeds then opened the same database. A second test attempted to write obsolete `pages_fts_data`, so a missing-table error passed as proof of defensive mode.

## Reproduce

Create a schema fixture named `literal # percent%20.db` and check that exact file exists. For defensive mode, disable it on an isolated control connection and run the old test statement: the nonexistent table still causes an error.

## Rules involved

Real file-backed, isolated tests and meaningful failure-path assertions in the repository engineering agreement; [connection setup](../contract/connections.md).

## Resolution

Encode each filename component and test literal creation/read-only reopen. The defensive test uses the actual shadow table, checks its existence and contrasts a deliberately unhardened control. See [plan 075](../plans/075-schema-validation-depth.md).
