// lifelog is the writing application of a life.db (docs/guides/building-a-writer.md): one binary that serves
// the API, the MCP server and the CLI, all three through the same handler.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"lifelog/internal/api"
	"lifelog/internal/chat"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/mcp"
)

const version = "0.1.0"

// opts are the flags of a command line and its words. A flag may stand anywhere on the line; a command takes the
// global flags and its own (command.flags), and refuses any other.
type opts struct {
	db, url, source, addr, agent, mood, day, from, to, title, text, preview, mime, at, radius, column string
	tokenFile, tlsCert, tlsKey, snapshots                                                             string
	allowOrigins                                                                                      []string
	human, dryRun, dbExplicit, addrExplicit, public, force                                            bool
	args                                                                                              []string
	given                                                                                             []string           // the flags the line named
	pub                                                                                               *api.PublicOptions // serve --public, read by its prepare
}

// globalFlags are the flags every command takes.
var globalFlags = []string{"--db", "--url", "--source", "--human"}

const globalUsage = `
Global flags, anywhere on the line:
  --db PATH      the life.db (default $LIFELOG_DB, else the life.db beside this program)
  --url URL      talk to a running "lifelog serve" instead of opening the file ($LIFELOG_TOKEN if it is --public)
  --source NAME  the writer recorded on new rows (default cli; lifelog_meta.source)
  --human        print a readable summary instead of JSON
`

// env is what a command runs with: the line, the client (nil for serve, which mounts the handler itself), and the
// handler (nil over --url, when a running server is the one).
type env struct {
	o       opts
	c       *client.Client
	handler http.Handler
}

// command is one subcommand. The table commands is the one list of them: the usage, the dispatch, the flags a
// command takes and whether an interrupt reaches it all read it.
type command struct {
	name    string
	gap     bool                // a blank line before its usage: the start of a group
	usage   string              // its lines of the usage text; "" for an alias with none of its own
	flags   []string            // the flags it takes, besides the global ones
	setup   bool                // a setup command of the machine (init, agentic-init): no client, and the OS's interrupt
	prepare func(o *opts) error // what it checks and sets before anything opens
	run     func(ctx context.Context, e env, args []string) error
}

