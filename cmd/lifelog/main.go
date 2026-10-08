// lifelog is the writing application of a life.db (docs/guides/building-a-writer.md): one binary that serves
// the API, the MCP server and the CLI, all three through the same handler.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
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
	"lifelog/internal/inventory"
	"lifelog/internal/mcp"
	"lifelog/internal/takeout"
)

const version = "0.1.0"

const usage = `lifelog — the writer of a life.db

  lifelog init                        create a new life.db from docs/schema/schema.sql
  lifelog serve [--addr 127.0.0.1:7777]   the API: Siren JSON, or HTML in a browser; this machine only
  lifelog serve --public [--addr 0.0.0.0:7777] [--allow-origin O]... [--tls-cert F --tls-key F]
                                      the API for other devices: every request needs the owner's token
                                      (--token-file PATH or $LIFELOG_TOKEN); a browser logs in at /login
  lifelog mcp [--agent NAME]          the MCP server on stdio (writes as agent:NAME)

  lifelog get PATH                    fetch a resource, e.g. /, /days/today, /pages/12 (or pages/12: Git Bash rewrites a leading /)
  lifelog do ACTION [field=value...]  run any action of the catalog (lifelog actions)
  lifelog actions                     list the actions and their fields

  lifelog capture TEXT... [--mood N] [--day YYYY-MM-DD]
  lifelog day [YYYY-MM-DD]            the day view (default today)
  lifelog page TITLE                  a page by its title
  lifelog search QUERY...             full-text search
  lifelog query SQL                   one read-only SQL statement
  lifelog habits [YYYY-MM-DD]         the day's habits and completion
  lifelog done METRIC [--day D]       check a habit in as done (skip METRIC: not done)
  lifelog tasks                       every task; lifelog do create-task label=... makes one
  lifelog due [--from D] [--to D]     what is due: every task's open occurrences in the window
  lifelog rename PAGE-ID TITLE        rename a named object, retaining its id and old-name aliases
  lifelog file PATH... [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day D] [--at PLACE [--radius M]] [--dry-run]
                                      keep a file (docs/cookbook/keep-a-file.md): the original is hashed,
                                      never stored; its text from FILE; a picture made from a JPEG, PNG or
                                      GIF, else from PICTURE; the title defaults to the file's name. A photo
                                      links its day to the place it was taken in; one near no place is
                                      reported, and --at names the place (docs/cookbook/place-of-a-photo.md)
                                      Several paths, or a folder's photos: the few chosen for a day, kept
                                      together, with a report; --dry-run writes nothing
  lifelog snapshot [--to DIR]         a dated copy of life.db (life-YYYY-MM-DD.db, beside it or in DIR),
                                      then its restore check (docs/cookbook/take-a-snapshot.md)

Import (docs/guides/importing.md), with --workspace <source>.lifelog:
  lifelog import setup [--from life.db]   make trial.db: a copy of the real database, or a new one
  lifelog import approve rules|metrics|prepared|selected-photo    the owner's stamp (an interactive terminal only)
  lifelog import status                   gates, ledger, questions, next file, what to do now
  lifelog import check FILE | apply FILE  check or apply one facts file
  lifelog import replay --to PATH         the real run: the whole workspace into another database,
                                          rehearsed on a copy first (nothing written unless it is clean)
  lifelog import replay --to PATH --dry-run   the rehearsal alone: every failure, nothing written
  lifelog import inventory FOLDER         survey an ingest folder without reading a content: folders, counts by
                                          extension and name pattern, archive listings, the sources found (JSON)
  lifelog import takeout inventory FOLDER privacy-safe Timeline/Fit/Fitbit inventory (prints JSON, writes nothing)
  lifelog mcp --workspace DIR             the MCP server with the import tools added

Global flags, anywhere on the line:
  --db PATH      the life.db (default $LIFELOG_DB)
  --url URL      talk to a running "lifelog serve" instead of opening the file ($LIFELOG_TOKEN if it is --public)
  --source NAME  the writer recorded on new rows (default cli; lifelog_meta.source)
  --human        print a readable summary instead of JSON
  --workspace D  an import workspace (<source>.lifelog); the database defaults to its trial.db
`

