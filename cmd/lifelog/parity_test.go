package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/mcp"
)

// TestParityOfTheSurfaces is the parity table of the surfaces (AGENTS.md: client parity is a first-class contract). A
// row is one scenario of the domain. It runs on each surface on a fresh file, and it holds the answer of the surface
// and the state of the file after it, read back through the HTTP API. The surfaces are the client in process (http),
// the client over a server (remote), the command lines of this program (cli), and the MCP server with the client in
// process (mcp) and with the client over a server (mcp-remote). http, remote and cli run once as an owner and once as
// an agent. The MCP server runs as an agent only, and an action of the owner is not one of its tools. Where a surface
// has a shortcut of an action, the row runs the shortcut there: the shortcut wraps the action, so its answer is the
// action's. Each surface keeps the tests of what is its own: the schema, the hints, the arguments and the cancellation
// of MCP; the flags, the output and snapshot --to of the CLI; the transport of the client. The clock of every row is
// fixed at parityToday, so no row depends on the date of the run.
func TestParityOfTheSurfaces(t *testing.T) {
	for _, row := range parityRows() {
		for _, s := range paritySurfaces {
			t.Run(row.name+"/"+s.name+"/"+s.source, func(t *testing.T) {
				t.Parallel()
				read, h, store := apitest.FreshOn(t, "cli", parityToday)
				x := &parityRun{t: t, mcp: strings.HasPrefix(s.name, "mcp"), owner: !strings.HasPrefix(s.source, "agent:"), read: read, store: store}
				x.call = s.open(t, h, store.DB.Path(), s.source)
				row.run(x)
			})
		}
	}
}

// paritySurfaces are the columns of the table: a surface and the source it writes as. The three names of an owner
// (cli, api, ui) are each on one surface; the API decides by owner or agent only.
var paritySurfaces = []struct {
	name, source string
	open         func(t *testing.T, h http.Handler, path, source string) surfaceCall
}{
	{"http", "api", openHTTP}, {"http", "agent:parity", openHTTP},
	{"remote", "ui", openRemote}, {"remote", "agent:parity", openRemote},
	{"cli", "cli", openCLI}, {"cli", "agent:parity", openCLI},
	{"mcp", "agent:parity", openMCP}, {"mcp-remote", "agent:parity", openMCPRemote},
}

// parityToday is today on the clock of every row: the clock of the store of the API, and of the store that a command
// line opens.
const parityToday = "2031-05-20"

// surfaceCall runs the catalog's action with the field values on one surface. It returns the entity of the answer
// (the error entity of a refusal too) and the error of a refusal.
type surfaceCall func(action string, args map[string]string) (*siren.Entity, error)

// errNoTool is the answer of the MCP surface to an action that is not one of its tools.
var errNoTool = errors.New("the MCP server has no tool of this action")

func openHTTP(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return catalogCall(t, client.InProcess(h, source))
}

func openRemote(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return catalogCall(t, apitest.Remote(t, h, source))
}

// catalogCall runs an action of GET /actions with the client.
func catalogCall(t *testing.T, c *client.Client) surfaceCall {
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	return func(action string, args map[string]string) (*siren.Entity, error) {
		t.Helper()
		return c.Do(t.Context(), apitest.Action(t, catalog, action), args)
	}
}