var commands = []command{
	{name: "init", setup: true, gap: true,
		usage: "  lifelog init [PATH]                 create a new life.db from docs/schema/schema.sql (default: beside this program)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) > 1 {
				return errors.New("init [PATH]")
			}
			path, err := dbPath(e.o, strings.Join(args, ""))
			if err != nil {
				return err
			}
			if err := db.Init(ctx, path); err != nil {
				return err
			}
			fmt.Println("created", path)
			return nil
		}},
	{name: "agentic-init", setup: true, flags: []string{"--force"},
		usage: "  lifelog agentic-init [--force]      make this program's folder ready for a coding agent: AGENTS.md, CLAUDE.md,\n" +
			"                                      skills, and the MCP config of Claude Code, Codex, OpenCode and Pi; a file\n" +
			"                                      with another text is kept unless --force",
		run: func(_ context.Context, e env, _ []string) error { return agenticInit(e.o) }},
	{name: "serve", flags: []string{"--addr", "--snapshots", "--public", "--token-file", "--allow-origin", "--tls-cert", "--tls-key"},
		usage: "  lifelog serve [--addr 127.0.0.1:7777] [--snapshots DIR]   the API: Siren JSON, or HTML in a browser; this machine only\n" +
			"  lifelog serve --public [--addr 0.0.0.0:7777] [--allow-origin O]... [--tls-cert F --tls-key F]\n" +
			"                                      the API for other devices: every request needs the owner's token\n" +
			"                                      (--token-file PATH or $LIFELOG_TOKEN); a browser logs in at /login",
		prepare: prepareServe, run: serve},
	{name: "mcp", flags: []string{"--agent"},
		usage: "  lifelog mcp [--agent NAME]          the MCP server on stdio (writes as agent:NAME)",
		prepare: func(o *opts) error {
			name := o.agent
			if name == "" {
				name = "mcp"
			}
			o.source = "agent:" + name
			return nil
		},
		run: func(ctx context.Context, e env, _ []string) error { return mcp.Serve(ctx, e.c, version) }},
	{name: "get", gap: true,
		usage: "  lifelog get PATH                    fetch a resource, e.g. /, /days/today, /pages/12 (or pages/12: Git Bash rewrites a leading /)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 1 {
				return errors.New("get PATH")
			}
			return show(e.o)(e.c.Get(ctx, "/"+strings.TrimPrefix(args[0], "/"))) // "pages/1" too: some shells rewrite a leading /
		}},
	{name: "do",
		usage: "  lifelog do ACTION [field=value...]  run any action of the catalog (lifelog actions)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) == 0 {
				return errors.New("do ACTION [field=value...]")
			}
			vals := map[string]string{}
			for _, kv := range args[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("%q is not field=value", kv)
				}
				vals[k] = v
			}
			return doAction(ctx, e, args[0], vals)
		}},
	{name: "actions",
		usage: "  lifelog actions                     list the actions and their fields",
		run:   func(ctx context.Context, e env, _ []string) error { return listActions(ctx, e.o, e.c) }},
	{name: "capture", gap: true, flags: []string{"--mood", "--day"},
		usage: "  lifelog capture TEXT... [--mood N] [--day YYYY-MM-DD]",
		run: func(ctx context.Context, e env, args []string) error {
			// The day is the server's today unless --day names one: with --url, the server's clock is the one that counts.
			vals := map[string]string{"day": dayOrToday(e.o), "text": strings.Join(args, " ")}
			if e.o.mood != "" {
				vals["mood"] = e.o.mood
			}
			return doAction(ctx, e, "capture", vals)
		}},
	{name: "day",
		usage: "  lifelog day [YYYY-MM-DD]            the day view (default today)",
		run: func(ctx context.Context, e env, args []string) error {
			day := "today"
			if len(args) == 1 {
				day = args[0]
			}
			return show(e.o)(e.c.Get(ctx, "/days/"+url.PathEscape(day)))
		}},
	{name: "ask",
		usage: "  lifelog ask QUESTION...             ask lifelog in words: a model answers with the actions (the model of\n" +
			"                                      $LIFELOG_MODEL_URL and $LIFELOG_MODEL where lifelog runs); the turn is\n" +
			"                                      kept on the conversation page of the day",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, "ask", map[string]string{"question": strings.Join(args, " ")})
		}},
	{name: "page",
		usage: "  lifelog page TITLE                  a page by its title",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, "find", map[string]string{"title": strings.Join(args, " ")})
		}},
	{name: "search",
		usage: "  lifelog search QUERY...             full-text search",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, "search", map[string]string{"q": strings.Join(args, " ")})
		}},
	{name: "query",
		usage: "  lifelog query SQL                   one read-only SQL statement",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, "query", map[string]string{"sql": strings.Join(args, " ")})
		}},
	{name: "habits",
		usage: "  lifelog habits [YYYY-MM-DD]         the day's habits and completion",
		run: func(ctx context.Context, e env, args []string) error {
			href := "/habits" // the server's today
			if len(args) == 1 {
				href += "?day=" + url.QueryEscape(args[0])
			}
			return show(e.o)(e.c.Get(ctx, href))
		}},
	{name: "done", flags: []string{"--day"},
		usage: "  lifelog done METRIC [--day D]       check a habit in as done (skip METRIC: not done)",
		run:   checkIn("1")},
	{name: "skip", flags: []string{"--day"}, run: checkIn("0")},
	{name: "tasks",
		usage: "  lifelog tasks                       every task; lifelog do create-task label=... makes one",
		run:   func(ctx context.Context, e env, _ []string) error { return show(e.o)(e.c.Get(ctx, "/tasks")) }},
	{name: "due", flags: []string{"--from", "--to"},
		usage: "  lifelog due [--from D] [--to D]     what is due: every task's open occurrences in the window",
		run: func(ctx context.Context, e env, _ []string) error {
			vals := map[string]string{}
			if e.o.from != "" {
				vals["from"] = e.o.from
			}
			if e.o.to != "" {
				vals["through"] = e.o.to
			}
			return doAction(ctx, e, "deadlines", vals)
		}},
	{name: "rename",
		usage: "  lifelog rename PAGE-ID NEW-TITLE    rename a named object, retaining its id and old-name aliases",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 2 {
				return errors.New("rename PAGE-ID NEW-TITLE")
			}
			return doAction(ctx, e, "rename", map[string]string{"id": args[0], "title": args[1]})
		}},
	{name: "file", flags: []string{"--title", "--text", "--preview", "--mime", "--day", "--at", "--radius", "--dry-run"},
		usage: "  lifelog file PATH... [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day D] [--at PLACE [--radius M]] [--dry-run]\n" +
			"                                      keep a file (docs/cookbook/keep-a-file.md): the original is hashed,\n" +
			"                                      never stored; its text from FILE; a picture made from a JPEG, PNG or\n" +
			"                                      GIF, else from PICTURE; the title defaults to the file's name. A photo\n" +
			"                                      links its day to the place it was taken in; one near no place is\n" +
			"                                      reported, and --at names the place (docs/cookbook/place-of-a-photo.md)\n" +
			"                                      Several paths, or a folder's photos: the few chosen for a day, kept\n" +
			"                                      together, with a report; --dry-run writes nothing",
		run: func(ctx context.Context, e env, args []string) error { return keepFile(ctx, e.o, e.c, args) }},
	{name: "snapshot", flags: []string{"--to"},
		usage: "  lifelog snapshot [--to DIR]         a dated copy of life.db (life-YYYY-MM-DD.db, beside it or in DIR),\n" +
			"                                      then its restore check (docs/cookbook/take-a-snapshot.md); with --url the\n" +
			"                                      server's folder (serve --snapshots), and --to is refused",
		prepare: func(o *opts) error {
			if o.url != "" && o.to != "" {
				return errors.New("snapshot over --url lands in the server's folder (serve --snapshots): drop --to")
			}
			if o.url == "" {
				o.snapshots = o.to
				if o.snapshots != "" {
					// Refused before the database is opened: a mistaken folder leaves life.db untouched.
					if _, err := db.CheckSnapshotDir(o.snapshots); err != nil {
						return err
					}
				}
			}
			return nil
		},
		run: func(ctx context.Context, e env, _ []string) error { return doAction(ctx, e, "snapshot", nil) }},
	{name: "readings", gap: true, flags: []string{"--column"},
		usage: "An import is done by a local agent with the owner, through the catalog (lifelog mcp --agent NAME;\n" +
			"docs/plans/089-no-import-process.md). One action helps it:\n" +
			"  lifelog readings PAGE METRIC [--column NAME]   readings of METRIC from the table of PAGE (readings-from-table):\n" +
			"                                      a row with a day and a plain number in the metric's unit; the\n" +
			"                                      others are reported; a second run writes nothing",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 2 {
				return errors.New("readings PAGE METRIC [--column NAME]")
			}
			vals := map[string]string{"page": args[0], "metric": args[1]}
			if e.o.column != "" {
				vals["column"] = e.o.column
			}
			return doAction(ctx, e, "readings-from-table", vals)
		}},
}