type opts struct {
	db, url, source, addr, agent, mood, day, workspace, from, to, title, text, preview, mime, at, radius string
	tokenFile, tlsCert, tlsKey                                                                           string
	allowOrigins                                                                                         []string
	human, dryRun, dbExplicit, addrExplicit, public                                                      bool
	args                                                                                                 []string
}

// parse takes flags anywhere on the line (an old CLI ignored --db after the subcommand).
func parse(argv []string) (opts, error) {
	o := opts{db: os.Getenv("LIFELOG_DB"), source: "cli", addr: "127.0.0.1:7777"}
	vals := map[string]*string{"--db": &o.db, "--url": &o.url, "--source": &o.source, "--addr": &o.addr,
		"--agent": &o.agent, "--mood": &o.mood, "--day": &o.day, "--workspace": &o.workspace, "--from": &o.from, "--to": &o.to,
		"--title": &o.title, "--text": &o.text, "--preview": &o.preview, "--mime": &o.mime, "--at": &o.at, "--radius": &o.radius,
		"--token-file": &o.tokenFile, "--tls-cert": &o.tlsCert, "--tls-key": &o.tlsKey}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--human" {
			o.human = true
			continue
		}
		if a == "--dry-run" {
			o.dryRun = true
			continue
		}
		if a == "--public" {
			o.public = true
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
			if name == "--db" {
				o.dbExplicit = true
			}
			if name == "--addr" {
				o.addrExplicit = true
			}
			continue
		}
		if name == "--allow-origin" {
			if !hasVal {
				if i+1 >= len(argv) {
					return o, fmt.Errorf("%s needs a value", name)
				}
				i++
				val = argv[i]
			}
			o.allowOrigins = append(o.allowOrigins, val)
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

// Only intercept interrupts on paths that consume the resulting context. Owner-local
// operations without cancellation support retain the OS's normal interrupt behavior.
func commandConsumesContext(o opts) bool {
	if len(o.args) == 0 {
		return false
	}
	switch o.args[0] {
	case "serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "query", "habits", "done", "skip", "rename", "file", "tasks", "due":
		return true
	case "import":
		if len(o.args) == 1 {
			return true
		}
		switch o.args[1] {
		case "status", "check", "apply", "replay":
			return true
		case "inventory":
			return len(o.args) == 3
		case "takeout":
			return len(o.args) == 4 && o.args[2] == "inventory"
		}
	}
	return false
}

func run(o opts) error {
	if !commandConsumesContext(o) {
		return runContext(context.Background(), o)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runContext(ctx, o)
}

func runContext(ctx context.Context, o opts) error {
	cmd, args := o.args[0], o.args[1:]
	var pub *api.PublicOptions
	if cmd == "serve" {
		var err error
		if pub, err = publicOptions(o); err != nil {
			return err
		}
		if pub == nil {
			o.addr, err = api.LoopbackAddress(o.addr)
		} else {
			if !o.addrExplicit {
				o.addr = "0.0.0.0:7777"
			}
			o.addr, err = api.PublicAddress(o.addr)
		}
		if err != nil {
			return err
		}
	}
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
	if cmd == "import" && len(args) > 0 && args[0] == "takeout" {
		return importTakeoutContext(ctx, o, args[1:])
	}
	if cmd == "import" && len(args) > 0 && args[0] == "inventory" {
		if len(args) != 2 {
			return errors.New("import inventory FOLDER")
		}
		report, err := inventory.Run(ctx, args[1])
		if err != nil {
			return err
		}
		return printJSON(report)
	}

	var ws *importer.Workspace
	if o.workspace != "" {
		var err error
		if ws, err = importer.Open(o.workspace); err != nil {
			return err
		}
		if !o.dbExplicit {
			o.db = ws.TrialDB() // an import works on its trial database unless --db names another
		}
	}
	if cmd == "snapshot" {
		return snapshot(o)
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
		if token := os.Getenv("LIFELOG_TOKEN"); token != "" {
			c.SetToken(token)
		}
	} else {
		if o.db == "" {
			return errors.New("no database: pass --db PATH or set LIFELOG_DB")
		}
		d, err := db.Open(o.db)
		if err != nil {
			return err
		}
		defer d.Close()
		var origins []string
		if pub != nil {
			origins = pub.Origins
		}
		handler = api.New(&core.Store{DB: d}, ws, origins...)
		c = client.InProcess(handler, o.source)
	}

	switch cmd {
	case "serve":
		if o.url != "" {
			return errors.New("serve opens the file itself: drop --url")
		}
		listener, err := net.Listen("tcp", o.addr)
		if err != nil {
			return err
		}
		defer listener.Close()
		var networkHandler http.Handler
		scheme, note := "http", ""
		if pub == nil {
			networkHandler, err = api.NetworkAuthority(listener.Addr(), handler)
		} else {
			networkHandler, err = api.Public(handler, *pub)
			note = " (public: token required; the token travels in clear, use a private network)"
			if pub.TLS {
				scheme, note = "https", " (public: token required)"
				if listener, err = tlsListener(listener, o.tlsCert, o.tlsKey); err != nil {
					return err
				}
			}
		}
		if err != nil {
			return err
		}
		srv := &http.Server{Addr: listener.Addr().String(), Handler: networkHandler, ReadHeaderTimeout: 10 * time.Second}
		fmt.Fprintf(os.Stderr, "lifelog: serving %s on %s://%s%s\n", o.db, scheme, listener.Addr(), note)
		return serveHTTP(ctx, srv, listener, 10*time.Second)
	case "mcp":
		return mcp.Serve(ctx, c, version)
	case "get":
		if len(args) != 1 {
			return errors.New("get PATH")
		}
		return show(o)(c.GetContext(ctx, "/"+strings.TrimPrefix(args[0], "/"))) // "pages/1" too: some shells rewrite a leading /
	case "actions":
		return listActionsContext(ctx, o, c)
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
		return doActionContext(ctx, o, c, args[0], vals)
	case "capture":
		day := o.day
		if day == "" {
			day = core.Today()
		}
		vals := map[string]string{"day": day, "text": strings.Join(args, " ")}
		if o.mood != "" {
			vals["mood"] = o.mood
		}
		return doActionContext(ctx, o, c, "capture", vals)
	case "day":
		day := "today"
		if len(args) == 1 {
			day = args[0]
		}
		return show(o)(c.GetContext(ctx, "/days/"+day))
	case "page":
		return show(o)(c.GetContext(ctx, "/pages?title="+url.QueryEscape(strings.Join(args, " "))))
	case "search":
		return doActionContext(ctx, o, c, "search", map[string]string{"q": strings.Join(args, " ")})
	case "query":
		return doActionContext(ctx, o, c, "query", map[string]string{"sql": strings.Join(args, " ")})
	case "habits":
		day := core.Today()
		if len(args) == 1 {
			day = args[0]
		}
		return show(o)(c.GetContext(ctx, "/habits?day="+day))
	case "done", "skip":
		if len(args) != 1 {
			return fmt.Errorf("%s METRIC [--day YYYY-MM-DD]", cmd)
		}
		day := o.day
		if day == "" {
			day = core.Today()
		}
		done := map[string]string{"done": "1", "skip": "0"}[cmd]
		return doActionContext(ctx, o, c, "check-in", map[string]string{"name": args[0], "day": day, "done": done})
	case "tasks":
		return show(o)(c.GetContext(ctx, "/tasks"))
	case "due":
		vals := map[string]string{}
		if o.from != "" {
			vals["from"] = o.from
		}
		if o.to != "" {
			vals["through"] = o.to
		}
		return doActionContext(ctx, o, c, "deadlines", vals)
	case "rename":
		if len(args) != 2 {
			return errors.New("rename PAGE-ID NEW-TITLE")
		}
		return doActionContext(ctx, o, c, "rename", map[string]string{"id": args[0], "title": args[1]})
	case "file":
		return keepFileContext(ctx, o, c, args)
	case "import":
		return importCommandContext(ctx, o, c, args)
	}
	return fmt.Errorf("unknown command %q (lifelog help)", cmd)
}

// publicOptions reads the --public flags (docs/plans/078-network-server.md): nil without --public, when the
// flags that belong to it are refused rather than ignored; with it, the token from --token-file or $LIFELOG_TOKEN
// is required before any listener opens.
func publicOptions(o opts) (*api.PublicOptions, error) {
	if !o.public {
		switch {
		case o.tokenFile != "":
			return nil, errors.New("--token-file needs --public")
		case len(o.allowOrigins) > 0:
			return nil, errors.New("--allow-origin needs --public")
		case o.tlsCert != "" || o.tlsKey != "":
			return nil, errors.New("--tls-cert and --tls-key need --public")
		}
		return nil, nil
	}
	token := os.Getenv("LIFELOG_TOKEN")
	if o.tokenFile != "" {
		b, err := os.ReadFile(o.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("token file: %w", err)
		}
		token = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	}
	if token == "" {
		return nil, errors.New("--public needs a token: --token-file PATH (one line, 32 characters or more) or $LIFELOG_TOKEN")
	}
	if err := api.CheckToken(token); err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	for _, origin := range o.allowOrigins {
		if err := api.CheckOrigin(origin); err != nil {
			return nil, err
		}
	}
	if (o.tlsCert == "") != (o.tlsKey == "") {
		return nil, errors.New("--tls-cert and --tls-key go together")
	}
	return &api.PublicOptions{Token: token, Origins: o.allowOrigins, TLS: o.tlsCert != ""}, nil
}

// tlsListener serves TLS on a public listener with the owner's certificate; the standard library does the rest.
func tlsListener(l net.Listener, certFile, keyFile string) (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	return tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}), nil
}

