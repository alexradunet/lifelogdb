# 0006 — One edited word in a stamped file stops the whole import, and status does not say what changed

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the import, after a one-line clarification of `rules.md`

## What happened

`rules.md` and `metrics.md` carry the owner's stamp: `status: approved <date> (owner) sha256:<hash of
the body>`. Any edit — a single sentence — makes the hash mismatch, and then **every** apply, check and
replay refuses until the file is stamped again:

```
Stop: rules.md is stale; the owner approves it (lifelog import approve rules)
```

The stamp is right (only the owner may approve), but nothing tells the owner *what* changed, so each
re-approval means re-reading the whole file in a terminal. In practice a clarification written after the
facts were already applied left a finished import unable to replay at all.

## Reproduce

1. Approve `rules.md` (`lifelog import approve rules`).
2. Edit one word in the body.
3. `lifelog import status` → `gates: {"rules.md": "stale"}`; `import replay` → 422.
4. `lifelog import approve rules` prints the whole file again, with no diff.

## Rules involved

- [Importing](../guides/importing.md) — the two gates and who may stamp them.
- `Workspace.Gate` and `Workspace.Approve` in `internal/importer/workspace.go`.

## Resolution

_(open)_ `Approve` could store the previous body and have `approve` print only the lines that changed,
and `status` could name the changed lines when a gate is stale.
