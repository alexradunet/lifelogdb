// lifelog is the writing application of a life.db (docs/guides/building-a-writer.md): one binary that serves
// the API, the MCP server and the CLI, all three through the same handler.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/mcp"
)

const version = "0.1.0"

const usage = `lifelog — the writer of a life.db

  lifelog init                        create a new life.db from docs/schema/schema.sql
  lifelog serve [--addr 127.0.0.1:7777]   the API: Siren JSON, or HTML in a browser
  lifelog mcp [--agent NAME]          the MCP server on stdio (writes as agent:NAME)

  lifelog get PATH                    fetch a resource, e.g. /, /days/today, /pages/12
  lifelog do ACTION [field=value...]  run any action of the catalog (lifelog actions)
  lifelog actions                     list the actions and their fields

  lifelog capture TEXT... [--mood N] [--day YYYY-MM-DD]
  lifelog day [YYYY-MM-DD]            the day view (default today)
  lifelog page TITLE                  a page by its title
  lifelog search QUERY...             full-text search
  lifelog query SQL                   one read-only SQL statement
  lifelog habits [YYYY-MM-DD]         the day's habits and completion
  lifelog done METRIC [--day D]       check a habit in as done (skip METRIC: not done)
  lifelog rename PAGE-ID TITLE        rename a plain page (the old one becomes a redirect stub)

Import (docs/guides/importing.md), with --workspace <source>.lifelog:
  lifelog import setup [--from life.db]   make trial.db: a copy of the real database, or a new one
  lifelog import approve rules|metrics    the owner's stamp (an interactive terminal only)
  lifelog import status                   gates, ledger, questions, next file, what to do now
  lifelog import check FILE | apply FILE  check or apply one facts file
  lifelog import replay --to PATH         the real run: the whole workspace into another database
  lifelog mcp --workspace DIR             the MCP server with the import tools added

Global flags, anywhere on the line:
  --db PATH      the life.db (default $LIFELOG_DB)
  --url URL      talk to a running "lifelog serve" instead of opening the file
  --source NAME  the writer recorded on new rows (default cli; lifelog_meta.source)
  --human        print a readable summary instead of JSON
  --workspace D  an import workspace (<source>.lifelog); the database defaults to its trial.db
`

type opts struct {
	db, url, source, addr, agent, mood, day, workspace, from, to string
	human                                                        bool
	args                                                         []string
}

// parse takes flags anywhere on the line (an old CLI ignored --db after the subcommand).
func parse(argv []string) (opts, error) {
	o := opts{db: os.Getenv("LIFELOG_DB"), source: "cli", addr: "127.0.0.1:7777"}
	vals := map[string]*string{"--db": &o.db, "--url": &o.url, "--source": &o.source, "--addr": &o.addr,
		"--agent": &o.agent, "--mood": &o.mood, "--day": &o.day, "--workspace": &o.workspace, "--from": &o.from, "--to": &o.to}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--human" {
			o.human = true
			continue
		}
		if a == "--" {
			o.args = append(o.args, argv[i+1:]...)
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		if p, ok := vals[name]; ok {
			if !hasVal {
				if i+1 >= len(argv) {
					return o, fmt.Errorf("%s needs a value", name)
				}
				i++
				val = argv[i]
			}
			*p = val
			continue
		}
		if strings.HasPrefix(a, "--") {
			return o, fmt.Errorf("unknown flag %s", a)
		}
		o.args = append(o.args, a)
	}
	return o, nil
}

func main() {
	o, err := parse(os.Args[1:])
	if err != nil || len(o.args) == 0 || o.args[0] == "help" {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "lifelog:", err)
		os.Exit(1)
	}
}