// serveHTTP owns shutdown until admitted requests finish or the drain deadline
// expires. The caller must not close their database while Shutdown is still running.
func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener, drainTimeout time.Duration) error {
	stopped := make(chan struct{})
	drained := make(chan struct{})
	var drainErr error
	go func() {
		defer close(drained)
		select {
		case <-ctx.Done():
		case <-stopped:
		}
		// An interrupt stops new work but lets admitted HTTP requests finish. This
		// separate, bounded lifetime preserves command context values and is joined below.
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		defer cancel()
		err := srv.Shutdown(drainCtx)
		if err != nil {
			// Expiry closes connections and cancels their request contexts instead of
			// leaving a blocked upload or reader alive after the server has returned.
			err = errors.Join(err, srv.Close())
		}
		drainErr = err
	}()
	err := srv.Serve(listener)
	close(stopped)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	<-drained
	return errors.Join(err, drainErr)
}

func doActionContext(ctx context.Context, o opts, c *client.Client, name string, vals map[string]string) error {
	return show(o)(doContext(ctx, c, name, vals))
}

func do(c *client.Client, name string, vals map[string]string) (*api.Entity, error) {
	return doContext(context.Background(), c, name, vals)
}

func doContext(ctx context.Context, c *client.Client, name string, vals map[string]string) (*api.Entity, error) {
	actions, err := c.CatalogContext(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		if a.Name == name {
			return c.DoContext(ctx, a, vals)
		}
	}
	return nil, fmt.Errorf("no action %q (lifelog actions)", name)
}