// openCLI runs each action as a command line in process, on the file at path, and reads the JSON that the command
// writes. The source cli is the default of the CLI, so its line names none. The store that the line opens is in the
// zone of this machine, and its clock is at noon of parityToday in that zone: its today is parityToday in any zone.
func openCLI(t *testing.T, _ http.Handler, path, source string) surfaceCall {
	e, stdout, _ := testEnv(t)
	today, err := time.ParseInLocation(time.DateOnly, parityToday, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	e.clock = func() time.Time { return today.Add(12 * time.Hour) }
	return func(action string, args map[string]string) (*siren.Entity, error) {
		t.Helper()
		argv := []string{"--db", path}
		if source != "cli" {
			argv = append(argv, "--source", source)
		}
		stdout.Reset()
		err := runArgs(t.Context(), e, append(argv, cliLine(action, args)...))
		if stdout.Len() == 0 {
			return nil, err
		}
		return decodeEntity(t, stdout.Bytes()), err
	}
}

// cliLine is the command line of an action: the shortcut of the CLI when one takes these fields (page TITLE, rename
// ID TITLE, day [DAY]), else do ACTION field=value, with the fields in the order of their names.
func cliLine(action string, args map[string]string) []string {
	day, onlyDay := args["day"]
	switch {
	case action == "find" && len(args) == 1 && args["title"] != "":
		return []string{"page", args["title"]}
	case action == "rename" && len(args) == 2 && args["id"] != "" && args["title"] != "":
		return []string{"rename", args["id"], args["title"]}
	case action == "open-day" && len(args) == 0:
		return []string{"day"}
	case action == "open-day" && len(args) == 1 && onlyDay:
		return []string{"day", day}
	}
	line := []string{"do", action}
	for _, k := range slices.Sorted(maps.Keys(args)) {
		line = append(line, k+"="+args[k])
	}
	return line
}

func openMCP(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return mcpCall(t, client.InProcess(h, source))
}

func openMCPRemote(t *testing.T, h http.Handler, _, source string) surfaceCall {
	return mcpCall(t, apitest.Remote(t, h, source))
}

// mcpCall serves the tools of the MCP server over an in-memory transport of the SDK, with the client as the source. A
// call sends a field that the schema of the tool calls a number as a JSON number, as a model does. The action open-day
// runs as the tool get_day, the shortcut of the server that wraps it. An error result of a tool is an error; its text
// is the error entity of the API when the API answered.
func mcpCall(t *testing.T, c *client.Client) surfaceCall {
	srv, err := mcp.New(t.Context(), c, version)
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background()) // the life of the server; the cleanup of the test ends it
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "parity", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close the MCP session: %v", err)
		}
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("the MCP server: %v", err)
		}
	})
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	numbers := map[string]map[string]bool{} // a tool, and its fields that are numbers
	for _, tool := range listed.Tools {
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]struct{ Type string }
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatalf("the schema of %s: %v", tool.Name, err)
		}
		numbers[tool.Name] = map[string]bool{}
		for name, p := range schema.Properties {
			numbers[tool.Name][name] = p.Type == "number"
		}
	}
	return func(action string, args map[string]string) (*siren.Entity, error) {
		t.Helper()
		name := strings.ReplaceAll(action, "-", "_")
		if action == "open-day" {
			name = "get_day"
		}
		number, ok := numbers[name]
		if !ok {
			return nil, errNoTool
		}
		arguments := map[string]any{}
		for k, v := range args {
			if number[k] {
				arguments[k] = json.Number(v)
			} else {
				arguments[k] = v
			}
		}
		res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			return nil, err
		}
		if len(res.Content) != 1 {
			t.Fatalf("%s returned %d contents, want 1", name, len(res.Content))
		}
		text, ok := res.Content[0].(*mcpsdk.TextContent)
		if !ok {
			t.Fatalf("%s returned %T, want text", name, res.Content[0])
		}
		if !res.IsError {
			return decodeEntity(t, []byte(text.Text)), nil
		}
		if !json.Valid([]byte(text.Text)) {
			return nil, errors.New(text.Text) // the tool failed before the API answered
		}
		return decodeEntity(t, []byte(text.Text)), fmt.Errorf("the tool %s returned an error", name)
	}
}

// decodeEntity reads the JSON of an entity with its numbers exact, as the client reads them.
func decodeEntity(t *testing.T, b []byte) *siren.Entity {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var e siren.Entity
	if err := d.Decode(&e); err != nil {
		t.Fatalf("not an entity: %v: %s", err, b)
	}
	return &e
}

