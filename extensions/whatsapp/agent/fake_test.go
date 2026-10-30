package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
)

const owner = "40712345678"

// token is the bearer token of the owner, as both clients of the rig send it.
const token = "owner-token"

// wait is a guard against a deadlock. Nothing waits for it in a green run.
const wait = 30 * time.Second

var fixedNow = time.Date(2031, 5, 20, 9, 30, 0, 0, time.UTC)

// page is a page of the fake API.
type page struct {
	id       int
	title    string
	body     string
	category string
	version  int
}

// seen is one request that the fake API received.
type seen struct{ Method, Path, Source, Auth, Body string }

// fakeAPI serves the catalog and a few pages, and answers every other path with an entity. A path that ends in /big
// answers a title that is longer than the bound of a result. A page title is redirected to its page, as the real
// API does.
type fakeAPI struct {
	mu    sync.Mutex
	reqs  []seen
	pages []*page
	// sneak, when it is not empty, is added to the body of the page by someone else just before the next save
	// (the version changes, so the save is stale). alwaysStale does it before every save.
	sneak       string
	alwaysStale bool
	// the settings of the model, as GET /settings answers them (an empty value is absent)
	modelURL, model, modelKey string
	ownerNumber               string         // owner_number of the settings
	extra                     []siren.Action // actions that a connected app added to the catalog
	catalogFails              bool           // GET /actions answers 503
}

// addAction adds an action to the catalog, as a connected app does when it registers.
func (f *fakeAPI) addAction(a siren.Action) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra = append(f.extra, a)
}

// failCatalog makes GET /actions fail (or work again).
func (f *fakeAPI) failCatalog(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogFails = on
}

// setOwnerNumber changes the owner's number in the settings, as the owner would in lifelog.
func (f *fakeAPI) setOwnerNumber(n string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerNumber = n
}

// setModel changes the settings, as the owner would in lifelog.
func (f *fakeAPI) setModel(url, name, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelURL, f.model, f.modelKey = url, name, key
}

// today is the day that the fake API calls today.
const today = "2031-05-20"

// conversation is the entity of GET /days/{day}/conversation and of say: the page's id, version and body when the
// page exists.
func (f *fakeAPI) conversation(day string) siren.Entity {
	props := map[string]any{"day": day, "turns": []any{}}
	if p := f.find("Conversation " + day); p != nil {
		props["id"], props["version"], props["body"] = p.id, p.version, p.body
		// The turns are the paragraphs of the body, with the mark as who. The real API does the same.
		turns := []any{}
		for _, para := range strings.Split(p.body, "\n\n") {
			who := "text"
			switch {
			case strings.HasPrefix(para, "**Owner**"):
				who = "owner"
			case strings.HasPrefix(para, "**lifelog**"):
				who = "lifelog"
			}
			text := para // the real API gives the words after the mark
			if i := strings.Index(para, "): "); who != "text" && i >= 0 {
				text = para[i+3:]
			}
			turns = append(turns, map[string]any{"who": who, "text": text})
		}
		props["turns"] = turns
	}
	return siren.Entity{Class: []string{"conversation"}, Properties: props}
}

func (f *fakeAPI) find(title string) *page {
	for _, p := range f.pages {
		if p.title == title {
			return p
		}
	}
	return nil
}

// add makes a page, as another surface would.
func (f *fakeAPI) add(title, body string) *page {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &page{id: len(f.pages) + 1, title: title, body: body, version: 1}
	f.pages = append(f.pages, p)
	return p
}

// edit replaces the body of a page, as another surface would.
func (f *fakeAPI) edit(title, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.find(title)
	p.body, p.version = body, p.version+1
}

func (f *fakeAPI) body(title string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.find(title); p != nil {
		return p.body
	}
	return ""
}

