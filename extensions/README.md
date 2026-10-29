# Extensions

An extension is a program that extends `lifelog` from the outside. It is a client of [API.md](../API.md), written in
any language. It may also serve a catalog of its own ([a catalog of its own](#a-catalog-of-its-own)). The core names no
third party: an extension does.

[`googlephotos`](googlephotos/README.md) is the worked example. Read its code and its end-to-end test first.

## What an extension is

- It lives in its own folder under `extensions/`, with its own module and its own `README.md` that gives its build line.
- It never imports `lifelog/internal`: Go refuses it. In Go it requires `lifelog v0.0.0` with `replace lifelog => ../..`
  and imports `lifelog/siren`, `lifelog/client` and, for the tools of a model, `lifelog/client/tools`.
  `go mod verify` reports `lifelog v0.0.0: missing ziphash`: a directory replace has no zip, and this is not a fault; `go mod tidy -diff` is the check that the module files are in order.
- It never opens `life.db`. There is one writer per file. The extension reads and writes through the HTTP API.
- It keeps its own state (a token, a cursor) in its own folder under the user's configuration folder, for example
  `os.UserConfigDir()/lifelog-NAME/`. It never keeps state in `life.db`.
- In Go, it uses the standard library unless a protocol needs an SDK.
- A page or a catalog that it serves listens on a loopback address only.

## The command line

`lifelog NAME [ARGS...]` runs the program `lifelog-NAME` (`lifelog-NAME.exe` on Windows). A name that is a command of
`lifelog` is that command and never an extension.

```mermaid
flowchart LR
  U["lifelog NAME ARGS..."] --> B{"NAME is a command of the table?"}
  B -->|"yes"| R["run it, as today"]
  B -->|"no"| F["find lifelog-NAME: the folder of the program first, then PATH"]
  F -->|"none"| N["refuse: unknown command NAME, no lifelog-NAME beside this program or on PATH"]
  F -->|"found"| D{"--url given?"}
  D -->|"yes"| X["run lifelog-NAME with LIFELOG_URL = --url"]
  D -->|"no, and the file exists"| T["open the file, serve it on 127.0.0.1:0 for the life of the extension, LIFELOG_URL = that address"]
  D -->|"no, and no file"| Z["run it with no LIFELOG_URL: a setup extension needs none"]
  T --> X
  Z --> X
  X --> W["wait for it; then stop the server, close the file; exit with its status"]
```

- **The search.** The folder of the program comes first, with symbolic links resolved. Then `PATH`. So the extension that
  the owner installed beside `lifelog` wins over a program on `PATH`. A program on `PATH` runs with the owner's rights,
  as a `git-NAME` does.
- **The line.** Before `NAME`, `lifelog` reads its global flags as for any command. After `NAME`, `lifelog` reads only
  `--db` and `--url` (as `--db X` or `--db=X`). It passes every other word to the extension as it is, in its order. So
  an extension may have a flag of any name.
- **Refused flags.** `--source` and `--human` before `NAME` are refused. An extension names its own writer and prints
  its own output.

## What the extension receives

| item | value |
|---|---|
| `LIFELOG_URL` | With `--url`: that URL. Else, when the file exists: the address of a temporary server on `127.0.0.1`, which lives as long as the extension. Else: not set. |
| `LIFELOG_EXE` | The path of the `lifelog` program, symbolic links resolved. |
| `LIFELOG_DB` | The absolute path of the file that the line names (`--db`, `$LIFELOG_DB`, or the `life.db` beside the program). It is set whether the file exists or not. |
| `LIFELOG_TOKEN` | As inherited. Send it as a bearer token when it is set. |
| `LIFELOG_LISTEN` | Set by `serve --with` only: the loopback address (`127.0.0.1:PORT`) where a connected app serves its catalog. The address is new at each start. A program that serves nothing ignores it. |
| stdin, stdout, stderr | Inherited. An MCP server on stdio works. |

The temporary server is the same writer as the CLI. [Connections](../docs/contract/connections.md) allows it beside
the service. A setup extension needs no server, and a client extension that has no `LIFELOG_URL` and no `--url` must refuse with a
line that says what to run. It never guesses an address: `127.0.0.1:7777` may be the server of another file.

## Exit and signals

- The exit status of `lifelog` is the status of the extension. `lifelog` writes no second message.
- An interrupt or SIGTERM reaches `lifelog` and the extension. `lifelog` sends the extension an interrupt (on Windows:
  a kill), waits for it for ten seconds at most, then stops the server and closes the file.

## Run by serve

`lifelog serve --with NAME` (repeatable) runs the extension as a child of the server and keeps it running.

```mermaid
flowchart TD
  S["serve: the listener is up"] --> A["start lifelog-NAME with no arguments"]
  A --> W["wait for the end"]
  W -->|"exit 0"| E0["ended: not started again"]
  W -->|"exit 2 or 3"| E2["the owner acts first: not started again"]
  W -->|"any other end: 1, a signal"| R["wait 10 seconds, then start again"]
  R --> A
  T["interrupt or SIGTERM"] -->|"interrupt the child, 10 seconds at most, then a kill"| W
  W -->|"the child ended"| D["serve drains its requests and closes the file"]
```

- **The environment** is the one above. `LIFELOG_URL` is the address of the listener on the loopback
  (`http://127.0.0.1:PORT`, also for a listener on every interface). `LIFELOG_TOKEN` is the token of a `--public`
  listener. `LIFELOG_EXE` and `LIFELOG_DB` are as for any extension. `LIFELOG_LISTEN` is a loopback address
  (`127.0.0.1:PORT`), new at each start, on which the extension may serve a catalog
  ([a catalog of its own](#a-catalog-of-its-own)).
- **No arguments, and an empty stdin** (`/dev/null`). The extension configures itself from the settings of the API.
- **Its state folder must be writable under the unit.** A unit with `ProtectHome=read-only` lists
  `os.UserConfigDir()/lifelog-NAME` in `ReadWritePaths`, else the extension fails at its start and `serve` starts it
  again every 10 seconds.
  stdout and stderr are those of the server, so the lines go to its log.
- **The exit status** decides. 0: the extension ended by its own choice, and is not started again. 2 or 3: the owner
  must act first (for example pair a device); it is not started again until `serve` starts again. Any other end (1, a
  signal, a crash): `serve` starts it again after 10 seconds. `serve` logs each start and each end.
- **The stop.** On an interrupt or SIGTERM, `serve` interrupts the child first and waits for it, 10 seconds at most,
  then kills it. Then it drains its own requests and closes the file, so the child may still write while it ends.
- `--with` and `--tls-cert` together are refused: the child would have to trust the certificate.
- **A catalog of its own.** A child may serve a Siren catalog on `LIFELOG_LISTEN`: see [a catalog of its own](#a-catalog-of-its-own).

### A catalog of its own

A child may serve a Siren catalog on the address in `LIFELOG_LISTEN`. `serve` reads `GET /actions` once for each start
and adds the actions to its own catalog as `NAME-action`, at `/with/NAME`.

## A catalog of its own

An extension that `serve --with NAME` runs may serve a catalog of its own. It is then a connected app. The server
merges the actions of the app into its own `GET /actions`, and it forwards the requests for them to the app. The
server names no app and no domain: the catalog of the app is the whole interface. A connector in any language
implements this section and nothing else of `lifelog`.

```mermaid
flowchart LR
  B["the browser, lifelog do, the MCP tools"] -->|"GET /actions; GET or POST /with/NAME/..."| L["lifelog serve: the one catalog"]
  L -->|"the same request, as the writer of the request, on LIFELOG_LISTEN"| A["lifelog-NAME: its own catalog"]
  A -->|"capture with import_key, as import:NAME"| L
  A -->|"the API of the third party"| T["the app outside this repository"]
```

### The handshake

```mermaid
flowchart TD
  S["serve picks a free loopback port and starts lifelog-NAME with LIFELOG_LISTEN=127.0.0.1:PORT"] --> P["serve asks GET /actions every 250 ms, for 30 seconds at most"]
  P -->|"200, class actions"| R["registered: GET / gives the title; the actions are in GET /actions as NAME-action; the routes are at /with/NAME"]
  P -->|"another answer, the exit of the app, or no answer in 30 seconds"| N["serves no catalog: one line in the log; the app stays a plain child"]
  R -->|"the app ends"| U["unregistered: the actions leave GET /actions; /with/NAME is 503 unavailable"]
  N -->|"the app ends"| U
  U -->|"the exit rule: 0, 2 and 3 end it; any other end starts it again"| S
```

- **The address is new at each start.** A restart never meets the address of the app that ended. The server chooses the
  address and probes that one. It does not read the output of the app. An app that serves nothing ignores
  `LIFELOG_LISTEN`.
- **The catalog is read again at each start.** An app that ended has no actions in the catalog.
- **The address is a loopback address.** The app listens on no other. The server calls it and nothing else does.
- **A catalog is refused as a whole** (one line in the log, and the app stays unregistered) when:
  a name of an action is not `[a-z0-9-]+`; an `href` is not a path (it must start with one `/` and have no scheme,
  host, query or fragment); `NAME-` and the name of an action is the name of an action of the server or of another app;
  or an action has a field of the type `file`. A multipart body is not forwarded.

### What the app serves

All answers are Siren JSON ([API.md](../API.md#the-entity)). The app answers `GET` and the method of each action.

| route | answer |
|---|---|
| `GET /actions` | An entity of class `actions`. Its `actions` are the actions of the app. Each has `name`, `title`, `description`, `method`, `href` and `fields`. |
| `GET /` | The home of the app. Its `title` names the app in the rail of the browser. Without a title, the name is the title. |
| one route for each action | The `method` and the `href` of the action. The answer is an entity. |
| any other route | A read of a resource that the entities of the app link to. The server forwards a `GET` that matches no action as it is. |

An `href` is templated: `/items/{id}` has a path field `id`. A field has `name`, `type`, `title`, `required`,
`options`, `in` and `value`, as in [API.md](../API.md#the-entity). `in: "path"` marks a field of the `href`. Two marks
of an action have an effect on the server:

- `owner: true`: the server refuses the action to an `agent:*` or `import:*` writer (403 `forbidden`) and never calls the
  app. The server holds this rule. The app does not check the writer.
- `danger: true`: a client warns before it sends the action.

An error is an entity of class `error` with the properties `status`, `code` and `message`, and the HTTP status of
`status`. The server passes it on with the same status and code. Any other answer to a call of the server is a fault
of the app (502 `upstream`).

### What the server does with a request

The route is `/with/NAME` and everything under it, for every method. The path after `/with/NAME` is the `href` of the
action, with the path fields filled.

| step | rule |
|---|---|
| the writer | The server calls the app with the header `Lifelog-Source` of the request. The app sees no cookie and no token of the server. |
| the fields | The server sends the fields that the action names. A field that the action does not name, a missing required field or a value outside `options` is refused by the server (422 `invalid`), as for every action of the catalog. The fields `back`, `feedback` and `new` of a browser never reach the app. |
| the owner | An `owner: true` action is refused to an `agent:*` or `import:*` writer (403). The app is not called. |
| the answer | The `href` of each link, embedded entity and action gets the prefix `/with/NAME`. Each action name gets the prefix `NAME-`. A link or action whose `href` has a scheme or a host is dropped: nothing the app says leads off the server. |
| a browser | A `POST` from a browser is answered with a redirect to the `self` link of the answer, with the toast of the server. A `GET` is drawn by the generic view of the browser. |
| no retry | The server sends a request once. A write that was sent may be done, and the caller decides. |
| the time | The server waits 30 seconds for the app. A slow app is 504 `timeout`. |
| the errors | An app that does not run is 503 `unavailable` with `Retry-After`. An unknown app is 404. An answer that is not an entity is 502 `upstream`. The error entity of the app passes with its status. |

The rail of the browser has one entry for each app that runs, titled by its home. A phone shows the apps in the More
sheet. `lifelog do NAME-action field=value` and the MCP tool `NAME_action` follow from the merged catalog. An owner-only
action is no MCP tool. Both need the address of a running `serve` (`--url`): the in-process CLI and the temporary
server of `lifelog NAME` run no child, so they have no app.

### What the app writes back

What the owner did in the app comes back as entries of the journal. The app calls the API with `capture`, with an
`import_key` for each entry, as `import:NAME` ([imports](../docs/contract/imports.md)). A second run of the connector
writes nothing that it wrote before. The app uses `LIFELOG_URL` and `LIFELOG_TOKEN` as any extension does. It polls the
third party on a timer of its own, and it writes through the API and never into the file.

### State and credentials

The app keeps its state in `os.UserConfigDir()/lifelog-NAME/`, with mode `0700`, as any extension does. A secret of the
third party lives there and nowhere else: not in the settings of `lifelog`, not in `life.db`. The step that makes it is
a command of the connector, `lifelog NAME login`, which the owner runs once. A connector that cannot go on until the
owner acts (the login is missing) ends with status 2, so `serve` does not start it again every 10 seconds.

### In Go: the package `lifelog/app`

The package `lifelog/app` serves a catalog and never dials. `Listen(ctx, handler)` serves on `LIFELOG_LISTEN`, refuses an
unset value or an address that is not a loopback address, and drains on the end of `ctx`. `Catalog(title, home, actions,
handlers)` is the handler: it serves `GET /actions`, `GET /` and one route for each action, and it panics at the start
when an action has no handler. `Fail` writes an error entity. `StateDir(name)` makes the state folder. `ExitOwnerActs`
is 2. [`cmd/lifelog/testdata/withapp/main.go`](../cmd/lifelog/testdata/withapp/main.go) is the whole example, and
`cmd/lifelog/with_app_test.go` runs it on the real `lifelog serve --with`. An app in another language serves the same
routes with any HTTP library.

## Who writes

An extension writes as `import:NAME` (an importer) or `agent:NAME` (a model), with the header
`Lifelog-Source: import:NAME` or `Lifelog-Source: agent:NAME`. The owner-only actions are refused to such a writer by
the rule that exists already ([API.md](../API.md)). `GET /actions` of the server is the catalog of the actions.

## Tests

- Use synthetic data. Fake the third party.
- Build the real `lifelog` for an end-to-end test: `go build -C ../.. -o $TMP/lifelog ./cmd/lifelog`.
- Do not open the owner's `life.db`.

## The extensions

| folder | binary | command line | writes as | needs the server |
|---|---|---|---|---|
| `extensions/googlephotos` | `lifelog-gphotos` | `lifelog gphotos [--addr A] [--credentials F] [--zone Z]` | `import:google-photos` | yes |
| `extensions/agentic` | `lifelog-agentic-init` | `lifelog agentic-init [--force]` | nothing | no: it reads `LIFELOG_EXE` and `LIFELOG_DB`, and refuses to run without them |
| `extensions/mcp` | `lifelog-mcp` | `lifelog mcp [--agent NAME]` | `agent:NAME`, default `agent:mcp` | yes: `LIFELOG_URL`, else `--url`, else it refuses; `--source` is refused as today |
| `extensions/whatsapp` | `lifelog-whatsapp` | `lifelog serve --with whatsapp` (the always-on way), `lifelog whatsapp login` and `lifelog whatsapp [--terminal] [--url U] [--token-file F]` (the number is the setting `owner_number`) | `agent:whatsapp` | yes: `LIFELOG_URL`, else `--url`, else it refuses; `login` needs none |