// parityRun is one row on one surface.
type parityRun struct {
	t     *testing.T
	mcp   bool           // the surface is the MCP server (mcp or mcp-remote)
	owner bool           // the source of the surface is an owner, not agent:*
	call  surfaceCall    // the surface under test
	read  *client.Client // the HTTP API in process, as the owner: the state of the file after a call
	store *core.Store    // the setup of a row, and a probe of the file
}

type parityRow struct {
	name string
	run  func(x *parityRun)
}

// ok is the entity of a call that must succeed.
func (x *parityRun) ok(action string, args map[string]string) *siren.Entity {
	x.t.Helper()
	e, err := x.call(action, args)
	if err != nil {
		x.t.Fatalf("%s %v: %v", action, args, err)
	}
	return e
}

// refused calls an action that must be refused with the status and the code of the error entity, and returns the
// message of the entity. On each surface other than the MCP server, the error has the status too (main writes the
// error of the CLI as "lifelog: 409: ..."). On the MCP server the error is a tool result: only its text has the status.
func (x *parityRun) refused(action string, args map[string]string, status int, code string) string {
	x.t.Helper()
	e, err := x.call(action, args)
	if err == nil || e == nil {
		x.t.Fatalf("%s %v: %v, %v; want a refusal %d %s", action, args, e, err, status, code)
	}
	p := props(x.t, e)
	if fmt.Sprint(p["status"]) != strconv.Itoa(status) || p["code"] != code {
		x.t.Errorf("%s %v: %v %v (%v), want %d %s", action, args, p["status"], p["code"], p["message"], status, code)
	}
	if got := apitest.Status(err); !x.mcp && got != status {
		x.t.Errorf("%s %v: the error %q has the status %d, want %d (-1: it is not a *client.Error)", action, args, err, got, status)
	}
	return fmt.Sprint(p["message"])
}

// page reads the page with the title through the HTTP API.
func (x *parityRun) page(title string) (*siren.Entity, error) {
	return x.read.Get(x.t.Context(), "/pages?title="+url.QueryEscape(title))
}

// pageByID reads the page with the id through the HTTP API, and returns its properties.
func (x *parityRun) pageByID(id int64) map[string]any {
	x.t.Helper()
	return props(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), fmt.Sprintf("/pages/%d", id))))
}

// absent holds that no page has the title, through the HTTP API.
func (x *parityRun) absent(title string) {
	x.t.Helper()
	if e, err := x.page(title); apitest.Status(err) != http.StatusNotFound {
		x.t.Errorf("the page %s: %v %v, want none", title, e, err)
	}
}

// categories are the titles of the categories of a page (its part-of links), through the HTTP API.
func (x *parityRun) categories(id int64) string {
	x.t.Helper()
	var out []string
	links, _ := x.pageByID(id)["links"].([]any)
	for _, l := range links {
		if l := l.(map[string]any); l["kind"] == "part-of" {
			out = append(out, fmt.Sprint(l["title"]))
		}
	}
	return strings.Join(out, ",")
}

// browse is the titles of the rows of a browse through the HTTP API.
func (x *parityRun) browse(query string) string {
	x.t.Helper()
	return rowField(x.t, apitest.Must(x.t)(x.read.Get(x.t.Context(), "/browse?"+query)), "title")
}

// rowField is a field of each row of a browse entity, in order.
func rowField(t *testing.T, e *siren.Entity, name string) string {
	t.Helper()
	var out []string
	rows, _ := props(t, e)["rows"].([]any)
	for _, r := range rows {
		out = append(out, fmt.Sprint(r.(map[string]any)[name]))
	}
	return strings.Join(out, ",")
}

// sql runs a statement on the file: the setup of a state that no action makes.
func (x *parityRun) sql(query string, args ...any) {
	x.t.Helper()
	if _, err := x.store.DB.W.ExecContext(x.t.Context(), query, args...); err != nil {
		x.t.Fatal(err)
	}
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("the entity %q has no properties: %#v", e.Title, e.Properties)
	}
	return p
}

