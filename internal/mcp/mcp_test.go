package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
)

// TestTheToolsAreTheActionsThatAreNotTheOwners: each action of the catalog is a tool exactly when it is not the
// owner's, with get and get_day, each once; the browser's preview is not a tool; browse, get_day and get are marked
// reads. The tool open_day opens a day, as get_day does in the parity table: get_day is the action open-day under a
// second name. The built-in tool get reads what no action reads: a month and a day with its trends.
func TestTheToolsAreTheActionsThatAreNotTheOwners(t *testing.T) {
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
		if (tool.Name == "browse" || tool.Name == "get_day" || tool.Name == "get") && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s is not marked a read: %+v", tool.Name, tool.Annotations)
		}
	}
	for _, name := range []string{"find", "get", "get_day", "open_day"} {
		if !seen[name] {
			t.Fatalf("MCP tools lack %q; have %v", name, seen)
		}
	}
	if seen["render"] {
		t.Fatal("the browser's preview was exposed as an MCP tool")
	}
	actions, err := c.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := 0
	for _, a := range actions {
		if seen[toolName(a.Name)] == a.Owner {
			t.Errorf("action %s (owner %v): the MCP tool %s is listed %v", a.Name, a.Owner, toolName(a.Name), seen[toolName(a.Name)])
		}
		if a.Owner {
			owner++
		}
	}
	if owner == 0 {
		t.Fatal("the catalog has no owner-only action: the check has nothing to check")
	}

	if day := callTool(t, ctx, session, "open_day", map[string]any{"day": "2031-05-14"}); day.Title != "2031-05-14" {
		t.Fatalf("open_day 2031-05-14 returned %+v", day)
	}
	// The browser's calendar is on this surface too (docs/plans/091-the-browser-for-daily-use.md).
	if month := callTool(t, ctx, session, "get", map[string]any{"href": "/months/2031-05"}); month.Title != "May 2031" {
		t.Fatalf("get /months/2031-05 returned %+v", month)
	}
	// A day's trends are on this surface too (docs/plans/093-words-phone-and-today.md).
	if day, ok := callTool(t, ctx, session, "get", map[string]any{"href": "/days/2031-05-14"}).Properties.(map[string]any); !ok || day["trends"] == nil {
		t.Fatalf("get /days/2031-05-14 lacks trends: %v", day)
	}
}

func mcpTestClient(t *testing.T) *client.Client {
	t.Helper()
	owner, handler, _ := apitest.Fresh(t, "cli")
	for _, fields := range []map[string]string{{"name": "walk"}, {"name": "weight", "unit": "kg"}} {
		apitest.Must(t)(owner.Do(t.Context(), apitest.Action(t, apitest.Must(t)(owner.Get(t.Context(), "/")), "register-metric"), fields))
	}
	c := client.InProcess(handler, "agent:test")
	if _, err := c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Ana", "body": "Synthetic page"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func callTool(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession, name string, args map[string]any) *siren.Entity {
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
	var e siren.Entity
	if err := json.Unmarshal([]byte(text.Text), &e); err != nil {
		t.Fatalf("%s returned non-entity JSON %q: %v", name, text.Text, err)
	}
	return &e
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
			json.NewEncoder(w).Encode(siren.Entity{Actions: actions})
			return
		}
		paths = append(paths, r.URL.Path)
		json.NewEncoder(w).Encode(siren.Entity{})
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
				c = apitest.Remote(t, handler, "agent:test")
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
			var entity siren.Entity
			if err := decoder.Decode(&entity); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Do(t.Context(), entity.Actions[0], nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
