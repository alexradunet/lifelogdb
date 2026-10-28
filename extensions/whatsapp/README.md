# lifelog-whatsapp

`lifelog-whatsapp` is an always-on agent for lifelog. The owner writes to it on WhatsApp. A model answers. The model
reads and writes `life.db` through the action catalog of the API, as the [MCP server](../mcp/README.md) offers it.

It is a client of the lifelog HTTP API ([API.md](../../API.md)). It is a separate Go module, `lifelog-whatsapp`. It
imports nothing from lifelog, and `lifelog` does not change when it does. It writes as `agent:whatsapp`. The API
refuses the owner-only actions to such a writer.

```mermaid
flowchart LR
  P["the owner's phone, WhatsApp"] <-->|"WhatsApp Web protocol, whatsmeow"| W["lifelog-whatsapp: the chat"]
  W --> A["the agent: one turn at a time"]
  A -->|"GET /settings at each turn, owner token, no source"| L
  A <-->|"chat completions with tools, net/http"| M["the model named in Settings"]
  A <-->|"HTTP, Lifelog-Source: agent:whatsapp, Bearer token"| L["lifelog serve, 127.0.0.1:7777"]
```

## How it works

- **One conversation a day, and it is a page.** The core owns the convention (the page `Conversation YYYY-MM-DD`,
  its category, its first line, the paragraph of a turn: see the conversation class in [API.md](../../API.md)). A
  message from the phone goes in with the action `say` (`day` = `today`). The extension reads the page with
  `GET /days/{day}/conversation` and holds no title and no category. The tool calls and the reply are added with
  `save-body`, as lines and `**lifelog** (HH.MM): text`. The model reads the whole page on each turn. The extension
  keeps nothing else of the conversation.
- **The model is set in lifelog.** The extension reads `GET /settings` at the start of each turn, with the owner's
  token and no `Lifelog-Source`, through a second client that only this read uses. Every other request, the tools of
  the model too, carries `Lifelog-Source: agent:whatsapp`, so the model's own `get /settings` is refused (403): the key
  of the model never reaches the model, the page or the phone. A change in Settings is seen
  at the next turn. With no model set, the reply of the turn is one line, "No model is set: open Settings in
  lifelog.", on the page and on the phone. The agent keeps running.
- **Continued anywhere.** Every 3 seconds the extension reads today's conversation. Text added to its end on another surface
  (the browser, `lifelog do save-body`, an MCP tool) is an owner's turn: the reply is added to the page and sent on
  WhatsApp. The first turn of a day on any surface is answered too: a page that is new to the extension, on a new day
  or after a restart, is answered when its last turn is the owner's, and a turn said before midnight is answered on
  its own page. Any other change of the page is adopted and answered with nothing. A body is read with its CRLF line
  endings made LF (a browser submits a textarea that way), and saved with LF. When the owner saves the page while
  the model runs, the reply is still appended after that text, and one line on WhatsApp says the text was not
  answered.
- **Earlier days** are pages. The model reads them with `get` on `/pages?title=Conversation%20YYYY-MM-DD`.
- **One number may talk.** A message from any other number, and any group message, is logged and ignored. A photo,
  a voice note or a document from the owner gets one line: the agent reads text for now. WhatsApp can deliver a
  message from a hidden id (a lid) without the number; then the sender does not match `--owner` and the message
  is logged as ignored. The log line shows the id.
- **No file.** An action with a file field (`add-file`, `set-cover`) is not a tool of the model. Its value would be
  the path of any file the process can read, and the model acts on text that any surface wrote to the page.
- **A reply only.** The agent sends nothing on its own.
- A turn has at most 16 model steps and 5 minutes. Up to 32 messages wait for the turn in progress.

## The second number