// fieldValue is the value that the action carries for the field, nil for none.
func fieldValue(a siren.Action, name string) any {
	for _, f := range a.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func parityRows() []parityRow {
	rows := []parityRow{
		{"find", rowFind},
		{"open-day", rowOpenDay},
		{"open-day of today", rowOpenToday},
		{"the word today in a day field", rowTodayInADayField},
		{"browse", rowBrowse},
		{"create-place in a category", rowCreatePlace},
		{"file-pages", rowFilePages},
		{"file-pages with a day page", rowFileADayPage},
	}
	for _, kind := range []string{"page", "person", "place", "metric", "file"} {
		rows = append(rows, parityRow{"rename a " + kind, func(x *parityRun) { rowRename(x, kind) }})
	}
	return append(rows,
		parityRow{"stale save-body", rowStaleSave},
		parityRow{"save-body at the largest version", rowLargestVersion},
		parityRow{"register-metric", rowRegisterMetric},
	)
}

// find opens the page with the title; a title that no page has is a 404.
func rowFind(x *parityRun) {
	ana, _, err := x.store.CreatePage(x.t.Context(), "cli", "Ana", "Synthetic page", "")
	if err != nil {
		x.t.Fatal(err)
	}
	e := x.ok("find", map[string]string{"title": "Ana"})
	if p := props(x.t, e); e.Title != "Ana" || apitest.Href(e, "self") != "/pages/"+id(ana) || p["id"] != json.Number(id(ana)) || p["body"] != "Synthetic page" {
		x.t.Errorf("find Ana: %q %v %v", e.Title, apitest.Href(e, "self"), p)
	}
	x.refused("find", map[string]string{"title": "Nobody"}, http.StatusNotFound, "not_found")
}

// open-day is the day view of a day; it is a read, so it makes no day page.
func rowOpenDay(x *parityRun) {
	e := x.ok("open-day", map[string]string{"day": "2031-05-14"})
	if e.Title != "2031-05-14" {
		x.t.Errorf("open-day 2031-05-14: %q", e.Title)
	}
	x.absent("2031-05-14")
}

// open-day with no day, with an empty one or with the word "today" is the day view of today, as GET /days/today is. A
// value that is not a day is refused. It is a read, so it makes no day page.
func rowOpenToday(x *parityRun) {
	for _, args := range []map[string]string{{}, {"day": ""}, {"day": "today"}} {
		if e := x.ok("open-day", args); e.Title != parityToday || apitest.Href(e, "self") != "/days/"+parityToday {
			x.t.Errorf("open-day %v: %q %q, want the day %s", args, e.Title, apitest.Href(e, "self"), parityToday)
		}
	}
	x.refused("open-day", map[string]string{"day": "x"}, http.StatusUnprocessableEntity, "invalid")
	x.absent(parityToday)
}

// The word "today" in a day field is the local day of the server's clock: a reading that record writes and a file
// that add-file keeps with the day today have the day parityToday, in the answer and in the file. A value that is
// neither a day nor the word is the 422 of the schema's CHECK (measurements_day, entities_day), and it writes nothing.
func rowTodayInADayField(x *parityRun) {
	ctx := x.t.Context()
	if _, err := x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		x.t.Fatal(err)
	}
	m := x.ok("record", map[string]string{"metric": "Weight", "day": "today", "value": "71"})
	if p := props(x.t, m); m.Class[0] != "measurement" || p["day"] != parityToday || p["value"] != json.Number("71") {
		x.t.Errorf("record with the day today: %v %v %v, want 71 on %s", m.Class, p["day"], p["value"], parityToday)
	}
	f := x.ok("add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf", "day": "today"})
	if p := props(x.t, f); f.Class[0] != "file" || p["day"] != parityToday {
		x.t.Errorf("add-file with the day today: %v %v, want the day %s", f.Class, p["day"], parityToday)
	}
	for _, tc := range []struct {
		action, check string
		args          map[string]string
	}{
		{"record", "measurements_day", map[string]string{"metric": "Weight", "day": "Today", "value": "72"}},
		{"add-file", "entities_day", map[string]string{"title": "Notes.pdf", "sha256": strings.Repeat("cd", 32), "mime": "application/pdf", "day": "yesterday"}},
	} {
		if msg := x.refused(tc.action, tc.args, http.StatusUnprocessableEntity, "invalid"); !strings.Contains(msg, tc.check) {
			x.t.Errorf("%s %v: %q, want the CHECK %s", tc.action, tc.args, msg, tc.check)
		}
	}
	x.absent("Notes.pdf")
	day := props(x.t, apitest.Must(x.t)(x.read.Get(ctx, "/days/"+parityToday)))
	var readings []string
	rows, _ := day["readings"].([]any)
	for _, r := range rows {
		r, _ := r.(map[string]any)
		readings = append(readings, fmt.Sprint(r["metric"], " ", r["value"]))
	}
	if strings.Join(readings, ",") != "Weight 71" {
		x.t.Errorf("the readings of %s: %v, want Weight 71 alone", parityToday, readings)
	}
	if e, err := x.page("Scan.pdf"); err != nil || props(x.t, e)["day"] != parityToday {
		x.t.Errorf("the page Scan.pdf: %v %v, want the day %s", e, err, parityToday)
	}
}

