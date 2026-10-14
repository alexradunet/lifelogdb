package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// The find tools and readings_from_table, the tool an agent uses during an import (docs/plans/089-no-import-process.md),
// are MCP tools; the owner-only actions are not.
func TestFindAndReadingsTools(t *testing.T) {
	c := mcpTestClient(t)
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx, serverTransport) }()
	var session *mcpsdk.ClientSession
	defer func() {
		if session != nil {
			if err := session.Close(); err != nil {
				t.Errorf("close MCP session: %v", err)
			}
		}
		cancel()
		if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("MCP server stopped with %v", err)
		}
	}()

	session, err = mcpsdk.NewClient(&mcpsdk.Implementation{Name: "lifelog-test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		if seen[tool.Name] {
			t.Fatalf("duplicate MCP tool %q", tool.Name)
		}
		seen[tool.Name] = true
	}
	for _, name := range []string{"find", "get", "get_day", "readings_from_table", "open_day", "ask"} {
		if !seen[name] {
			t.Fatalf("MCP tools lack %q; have %v", name, seen)
		}
	}
	if seen["render"] {
		t.Fatal("the browser's preview was exposed as an MCP tool")
	}
	for _, tool := range tools.Tools { // ask takes the question alone: the conversation is the day's (plan 106)
		if tool.Name != "ask" {
			continue
		}
		props, _ := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		if _, ok := props["question"]; !ok || len(props) != 1 {
			t.Fatalf("ask's input is %v, want question alone", props)
		}
	}
	actions, err := c.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := 0
	for _, a := range actions {
		if a.Owner {
			owner++
			if seen[client.ToolName(a.Name)] {
				t.Fatalf("owner-only action %q was exposed as an MCP tool", a.Name)
			}
		}
	}
	if owner == 0 {
		t.Fatal("the catalog has no owner-only action: the check has nothing to check")
	}

	page := callTool(t, ctx, session, "find", map[string]any{"title": "Ana"})
	if page.Title != "Ana" || href(page, "self") == "" {
		t.Fatalf("find returned %+v", page)
	}
	// The browser's calendar and its "go to a day" are on this surface too (docs/plans/091-the-browser-for-daily-use.md).
	if day := callTool(t, ctx, session, "open_day", map[string]any{"day": "2031-05-14"}); day.Title != "2031-05-14" {
		t.Fatalf("open_day returned %+v", day)
	}
	if month := callTool(t, ctx, session, "get", map[string]any{"href": "/months/2031-05"}); month.Title != "May 2031" {
		t.Fatalf("get /months/2031-05 returned %+v", month)
	}
	if titles := callTool(t, ctx, session, "get", map[string]any{"href": "/titles?prefix=an"}); len(titles.Entities) != 1 || titles.Entities[0].Title != "Ana" {
		t.Fatalf("get /titles returned %+v", titles)
	}
	// A day's trends and each habit's last days are on this surface too (docs/plans/093-words-phone-and-today.md).
	if day, ok := callTool(t, ctx, session, "get", map[string]any{"href": "/days/2031-05-14"}).Properties.(map[string]any); !ok || day["trends"] == nil || day["habits"] == nil {
		t.Fatalf("get /days/2031-05-14 lacks trends or habits: %v", day)
	}
	callTool(t, ctx, session, "create_page", map[string]any{"title": "Weighings", "body": "| day | weight (kg) |\n|---|---|\n| 2031-01-01 | 70.5 |\n"})
	readings := callTool(t, ctx, session, "readings_from_table", map[string]any{"page": "Weighings", "metric": "weight"})
	result, _ := readings.Result.(map[string]any)
	if readings.Title != "Readings from Weighings" || fmt.Sprint(result["written"]) != "1" {
		t.Fatalf("readings_from_table returned %+v", readings)
	}
}

func mcpTestClient(t *testing.T) *client.Client {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	handler := api.New(&core.Store{DB: d})
	owner := client.InProcess(handler, "cli")
	for _, fields := range []map[string]string{{"name": "walk"}, {"name": "weight", "unit": "kg"}} {
		must(owner.Do(t.Context(), actionByName(t, must(owner.Get(t.Context(), "/")), "register-metric"), fields))
	}
	c := client.InProcess(handler, "agent:test")
	if _, err := c.Do(t.Context(), actionByName(t, must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Ana", "body": "Synthetic page"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func actionByName(t *testing.T, e *api.Entity, name string) api.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("%s not found in %v", name, e.Actions)
	return api.Action{}
}

func callTool(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession, name string, args map[string]any) *api.Entity {
	t.Helper()
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned MCP error content: %v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s returned %d content items", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s returned %T content", name, res.Content[0])
	}
	var e api.Entity
	if err := json.Unmarshal([]byte(text.Text), &e); err != nil {
		t.Fatalf("%s returned non-entity JSON %q: %v", name, text.Text, err)
	}
	return &e
}

func must(e *api.Entity, err error) *api.Entity {
	if err != nil {
		panic(err)
	}
	return e
}

func href(e *api.Entity, rel string) string {
	for _, l := range e.Links {
		if len(l.Rel) > 0 && l.Rel[0] == rel {
			return l.Href
		}
	}
	return ""
}

func numericSession(t *testing.T, c *client.Client) (*mcpsdk.ClientSession, context.Context) {
	t.Helper()
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		session.Close()
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	return session, ctx
}