// commandNamed is the command of a name, nil for none.
func commandNamed(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// usageText is the usage, assembled from the table so that a command cannot be missing from it.
func usageText() string {
	var b strings.Builder
	b.WriteString("lifelog — the writer of a life.db\n")
	for _, c := range commands {
		if c.usage == "" {
			continue
		}
		if c.gap {
			b.WriteString("\n")
		}
		b.WriteString(c.usage)
		b.WriteString("\n")
	}
	b.WriteString(globalUsage)
	return b.String()
}

// checkIn is done and skip: a habit's check-in on --day, else the server's today.
func checkIn(done string) func(ctx context.Context, e env, args []string) error {
	return func(ctx context.Context, e env, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("%s METRIC [--day YYYY-MM-DD]", e.o.args[0])
		}
		return doAction(ctx, e, "check-in", map[string]string{"name": args[0], "day": dayOrToday(e.o), "done": done})
	}
}

// parse takes flags anywhere on the line (an old CLI ignored --db after the subcommand). A flag the command does not
// take is refused, never ignored.
func parse(argv []string) (opts, error) {
	o := opts{db: os.Getenv("LIFELOG_DB"), source: "cli", addr: "127.0.0.1:7777"}
	vals := map[string]*string{"--db": &o.db, "--url": &o.url, "--source": &o.source, "--addr": &o.addr,
		"--agent": &o.agent, "--mood": &o.mood, "--day": &o.day, "--from": &o.from, "--to": &o.to,
		"--title": &o.title, "--text": &o.text, "--preview": &o.preview, "--mime": &o.mime, "--at": &o.at, "--radius": &o.radius,
		"--token-file": &o.tokenFile, "--tls-cert": &o.tlsCert, "--tls-key": &o.tlsKey, "--snapshots": &o.snapshots, "--column": &o.column}
	bools := map[string]*bool{"--human": &o.human, "--dry-run": &o.dryRun, "--public": &o.public, "--force": &o.force}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.args = append(o.args, argv[i+1:]...)
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		if p, ok := bools[name]; ok {
			*p = true
			o.given = append(o.given, name)
			continue
		}
		p, ok := vals[name]
		if !ok && name != "--allow-origin" {
			if strings.HasPrefix(a, "--") {
				return o, fmt.Errorf("unknown flag %s", a)
			}
			o.args = append(o.args, a)
			continue
		}
		if !hasVal {
			if i+1 >= len(argv) {
				return o, fmt.Errorf("%s needs a value", name)
			}
			i++
			val = argv[i]
		}
		o.given = append(o.given, name)
		switch name {
		case "--allow-origin":
			o.allowOrigins = append(o.allowOrigins, val)
		case "--db":
			*p, o.dbExplicit = val, true
		case "--addr":
			*p, o.addrExplicit = val, true
		default:
			*p = val
		}
	}
	if len(o.args) > 0 {
		if cmd := commandNamed(o.args[0]); cmd != nil {
			for _, f := range o.given {
				if !slices.Contains(globalFlags, f) && !slices.Contains(cmd.flags, f) {
					return o, fmt.Errorf("%s does not take %s", cmd.name, f)
				}
			}
		}
	}
	return o, nil
}