// browse lists the things of one kind, in a category or in none, a page of rows at a time; a combination with no
// meaning is a 422.
func rowBrowse(x *parityRun) {
	ctx := x.t.Context()
	var people []int64
	for _, title := range []string{"Ana", "Bo", "Cy"} {
		p, err := x.store.CreatePerson(ctx, "cli", title, "", "", "", "")
		if err != nil {
			x.t.Fatal(err)
		}
		people = append(people, p)
	}
	if _, err := x.store.FilePages(ctx, "cli", []int64{people[2], people[0]}, "Family", false); err != nil {
		x.t.Fatal(err)
	}
	for _, title := range []string{"Pia", "Bea", "Cal"} {
		category := "Notes"
		if title == "Pia" {
			category = ""
		}
		if _, _, err := x.store.CreatePage(ctx, "cli", title, "", category); err != nil {
			x.t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args           map[string]string
		titles, counts string // counts: the mentions of each person
	}{
		{map[string]string{}, "Bea,Cal,Family,Notes,Pia", ""},
		{map[string]string{"in": "Notes"}, "Bea,Cal", ""},
		{map[string]string{"unfiled": "1"}, "Pia", ""}, // a category is not in no category
		{map[string]string{"limit": "1", "offset": "1"}, "Cal", ""},
		{map[string]string{"in": "Family", "kind": "person"}, "Ana,Cy", "0,0"},
		{map[string]string{"kind": "person", "unfiled": "1"}, "Bo", "0"},
	} {
		e := x.ok("browse", tc.args)
		if titles, counts := rowField(x.t, e, "title"), rowField(x.t, e, "mentions"); e.Class[0] != "browse" || titles != tc.titles || (tc.counts != "" && counts != tc.counts) {
			x.t.Errorf("browse %v: %v %s (mentions %s), want %s (mentions %s)", tc.args, e.Class, titles, counts, tc.titles, tc.counts)
		}
	}
	x.refused("browse", map[string]string{"kind": "all"}, http.StatusUnprocessableEntity, "invalid")
}

// create-place with a category makes the place and its part-of link in one write, and the category when it is new.
func rowCreatePlace(x *parityRun) {
	e := x.ok("create-place", map[string]string{"title": "Lakeside", "category": "Trips"})
	var parts []string
	for _, l := range e.Entities {
		if slices.Equal(l.Rel, []string{"part-of"}) {
			parts = append(parts, l.Title)
		}
	}
	if e.Title != "Lakeside" || strings.Join(parts, ",") != "Trips" {
		x.t.Errorf("create-place Lakeside in Trips: %q part-of %v", e.Title, parts)
	}
	if got := x.browse("in=Trips&kind=place"); got != "Lakeside" {
		x.t.Errorf("the places in Trips: %s, want Lakeside", got)
	}
}

// file-pages files several pages in a category in one write, and makes the category when it is new.
func rowFilePages(x *parityRun) {
	var ids []int64
	for _, m := range []string{"Ferritin", "LDL"} {
		if _, err := x.store.CreateMetric(x.t.Context(), "cli", core.NewMetric{Name: m, Unit: "mg/dL"}); err != nil {
			x.t.Fatal(err)
		}
		p, err := x.store.PageID(x.t.Context(), m)
		if err != nil || p == 0 {
			x.t.Fatalf("%s: %d %v", m, p, err)
		}
		ids = append(ids, p)
	}
	e := x.ok("file-pages", map[string]string{"ids": id(ids[0]) + "," + id(ids[1]), "category": "Bloodwork"})
	if p := props(x.t, e); e.Class[0] != "filed" || p["added"] != json.Number("2") || p["created"] != true {
		x.t.Errorf("file-pages: %v %v, want 2 added and Bloodwork made", e.Class, p)
	}
	for _, p := range ids {
		if got := x.categories(p); got != "Bloodwork" {
			x.t.Errorf("page %d is in %q, want Bloodwork", p, got)
		}
	}
}

// A day page is never filed (D26): file-pages with a day page among its ids is the 422 of the trigger, and nothing is
// written: not the link of the other page, not the category, and no version changes.
func rowFileADayPage(x *parityRun) {
	plain, _, err := x.store.CreatePage(x.t.Context(), "cli", "Plans", "", "")
	if err != nil {
		x.t.Fatal(err)
	}
	day, _, err := x.store.Capture(x.t.Context(), "cli", "2031-05-02", "a synthetic entry", nil)
	if err != nil {
		x.t.Fatal(err)
	}
	before := []any{x.pageByID(plain)["version"], x.pageByID(day)["version"]}
	msg := x.refused("file-pages", map[string]string{"ids": id(plain) + "," + id(day), "category": "Garden"}, http.StatusUnprocessableEntity, "invalid")
	if !strings.Contains(msg, "a day page is never filed in a category (D26)") {
		x.t.Errorf("file-pages with a day page: %q, want the sentence of the trigger", msg)
	}
	x.absent("Garden")
	for i, p := range []int64{plain, day} {
		if v, c := x.pageByID(p)["version"], x.categories(p); v != before[i] || c != "" {
			x.t.Errorf("page %d after a refused filing: version %v (was %v), in %q", p, v, before[i], c)
		}
	}
}

// rename gives a page of each type a new title and keeps the old one as an alias: find by the old title and by the
// new one finds the same page, and its other fields and the links to it stay. A title that another page holds, a date, or a title that
// is not a valid name is refused, and nothing is written, not even a name.
func rowRename(x *parityRun, kind string) {
	ctx := x.t.Context()
	old := "Old " + kind
	var p int64
	var err error
	switch kind {
	case "page":
		p, _, err = x.store.CreatePage(ctx, "cli", old, "original prose", "")
	case "person":
		p, err = x.store.CreatePerson(ctx, "cli", old, "Synthetic Person", "", "2031-01-01", "")
	case "place":
		p, err = x.store.CreatePlace(ctx, "cli", old, "")
	case "metric":
		if _, err = x.store.CreateMetric(ctx, "cli", core.NewMetric{Name: old, Unit: "kg", Note: "original metric"}); err == nil {
			p, err = x.store.PageID(ctx, old)
		}
	case "file":
		var kept core.Kept
		kept, err = x.store.AddFile(ctx, "cli", core.FileIn{Title: old, SHA256: strings.Repeat("a", 64), MIME: "text/plain", Body: "original file", Day: "2031-01-01"})
		p = kept.ID
	}
	if err != nil {
		x.t.Fatal(err)
	}
	ref, _, err := x.store.CreatePage(ctx, "cli", "Reference", "See [["+old+"]].", "")
	if err != nil {
		x.t.Fatal(err)
	}
	same := func(m map[string]any) map[string]any {
		return map[string]any{"id": m["id"], "type": m["entity_type"], "body": m["body"], "day": m["day"], "person": m["person"], "file": m["file"]}
	}
	initial := same(x.pageByID(p))
	for _, title := range []string{"New " + kind, "NEW " + strings.ToUpper(kind), old, "Final " + kind} {
		if e := x.ok("rename", map[string]string{"id": id(p), "title": title}); e.Title != title {
			x.t.Fatalf("rename to %q: %q", title, e.Title)
		}
		for _, name := range []string{old, title} {
			found := x.ok("find", map[string]string{"title": name})
			apitest.Action(x.t, found, "rename")
			if found.Title != title || props(x.t, found)["id"] != json.Number(id(p)) {
				x.t.Fatalf("find %q after the rename to %q: %q %v", name, title, found.Title, props(x.t, found)["id"])
			}
		}
		if now := same(x.pageByID(p)); !reflect.DeepEqual(now, initial) {
			x.t.Fatalf("the rename to %q changed the page: %v, was %v", title, now, initial)
		}
		r := x.pageByID(ref)
		links, _ := r["links"].([]any)
		if r["body"] != "See [["+old+"]]." || len(links) != 1 || links[0].(map[string]any)["id"] != json.Number(id(p)) || links[0].(map[string]any)["title"] != title {
			x.t.Fatalf("the page that links %q after the rename to %q: %v %v", old, title, r["body"], links)
		}
	}
	if _, _, err := x.store.CreatePage(ctx, "cli", "Ghost ref", "[[Reserved ghost]]", ""); err != nil {
		x.t.Fatal(err)
	}
	tomb, _, err := x.store.CreatePage(ctx, "cli", "Reserved tombstone", "", "")
	if err != nil {
		x.t.Fatal(err)
	}
	if err := x.store.Tombstone(ctx, "cli", tomb); err != nil {
		x.t.Fatal(err)
	}
	before := x.pageByID(p)
	names := func() (n int) {
		if err := x.store.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM entity_names").Scan(&n); err != nil {
			x.t.Fatal(err)
		}
		return n
	}
	count := names()
	for _, tc := range []struct {
		title  string
		status int
		code   string
		says   string // a part of the message; "" for any
	}{
		{"Reserved ghost", http.StatusConflict, "exists", ""},
		{"Reserved tombstone", http.StatusConflict, "exists", ""},
		{"2031-02-01", http.StatusUnprocessableEntity, "invalid", "a journal day has only its canonical date name"}, // entity_names_day_insert
		{"Lab [unsafe]", http.StatusUnprocessableEntity, "invalid", ""},
	} {
		if msg := x.refused("rename", map[string]string{"id": id(p), "title": tc.title}, tc.status, tc.code); !strings.Contains(msg, tc.says) {
			x.t.Errorf("the refused rename to %q says %q, want %q in it", tc.title, msg, tc.says)
		}
		if after := x.pageByID(p); !reflect.DeepEqual(after, before) {
			x.t.Fatalf("the refused rename to %q changed the page: %v, was %v", tc.title, after, before)
		}
	}
	if n := names(); n != count {
		x.t.Errorf("the refused renames left %d names, were %d", n, count)
	}
}