// keepFileContext is `lifelog file PATH`: the add-file action with the original streamed from disk (hashed by the API,
// never stored), its text read from --text, and a picture from --preview when the original is not one lifelog reads.
func keepFileContext(ctx context.Context, o opts, c *client.Client, args []string) error {
	if len(args) == 0 {
		return errors.New("file PATH... [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day YYYY-MM-DD] [--at PLACE [--radius M]] [--dry-run]")
	}
	paths, err := mediaPaths(args)
	if err != nil {
		return err
	}
	one := len(args) == 1 && len(paths) == 1 && paths[0] == args[0]
	if !one && (o.title != "" || o.text != "" || o.preview != "" || o.mime != "") {
		return errors.New("--title, --text, --preview and --mime describe one file: keep it alone")
	}
	actions, err := c.CatalogContext(ctx)
	if err != nil {
		return err
	}
	var add api.Action
	for _, a := range actions {
		if a.Name == "add-file" {
			add = a
		}
	}
	if add.Name == "" {
		return errors.New("the API has no add-file action")
	}
	keep := func(p string) (*api.Entity, error) {
		vals := map[string]string{"title": o.title}
		if o.title == "" {
			vals["title"] = filepath.Base(p)
		}
		if o.text != "" {
			b, err := os.ReadFile(o.text)
			if err != nil {
				return nil, err
			}
			vals["body"] = string(b)
		}
		for k, v := range map[string]string{"mime": o.mime, "day": o.day, "at": o.at, "radius": o.radius} {
			if v != "" {
				vals[k] = v
			}
		}
		if o.dryRun {
			vals["dry_run"] = "1"
		}
		files := map[string]string{"original": p}
		if o.preview != "" {
			files["preview"] = o.preview
		}
		return c.DoFilesContext(ctx, add, vals, files)
	}
	if one {
		e, err := keep(paths[0])
		if err := show(o)(e, err); err != nil {
			return err
		}
		if u := unmatchedOf(e); u != nil {
			fmt.Fprintf(os.Stderr, "near no place: %v, %v (%v)\nname it: lifelog file %s --at PLACE [--radius METRES]\n", u["lat"], u["lon"], u["map"], paths[0])
		}
		return nil
	}
	var kept []keptFile
	failed := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		k := keptFile{Path: p}
		e, err := keep(p)
		if err != nil {
			k.Error = err.Error()
			failed++
		} else if r, ok := e.Result.(map[string]any); ok {
			k.Result = r
		}
		kept = append(kept, k)
	}
	s, err := summarise(kept, o.dryRun)
	if err != nil {
		return err
	}
	if o.human {
		s.print(os.Stdout)
	} else if err := printJSON(s); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files could not be kept", failed, len(paths))
	}
	return nil
}