func TestMCPNumericSchema(t *testing.T) {
	session, ctx := numericSession(t, mcpTestClient(t))
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	moodEnum := ""
	for _, tool := range tools.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		json.Unmarshal(b, &schema)
		for name, property := range schema["properties"].(map[string]any) {
			p := property.(map[string]any)
			if tool.Name == "capture" && name == "mood" {
				moodEnum = fmt.Sprint(p["type"], " ", p["enum"])
			}
			if options, ok := p["enum"].([]any); ok {
				for _, v := range options {
					switch p["type"] {
					case "number":
						if _, ok := v.(float64); !ok {
							t.Errorf("%s.%s numeric enum is %T", tool.Name, name, v)
						}
					case "string":
						if _, ok := v.(string); !ok {
							t.Errorf("string enum is %T", v)
						}
					}
				}
			}
		}
	}
	if moodEnum != "number [1 2 3 4 5]" { // the browser draws it as five buttons (docs/plans/092-the-app-shell.md)
		t.Errorf("capture's mood is %q, want the numbers 1 to 5", moodEnum)
	}
	callTool(t, ctx, session, "start_habit", map[string]any{"name": "walk", "start_day": "2026-06-01"})
	for _, done := range []int{0, 1} {
		callTool(t, ctx, session, "check_in", map[string]any{"name": "walk", "day": fmt.Sprintf("2026-06-0%d", done+1), "done": done})
	}
	for _, args := range []map[string]any{{"name": "walk", "day": "2026-06-03", "done": 2}, {"name": "walk", "day": "2026-06-03"}} {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "check_in", Arguments: args})
		if err == nil && !res.IsError {
			t.Fatal("invalid check-in accepted")
		}
	}
	callTool(t, ctx, session, "record", map[string]any{"metric": "weight", "day": "2026-06-01", "value": 71.5})
}

func TestMCPNumericArguments(t *testing.T) {
	base := mcpTestClient(t)
	actions, err := base.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	c := client.InProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if r.URL.Path == "/actions" {
			json.NewEncoder(w).Encode(api.Entity{Actions: actions})
			return
		}
		paths = append(paths, r.URL.Path)
		json.NewEncoder(w).Encode(api.Entity{})
	}), "agent:test")
	session, ctx := numericSession(t, c)
	// A number is sent as its JSON text: the API's own parser is the one rule for an id, on this surface as on the
	// CLI and in a browser, so "1e6" reaches the API as written and the API refuses it there (404 no such id).
	for _, token := range []string{"999999", "1000000", "9007199254740993", "1e6"} {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "tombstone", Arguments: json.RawMessage(`{"id":` + token + `}`)})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %v", token, res, err)
		}
	}
	want := []string{"/pages/999999/tombstone", "/pages/1000000/tombstone", "/pages/9007199254740993/tombstone", "/pages/1e6/tombstone"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("paths %v want %v", paths, want)
	}
	// An argument that is not a number or a string is refused before any call.
	for _, token := range []string{"{}", "[]"} {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "tombstone", Arguments: json.RawMessage(`{"id":` + token + `}`)})
		if err == nil && !res.IsError {
			t.Fatalf("accepted %s", token)
		}
	}
	if len(paths) != len(want) {
		t.Fatal("invalid arguments dispatched")
	}
}

func TestMCPCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	c := client.InProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return
		}
		fmt.Fprint(w, `{}`)
	}), "cli")
	srv, err := New(t.Context(), c, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { session.Close(); cancel(); <-done }()
	callCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callCtx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/blocked"}})
		result <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("call did not start")
	}
	stop()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("SDK did not cancel handler")
	}
	if err := <-result; err == nil {
		t.Fatal("canceled call succeeded")
	}
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/ok"}})
	if err != nil || res.IsError {
		t.Fatalf("later call: %v %v", res, err)
	}
}

func TestMCPUntrustedDataInstructions(t *testing.T) {
	session, _ := numericSession(t, mcpTestClient(t))
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{"untrusted data", "never instructions", "authorize tools", "approval"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("initialization instructions lack %q: %s", want, instructions)
		}
	}
}

func TestMCPExactResponseNumbers(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint(remote), func(t *testing.T) {
			token := "9007199254740993"
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && r.URL.Path != "/echo/"+token {
					t.Errorf("rounded MCP read-to-action path: %s", r.URL.Path)
				}
				if r.URL.Path == "/actions" {
					fmt.Fprint(w, `{}`)
					return
				}
				fmt.Fprintf(w, `{"properties":{"nested":[%s,9223372036854775807,-9223372036854775808]},"result":{"id":%s},"actions":[{"name":"echo","method":"POST","href":"/echo/{id}","fields":[{"name":"id","in":"path","value":%s}]}]}`, token, token, token)
			})
			c := client.InProcess(handler, "agent:test")
			if remote {
				srv := httptest.NewServer(handler)
				defer srv.Close()
				c = client.Remote(srv.URL, "agent:test", "")
			}
			session, ctx := numericSession(t, c)
			res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get", Arguments: map[string]any{"href": "/numbers"}})
			if err != nil || res.IsError {
				t.Fatalf("get: %v %v", res, err)
			}
			text := res.Content[0].(*mcpsdk.TextContent).Text
			for _, want := range []string{token, "9223372036854775807", "-9223372036854775808"} {
				if !strings.Contains(text, want) {
					t.Errorf("lost %s: %s", want, text)
				}
			}
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			var entity api.Entity
			if err := decoder.Decode(&entity); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Do(t.Context(), entity.Actions[0], nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