// A save with an old version is a 409 stale_version and writes nothing: the version of the last read, and a clock
// token equal to updated_at (the token of an old program). The version is a string exact above 2^53.
func rowStaleSave(x *parityRun) {
	p, _, err := x.store.CreatePage(x.t.Context(), "cli", "Version parity", "first", "")
	if err != nil {
		x.t.Fatal(err)
	}
	x.sql(`UPDATE entities SET revision = 9007199254740993 WHERE id = ?`, p)
	var clock string
	if err := x.store.DB.R.QueryRowContext(x.t.Context(), `SELECT updated_at FROM entities WHERE id = ?`, p).Scan(&clock); err != nil {
		x.t.Fatal(err)
	}
	read := x.ok("find", map[string]string{"title": "Version parity"})
	if v, filled := props(x.t, read)["version"], fieldValue(apitest.Action(x.t, read, "save-body"), "version"); v != "9007199254740993" || filled != "9007199254740993" {
		x.t.Fatalf("the version %#v and the save-body version %#v, want 9007199254740993 as a string", v, filled)
	}
	saved := x.ok("save-body", map[string]string{"id": id(p), "version": "9007199254740993", "body": "committed [[Kept]]"})
	if v := props(x.t, saved)["version"]; v != "9007199254740994" {
		x.t.Fatalf("the version after the save: %#v, want 9007199254740994", v)
	}
	x.sql(`UPDATE entities SET updated_at = ? WHERE id = ?`, clock, p)
	for _, stale := range []string{"9007199254740993", clock, "2026-01-01T00:00:00.000Z"} {
		x.refused("save-body", map[string]string{"id": id(p), "version": stale, "body": "stale [[Forbidden]]"}, http.StatusConflict, "stale_version")
	}
	if now := x.pageByID(p); now["body"] != "committed [[Kept]]" || now["version"] != "9007199254740994" {
		x.t.Errorf("after the stale saves: %v %v, want the committed body at 9007199254740994", now["body"], now["version"])
	}
	x.absent("Forbidden")
}