// mediaPaths are the files to keep: a path as it is, a folder as its photos and videos (not its sub-folders), sorted.
func mediaPaths(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, a)
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			t := core.MimeOf(e.Name(), nil)
			if !e.IsDir() && (strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/")) {
				found = append(found, filepath.Join(a, e.Name()))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s holds no photo or video", a)
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
}

func unmatchedOf(e *api.Entity) map[string]any {
	if e == nil {
		return nil
	}
	r, _ := e.Result.(map[string]any)
	u, _ := r["unmatched"].(map[string]any)
	return u
}

// keptFile is what keeping one file of a batch did: the add-file result, or the refusal.
type keptFile struct {
	Path   string         `json:"path"`
	Error  string         `json:"error,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}

// batch is the report of a batch (docs/plans/033): per day what was kept and linked, and the photos near no place in
// groups, each to be named once.
type batch struct {
	DryRun    bool              `json:"dry_run,omitempty"`
	Kept      int               `json:"kept"`
	Already   int               `json:"kept_already"`
	Failed    int               `json:"failed"`
	Days      map[string]*daySo `json:"days"`
	NoDay     []string          `json:"no_day_of_their_own,omitempty"`
	Unmatched []*group          `json:"near_no_place,omitempty"`
	Files     []keptFile        `json:"files"`
}

type daySo struct {
	Photos int      `json:"photos"`
	At     []string `json:"at,omitempty"`         // the places linked
	Known  []string `json:"recognised,omitempty"` // places matched and never linked (home, work)
	Near   int      `json:"near_no_place,omitempty"`
}

type group struct {
	Lat, Lon float64  `json:"-"`
	Count    int      `json:"photos"`
	Days     []string `json:"days"`
	Map      string   `json:"map"`
	NameWith string   `json:"name_it_with"`
}

// Coordinates are a floating-point domain; leave all other response numbers (IDs
// in particular) in their lossless client representation.
func coordinate(v any) (float64, error) {
	var n float64
	var err error
	switch v := v.(type) {
	case json.Number:
		n, err = v.Float64()
	case float64:
		n = v
	default:
		return 0, fmt.Errorf("invalid photo coordinate %v", v)
	}
	if err != nil {
		return 0, fmt.Errorf("invalid photo coordinate: %w", err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("photo coordinate must be finite")
	}
	return n, nil
}

func summarise(files []keptFile, dry bool) (*batch, error) {
	b := &batch{DryRun: dry, Days: map[string]*daySo{}, Files: files}
	add := func(list []string, s string) []string {
		for _, x := range list {
			if x == s {
				return list
			}
		}
		return append(list, s)
	}
	for _, f := range files {
		r := f.Result
		switch {
		case f.Error != "":
			b.Failed++
			continue
		case r["existing"] == true:
			b.Already++
		default:
			b.Kept++
		}
		day, _ := r["day"].(string)
		u, _ := r["unmatched"].(map[string]any)
		if day == "" {
			b.NoDay = append(b.NoDay, filepath.Base(f.Path))
		} else {
			d := b.Days[day]
			if d == nil {
				d = &daySo{}
				b.Days[day] = d
			}
			d.Photos++
			if place, _ := r["place"].(string); place != "" {
				if r["linked"] == true {
					d.At = add(d.At, place)
				} else {
					d.Known = add(d.Known, place)
				}
			}
			if u != nil {
				d.Near++
			}
		}
		if u == nil {
			continue
		}
		lat, err := coordinate(u["lat"])
		if err != nil {
			return nil, err
		}
		lon, err := coordinate(u["lon"])
		if err != nil {
			return nil, err
		}
		var g *group
		for _, x := range b.Unmatched {
			if haversine(lat, lon, x.Lat, x.Lon) <= 500 {
				g = x
				break
			}
		}
		if g == nil {
			m, _ := u["map"].(string)
			g = &group{Lat: lat, Lon: lon, Map: m,
				NameWith: fmt.Sprintf("lifelog file \"%s\" --at PLACE [--radius METRES], then keep them all again", f.Path)}
			b.Unmatched = append(b.Unmatched, g)
		}
		g.Count++
		if day != "" {
			g.Days = add(g.Days, day)
		}
	}
	for _, g := range b.Unmatched {
		sort.Strings(g.Days)
	}
	return b, nil
}

func (b *batch) print(w io.Writer) {
	if b.DryRun {
		fmt.Fprintln(w, "dry run: nothing written")
	}
	fmt.Fprintf(w, "%d files: %d kept, %d kept already, %d failed\n", b.Kept+b.Already+b.Failed, b.Kept, b.Already, b.Failed)
	days := make([]string, 0, len(b.Days))
	for d := range b.Days {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		x := b.Days[d]
		line := fmt.Sprintf("%s: %d photo(s)", d, x.Photos)
		if len(x.At) > 0 {
			line += " · at " + strings.Join(x.At, ", ")
		}
		if len(x.Known) > 0 {
			line += " · recognised, not linked: " + strings.Join(x.Known, ", ")
		}
		if x.Near > 0 {
			line += fmt.Sprintf(" · %d near no place", x.Near)
		}
		fmt.Fprintln(w, line)
	}
	if len(b.NoDay) > 0 {
		fmt.Fprintf(w, "no day of their own (no EXIF date): %s\n", strings.Join(b.NoDay, ", "))
	}
	if len(b.Unmatched) > 0 {
		fmt.Fprintf(w, "near no place, %d group(s):\n", len(b.Unmatched))
		for i, g := range b.Unmatched {
			fmt.Fprintf(w, "  %d. %d photo(s), %s, %s\n     name it: %s\n", i+1, g.Count, strings.Join(g.Days, " "), g.Map, g.NameWith)
		}
	}
	for _, f := range b.Files {
		if f.Error != "" {
			fmt.Fprintf(w, "failed: %s: %s\n", f.Path, f.Error)
		}
	}
}

// haversine is the great-circle distance in metres, for grouping the photos of a report.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dlat, dlon := (lat2-lat1)*rad, (lon2-lon1)*rad
	h := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Pow(math.Sin(dlon/2), 2)
	return 2 * 6371008.8 * math.Asin(math.Sqrt(h))
}

func listActionsContext(ctx context.Context, o opts, c *client.Client) error {
	actions, err := c.CatalogContext(ctx)
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

// snapshot is the owner's `lifelog snapshot [--to DIR]` (docs/cookbook/take-a-snapshot.md, D25): a file on this
// machine, never an API action or an MCP tool. The folder defaults to the one that holds life.db.
func snapshot(o opts) error {
	if o.url != "" {
		return errors.New("snapshot reads the file itself: drop --url")
	}
	if o.db == "" {
		return errors.New("no database: pass --db PATH or set LIFELOG_DB")
	}
	dir := o.to
	if dir == "" {
		dir = filepath.Dir(o.db)
	}
	path, res, err := takeSnapshot(context.Background(), o.db, dir, time.Now())
	if err != nil {
		return err
	}
	if o.human && res.OK {
		fmt.Printf("snapshot: %s\nrestore check: ok\n", path)
	} else if err := printJSON(map[string]any{"snapshot": path, "restore_check": res}); err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("%s failed its restore check: it is kept, but it is not one to restore", path)
	}
	return nil
}

// takeSnapshot takes the snapshot and runs its restore check: the four integrity checks, which leave it as it was.
func takeSnapshot(ctx context.Context, from, dir string, now time.Time) (string, *core.IntegrityResult, error) {
	path, err := db.Snapshot(from, dir, now)
	if err != nil {
		return "", nil, err
	}
	d, err := db.OpenSnapshot(path)
	if err != nil {
		return path, nil, fmt.Errorf("the snapshot %s cannot be checked: %w", path, err)
	}
	defer d.Close()
	res, err := (&core.Store{DB: d}).Integrity(ctx)
	if err != nil {
		return path, nil, fmt.Errorf("the restore check of %s: %w", path, err)
	}
	return path, res, nil
}

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
			if err := printJSON(res); err != nil {
				return err
			}
			return fmt.Errorf("%s failed trial integrity: the trial is kept for diagnosis", ws.TrialDB())
		}
		fmt.Println("integrity: ok")
		return nil
	case "approve":
		if len(args) != 2 || (args[1] != "rules" && args[1] != "metrics" && args[1] != "prepared" && args[1] != "selected-photo") {
			return errors.New("import approve rules|metrics|prepared|selected-photo")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("approve runs only at an interactive terminal: the owner approves, never a script or a model")
		}
		file := args[1] + ".md"
		rv, err := ws.Review(file)
		if err != nil {
			return err
		}
		switch {
		case rv.Since == "":
			fmt.Printf("%s, the whole text (no earlier approval to compare with):\n\n%s\n---\n", file, rv.Text)
		case rv.Text == "":
			fmt.Printf("%s: no line changed since your approval of %s.\n---\n", file, rv.Since)
		default:
			fmt.Printf("%s: the lines changed since your approval of %s (- as you approved it, + now; line numbers of the file):\n\n%s\n---\n", file, rv.Since, rv.Text)
		}
		fmt.Printf("Type approve to stamp %s as yours: ", file)
		var answer string
		fmt.Scanln(&answer)
		if answer != "approve" {
			return errors.New("not approved")
		}
		if err := ws.Approve(file, time.Now(), rv.Hash); err != nil {
			return err
		}
		fmt.Println(file, "approved")
		return nil
	}
	return nil
}

func importTakeoutContext(ctx context.Context, _ opts, args []string) error {
	if len(args) == 2 && args[0] == "inventory" {
		report, err := takeout.InventoryContext(ctx, args[1])
		if err != nil {
			return err
		}
		return printJSON(report)
	}
	return errors.New("import takeout inventory FOLDER")
}

// importCommandContext provides import shortcuts; every other operation is `lifelog do <name> field=value`.
func importCommandContext(ctx context.Context, o opts, c *client.Client, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		return show(o)(c.GetContext(ctx, "/import"))
	case "check", "apply":
		if len(args) != 2 {
			return fmt.Errorf("import %s FILE", args[0])
		}
		return doActionContext(ctx, o, c, args[0]+"-facts", map[string]string{"file": args[1]})
	case "replay":
		if o.to == "" {
			return errors.New("import replay --to PATH [--dry-run]")
		}
		if !o.dryRun {
			return doActionContext(ctx, o, c, "replay", map[string]string{"to": o.to})
		}
		e, err := doContext(ctx, c, "replay", map[string]string{"to": o.to, "dry_run": "1"})
		if err := show(o)(e, err); err != nil {
			return err
		}
		if p, ok := e.Properties.(map[string]any); ok {
			if fs, _ := p["failures"].([]any); len(fs) > 0 {
				return fmt.Errorf("the rehearsal found %d failures; nothing was written to %s", len(fs), o.to)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown import step %q (lifelog help)", args[0])
}
