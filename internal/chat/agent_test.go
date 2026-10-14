package chat_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/chat"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// fakeModel is a chat-completions endpoint that answers from a script, one answer a request, and keeps what it
// received. An answer is a reply in words, a list of tool calls, or an HTTP status other than 200.
type fakeModel struct {
	t        *testing.T
	mu       sync.Mutex
	script   []answer
	repeat   *answer // answered when the script is used up; nil fails the test
	received []request
}

type answer struct {
	reply  string
	calls  [][2]string // tool name, JSON arguments
	status int
}

type request struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string  `json:"role"`
		Content    *string `json:"content"`
		ToolCallID string  `json:"tool_call_id"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic-key" {
		http.Error(w, "wrong path or key", http.StatusNotFound)
		return
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.received = append(f.received, req)
	var a answer
	switch {
	case len(f.script) > 0:
		a, f.script = f.script[0], f.script[1:]
	case f.repeat != nil:
		a = *f.repeat
	default:
		f.mu.Unlock()
		http.Error(w, "script used up", http.StatusInternalServerError)
		return
	}
	n := len(f.received)
	f.mu.Unlock()
	if a.status != 0 {
		http.Error(w, "synthetic failure", a.status)
		return
	}
	msg := map[string]any{"role": "assistant", "content": a.reply}
	if len(a.calls) > 0 {
		var calls []any
		for i, c := range a.calls {
			calls = append(calls, map[string]any{"id": fmt.Sprintf("call-%d-%d", n, i), "type": "function",
				"function": map[string]any{"name": c[0], "arguments": c[1]}})
		}
		msg = map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}
	}
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}})
}

func (f *fakeModel) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.received)
}

const today = "2031-05-14"

// named is entities with their preferred title, n.title.
const named = "entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key"

var noon = time.Date(2031, 5, 14, 12, 30, 5, 0, time.UTC)

// setup is a fresh life.db, its API with the agent of ask on the fake model, and an owner's client.
func setup(t *testing.T, script ...answer) (*fakeModel, *client.Client, *db.DB, http.Handler) {
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
	f := &fakeModel{t: t, script: script}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	calls := 0 // each ask reads the clock once: a second conversation is one second later, so its title is new
	agent := &chat.Agent{Model: &chat.Model{URL: srv.URL + "/v1", Name: "synthetic", Key: "synthetic-key"},
		Now: func() time.Time { calls++; return noon.Add(time.Duration(calls-1) * time.Second) }}
	h := api.NewAsking(&core.Store{DB: d}, agent)
	agent.Client = client.InProcess(h, chat.Source)
	return f, client.InProcess(h, "cli"), d, h
}

func catalogAction(t *testing.T, c *client.Client, name string) api.Action {
	t.Helper()
	actions, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s", name)
	return api.Action{}
}

func ask(t *testing.T, c *client.Client, values map[string]string) (*api.Entity, error) {
	t.Helper()
	return c.Do(t.Context(), catalogAction(t, c, "ask"), values)
}

func scalar(t *testing.T, d *db.DB, query string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := d.R.QueryRowContext(t.Context(), query, args...).Scan(&s); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return s.String
}

func TestAskWritesThroughTheCatalogAndKeepsTheTurn(t *testing.T) {
	f, owner, d, _ := setup(t,
		answer{calls: [][2]string{{"capture", `{"day":"2031-05-14","text":"Walked by the river.","mood":4}`}}},
		answer{reply: "I wrote it in today's journal."})
	e, err := ask(t, owner, map[string]string{"question": "Note that I walked by the river, mood 4."})
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Conversation 2031-05-14 12.30.05" {
		t.Fatalf("title = %q", e.Title)
	}
	result, _ := e.Result.(map[string]any)
	if result["reply"] != "I wrote it in today's journal." {
		t.Fatalf("result = %v", e.Result)
	}
	steps, _ := result["steps"].([]any)
	if len(steps) != 1 || fmt.Sprint(steps[0]) != "map[href:/days/2031-05-14 tool:capture]" {
		t.Fatalf("steps = %v", steps)
	}
	// The entry is written by the model, with its provenance; the day page is the one the capture made.
	if got := scalar(t, d, `SELECT e.source FROM `+named+` WHERE n.title = ?`, today); got != chat.Source {
		t.Fatalf("day page source = %q, want %s", got, chat.Source)
	}
	if got := scalar(t, d, `SELECT e.body FROM `+named+` WHERE n.title = ?`, today); !strings.Contains(got, "Walked by the river.") {
		t.Fatalf("day page body = %q", got)
	}
	// The turn, as the page keeps it, worked out by hand.
	want := "**Owner** (12.30): Note that I walked by the river, mood 4.\n\n" +
		"- `capture` → [/days/2031-05-14](/days/2031-05-14)\n\n" +
		"**lifelog:** I wrote it in today's journal.\n"
	if got := scalar(t, d, `SELECT e.body FROM `+named+` WHERE n.title = 'Conversation 2031-05-14 12.30.05'`); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
	if got := scalar(t, d, `SELECT c.title || ' ' || l.source FROM links l JOIN entity_names p ON p.entity_id = l.from_id JOIN entity_names c ON c.entity_id = l.to_id
		WHERE l.kind = 'part-of' AND p.title = 'Conversation 2031-05-14 12.30.05'`); got != "Conversations agent:chat" {
		t.Fatalf("part-of link = %q", got)
	}
	// The model was given the catalog without the owner's actions and without ask.
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].Model != "synthetic" {
		t.Fatalf("requests = %d", len(reqs))
	}
	var tools []string
	for _, tool := range reqs[0].Tools {
		tools = append(tools, tool.Function.Name)
	}
	for _, name := range []string{"capture", "save_body", "search", "get_day", "get"} {
		if !slices.Contains(tools, name) {
			t.Fatalf("tool %s missing from %v", name, tools)
		}
	}
	for _, name := range []string{"ask", "register_metric", "relocate_reading", "snapshot"} {
		if slices.Contains(tools, name) {
			t.Fatalf("tool %s was given to the model", name)
		}
	}
	// The second request carries the tool's result, answered to its call.
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != "tool" || last.ToolCallID != "call-1-0" || last.Content == nil || !strings.Contains(*last.Content, "Walked by the river.") {
		t.Fatalf("tool message = %+v", last)
	}
}

func TestAskContinuesAConversation(t *testing.T) {
	f, owner, d, _ := setup(t, answer{reply: "Hello."}, answer{reply: "You said hello."})
	first, err := ask(t, owner, map[string]string{"question": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(first.Properties.(map[string]any)["id"])
	if first.Class[0] != "conversation" || selfHref(first) != "/conversations/"+id {
		t.Fatalf("ask answered with %v %s, want the conversation", first.Class, selfHref(first))
	}
	cont := actionNamed(first, "ask")
	if cont == nil || fmt.Sprint(cont.Fields[1].Value) != id {
		t.Fatalf("a conversation offers no ask that continues it: %+v", first.Actions)
	}
	if _, err := ask(t, owner, map[string]string{"question": "What did I say?", "conversation": id}); err != nil {
		t.Fatal(err)
	}
	want := "**Owner** (12.30): Hello\n\n**lifelog:** Hello.\n\n**Owner** (12.30): What did I say?\n\n**lifelog:** You said hello.\n"
	if got := scalar(t, d, `SELECT body FROM entities WHERE id = ?`, id); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
	// The model reads the conversation so far, then the new question.
	msgs := f.requests()[1].Messages
	if len(msgs) != 3 || !strings.Contains(*msgs[1].Content, "**lifelog:** Hello.") || *msgs[2].Content != "What did I say?" {
		t.Fatalf("messages of the second turn = %+v", msgs)
	}
	if got := scalar(t, d, `SELECT count(*) FROM `+named+` WHERE n.title LIKE 'Conversation %'`); got != "1" {
		t.Fatalf("conversation pages = %s, want 1", got)
	}
}

func TestAskGivesARefusalBackToTheModel(t *testing.T) {
	f, owner, d, _ := setup(t,
		answer{calls: [][2]string{
			{"record", `{"metric":"weight","day":"2031-05-14","value":72.5}`}, // no such metric
			{"register_metric", `{"name":"weight","unit":"kg"}`},              // the owner's: not a tool
			{"capture", `{"day":`}, // malformed
		}},
		answer{reply: "The metric weight is not registered; please register it."})
	e, err := ask(t, owner, map[string]string{"question": "Record my weight, 72.5 kg."})
	if err != nil {
		t.Fatal(err)
	}
	steps := e.Result.(map[string]any)["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("steps = %v", steps)
	}
	for i, want := range []string{"record", "register_metric", "capture"} {
		s := steps[i].(map[string]any)
		if s["tool"] != want || s["error"] == nil || s["href"] != nil {
			t.Fatalf("step %d = %v, want an error of %s", i, s, want)
		}
	}
	if got := scalar(t, d, `SELECT count(*) FROM entity_names WHERE name_key = 'weight'`); got != "0" {
		t.Fatalf("metrics = %s: the owner's action ran", got)
	}
	// Each refusal reached the model as the result of its call.
	var results []string
	for _, m := range f.requests()[1].Messages {
		if m.Role == "tool" {
			results = append(results, *m.Content)
		}
	}
	if len(results) != 3 || !strings.Contains(results[1], "no tool register_metric") {
		t.Fatalf("tool results = %q", results)
	}
}

func TestAskStopsAModelThatNeverAnswers(t *testing.T) {
	f, owner, d, _ := setup(t)
	f.repeat = &answer{calls: [][2]string{{"get_day", `{"day":"2031-05-14"}`}}}
	_, err := ask(t, owner, map[string]string{"question": "Loop"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadGateway || !strings.Contains(err.Error(), "within 16 steps") {
		t.Fatalf("err = %v, want 502 after 16 steps", err)
	}
	if n := len(f.requests()); n != 16 {
		t.Fatalf("model requests = %d, want 16", n)
	}
	// Tools ran, so the turn is kept, with the error.
	body := scalar(t, d, `SELECT e.body FROM `+named+` WHERE n.title LIKE 'Conversation %'`)
	if strings.Count(body, "- `get_day` → [/days/2031-05-14]") != 16 || !strings.Contains(body, "**lifelog** did not answer: the model did not answer within 16 steps") {
		t.Fatalf("transcript = %s", body)
	}
}

func TestAskWritesNothingWhenTheModelFailsFirst(t *testing.T) {
	_, owner, d, _ := setup(t, answer{status: http.StatusInternalServerError})
	_, err := ask(t, owner, map[string]string{"question": "Hello"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadGateway || fmt.Sprint(ce.Entity.Properties.(map[string]any)["code"]) != "model" {
		t.Fatalf("err = %v, want 502 model", err)
	}
	if got := scalar(t, d, `SELECT count(*) FROM entities WHERE source <> 'schema'`); got != "0" {
		t.Fatalf("entities = %s, want none", got)
	}
}

func TestAskStopsWhenTheRequestIsCancelled(t *testing.T) {
	_, owner, d, _ := setup(t)
	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // a server sees the client go away only once the body is read
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(blocked.Close)
	// A second handler on the same file, whose agent waits on a model that never answers.
	agent := &chat.Agent{Model: &chat.Model{URL: blocked.URL + "/v1", Name: "synthetic"}}
	h := api.NewAsking(&core.Store{DB: d}, agent)
	agent.Client = client.InProcess(h, chat.Source)
	c := client.InProcess(h, "cli")
	a := catalogAction(t, owner, "ask")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, a, map[string]string{"question": "Hello"})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := scalar(t, d, `SELECT count(*) FROM entities WHERE source <> 'schema'`); got != "0" {
		t.Fatalf("entities = %s, want none", got)
	}
}

func TestAskRefusesAPageThatIsNotAConversation(t *testing.T) {
	f, owner, _, _ := setup(t)
	page := must(owner.Do(t.Context(), catalogAction(t, owner, "create-page"), map[string]string{"title": "Recipes", "body": "Soup."}))
	id := fmt.Sprint(page.Properties.(map[string]any)["id"])
	for _, conversation := range []string{id, "0", "x", "999"} {
		_, err := ask(t, owner, map[string]string{"question": "Hello", "conversation": conversation})
		var ce *client.Error
		if !errors.As(err, &ce) || (ce.Status != http.StatusUnprocessableEntity && ce.Status != http.StatusNotFound) {
			t.Fatalf("conversation %s: err = %v, want 422 or 404", conversation, err)
		}
	}
	if len(f.requests()) != 0 {
		t.Fatal("the model was asked")
	}
	if actionNamed(page, "ask") != nil {
		t.Fatal("an ordinary page offers ask")
	}
}

func TestAskWithoutAModel(t *testing.T) {
	_, _, d, _ := setup(t)
	c := client.InProcess(api.New(&core.Store{DB: d}), "cli")
	_, err := c.Do(t.Context(), catalogAction(t, c, "ask"), map[string]string{"question": "Hello"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusServiceUnavailable || fmt.Sprint(ce.Entity.Properties.(map[string]any)["code"]) != "no_model" {
		t.Fatalf("err = %v, want 503 no_model", err)
	}
}

func TestAskFromABrowserForm(t *testing.T) {
	_, _, d, h := setup(t, answer{reply: "Hello."})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/conversations", strings.NewReader("question=Hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	id := scalar(t, d, `SELECT e.id FROM `+named+` WHERE n.title LIKE 'Conversation %'`)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/conversations/"+id {
		t.Fatalf("answer %d to %q, want 303 to /conversations/%s and no receipt", rec.Code, rec.Header().Get("Location"), id)
	}
	if got := scalar(t, d, `SELECT source FROM entities WHERE id = ?`, id); got != chat.Source {
		t.Fatalf("conversation source = %q", got)
	}
	// Home draws the form of ask, and the conversation page draws the one that continues it.
	for _, tc := range []struct{ path, want string }{
		{"/", `<form method="POST" action="/conversations" data-action="ask">`},
		{"/conversations/" + id, `<input type="hidden" name="conversation" value="` + id + `">`},
	} {
		get := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
		get.Header.Set("Accept", "text/html")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, get)
		if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, tc.want) || !strings.Contains(body, `name="question"`) {
			t.Fatalf("%s (%d) does not draw the form of ask: want %s", tc.path, rec.Code, tc.want)
		}
	}
}

func TestConversationsListAndRead(t *testing.T) {
	_, owner, d, h := setup(t, answer{reply: "First."}, answer{reply: "Second."})
	first := must(ask(t, owner, map[string]string{"question": "One"}))
	second := must(ask(t, owner, map[string]string{"question": "Two"}))
	id1, id2 := fmt.Sprint(first.Properties.(map[string]any)["id"]), fmt.Sprint(second.Properties.(map[string]any)["id"])
	list := func() string {
		var out []string
		for _, c := range must(owner.Get(t.Context(), "/conversations")).Properties.(map[string]any)["conversations"].([]any) {
			m := c.(map[string]any)
			out = append(out, fmt.Sprint(m["id"])+" "+fmt.Sprint(m["title"]))
		}
		return strings.Join(out, "; ")
	}
	if got, want := list(), id2+" Conversation 2031-05-14 12.30.06; "+id1+" Conversation 2031-05-14 12.30.05"; got != want {
		t.Fatalf("conversations = %s, want %s", got, want)
	}
	turns := must(owner.Get(t.Context(), "/conversations/"+id2)).Properties.(map[string]any)["turns"]
	if got := fmt.Sprint(turns); got != "[map[at:12.30 question:Two reply:Second. steps:[]]]" {
		t.Fatalf("turns = %s", got)
	}
	// A tombstoned conversation leaves the list and is not found; neither is a page that is not a conversation.
	must(owner.Do(t.Context(), catalogAction(t, owner, "tombstone"), map[string]string{"id": id1}))
	if got := list(); got != id2+" Conversation 2031-05-14 12.30.06" {
		t.Fatalf("conversations after a tombstone = %s", got)
	}
	cid := scalar(t, d, `SELECT entity_id FROM entity_names WHERE title = 'Conversations'`)
	for _, id := range []string{id1, cid} {
		var ce *client.Error
		if _, err := owner.Get(t.Context(), "/conversations/"+id); !errors.As(err, &ce) || ce.Status != http.StatusNotFound {
			t.Fatalf("/conversations/%s: err = %v, want 404", id, err)
		}
	}
	// In a browser, a conversation's page is its chat page; ?view=page shows the page itself.
	for _, tc := range []struct {
		path string
		code int
		loc  string
	}{{"/pages/" + id2, http.StatusSeeOther, "/conversations/" + id2}, {"/pages/" + id2 + "?view=page", http.StatusOK, ""}} {
		rec := browse(t, h, tc.path)
		if rec.Code != tc.code || rec.Header().Get("Location") != tc.loc {
			t.Fatalf("%s: %d to %q, want %d to %q", tc.path, rec.Code, rec.Header().Get("Location"), tc.code, tc.loc)
		}
	}
}

func TestTheChatSidebarAndTheChatPage(t *testing.T) {
	_, owner, _, h := setup(t, answer{reply: "Hello **there**."})
	id := fmt.Sprint(must(ask(t, owner, map[string]string{"question": "Hi"})).Properties.(map[string]any)["id"])
	home := browse(t, h, "/").Body.String()
	for _, want := range []string{`<aside class="chat-panel" id="chat-panel"`, `data-chat="toggle"`, `<a href="/conversations"`} {
		if !strings.Contains(home, want) {
			t.Fatalf("Home has no %s", want)
		}
	}
	page := browse(t, h, "/conversations/"+id).Body.String()
	if strings.Contains(page, `id="chat-panel"`) {
		t.Fatal("the chat page draws the sidebar too")
	}
	for _, want := range []string{`data-chat-page="` + id + `"`, `<div class="msg owner"><div class="bubble prose"><p>Hi</p>`,
		`<div class="msg lifelog"><div class="bubble prose"><p>Hello <strong>there</strong>.</p>`, `aria-current="page">Conversation 2031-05-14 12.30.05</a>`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the chat page has no %s", want)
		}
	}
	if blank := browse(t, h, "/conversations").Body.String(); !strings.Contains(blank, "New conversation</h1>") || !strings.Contains(blank, `data-chat-page=""`) {
		t.Fatal("/conversations is not a new conversation")
	}
}

func browse(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func selfHref(e *api.Entity) string {
	for _, l := range e.Links {
		if slices.Contains(l.Rel, "self") {
			return l.Href
		}
	}
	return ""
}

func actionNamed(e *api.Entity, name string) *api.Action {
	for i := range e.Actions {
		if e.Actions[i].Name == name {
			return &e.Actions[i]
		}
	}
	return nil
}

func must(e *api.Entity, err error) *api.Entity {
	if err != nil {
		panic(err)
	}
	return e
}