// At the largest version, 2^63-1, the version is exact on the surface and in the action's field. A save of the same
// body writes nothing and is accepted; a save of a new body is refused, as the version cannot grow, and nothing is
// written.
func rowLargestVersion(x *parityRun) {
	const largest = "9223372036854775807"
	p, _, err := x.store.CreatePage(x.t.Context(), "cli", "Version max", "committed [[Kept]]", "")
	if err != nil {
		x.t.Fatal(err)
	}
	x.sql(`UPDATE entities SET revision = `+largest+` WHERE id = ?`, p)
	read := x.ok("find", map[string]string{"title": "Version max"})
	if v, filled := props(x.t, read)["version"], fieldValue(apitest.Action(x.t, read, "save-body"), "version"); v != largest || filled != largest {
		x.t.Fatalf("the version %#v and the save-body version %#v, want %s as a string", v, filled, largest)
	}
	x.ok("save-body", map[string]string{"id": id(p), "version": largest, "body": "committed [[Kept]]"})
	x.refused("save-body", map[string]string{"id": id(p), "version": largest, "body": "overflow [[Forbidden]]"}, http.StatusUnprocessableEntity, "invalid")
	if now := x.pageByID(p); now["body"] != "committed [[Kept]]" || now["version"] != largest {
		x.t.Errorf("after the refused save: %v %v, want the committed body at %s", now["body"], now["version"], largest)
	}
	x.absent("Forbidden")
}

// register-metric is the owner's: an owner registers a metric, an agent gets a 403 and nothing is written, and the
// MCP server has no tool for it.
func rowRegisterMetric(x *parityRun) {
	args := map[string]string{"name": "Weight", "unit": "kg"}
	switch {
	case x.mcp:
		if _, err := x.call("register-metric", args); !errors.Is(err, errNoTool) {
			x.t.Errorf("register-metric on MCP: %v, want no tool", err)
		}
	case x.owner:
		if e := x.ok("register-metric", args); e.Title != "Weight" {
			x.t.Errorf("register-metric Weight: %q", e.Title)
		}
		if e, err := x.page("Weight"); err != nil || props(x.t, e)["entity_type"] != "metric" {
			x.t.Errorf("the page Weight: %v %v, want a metric", e, err)
		}
		return
	default:
		x.refused("register-metric", args, http.StatusForbidden, "forbidden")
	}
	x.absent("Weight")
}
