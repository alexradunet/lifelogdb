---
name: lifelog-extension
description: Build an extension of lifelog: a program lifelog-NAME that reads and writes the life log through the API. Use when the owner asks for an importer, a bridge to a service, or any new program around lifelog.
---

# Build an extension of lifelog

An extension is a client of the API of lifelog. It is a separate program. It does not change `lifelog`.

## What it is

- It talks HTTP to the server at `LIFELOG_URL`. `GET /actions` of that server is the catalog of what it can do.
- Every answer is Siren JSON: an entity with properties, links and actions. An action names its method, its address
  and its fields. Follow the links and the actions. Do not build addresses by hand.

## How it starts

`lifelog NAME ARGS...` runs the program `lifelog-NAME`. `lifelog` looks in the folder of `{{.Exe}}` first, then on
`PATH`. A name that is a command of `lifelog` is never an extension.

| variable | meaning |
|---|---|
| `LIFELOG_URL` | the API. `lifelog` starts a temporary server on loopback when the file exists. |
| `LIFELOG_EXE` | the path of `lifelog` |
| `LIFELOG_DB` | the absolute path of the file, `{{.DB}}` here |
| `LIFELOG_TOKEN` | when set, send it as a bearer token |

## Rules

1. Send the header `Lifelog-Source: import:NAME` (an importer) or `Lifelog-Source: agent:NAME` (a model). The
   owner-only actions are refused to such a writer.
2. Never open `{{.DB}}`. Write only through the API.
3. Keep the state of the extension (a token, a cursor) in its own folder, for example `os.UserConfigDir()/lifelog-NAME/`.
   Never keep it in `life.db`.
4. A page that the extension serves listens on loopback (`127.0.0.1`) only.
5. In Go, make it its own module. Use the standard library, unless a protocol needs an SDK. Import nothing from
   `lifelog/internal`. Build it as `lifelog-NAME` and put it beside `{{.Exe}}`.
6. Test it with synthetic data. Fake the third party. Never use the owner's `life.db`.
7. Ask the owner before you build it, and before you run it on real data.
