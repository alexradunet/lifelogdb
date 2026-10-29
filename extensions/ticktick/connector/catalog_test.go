package connector

import (
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/client"
	"lifelog/siren"
)

// catalogEnv is the catalog on a server, with a fake TickTick and a fake lifelog behind it.
type catalogEnv struct {
	tick  *fakeTick
	life  *fakeLife
	c     *client.Client
	p     *Poller
	login bool // onLogin was called
}

func newCatalogEnv(t *testing.T) *catalogEnv {
	t.Helper()
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	p, err := NewPoller(tt, ll, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Now = func() time.Time { return time.Date(2030, 3, 5, 12, 0, 0, 0, time.Local) }
	env := &catalogEnv{tick: tick, life: life, p: p}
	p.onLogin = func() { env.login = true }
	srv := httptest.NewServer(Catalog(p, tick.lists))
	t.Cleanup(srv.Close)
	env.c = client.Remote(srv.URL, "", "")
	return env
}

func (e *catalogEnv) action(t *testing.T, name string) siren.Action {
	t.Helper()
	for _, a := range actions(e.tick.lists) {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s", name)
	return siren.Action{}
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	m, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("entity %q has no properties: %+v", e.Title, e)
	}
	return m
}

// wantError checks the status and the code of a refusal and that its message holds none of the forbidden words.
func wantError(t *testing.T, err error, status int, code, msg string) {
	t.Helper()
	var ce *client.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want an error entity", err)
	}
	if ce.Status != status || ce.Code() != code {
		t.Errorf("error = %d %s, want %d %s", ce.Status, ce.Code(), status, code)
	}
	if got := props(t, ce.Entity)["message"]; got != msg {
		t.Errorf("message = %v, want %q", got, msg)
	}
}

func TestTheCatalogListsThreeActionsWithTheirFields(t *testing.T) {
	env := newCatalogEnv(t)
	got, err := env.c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byName := map[string]siren.Action{}
	for _, a := range got {
		names = append(names, a.Name)
		byName[a.Name] = a
	}
	slices.Sort(names)
	if want := []string{"add-task", "complete-task", "sync"}; !slices.Equal(names, want) {
		t.Fatalf("actions = %v, want %v", names, want)
	}
	add := byName["add-task"]
	field := func(name string) siren.Field { return *add.Field(name) }
	complete := byName["complete-task"]
	if f := field("title"); f.Type != "text" || !f.Required {
		t.Errorf("title = %+v, want a required text", f)
	}
	if f := field("list"); !slices.Equal(f.Options, []string{"Inbox", "Home", "Work"}) {
		t.Errorf("list options = %v, want Inbox Home Work", f.Options)
	}
	if f := field("due"); f.Type != "date" {
		t.Errorf("due type = %q, want date", f.Type)
	}
	if f := field("notes"); f.Type != "textarea" {
		t.Errorf("notes type = %q, want textarea", f.Type)
	}
	if f := complete.Field("id"); f == nil || f.In != "path" || !f.Required {
		t.Errorf("complete-task id = %+v, want a required path field", f)
	}
	for _, a := range got {
		if a.Owner {
			t.Errorf("%s is owner-only: an agent may add and complete a task", a.Name)
		}
	}
}