func main() {
	o, err := parse(os.Args[1:])
	if err != nil || len(o.args) == 0 || o.args[0] == "help" {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		fmt.Fprint(os.Stderr, usageText())
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "lifelog:", err)
		os.Exit(1)
	}
}

// commandConsumesContext says whether an interrupt reaches the command through its context: every command that runs
// through the client or the server does; a setup command of the machine keeps the OS's interrupt.
func commandConsumesContext(o opts) bool {
	if len(o.args) == 0 {
		return false
	}
	cmd := commandNamed(o.args[0])
	return cmd != nil && !cmd.setup
}

func run(o opts) error {
	if !commandConsumesContext(o) {
		return runContext(context.Background(), o)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runContext(ctx, o)
}

// runContext runs the command of the line: its prepare, then the client (a running server over --url, or the handler
// in-process on the file dbPath names), then its run.
func runContext(ctx context.Context, o opts) error {
	cmd := commandNamed(o.args[0])
	if cmd == nil {
		return fmt.Errorf("unknown command %q (lifelog help)", o.args[0])
	}
	args := o.args[1:]
	if cmd.prepare != nil {
		if err := cmd.prepare(&o); err != nil {
			return err
		}
	}
	if cmd.setup {
		return cmd.run(ctx, env{o: o}, args)
	}
	if err := core.CheckSource(o.source); err != nil {
		return err
	}
	e := env{o: o}
	if o.url != "" {
		e.c = client.Remote(o.url, o.source, os.Getenv("LIFELOG_TOKEN"))
		return cmd.run(ctx, e, args)
	}
	path, err := dbPath(o, "")
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, path)
	if err != nil {
		return err
	}
	defer d.Close()
	var origins []string
	if o.pub != nil {
		origins = o.pub.Origins
	}
	e.o.db = path
	// The agent of ask calls this same handler in-process, as agent:chat (docs/plans/102-a-conversation-with-lifelog.md).
	var asker api.Asker
	agent := &chat.Agent{Model: chat.ModelFromEnv()}
	if agent.Model != nil {
		asker = agent
	}
	e.handler = api.NewAsking(&core.Store{DB: d, SnapshotDir: o.snapshots}, asker, origins...)
	agent.Client = client.InProcess(e.handler, chat.Source)
	if cmd.name != "serve" { // serve mounts the handler on a socket and calls it through no client
		e.c = client.InProcess(e.handler, o.source)
	}
	return cmd.run(ctx, e, args)
}

// doAction runs a catalog action by name and shows its answer.
func doAction(ctx context.Context, e env, name string, vals map[string]string) error {
	return show(e.o)(do(ctx, e.c, name, vals))
}

func do(ctx context.Context, c *client.Client, name string, vals map[string]string) (*api.Entity, error) {
	actions, err := c.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		if a.Name == name {
			return c.Do(ctx, a, vals)
		}
	}
	return nil, fmt.Errorf("no action %q (lifelog actions)", name)
}

// dayOrToday is the day a shortcut sends: --day, else "today", which the API reads as the server's local day.
func dayOrToday(o opts) string {
	if o.day != "" {
		return o.day
	}
	return "today"
}

// dbPath is the life.db a command uses, in this order: --db, the PATH init alone takes, $LIFELOG_DB, the file beside
// this program (docs/plans/090-agentic-init.md). PATH with --db is a mistake, refused.
func dbPath(o opts, positional string) (string, error) {
	switch {
	case o.dbExplicit && positional != "":
		return "", errors.New("init takes PATH or --db, not both")
	case o.dbExplicit:
		return o.db, nil
	case positional != "":
		return positional, nil
	case o.db != "": // $LIFELOG_DB, read by parse
		return o.db, nil
	}
	return besideExecutable("life.db")
}

// besideExecutable is a file in the folder of the running program, where life.db lives when no --db or LIFELOG_DB
// names another (docs/plans/090-agentic-init.md).
func besideExecutable(name string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("the folder of this program: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Join(filepath.Dir(exe), name), nil
}