The transport is [whatsmeow](https://github.com/tulir/whatsmeow), the Go client of the unofficial WhatsApp Web
protocol. WhatsApp may ban a number that uses it, and the protocol may change. Use a number that is the agent's own,
never the owner's main number. The owner writes to the agent's number from the main number.

## Build

```
go build -o lifelog-whatsapp ./cmd/lifelog-whatsapp
```

From the repository root: `go build -C extensions/whatsapp -o ../../lifelog-whatsapp ./cmd/lifelog-whatsapp`. Put
`lifelog-whatsapp` beside `lifelog`. The build needs no cgo.

## Run

| line | what happens |
|---|---|
| `lifelog whatsapp login` | Pairs the agent's number. It draws a QR code. On the agent's phone, open WhatsApp, Linked devices, Link a device, and scan it. The session is saved. It exits. It needs nothing else. |
| `lifelog whatsapp --owner NUMBER [--db FILE]` | Serves: answers the owner on WhatsApp. `lifelog` serves the file on a temporary loopback server for it ([extensions/README.md](../README.md)). |
| `lifelog-whatsapp --owner NUMBER --url URL [--token-file F]` | The same against a running `lifelog serve` at `URL` (default `$LIFELOG_URL`). With neither, it refuses: a guessed address could be another file's server. |
| add `--terminal` | Answers on the terminal: a line of input is a message of the owner, a reply is written to the output. It needs no WhatsApp session. Use it to try the agent without a phone. An interrupt ends it at once; the read of the terminal is the one part of the program that is not awaited (it ends with the process). |

| flag | meaning |
|---|---|
| `--owner` | The number of the owner: digits with the country code, such as `40712345678`. Required to serve. |
| `--url` | The address of lifelog. Default `$LIFELOG_URL`. |
| `--token-file` | A file with the bearer token. `$LIFELOG_TOKEN` wins when it is set. |
| `--terminal` | Answer on the terminal. |

`--source` is not a flag: the extension always writes as `agent:whatsapp`.

The model is not set here. Set it on the Settings page of lifelog (`/settings`), or with
`lifelog do settings model_url=http://HOST/v1 model=ID model_key=KEY`. `model_url` is the base URL of a server that
speaks the OpenAI chat-completions protocol, ending in `/v1`. `model` is the model id. `model_key` is a bearer
credential for the model server, and it is optional. The settings are in a file beside `life.db` (mode 0600). The key
is never a flag, never an environment variable and never in a log line.

`LIFELOG_URL` and `LIFELOG_TOKEN` are set by `lifelog whatsapp`, or by the owner.

The state folder is `os.UserConfigDir()/lifelog-whatsapp/` (mode 0700). It holds `session.db` (mode 0600), the
store of the keys of the account, and `state.json`, the id, the version and the body of the conversation page after
the extension's own last save. To pair again, delete `session.db`.

## Exit codes

| code | meaning |
|---|---|
| 0 | An interrupt or SIGTERM, or (with `--terminal`) the end of the input. |
| 1 | An error, or a failure of the connection that a new process may try again. The unit restarts it. |
| 2 | Not paired: run `lifelog whatsapp login`. |
| 3 | The connection ended for good: logged out (the owner unlinked the device on the phone), the session taken over by another client, the number banned for a time, or the client refused as outdated. The unit does not restart it: act first (the line on stderr says what), then start it. |

## The unit

[`deploy/lifelog-whatsapp.service`](../../deploy/lifelog-whatsapp.service) is a reference copy of the unit that runs
the agent beside `lifelog.service`. It has no `EnvironmentFile`: the model comes from the Settings of lifelog. Run
`lifelog whatsapp login` once, as the same user, before the unit starts.

## Privacy

- The text of the chat, and the text of the pages that the model reads, go to the model server that the owner
  sets in Settings. A hosted model sees them.
- The conversation pages are in `life.db`, so every snapshot of it holds them.
- The session store holds the keys of the account. Keep the state folder private and out of git.
- A model writes with no approval step. Its rows carry `agent:whatsapp`. Nothing is ever deleted.

## Tests

`go vet ./... && go test ./...` in this folder. The tests of `agent/` run on a fake catalog, a fake model and a fake
chat. `e2e_test.go` builds the real `lifelog` and `lifelog-whatsapp` into one folder and runs
`lifelog whatsapp --terminal` against a fake model. It sets the model with `lifelog do settings`, so it needs a `lifelog` that has the action. It needs the `go` command and the sources of the repository.

**What has no test:** the connection to WhatsApp (`login`, the connection, `Send`, `Typing`, `MarkRead`, the
`LoggedOut` event). It needs a real number. The filter, the QR renderer and the state folder have tests.