func TestTheHomeCountsTheTasksAndEmbedsTheOnesDueTodayOrBefore(t *testing.T) {
	env := newCatalogEnv(t)
	due := func(day string) string { return day + "T00:00:00.000+0000" }
	env.tick.add("late", "p-home", "Overdue").DueDate = due("2030-03-01")
	env.tick.add("today", "p-work", "Due today").DueDate = due("2030-03-05")
	env.tick.add("later", "p-work", "Due later").DueDate = due("2030-03-09")
	env.tick.add("nodue", "p-home", "No due day")

	home, err := env.c.Get(t.Context(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(home.Class, []string{"ticktick"}) || home.Title != "TickTick" {
		t.Errorf("home = %v %q, want class ticktick, title TickTick", home.Class, home.Title)
	}
	p := props(t, home)
	if p["open"].(interface{ String() string }).String() != "4" || p["overdue"].(interface{ String() string }).String() != "1" {
		t.Errorf("open = %v, overdue = %v, want 4 and 1", p["open"], p["overdue"])
	}
	if lists, _ := p["lists"].([]any); len(lists) != 2 || lists[0] != "Home" || lists[1] != "Work" {
		t.Errorf("lists = %v, want Home Work", p["lists"])
	}
	var hrefs []string
	for _, l := range home.Entities {
		if !slices.Contains(l.Rel, "item") || !slices.Contains(l.Class, "task") {
			t.Errorf("embedded %+v, want rel item, class task", l)
		}
		hrefs = append(hrefs, l.Href)
	}
	slices.Sort(hrefs)
	if want := []string{"/tasks/late", "/tasks/today"}; !slices.Equal(hrefs, want) {
		t.Errorf("embedded = %v, want %v", hrefs, want)
	}
	var acts []string
	for _, a := range home.Actions {
		acts = append(acts, a.Name)
	}
	if want := []string{"add-task", "sync"}; !slices.Equal(acts, want) {
		t.Errorf("home actions = %v, want %v", acts, want)
	}
}

func TestAddTaskMakesTheTaskInTheListAndAnswersWithIt(t *testing.T) {
	env := newCatalogEnv(t)
	e, err := env.c.Do(t.Context(), env.action(t, "add-task"),
		map[string]string{"title": "Milk", "list": "Home", "due": "2030-03-09", "notes": "2 litres"})
	if err != nil {
		t.Fatal(err)
	}
	made := env.tick.tasks["new1"]
	if made == nil || made.Title != "Milk" || made.ProjectID != "p-home" || made.Content != "2 litres" || !made.IsAllDay ||
		!strings.HasPrefix(made.DueDate, "2030-03-09T00:00:00.000") {
		t.Fatalf("task in TickTick = %+v", made)
	}
	if !slices.Equal(e.Class, []string{"task"}) {
		t.Errorf("class = %v, want task", e.Class)
	}
	if self := e.LinkByRel("self"); self == nil || self.Href != "/tasks/new1" {
		t.Errorf("self = %+v, want /tasks/new1", self)
	}
	if len(e.Actions) != 1 || e.Actions[0].Name != "complete-task" || e.Actions[0].Href != "/tasks/new1/complete" {
		t.Errorf("actions = %+v, want complete-task at /tasks/new1/complete", e.Actions)
	}
}

func TestAddTaskWithoutAListGoesToTheInbox(t *testing.T) {
	env := newCatalogEnv(t)
	for _, list := range []string{"", "Inbox"} {
		if _, err := env.c.Do(t.Context(), env.action(t, "add-task"), map[string]string{"title": "Milk", "list": list}); err != nil {
			t.Fatal(err)
		}
	}
	for id, task := range env.tick.tasks {
		if task.ProjectID != "" {
			t.Errorf("task %s has the list %q, want the Inbox (no projectId)", id, task.ProjectID)
		}
	}
	if len(env.tick.tasks) != 2 {
		t.Errorf("%d tasks, want 2", len(env.tick.tasks))
	}
}

func TestAddTaskRefusals(t *testing.T) {
	env := newCatalogEnv(t)
	add := env.action(t, "add-task")
	tests := []struct {
		name   string
		values map[string]string
		msg    string
	}{
		{"a due that is not a day", map[string]string{"title": "x", "due": "09/03/2030"}, "due must be a day, YYYY-MM-DD"},
		{"a due with a time", map[string]string{"title": "x", "due": "2030-03-09T10:00"}, "due must be a day, YYYY-MM-DD"},
		{"a list that is not a list", map[string]string{"title": "x", "list": "Nowhere"}, "list is not a list of TickTick: Nowhere"},
		{"a blank title", map[string]string{"title": "  "}, "title is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.c.Do(t.Context(), add, tc.values)
			wantError(t, err, 422, "invalid", tc.msg)
		})
	}
	if len(env.tick.tasks) != 0 {
		t.Errorf("a refused add made %d tasks in TickTick", len(env.tick.tasks))
	}
}

func TestCompleteTaskCompletesItInTickTick(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("t1", "p-work", "Write report")
	e, err := env.c.Do(t.Context(), env.action(t, "complete-task"), map[string]string{"id": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if env.tick.tasks["t1"].Status != 2 {
		t.Errorf("status in TickTick = %d, want 2", env.tick.tasks["t1"].Status)
	}
	if props(t, e)["done"] != true || len(e.Actions) != 0 {
		t.Errorf("answer = %+v, want a done task with no action", e)
	}
}

func TestAnUnknownTaskIsNotFound(t *testing.T) {
	env := newCatalogEnv(t)
	_, err := env.c.Do(t.Context(), env.action(t, "complete-task"), map[string]string{"id": "nope"})
	wantError(t, err, 404, "not_found", "no such task")
	_, err = env.c.Get(t.Context(), "/tasks/nope")
	wantError(t, err, 404, "not_found", "no such task")

	// A task that this app learned and TickTick forgot: TickTick's 404 passes as not_found.
	env.tick.add("t1", "p-work", "Gone soon")
	if _, err := env.c.Get(t.Context(), "/tasks/t1"); err != nil {
		t.Fatal(err)
	}
	delete(env.tick.tasks, "t1")
	_, err = env.c.Do(t.Context(), env.action(t, "complete-task"), map[string]string{"id": "t1"})
	wantError(t, err, 404, "not_found", "no such task")
}

func TestATaskIsAResourceWithItsCompleteAction(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("t1", "p-work", "Write report")
	e, err := env.c.Get(t.Context(), "/tasks/t1")
	if err != nil {
		t.Fatal(err)
	}
	p := props(t, e)
	if e.Title != "Write report" || p["list"] != "Work" || p["done"] != false {
		t.Errorf("task = %q %v", e.Title, p)
	}
	if len(e.Actions) != 1 || e.Actions[0].Name != "complete-task" {
		t.Errorf("actions = %+v, want complete-task", e.Actions)
	}
}

func TestATickTickFaultIsBadGatewayWithoutItsBody(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.unauthorized = true
	_, err := env.c.Get(t.Context(), "/")
	wantError(t, err, 502, "upstream", "TickTick answered 401")
	if !env.login {
		t.Error("a 401 in a handler must tell Serve to stop")
	}
	_, err = env.c.Do(t.Context(), env.action(t, "add-task"), map[string]string{"title": "x"})
	wantError(t, err, 502, "upstream", "TickTick answered 401")
}

func TestTheSyncActionPollsAndAnswersTheHomeWithTheCount(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.finish("t1", now.Add(-time.Hour)) // the clock of the poller is now: the cursor keeps it
	e, err := env.c.Do(t.Context(), env.action(t, "sync"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := props(t, e)["written"].(interface{ String() string }).String(); got != "1" {
		t.Errorf("written = %s, want 1", got)
	}
	if len(env.life.got()) != 1 {
		t.Errorf("captures = %+v, want 1", env.life.got())
	}
	e, err = env.c.Do(t.Context(), env.action(t, "sync"), nil)
	if err != nil || props(t, e)["written"].(interface{ String() string }).String() != "0" {
		t.Errorf("the second sync = %+v, %v, want written 0", e, err)
	}
}

func TestTheSyncActionAnswersALifelogFaultWithoutItsWords(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.finish("t1", now.Add(-time.Hour)) // the clock of the poller is now: the cursor keeps it
	env.life.refuse = 422
	_, err := env.c.Do(t.Context(), env.action(t, "sync"), nil)
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 502 {
		t.Fatalf("err = %v, want 502", err)
	}
	if msg := props(t, ce.Entity)["message"].(string); !strings.Contains(msg, "t1") || strings.Contains(msg, "words") {
		t.Errorf("message = %q, want the task id and no words", msg)
	}
}