func (p *page) entity() siren.Entity {
	return siren.Entity{Title: p.title, Properties: map[string]any{"id": p.id, "version": p.version, "body": p.body},
		Links: []siren.Link{{Rel: []string{"self"}, Href: fmt.Sprintf("/pages/%d", p.id)}}}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, seen{r.Method, r.URL.Path, r.Header.Get(siren.SourceHeader), r.Header.Get("Authorization"), string(data)})
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	e := siren.Entity{Title: "ok " + r.URL.Path, Links: []siren.Link{{Rel: []string{"self"}, Href: r.URL.Path}}}
	fail := func(status int, code string) {
		w.WriteHeader(status)
		e = siren.Entity{Properties: map[string]any{"status": status, "code": code, "message": "no"}}
	}
	var id int
	src := r.Header.Get(siren.SourceHeader) // an agent or an import is not the owner
	notOwner := strings.HasPrefix(src, "agent:") || strings.HasPrefix(src, "import:")
	switch {
	case r.URL.Path == "/actions":
		if f.catalogFails {
			fail(503, "unavailable")
			break
		}
		e = siren.Entity{Actions: append(catalog(), f.extra...)}
	case r.URL.Path == "/settings" && r.Method == "GET":
		if notOwner {
			fail(403, "forbidden")
			break
		}
		props := map[string]any{}
		for k, v := range map[string]string{"model_url": f.modelURL, "model": f.model, "model_key": f.modelKey, "owner_number": f.ownerNumber} {
			if v != "" {
				props[k] = v
			}
		}
		e = siren.Entity{Class: []string{"settings"}, Properties: props}
	case strings.HasSuffix(r.URL.Path, "/conversation") && r.Method == "GET":
		e = f.conversation(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/conversation"))
	case strings.HasSuffix(r.URL.Path, "/say") && r.Method == "POST":
		if notOwner {
			fail(403, "forbidden")
			break
		}
		if refuseUnknown(r, "say") {
			fail(422, "invalid")
			break
		}
		day := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/days/"), "/say")
		if day == "today" {
			day = today
		}
		p := f.find("Conversation " + day)
		if p == nil {
			p = &page{id: len(f.pages) + 1, title: "Conversation " + day, body: "[[" + day + "]]", category: "Conversations", version: 1}
			f.pages = append(f.pages, p)
		}
		stamp := "(09:30)"
		if via := r.PostForm.Get("via"); via != "" {
			stamp = "(09:30, via " + via + ")"
		}
		p.body, p.version = p.body+"\n\n**Owner** "+stamp+": "+r.PostForm.Get("text"), p.version+1
		e = f.conversation(day)
	case strings.HasSuffix(r.URL.Path, "/body") && r.Method == "POST":
		if refuseUnknown(r, "save-body") {
			fail(422, "invalid")
			break
		}
		fmt.Sscanf(r.URL.Path, "/pages/%d/body", &id)
		p := f.pages[id-1]
		if f.sneak != "" || f.alwaysStale {
			p.body, p.version = p.body+"\n\n"+f.sneak+"x", p.version+1
			f.sneak = ""
		}
		if r.PostForm.Get("version") != fmt.Sprint(p.version) {
			fail(409, "stale_version")
			break
		}
		p.body, p.version = r.PostForm.Get("body"), p.version+1
		e = p.entity()
	case strings.HasSuffix(r.URL.Path, "/big"):
		e.Title = strings.Repeat("é", maxResult)
	case strings.HasSuffix(r.URL.Path, "/gone"):
		fail(404, "not_found")
	}
	json.NewEncoder(w).Encode(e)
}

func (f *fakeAPI) requests() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.reqs...)
}

// writes are the requests that are not a GET and not a write of a conversation page (save-body or say).
func (f *fakeAPI) writes() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, r := range f.reqs {
		if r.Method != "GET" && !strings.HasPrefix(r.Path, "/pages") && !strings.HasSuffix(r.Path, "/say") {
			out = append(out, r)
		}
	}
	return out
}

// refuseUnknown reports a form name that the row of the action in the catalog does not list: the real API answers 422
// for it (API.md, "A request names only the fields of the action").
func refuseUnknown(r *http.Request, action string) bool {
	for _, a := range catalog() {
		if a.Name != action {
			continue
		}
		for name := range r.PostForm {
			if a.Field(name) == nil {
				return true
			}
		}
	}
	return false
}

