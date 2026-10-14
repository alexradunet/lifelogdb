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
// received. An answer is a reply in words, a list of tool calls, or an HTTP status other than 200. A request with no
// tools asks for a summary (the compaction of an earlier day); one with tools asks for the turn's answer.
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
	during func() // run while the request is answered: what the owner does meanwhile
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
	if a.during != nil {
		a.during()
	}
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

// summaries are the requests that ask for a summary: the ones with no tools.
func (f *fakeModel) summaries() []request {
	var out []request
	for _, r := range f.requests() {
		if len(r.Tools) == 0 {
			out = append(out, r)
		}
	}
	return out
}

const today = "2031-05-14"

// named is entities with their preferred title, n.title.
const named = "entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key"

// clock is the server's local day, which a test moves: the conversation is the day's (plan 106). The agent's time
// is 12.30 of that day.
type clock struct {
	mu  sync.Mutex
	day string
}

func (c *clock) today() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.day
}

func (c *clock) set(day string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.day = day
}

func (c *clock) now() time.Time {
	t, err := time.Parse(time.DateOnly, c.today())
	if err != nil {
		panic(err)
	}
	return t.Add(12*time.Hour + 30*time.Minute + 5*time.Second)
}

// setup is a fresh life.db, its API with the agent of ask on the fake model and the day fixed to today, and an
// owner's client.
func setup(t *testing.T, script ...answer) (*fakeModel, *client.Client, *db.DB, http.Handler, *clock) {
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
	c := &clock{day: today}
	agent := &chat.Agent{Model: &chat.Model{URL: srv.URL + "/v1", Name: "synthetic", Key: "synthetic-key"}, Now: c.now}
	h := api.NewOn(&core.Store{DB: d}, c.today, time.UTC, agent, nil)
	agent.Client = client.InProcess(h, chat.Source)
	return f, client.InProcess(h, "cli"), d, h, c
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

func ask(t *testing.T, c *client.Client, question string) (*api.Entity, error) {
	t.Helper()
	return c.Do(t.Context(), catalogAction(t, c, "ask"), map[string]string{"question": question})
}

func scalar(t *testing.T, d *db.DB, query string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := d.R.QueryRowContext(t.Context(), query, args...).Scan(&s); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return s.String
}

// bodyOf is the body of the page with that title; "" when there is none.
func bodyOf(t *testing.T, d *db.DB, title string) string {
	t.Helper()
	return scalar(t, d, `SELECT e.body FROM `+named+` WHERE n.title = ?`, title)
}

// seedConversation is a conversation page the owner made and filed in Conversations, as an earlier day's would be.
func seedConversation(t *testing.T, owner *client.Client, title, body string) int64 {
	t.Helper()
	if _, err := owner.Do(t.Context(), catalogAction(t, owner, "find"), map[string]string{"title": api.ConversationsTitle}); err != nil {
		must(owner.Do(t.Context(), catalogAction(t, owner, "create-page"), map[string]string{"title": api.ConversationsTitle, "body": "The conversations."}))
	}
	e := must(owner.Do(t.Context(), catalogAction(t, owner, "create-page"), map[string]string{"title": title, "body": body}))
	id := idOf(e)
	must(owner.Do(t.Context(), catalogAction(t, owner, "link"), map[string]string{"id": fmt.Sprint(id), "to": api.ConversationsTitle, "kind": "part-of"}))
	return id
}

func idOf(e *api.Entity) int64 {
	n, _ := e.Properties.(map[string]any)["id"].(json.Number)
	id, _ := n.Int64()
	return id
}

// turn is a transcript of one turn, as the page keeps it.
func turn(at, question, reply string) string {
	return "**Owner** (" + at + "): " + question + "\n\n**lifelog:** " + reply + "\n"
}

func TestAskWritesThroughTheCatalogAndKeepsTheTurn(t *testing.T) {
	f, owner, d, _, _ := setup(t,
		answer{calls: [][2]string{{"capture", `{"day":"2031-05-14","text":"Walked by the river.","mood":4}`}}},
		answer{reply: "I wrote it in today's journal."})
	e, err := ask(t, owner, "Note that I walked by the river, mood 4.")
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Conversation 2031-05-14" || selfHref(e) != "/conversations" {
		t.Fatalf("ask answered with %q at %s, want today's conversation", e.Title, selfHref(e))
	}
	result, _ := e.Result.(map[string]any)
	if result["reply"] != "I wrote it in today's journal." {
		t.Fatalf("result = %v", e.Result)
	}
	steps, _ := result["steps"].([]any)
	if len(steps) != 1 || fmt.Sprint(steps[0]) != "map[href:/days/2031-05-14 tool:capture]" {
		t.Fatalf("steps = %v", steps)
	}
	if a := actionNamed(e, "ask"); a == nil || len(a.Fields) != 1 || a.Fields[0].Name != "question" {
		t.Fatalf("ask takes %+v, want the question alone", e.Actions)
	}
	// The entry is written by the model, with its provenance; the day page is the one the capture made.
	if got := scalar(t, d, `SELECT e.source FROM `+named+` WHERE n.title = ?`, today); got != chat.Source {
		t.Fatalf("day page source = %q, want %s", got, chat.Source)
	}
	if got := bodyOf(t, d, today); !strings.Contains(got, "Walked by the river.") {
		t.Fatalf("day page body = %q", got)
	}
	// The turn, as the page keeps it, worked out by hand.
	want := "**Owner** (12.30): Note that I walked by the river, mood 4.\n\n" +
		"- `capture` → [/days/2031-05-14](/days/2031-05-14)\n\n" +
		"**lifelog:** I wrote it in today's journal.\n"
	if got := bodyOf(t, d, "Conversation 2031-05-14"); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
	if got := scalar(t, d, `SELECT c.title || ' ' || l.source FROM links l JOIN entity_names p ON p.entity_id = l.from_id JOIN entity_names c ON c.entity_id = l.to_id
		WHERE l.kind = 'part-of' AND p.title = 'Conversation 2031-05-14'`); got != "Conversations agent:chat" {
		t.Fatalf("part-of link = %q", got)
	}
	// The model was given the catalog without the owner's actions and without ask; no memory and no earlier turns.
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].Model != "synthetic" {
		t.Fatalf("requests = %d", len(reqs))
	}
	if msgs := reqs[0].Messages; len(msgs) != 2 || msgs[0].Role != "system" || !strings.Contains(*msgs[0].Content, "Today is 2031-05-14 (Wednesday)") ||
		*msgs[1].Content != "Note that I walked by the river, mood 4." {
		t.Fatalf("messages of the first turn = %+v", msgs)
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

func TestAskContinuesTheDaysConversation(t *testing.T) {
	f, owner, d, _, _ := setup(t, answer{reply: "Hello."}, answer{reply: "You said hello."})
	first := must(ask(t, owner, "Hello"))
	if _, err := ask(t, owner, "What did I say?"); err != nil {
		t.Fatal(err)
	}
	want := turn("12.30", "Hello", "Hello.") + "\n" + turn("12.30", "What did I say?", "You said hello.")
	if got := bodyOf(t, d, "Conversation 2031-05-14"); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
	// The model reads the conversation so far, then the new question.
	msgs := f.requests()[1].Messages
	if len(msgs) != 3 || !strings.HasPrefix(*msgs[1].Content, "The conversation so far today, from the page Conversation 2031-05-14:") ||
		!strings.Contains(*msgs[1].Content, "**lifelog:** Hello.") || *msgs[2].Content != "What did I say?" {
		t.Fatalf("messages of the second turn = %+v", msgs)
	}
	if got := scalar(t, d, `SELECT count(*) FROM `+named+` WHERE n.title LIKE 'Conversation %'`); got != "1" {
		t.Fatalf("conversation pages = %s, want 1", got)
	}
	e := must(owner.Get(t.Context(), "/conversations"))
	p := e.Properties.(map[string]any)
	if turns, _ := p["turns"].([]any); len(turns) != 2 || fmt.Sprint(p["id"]) != fmt.Sprint(idOf(first)) || p["day"] != today || p["compacted"] != false || p["previous"] != nil {
		t.Fatalf("today's conversation = %v", p)
	}
}

func TestAskGivesARefusalBackToTheModel(t *testing.T) {
	f, owner, d, _, _ := setup(t,
		answer{calls: [][2]string{
			{"record", `{"metric":"weight","day":"2031-05-14","value":72.5}`}, // no such metric
			{"register_metric", `{"name":"weight","unit":"kg"}`},              // the owner's: not a tool
			{"capture", `{"day":`}, // malformed
		}},
		answer{reply: "The metric weight is not registered; please register it."})
	e, err := ask(t, owner, "Record my weight, 72.5 kg.")
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
	f, owner, d, _, _ := setup(t)
	f.repeat = &answer{calls: [][2]string{{"get_day", `{"day":"2031-05-14"}`}}}
	_, err := ask(t, owner, "Loop")
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadGateway || !strings.Contains(err.Error(), "within 16 steps") {
		t.Fatalf("err = %v, want 502 after 16 steps", err)
	}
	if n := len(f.requests()); n != 16 {
		t.Fatalf("model requests = %d, want 16", n)
	}
	// Tools ran, so the turn is kept, with the error.
	body := bodyOf(t, d, "Conversation 2031-05-14")
	if strings.Count(body, "- `get_day` → [/days/2031-05-14]") != 16 || !strings.Contains(body, "**lifelog** did not answer: the model did not answer within 16 steps") {
		t.Fatalf("transcript = %s", body)
	}
}

func TestAskWritesNothingWhenTheModelFailsFirst(t *testing.T) {
	_, owner, d, _, _ := setup(t, answer{status: http.StatusInternalServerError})
	_, err := ask(t, owner, "Hello")
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadGateway || fmt.Sprint(ce.Entity.Properties.(map[string]any)["code"]) != "model" {
		t.Fatalf("err = %v, want 502 model", err)
	}
	if got := scalar(t, d, `SELECT count(*) FROM entities WHERE source <> 'schema'`); got != "0" {
		t.Fatalf("entities = %s, want none", got)
	}
}

func TestAskStopsWhenTheRequestIsCancelled(t *testing.T) {
	_, owner, d, _, _ := setup(t)
	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // a server sees the client go away only once the body is read
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(blocked.Close)
	// A second handler on the same file, whose agent waits on a model that never answers.
	agent := &chat.Agent{Model: &chat.Model{URL: blocked.URL + "/v1", Name: "synthetic"}}
	h := api.NewWith(&core.Store{DB: d}, agent, nil)
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

func TestAskWithoutAModel(t *testing.T) {
	_, _, d, _, _ := setup(t)
	c := client.InProcess(api.New(&core.Store{DB: d}), "cli")
	_, err := c.Do(t.Context(), catalogAction(t, c, "ask"), map[string]string{"question": "Hello"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusServiceUnavailable || fmt.Sprint(ce.Entity.Properties.(map[string]any)["code"]) != "no_model" {
		t.Fatalf("err = %v, want 503 no_model", err)
	}
}

func TestAskFromABrowserForm(t *testing.T) {
	_, _, d, h, _ := setup(t, answer{reply: "Hello."})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/conversations", strings.NewReader("question=Hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/conversations" {
		t.Fatalf("answer %d to %q, want 303 to /conversations and no receipt", rec.Code, rec.Header().Get("Location"))
	}
	id := scalar(t, d, `SELECT e.id FROM `+named+` WHERE n.title = 'Conversation 2031-05-14'`)
	if got := scalar(t, d, `SELECT source FROM entities WHERE id = ?`, id); got != chat.Source {
		t.Fatalf("conversation source = %q", got)
	}
	// Home and the chat page draw the form of ask, with the question alone: the conversation is the day's.
	for _, path := range []string{"/", "/conversations", "/conversations/" + id} {
		body := browse(t, h, path).Body.String()
		if !strings.Contains(body, `action="/conversations" data-action="ask">`) || !strings.Contains(body, `name="question"`) || strings.Contains(body, `name="conversation"`) {
			t.Fatalf("%s does not draw the form of ask with the question alone", path)
		}
	}
}

func TestTheConversationOfTodayAndAnEarlierDay(t *testing.T) {
	_, owner, d, h, clock := setup(t, answer{reply: "First."}, answer{reply: "Summed up: the owner said one."}, answer{reply: "Second."})
	clock.set("2031-05-13")
	id1 := idOf(must(ask(t, owner, "One")))
	if e := must(owner.Get(t.Context(), "/conversations")); fmt.Sprint(e.Properties.(map[string]any)["id"]) != fmt.Sprint(id1) {
		t.Fatalf("today's conversation on 05-13 is %v, want the page %d", e.Properties, id1)
	}
	clock.set("2031-05-14")
	// Before the first question of the day: no page yet, the previous conversation not compacted yet, ask offered.
	e := must(owner.Get(t.Context(), "/conversations"))
	p := e.Properties.(map[string]any)
	prev, _ := p["previous"].(map[string]any)
	if p["id"] != nil || p["day"] != today || p["title"] != "Conversation 2031-05-14" || len(p["turns"].([]any)) != 0 ||
		fmt.Sprint(prev["id"]) != fmt.Sprint(id1) || prev["day"] != "2031-05-13" || prev["compacted"] != false || actionNamed(e, "ask") == nil {
		t.Fatalf("today before its first question = %v %v", p, e.Actions)
	}
	id2 := idOf(must(ask(t, owner, "Two")))
	e = must(owner.Get(t.Context(), "/conversations"))
	p = e.Properties.(map[string]any)
	prev, _ = p["previous"].(map[string]any)
	if fmt.Sprint(p["id"]) != fmt.Sprint(id2) || fmt.Sprint(p["turns"]) != "[map[at:12.30 question:Two reply:Second. steps:[map[href:/days/2031-05-13 tool:compact]]]]" ||
		fmt.Sprint(prev) != fmt.Sprintf("map[compacted:true day:2031-05-13 id:%d title:Conversation 2031-05-13]", id1) {
		t.Fatalf("today's conversation = %v", p)
	}
	if links := hrefs(e); links["memory"] != "/days/2031-05-13" || links["previous"] != fmt.Sprintf("/conversations/%d", id1) || links["today"] != "" || links["self"] != "/conversations" {
		t.Fatalf("links of today's conversation = %v", links)
	}
	// An earlier day's conversation is read in full, with no ask: the conversation continues today only.
	e = must(owner.Get(t.Context(), fmt.Sprintf("/conversations/%d", id1)))
	p = e.Properties.(map[string]any)
	if fmt.Sprint(p["turns"]) != "[map[at:12.30 question:One reply:First. steps:[]]]" || p["compacted"] != true || p["previous"] != nil ||
		actionNamed(e, "ask") != nil || hrefs(e)["today"] != "/conversations" || hrefs(e)["memory"] != "" {
		t.Fatalf("the conversation of 05-13 = %v %v %v", p, e.Actions, hrefs(e))
	}
	if e = must(owner.Get(t.Context(), fmt.Sprintf("/conversations/%d", id2))); actionNamed(e, "ask") == nil || hrefs(e)["self"] != fmt.Sprintf("/conversations/%d", id2) {
		t.Fatalf("today's conversation by its id offers no ask: %v", e.Actions)
	}
	page := must(owner.Do(t.Context(), catalogAction(t, owner, "create-page"), map[string]string{"title": "Recipes", "body": "Soup."}))
	if actionNamed(page, "ask") != nil {
		t.Fatal("an ordinary page offers ask")
	}
	// A tombstoned conversation is no previous conversation and is not found; neither is a page that is not a conversation.
	must(owner.Do(t.Context(), catalogAction(t, owner, "tombstone"), map[string]string{"id": fmt.Sprint(id1)}))
	if e = must(owner.Get(t.Context(), "/conversations")); e.Properties.(map[string]any)["previous"] != nil {
		t.Fatalf("previous after a tombstone = %v", e.Properties)
	}
	cid := scalar(t, d, `SELECT entity_id FROM entity_names WHERE title = 'Conversations'`)
	for _, id := range []string{fmt.Sprint(id1), cid} {
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
	}{{fmt.Sprintf("/pages/%d", id2), http.StatusSeeOther, fmt.Sprintf("/conversations/%d", id2)}, {fmt.Sprintf("/pages/%d?view=page", id2), http.StatusOK, ""}} {
		rec := browse(t, h, tc.path)
		if rec.Code != tc.code || rec.Header().Get("Location") != tc.loc {
			t.Fatalf("%s: %d to %q, want %d to %q", tc.path, rec.Code, rec.Header().Get("Location"), tc.code, tc.loc)
		}
	}
}

func TestAskCompactsYesterdayAtTheFirstQuestionOfTheDay(t *testing.T) {
	f, owner, d, _, clock := setup(t, answer{reply: "Hello to you."},
		answer{reply: "The owner said hello; nothing is open."}, answer{reply: "Good morning."}, answer{reply: "Still here."})
	must(ask(t, owner, "Hello"))
	clock.set("2031-05-15")
	e := must(ask(t, owner, "Good morning"))
	// The summary was asked for with the transcript alone (no tools, no memory: 05-14 had no earlier page).
	reqs := f.requests()
	if len(reqs) != 3 || len(reqs[1].Tools) != 0 || len(reqs[1].Messages) != 2 || !strings.HasPrefix(*reqs[1].Messages[1].Content, "The conversation of 2031-05-14, from the page Conversation 2031-05-14:") ||
		!strings.Contains(*reqs[1].Messages[1].Content, "**lifelog:** Hello to you.") {
		t.Fatalf("the summary request = %+v", reqs[1].Messages)
	}
	// The summary is on the day page, written by the agent, and links the transcript; the transcript ends compacted.
	if got := bodyOf(t, d, "2031-05-14"); got != "**Conversation with lifelog** ([[Conversation 2031-05-14]]): The owner said hello; nothing is open." {
		t.Fatalf("the day page of 05-14 = %q", got)
	}
	if got := scalar(t, d, `SELECT e.source FROM `+named+` WHERE n.title = '2031-05-14'`); got != chat.Source {
		t.Fatalf("day page source = %q", got)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-14"); !strings.HasSuffix(got, "**lifelog:** Hello to you.\n\n**Compacted** into [[2031-05-14]]\n") {
		t.Fatalf("the transcript of 05-14 = %q", got)
	}
	both := `SELECT count(*) FROM links l JOIN entity_names a ON a.entity_id = l.from_id JOIN entity_names b ON b.entity_id = l.to_id WHERE a.title = ? AND b.title = ?`
	if scalar(t, d, both, "2031-05-14", "Conversation 2031-05-14") != "1" || scalar(t, d, both, "Conversation 2031-05-14", "2031-05-14") != "1" {
		t.Fatal("the day page and the transcript do not link each other")
	}
	// The new day's turn says what was compacted, then the model's memory was the day page with the summary.
	want := "**Owner** (12.30): Good morning\n\n- `compact` → [/days/2031-05-14](/days/2031-05-14)\n\n**lifelog:** Good morning.\n"
	if got := bodyOf(t, d, "Conversation 2031-05-15"); got != want {
		t.Fatalf("the transcript of 05-15 =\n%s\nwant\n%s", got, want)
	}
	if msgs := reqs[2].Messages; len(msgs) != 3 || !strings.HasPrefix(*msgs[1].Content, "Your memory of the days before: the journal page of 2031-05-14.") ||
		!strings.Contains(*msgs[1].Content, "The owner said hello; nothing is open.") || *msgs[2].Content != "Good morning" || !strings.Contains(*msgs[0].Content, "Today is 2031-05-15 (Thursday)") {
		t.Fatalf("messages of the new day = %+v", msgs)
	}
	if steps := e.Result.(map[string]any)["steps"].([]any); fmt.Sprint(steps) != "[map[href:/days/2031-05-14 tool:compact]]" {
		t.Fatalf("steps = %v", steps)
	}
	// The next question of the day has nothing to compact: one model request.
	must(ask(t, owner, "Again"))
	if n := len(f.requests()); n != 4 {
		t.Fatalf("requests after the third question = %d, want 4", n)
	}
}

func TestAskCompactsAtMostThreeOldestFirst(t *testing.T) {
	f, owner, d, _, _ := setup(t,
		answer{reply: "Summary of 10."}, answer{reply: "Summary of 11."}, answer{reply: "Summary of 12."}, answer{reply: "Hello."},
		answer{reply: "Summary of 13."}, answer{reply: "Hello again."})
	for _, day := range []string{"10", "11", "12", "13"} {
		seedConversation(t, owner, "Conversation 2031-05-"+day, turn("09.00", "Day "+day, "Yes, "+day+"."))
	}
	e := must(ask(t, owner, "Hello"))
	if steps := e.Result.(map[string]any)["steps"].([]any); fmt.Sprint(steps) != "[map[href:/days/2031-05-10 tool:compact] map[href:/days/2031-05-11 tool:compact] map[href:/days/2031-05-12 tool:compact]]" {
		t.Fatalf("steps = %v", steps)
	}
	for day, want := range map[string]string{"10": "Summary of 10.", "11": "Summary of 11.", "12": "Summary of 12.", "13": ""} {
		if got := bodyOf(t, d, "2031-05-"+day); (want == "" && got != "") || (want != "" && got != "**Conversation with lifelog** ([[Conversation 2031-05-"+day+"]]): "+want) {
			t.Fatalf("the day page of %s = %q", day, got)
		}
		if compacted := strings.Contains(bodyOf(t, d, "Conversation 2031-05-"+day), "**Compacted** into [[2031-05-"+day+"]]"); compacted != (want != "") {
			t.Fatalf("the transcript of %s compacted = %v", day, compacted)
		}
	}
	// Each summary was asked with the memory that day had: 10 had none, 11 had the page of 10 with its summary.
	sums := f.summaries()
	if len(sums) != 3 || len(sums[0].Messages) != 2 || len(sums[1].Messages) != 3 ||
		!strings.HasPrefix(*sums[1].Messages[1].Content, "The journal page of 2031-05-10, the memory lifelog had on 2031-05-11:") || !strings.Contains(*sums[1].Messages[1].Content, "Summary of 10.") {
		t.Fatalf("the summary requests = %+v", sums)
	}
	// The next question catches up: 13 is compacted, and today's memory is its page.
	must(ask(t, owner, "Hello again"))
	if got := bodyOf(t, d, "2031-05-13"); got != "**Conversation with lifelog** ([[Conversation 2031-05-13]]): Summary of 13." {
		t.Fatalf("the day page of 13 = %q", got)
	}
	last := f.requests()[len(f.requests())-1].Messages
	if len(last) != 4 || !strings.HasPrefix(*last[1].Content, "Your memory of the days before: the journal page of 2031-05-13.") || !strings.HasPrefix(*last[2].Content, "The conversation so far today") {
		t.Fatalf("messages of the second question = %+v", last)
	}
}

func TestAskAnswersWhenCompactionFails(t *testing.T) {
	_, owner, d, _, _ := setup(t, answer{status: http.StatusInternalServerError}, answer{reply: "Hello."},
		answer{reply: "Summary of 13."}, answer{status: http.StatusInternalServerError})
	seedConversation(t, owner, "Conversation 2031-05-13", turn("09.00", "Day 13", "Yes."))
	e, err := ask(t, owner, "Hello")
	if err != nil {
		t.Fatal(err)
	}
	steps := e.Result.(map[string]any)["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["tool"] != "compact" || !strings.Contains(fmt.Sprint(steps[0].(map[string]any)["error"]), "the summary of 2031-05-13: the model answered 500") {
		t.Fatalf("steps = %v", steps)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-14"); !strings.Contains(got, "- `compact`: the summary of 2031-05-13: the model answered 500") || !strings.HasSuffix(got, "**lifelog:** Hello.\n") {
		t.Fatalf("the transcript of today = %q", got)
	}
	if bodyOf(t, d, "2031-05-13") != "" || strings.Contains(bodyOf(t, d, "Conversation 2031-05-13"), "**Compacted**") {
		t.Fatal("a failed compaction wrote")
	}
	// The next question compacts it; when the model then fails, the turn is kept with the compaction and the failure.
	_, err = ask(t, owner, "Again")
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadGateway || !strings.Contains(err.Error(), "the turn is kept") {
		t.Fatalf("err = %v, want 502 with the turn kept", err)
	}
	if got := bodyOf(t, d, "2031-05-13"); got != "**Conversation with lifelog** ([[Conversation 2031-05-13]]): Summary of 13." {
		t.Fatalf("the day page of 13 = %q", got)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-14"); !strings.HasSuffix(got, "**Owner** (12.30): Again\n\n- `compact` → [/days/2031-05-13](/days/2031-05-13)\n\n**lifelog** did not answer: the model answered 500 Internal Server Error: synthetic failure\n") {
		t.Fatalf("the transcript of today = %q", got)
	}
}

func TestAskCompactsAnOldFormatConversation(t *testing.T) {
	_, owner, d, _, _ := setup(t, answer{reply: "Summary of the old one."}, answer{reply: "Hello."})
	old := seedConversation(t, owner, "Conversation 2031-05-13 09.12.33", turn("09.12", "Old", "Yes.")) // the title of plan 102
	notes := seedConversation(t, owner, "Conversation notes", "Kept by hand.")                          // no day: never compacted
	must(ask(t, owner, "Hello"))
	if got := bodyOf(t, d, "2031-05-13"); got != "**Conversation with lifelog** ([[Conversation 2031-05-13 09.12.33]]): Summary of the old one." {
		t.Fatalf("the day page of 13 = %q", got)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-13 09.12.33"); !strings.HasSuffix(got, "**Compacted** into [[2031-05-13]]\n") {
		t.Fatalf("the old transcript = %q", got)
	}
	e := must(owner.Get(t.Context(), fmt.Sprintf("/conversations/%d", notes)))
	if p := e.Properties.(map[string]any); p["day"] != "" || p["preamble"] != "Kept by hand." || p["previous"] == nil || fmt.Sprint(p["previous"].(map[string]any)["id"]) != fmt.Sprint(old) {
		t.Fatalf("the page with no day = %v", p)
	}
	if prev := must(owner.Get(t.Context(), "/conversations")).Properties.(map[string]any)["previous"].(map[string]any); fmt.Sprint(prev["id"]) != fmt.Sprint(old) {
		t.Fatalf("today's previous = %v, want the old one, never the page with no day", prev)
	}
}

func TestAskWritesTheSummaryAgainWhenTheTrailerIsStale(t *testing.T) {
	var owner *client.Client
	var earlier int64
	f, owner, d, _, _ := setup(t,
		answer{reply: "Summary.", during: func() { // the owner edits the earlier page while it is summed up
			e := must(owner.Get(t.Context(), fmt.Sprintf("/pages/%d", earlier)))
			p := e.Properties.(map[string]any)
			must(owner.Do(t.Context(), catalogAction(t, owner, "save-body"), map[string]string{"id": fmt.Sprint(earlier), "body": p["body"].(string) + "\nEdited.", "version": p["version"].(string)}))
		}},
		answer{reply: "Hello."}, answer{reply: "Summary again."}, answer{reply: "Hello again."})
	earlier = seedConversation(t, owner, "Conversation 2031-05-13", turn("09.00", "Day 13", "Yes."))
	e := must(ask(t, owner, "Hello"))
	steps := e.Result.(map[string]any)["steps"].([]any)
	if len(steps) != 1 || !strings.Contains(fmt.Sprint(steps[0].(map[string]any)["error"]), "the trailer of Conversation 2031-05-13: 409") {
		t.Fatalf("steps = %v", steps)
	}
	if got := bodyOf(t, d, "2031-05-13"); got != "**Conversation with lifelog** ([[Conversation 2031-05-13]]): Summary." {
		t.Fatalf("the day page of 13 = %q", got)
	}
	if strings.Contains(bodyOf(t, d, "Conversation 2031-05-13"), "**Compacted**") {
		t.Fatal("the stale trailer was written")
	}
	// The next question sums it up again: the owner sees the summary twice, never loses one.
	must(ask(t, owner, "Hello again"))
	if got := bodyOf(t, d, "2031-05-13"); got != "**Conversation with lifelog** ([[Conversation 2031-05-13]]): Summary.\n\n**Conversation with lifelog** ([[Conversation 2031-05-13]]): Summary again." {
		t.Fatalf("the day page of 13 = %q", got)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-13"); !strings.HasSuffix(got, "Edited.\n\n**Compacted** into [[2031-05-13]]\n") {
		t.Fatalf("the transcript of 13 = %q", got)
	}
	if n := len(f.summaries()); n != 2 {
		t.Fatalf("summaries asked = %d, want 2", n)
	}
}

func TestAskAppendsWhenTheDaysPageAppearsDuringTheTurn(t *testing.T) {
	var owner *client.Client
	_, owner, d, _, _ := setup(t, answer{reply: "Hello.", during: func() {
		seedConversation(t, owner, "Conversation 2031-05-14", "Made by hand.")
	}})
	must(ask(t, owner, "Hello"))
	if got := scalar(t, d, `SELECT count(*) FROM `+named+` WHERE n.title LIKE 'Conversation %'`); got != "1" {
		t.Fatalf("conversation pages = %s, want 1", got)
	}
	if got := bodyOf(t, d, "Conversation 2031-05-14"); got != "Made by hand.\n\n"+turn("12.30", "Hello", "Hello.") {
		t.Fatalf("the transcript = %q", got)
	}
}

func TestAskKeepsTheDayItWasAskedOn(t *testing.T) {
	var c *clock
	f, owner, d, _, c := setup(t, answer{reply: "Good night.", during: func() { c.set("2031-05-15") }})
	must(ask(t, owner, "Good night"))
	if got := bodyOf(t, d, "Conversation 2031-05-14"); got != turn("12.30", "Good night", "Good night.") {
		t.Fatalf("the transcript of 05-14 = %q", got)
	}
	if bodyOf(t, d, "Conversation 2031-05-15") != "" || len(f.summaries()) != 0 {
		t.Fatal("the turn moved to the next day, or the day was compacted during it")
	}
}

func TestTheChatSidebarAndTheChatPage(t *testing.T) {
	_, owner, _, h, clock := setup(t, answer{reply: "Hello **there**."}, answer{reply: "Summed up."}, answer{reply: "Morning."})
	clock.set("2031-05-13")
	id := idOf(must(ask(t, owner, "Hi")))
	clock.set("2031-05-14")
	home := browse(t, h, "/").Body.String()
	for _, want := range []string{`<aside class="chat-panel" id="chat-panel" popover aria-label="Chat">`, `<iframe src="/conversations" title="The conversation" loading="lazy"></iframe>`,
		`<button type="button" class="button chat-toggle" popovertarget="chat-panel">`, `<a href="/conversations"`} {
		if !strings.Contains(home, want) {
			t.Fatalf("Home has no %s", want)
		}
	}
	if strings.Contains(home, `data-chat="new"`) || strings.Contains(home, `name="conversation"`) {
		t.Fatal("Home offers a new conversation: the conversation is one")
	}
	// Before the first question of the day, the note says the previous day is summed up at that question.
	page := browse(t, h, "/conversations").Body.String()
	if !strings.Contains(page, "is summed up in that day's page at your first question of today") {
		t.Fatalf("the chat page before the first question has no note: %s", page)
	}
	must(ask(t, owner, "Morning"))
	page = browse(t, h, "/conversations").Body.String()
	if strings.Contains(page, `id="chat-panel"`) || strings.Contains(page, "chat-list") {
		t.Fatal("the chat page draws the sidebar or a list of conversations")
	}
	// Inside the sidebar's frame the page has no shell: the browser says it asks for a frame.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/conversations", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	framed := httptest.NewRecorder()
	h.ServeHTTP(framed, req)
	if !strings.Contains(page, `class="rail"`) || strings.Contains(framed.Body.String(), `class="rail"`) || !strings.Contains(framed.Body.String(), `<div class="chat-thread"><div class="chat">`) {
		t.Fatalf("the chat page in a frame draws the shell, or the page outside one does not")
	}
	for _, want := range []string{`<div class="chat-page">`, `<h1>Conversation 2031-05-14</h1>`, `<a href="/days/2031-05-13">Memory: 2031-05-13</a>`,
		`The conversation of <a href="/days/2031-05-13">2031-05-13</a> is summed up in that day's page. <a href="/conversations/` + fmt.Sprint(id) + `">Read it in full.</a>`,
		`<figcaption class="who">You · 12.30</figcaption></figure><div class="msg-main"><div class="body prose"><p>Morning</p>`,
		`<figcaption class="who">Lifelog</figcaption></figure><div class="msg-main"><div class="body prose"><p>Morning.</p>`,
		`<form class="msg msg-owner msg-draft" method="POST" action="/conversations" data-action="ask">`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the chat page has no %s", want)
		}
	}
	// An earlier day's conversation is read in full, with no draft and a way back to today.
	earlier := browse(t, h, fmt.Sprintf("/conversations/%d", id)).Body.String()
	if strings.Contains(earlier, `<form class="msg msg-owner msg-draft"`) || !strings.Contains(earlier, `<a href="/conversations">Today</a>`) || !strings.Contains(earlier, "Hello <strong>there</strong>.") {
		t.Fatalf("the conversation of 05-13 draws a draft or no way to today: %s", earlier)
	}
	for _, who := range []string{"lifelog", "owner"} { // the portraits the thread names are served, as PNG
		rec := browse(t, h, "/assets/portrait-"+who+".png")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
			t.Fatalf("portrait %s: %d %s", who, rec.Code, rec.Header().Get("Content-Type"))
		}
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

func selfHref(e *api.Entity) string { return hrefs(e)["self"] }

// hrefs is the href of each link by its first rel.
func hrefs(e *api.Entity) map[string]string {
	out := map[string]string{}
	for _, l := range e.Links {
		if len(l.Rel) > 0 {
			out[l.Rel[0]] = l.Href
		}
	}
	return out
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