func run(o opts) error {
	cmd, args := o.args[0], o.args[1:]
	if cmd == "init" {
		if o.db == "" && len(args) == 1 {
			o.db = args[0]
		}
		if o.db == "" {
			return errors.New("init needs --db PATH")
		}
		if err := db.Init(o.db); err != nil {
			return err
		}
		fmt.Println("created", o.db)
		return nil
	}

	var ws *importer.Workspace
	if o.workspace != "" {
		var err error
		if ws, err = importer.Open(o.workspace); err != nil {
			return err
		}
		if o.db == "" || os.Getenv("LIFELOG_DB") == o.db {
			o.db = ws.TrialDB() // an import works on its trial database unless --db names another
		}
	}
	if cmd == "import" && len(args) > 0 && (args[0] == "setup" || args[0] == "approve") {
		return importOwner(o, ws, args)
	}

	if cmd == "mcp" {
		name := o.agent
		if name == "" {
			name = "mcp"
		}
		o.source = "agent:" + name
	}
	if err := core.CheckSource(o.source); err != nil {
		return err
	}

	var c *client.Client
	var handler http.Handler
	if o.url != "" && cmd != "serve" {
		c = client.Remote(o.url, o.source)
	} else {
		if o.db == "" {
			return errors.New("no database: pass --db PATH or set LIFELOG_DB")
		}
		d, err := db.Open(o.db)
		if err != nil {
			return err
		}
		defer d.Close()
		handler = api.New(&core.Store{DB: d}, ws)
		c = client.InProcess(handler, o.source)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch cmd {
	case "serve":
		if o.url != "" {
			return errors.New("serve opens the file itself: drop --url")
		}
		srv := &http.Server{Addr: o.addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
		fmt.Fprintf(os.Stderr, "lifelog: serving %s on http://%s\n", o.db, o.addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case "mcp":
		return mcp.Serve(ctx, c, version)
	case "get":
		if len(args) != 1 {
			return errors.New("get PATH")
		}
		return show(o)(c.Get("/" + strings.TrimPrefix(args[0], "/"))) // "pages/1" too: some shells rewrite a leading /
	case "actions":
		return listActions(o, c)
	case "do":
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
		return doAction(o, c, args[0], vals)
	case "capture":
		day := o.day
		if day == "" {
			day = core.Today()
		}
		vals := map[string]string{"day": day, "text": strings.Join(args, " ")}
		if o.mood != "" {
			vals["mood"] = o.mood
		}
		return doAction(o, c, "capture", vals)
	case "day":
		day := "today"
		if len(args) == 1 {
			day = args[0]
		}
		return show(o)(c.Get("/days/" + day))
	case "page":
		return show(o)(c.Get("/pages?title=" + url.QueryEscape(strings.Join(args, " "))))
	case "search":
		return doAction(o, c, "search", map[string]string{"q": strings.Join(args, " ")})
	case "query":
		return doAction(o, c, "query", map[string]string{"sql": strings.Join(args, " ")})
	case "habits":
		day := core.Today()
		if len(args) == 1 {
			day = args[0]
		}
		return show(o)(c.Get("/habits?day=" + day))
	case "done", "skip":
		if len(args) != 1 {
			return fmt.Errorf("%s METRIC [--day YYYY-MM-DD]", cmd)
		}
		day := o.day
		if day == "" {
			day = core.Today()
		}
		done := map[string]string{"done": "1", "skip": "0"}[cmd]
		return doAction(o, c, "check-in", map[string]string{"name": args[0], "day": day, "done": done})
	case "rename":
		if len(args) != 2 {
			return errors.New("rename PAGE-ID NEW-TITLE")
		}
		return doAction(o, c, "rename", map[string]string{"id": args[0], "title": args[1]})
	case "import":
		return importCommand(o, c, args)
	}
	return fmt.Errorf("unknown command %q (lifelog help)", cmd)
}

func doAction(o opts, c *client.Client, name string, vals map[string]string) error {
	actions, err := c.Catalog()
	if err != nil {
		return err
	}
	for _, a := range actions {
		if a.Name == name {
			return show(o)(c.Do(a, vals))
		}
	}
	return fmt.Errorf("no action %q (lifelog actions)", name)
}

func listActions(o opts, c *client.Client) error {
	actions, err := c.Catalog()
	if err != nil {
		return err
	}
	if !o.human {
		return printJSON(actions)
	}
	for _, a := range actions {
		var fs []string
		for _, f := range a.Fields {
			s := f.Name
			if !f.Required {
				s = "[" + s + "]"
			}
			fs = append(fs, s)
		}
		fmt.Printf("%-14s %s %s\n  %s\n  fields: %s\n", a.Name, a.Method, a.Href, a.Description, strings.Join(fs, " "))
	}
	return nil
}

// show prints an entity (an error entity too: an agent reads the message and the links) and returns the error.
func show(o opts) func(*api.Entity, error) error {
	return func(e *api.Entity, err error) error {
		if e == nil {
			return err
		}
		if o.human {
			human(e)
		} else if perr := printJSON(e); perr != nil {
			return perr
		}
		return err
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func human(e *api.Entity) {
	fmt.Printf("# %s  (%s)\n", e.Title, strings.Join(e.Class, " "))
	if e.Result != nil {
		b, _ := json.Marshal(e.Result)
		fmt.Printf("result: %s\n", b)
	}
	if p, ok := e.Properties.(map[string]any); ok {
		keys := make([]string, 0, len(p))
		for k := range p {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch v := p[k].(type) {
			case string:
				if strings.Contains(v, "\n") {
					fmt.Printf("%s:\n%s\n", k, indent(v))
				} else if v != "" {
					fmt.Printf("%s: %s\n", k, v)
				}
			default:
				b, _ := json.Marshal(v)
				fmt.Printf("%s: %s\n", k, b)
			}
		}
	}
	for _, l := range e.Entities {
		fmt.Printf("  → %-12s %-28s %s\n", strings.Join(l.Rel, " "), l.Title, l.Href)
	}
	for _, l := range e.Links {
		fmt.Printf("  ↗ %-12s %s\n", strings.Join(l.Rel, " "), l.Href)
	}
	for _, a := range e.Actions {
		fmt.Printf("  ! %-14s %s %s\n", a.Name, a.Method, a.Href)
	}
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }

// importOwner runs the import steps that need no running API: setup makes the trial database, approve is the
// owner's stamp and is never reachable by the API or a model.
func importOwner(o opts, ws *importer.Workspace, args []string) error {
	if ws == nil {
		return errors.New("import needs --workspace <source>.lifelog")
	}
	switch args[0] {
	case "setup":
		what, err := ws.Setup(o.from)
		if err != nil {
			return err
		}
		fmt.Printf("trial.db %s: %s\n", what, ws.TrialDB())
		d, err := db.Open(ws.TrialDB())
		if err != nil {
			return err
		}
		defer d.Close()
		res, err := (&core.Store{DB: d}).Integrity(context.Background())
		if err != nil {
			return err
		}
		if !res.OK {
			return printJSON(res)
		}
		fmt.Println("integrity: ok")
		return nil
	case "approve":
		if len(args) != 2 || (args[1] != "rules" && args[1] != "metrics") {
			return errors.New("import approve rules|metrics")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("approve runs only at an interactive terminal: the owner approves, never a script or a model")
		}
		file := args[1] + ".md"
		b, err := os.ReadFile(filepath.Join(ws.Dir, file))
		if err != nil {
			return err
		}
		fmt.Printf("%s\n---\nType approve to stamp %s as yours: ", b, file)
		var answer string
		fmt.Scanln(&answer)
		if answer != "approve" {
			return errors.New("not approved")
		}
		if err := ws.Approve(file, time.Now()); err != nil {
			return err
		}
		fmt.Println(file, "approved")
		return nil
	}
	return nil
}

// importCommand is the import's shortcuts; every other operation is `lifelog do <name> field=value`.
func importCommand(o opts, c *client.Client, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		return show(o)(c.Get("/import"))
	case "check", "apply":
		if len(args) != 2 {
			return fmt.Errorf("import %s FILE", args[0])
		}
		return doAction(o, c, args[0]+"-facts", map[string]string{"file": args[1]})
	case "replay":
		if o.to == "" {
			return errors.New("import replay --to PATH")
		}
		return doAction(o, c, "replay", map[string]string{"to": o.to})
	}
	return fmt.Errorf("unknown import step %q (lifelog help)", args[0])
}