func catalog() []siren.Action {
	return []siren.Action{
		{Name: "open-day", Method: "GET", Href: "/days/{day}", Fields: []siren.Field{{Name: "day", Type: "date", In: "path"}}},
		{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea", Title: "Text"}}},
		{Name: "say", Owner: true, Method: "POST", Href: "/days/{day}/say", Fields: []siren.Field{
			{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea", Required: true}, {Name: "via", Type: "hidden"}}},
		{Name: "save-body", Method: "POST", Href: "/pages/{id}/body", Fields: []siren.Field{
			{Name: "id", Type: "number", In: "path", Required: true}, {Name: "body", Type: "textarea", Required: true}, {Name: "version", Type: "hidden", Required: true}}},
		{Name: "snapshot", Owner: true, Method: "POST", Href: "/snapshot"},
		{Name: "settings", Owner: true, Method: "POST", Href: "/settings", Fields: []siren.Field{
			{Name: "model_url", Type: "text"}, {Name: "model", Type: "text"}, {Name: "model_key", Type: "password"}, {Name: "owner_number", Type: "text"}}},
	}
}

// chatReq is the part of a request to the model that the tests read.
type chatReq struct {
	Model string
	Tools []struct {
		Function struct{ Name string }
	}
	Messages []struct {
		Role       string
		Content    string
		ToolCallID string `json:"tool_call_id"`
	}
}

// fakeModel answers the n-th request (from 1) with script(n, request). A script returns a JSON message.
type fakeModel struct {
	mu     sync.Mutex
	reqs   []chatReq
	script func(n int, r chatReq) (status int, body string)
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req chatReq
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	n := len(f.reqs)
	f.mu.Unlock()
	status, body := f.script(n, req)
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func (f *fakeModel) requests() []chatReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chatReq(nil), f.reqs...)
}

func text(s string) (int, string) {
	b, _ := json.Marshal(s)
	return 200, `{"choices":[{"message":{"role":"assistant","content":` + string(b) + `},"finish_reason":"stop"}]}`
}

func toolCall(name, args string) (int, string) {
	a, _ := json.Marshal(args)
	return 200, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call1","type":"function","function":{"name":"` +
		name + `","arguments":` + string(a) + `}}]},"finish_reason":"tool_calls"}]}`
}

// fakeChat is a chat that the test drives: in carries the messages, sent holds the replies.
type fakeChat struct {
	in   chan Message
	sent chan string
	mu   sync.Mutex
	typ  []bool
}

func newFakeChat() *fakeChat { return &fakeChat{in: make(chan Message), sent: make(chan string, 100)} }

func (c *fakeChat) Messages() <-chan Message { return c.in }

func (c *fakeChat) Send(_ context.Context, s string) error { c.sent <- s; return nil }

func (c *fakeChat) Typing(_ context.Context, on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.typ = append(c.typ, on)
	return nil
}

func (c *fakeChat) typing() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.typ...)
}

// say puts a text of the owner in the chat.
func (c *fakeChat) say(t *testing.T, s string) {
	c.deliver(t, Message{Sender: owner, Text: s, Kind: KindText, At: fixedNow})
}

func (c *fakeChat) deliver(t *testing.T, m Message) {
	t.Helper()
	select {
	case c.in <- m:
	case <-time.After(wait):
		t.Fatal("the agent did not take the message")
	}
}

func (c *fakeChat) reply(t *testing.T) string {
	t.Helper()
	select {
	case s := <-c.sent:
		return s
	case <-time.After(wait):
		t.Fatal("no reply")
		return ""
	}
}

// rig is an agent on fakes, started.
type rig struct {
	api    *fakeAPI
	mod    *fakeModel
	chat   *fakeChat
	state  string
	tick   chan time.Time
	cancel context.CancelFunc
	done   chan error
	mu     sync.Mutex
	now    time.Time
}

func (r *rig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

// setDay moves the clock of the agent to a later day.
func (r *rig) setDay(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = t
}

func start(t *testing.T, script func(n int, r chatReq) (int, string)) *rig {
	t.Helper()
	return startOn(t, script, &fakeAPI{}, filepath.Join(t.TempDir(), "state.json"))
}

// startOn starts an agent on an API and a state file that may have been used before.
func startOn(t *testing.T, script func(n int, r chatReq) (int, string), api *fakeAPI, state string) *rig {
	t.Helper()
	r := &rig{api: api, mod: &fakeModel{script: script}, chat: newFakeChat(), state: state, tick: make(chan time.Time),
		done: make(chan error, 1), now: fixedNow}
	modSrv := httptest.NewServer(r.mod)
	t.Cleanup(modSrv.Close)
	r.api.setModel(modSrv.URL, "test-model", "")
	apiSrv := httptest.NewServer(r.api)
	t.Cleanup(apiSrv.Close)
	a := &Agent{
		Chat: r.chat, Client: client.Remote(apiSrv.URL, siren.AgentPrefix+Source, token),
		AsOwner: client.Remote(apiSrv.URL, "", token),
		Owner:   owner, State: r.state, Now: r.clock, Log: log.New(io.Discard, "", 0), Tick: r.tick,
	}
	ctx, cancel := context.WithCancel(t.Context())
	r.cancel = cancel
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(wait):
			t.Error("the agent did not stop")
		}
	})
	go func() { r.done <- a.Run(ctx) }()
	return r
}

// poll makes the worker look at the page and waits until it has taken the tick.
func (r *rig) poll(t *testing.T) {
	t.Helper()
	select {
	case r.tick <- fixedNow:
	case <-time.After(wait):
		t.Fatal("the worker did not take the tick")
	}
}

// settle polls twice: the second tick is taken only after the first look is done.
func (r *rig) settle(t *testing.T) { r.poll(t); r.poll(t) }

// finish closes the chat and waits for Run, which answers what is queued first.
func (r *rig) finish(t *testing.T) error {
	t.Helper()
	close(r.chat.in)
	select {
	case err := <-r.done:
		r.done <- err // the cleanup reads it again
		return err
	case <-time.After(wait):
		t.Fatal("Run did not return")
		return nil
	}
}
