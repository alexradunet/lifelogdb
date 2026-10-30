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
	if len(e.Actions) != 0 {
		t.Errorf("actions = %+v, want none: Complete is the button of the row", e.Actions)
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

func TestAnUnknownListIsNotFound(t *testing.T) {
	env := newCatalogEnv(t)
	_, err := env.c.Get(t.Context(), "/lists/nope")
	wantError(t, err, 404, "not_found", "no such list")
}

// num is a number property: the client decodes numbers as json.Number.
func num(t *testing.T, e *siren.Entity, key string) string {
	t.Helper()
	n, ok := props(t, e)[key].(interface{ String() string })
	if !ok {
		t.Fatalf("property %s of %q = %v, want a number", key, e.Title, props(t, e)[key])
	}
	return n.String()
}

// rowProps are the properties of a row as strings, "" when it has none.
func rowProps(l siren.Link) map[string]any {
	m, _ := l.Properties.(map[string]any)
	return m
}

// titles are the titles of the embedded rows, in order.
func titles(e *siren.Entity) []string {
	var got []string
	for _, l := range e.Entities {
		got = append(got, l.Title)
	}
	return got
}

// wantNav checks that the links of a page are self, then the side column with these "TITLE HREF" pairs. The first
// link of the side column is Today, which is also the index: one link, so the side column draws it once.
func wantNav(t *testing.T, e *siren.Entity, self string, side ...string) {
	t.Helper()
	if len(e.Links) < 1+len(side) {
		t.Fatalf("%q has %d links, want at least %d", e.Title, len(e.Links), 1+len(side))
	}
	if l := e.Links[0]; !slices.Equal(l.Rel, []string{"self"}) || l.Href != self {
		t.Errorf("first link of %q = %+v, want self %s", e.Title, l, self)
	}
	if l := e.Links[1]; !slices.Equal(l.Rel, []string{"index", "nav"}) || l.Href != "/" || l.Title != "Today" {
		t.Errorf("second link of %q = %+v, want Today, the index", e.Title, l)
	}
	var got []string
	for _, l := range e.Links[1 : 1+len(side)] {
		got = append(got, l.Title+" "+l.Href)
	}
	if !slices.Equal(got, side) {
		t.Errorf("side column of %q = %q, want %q", e.Title, got, side)
	}
	seen := map[string]bool{}
	for _, l := range e.Links[1:] {
		if seen[l.Href] {
			t.Errorf("%q links %s twice beside self", e.Title, l.Href)
		}
		seen[l.Href] = true
	}
}

var sideWithoutCounts = []string{"Today /", "Next 7 days /next7", "Home /lists/p-home", "Work /lists/p-work", "Done /done"}

func TestTheHomeIsTodayWithTheRowsDueTodayOrBefore(t *testing.T) {
	env := newCatalogEnv(t)
	due := func(day string) string { return day + "T00:00:00.000+0000" }
	env.tick.add("late", "p-home", "Overdue").DueDate = due("2030-03-01")
	hi := env.tick.add("hi", "p-home", "Due today, high")
	hi.DueDate, hi.Priority = due("2030-03-05"), 5
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
	if num(t, home, "open") != "5" || num(t, home, "overdue") != "1" {
		t.Errorf("open = %v, overdue = %v, want 5 and 1", props(t, home)["open"], props(t, home)["overdue"])
	}
	if lists, _ := props(t, home)["lists"].([]any); len(lists) != 2 || lists[0] != "Home" || lists[1] != "Work" {
		t.Errorf("lists = %v, want Home Work", props(t, home)["lists"])
	}
	// The row of the due day 2030-03-01 comes first, then the high one of 2030-03-05, then the one with no priority.
	if want := []string{"Overdue", "Due today, high", "Due today"}; !slices.Equal(titles(home), want) {
		t.Errorf("rows = %q, want %q", titles(home), want)
	}
	for _, l := range home.Entities {
		if slices.Contains(l.Class, "list") || !slices.Contains(l.Class, "task") {
			t.Errorf("embedded %+v, want only task rows", l)
		}
		if rowProps(l)["list"] == nil {
			t.Errorf("row %q has no list chip: the rows of many lists are mixed", l.Title)
		}
	}
	wantNav(t, home, "/", "Today /", "Next 7 days /next7", "Home · 3 /lists/p-home", "Work · 2 /lists/p-work", "Done /done")
	var acts []string
	for _, a := range home.Actions {
		acts = append(acts, a.Name)
	}
	if want := []string{"add-task", "sync"}; !slices.Equal(acts, want) {
		t.Fatalf("home actions = %v, want %v", acts, want)
	}
	if f := home.Actions[0].Field("due"); f == nil || f.Value != "2030-03-05" {
		t.Errorf("due field = %+v, want the value 2030-03-05 (today)", f)
	}
}

func TestARowHasItsChipsAndTheCompleteButtonOfAnOpenTask(t *testing.T) {
	env := newCatalogEnv(t)
	a := env.tick.add("a", "p-work", "Write report")
	a.DueDate, a.Priority = "2030-03-09T00:00:00.000+0000", 3
	env.tick.add("b", "p-work", "Plain")

	e, err := env.c.Get(t.Context(), "/lists/p-work")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Write report", "Plain"}; !slices.Equal(titles(e), want) {
		t.Fatalf("rows = %q, want %q: plain titles", titles(e), want)
	}
	row, plain := e.Entities[0], e.Entities[1]
	if !slices.Equal(row.Rel, []string{"item"}) || !slices.Equal(row.Class, []string{"task"}) || row.Href != "/tasks/a" {
		t.Errorf("row = %+v, want rel item, class task, href /tasks/a", row)
	}
	want := map[string]any{"due": "2030-03-09", "priority": "medium"}
	if got := rowProps(row); len(got) != 2 || got["due"] != want["due"] || got["priority"] != want["priority"] {
		t.Errorf("properties = %v, want %v (no list chip on the page of a list)", got, want)
	}
	if len(row.Actions) != 1 || row.Actions[0].Name != "complete-task" || row.Actions[0].Method != "POST" ||
		row.Actions[0].Href != "/tasks/a/complete" || row.Actions[0].Title != "Complete" || len(row.Actions[0].Fields) != 0 {
		t.Errorf("actions = %+v, want complete-task, POST /tasks/a/complete, Complete, no field", row.Actions)
	}
	if plain.Properties != nil {
		t.Errorf("a row with no chip has properties %v, want none", plain.Properties)
	}
}

func TestTheRowsAreSortedByDueDayThenPriority(t *testing.T) {
	env := newCatalogEnv(t)
	set := func(id, due string, priority int) {
		task := env.tick.add(id, "p-work", id)
		task.Priority = priority
		if due != "" {
			task.DueDate = due + "T00:00:00.000+0000"
		}
	}
	set("nodue-high", "", 5)
	set("d9-low", "2030-03-09", 1)
	set("d7-none", "2030-03-07", 0)
	set("d9-high", "2030-03-09", 5)
	set("nodue-none", "", 0)
	e, err := env.c.Get(t.Context(), "/lists/p-work")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"d7-none", "d9-high", "d9-low", "nodue-high", "nodue-none"}
	if got := titles(e); !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestAListHasItsRowsAddTaskWithTheListAndALinkToItsDoneTasks(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("a", "p-work", "Write report").DueDate = "2030-03-09T00:00:00.000+0000"
	env.tick.add("b", "p-home", "Pay rent")
	env.tick.add("c", "p-work", "Old task").DueDate = "2030-03-01T00:00:00.000+0000"
	env.tick.add("d", "p-work", "Done already")
	env.tick.finish("d", now)

	e, err := env.c.Get(t.Context(), "/lists/p-work")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"list"}) || e.Title != "Work" {
		t.Errorf("list = %v %q, want class list, title Work", e.Class, e.Title)
	}
	if num(t, e, "open") != "2" || num(t, e, "overdue") != "1" {
		t.Errorf("open = %v, overdue = %v, want 2 and 1", props(t, e)["open"], props(t, e)["overdue"])
	}
	if want := []string{"Old task", "Write report"}; !slices.Equal(titles(e), want) {
		t.Errorf("rows = %q, want %q", titles(e), want)
	}
	for _, l := range e.Entities {
		if rowProps(l)["list"] != nil {
			t.Errorf("row %q has a list chip, want none on the page of its list", l.Title)
		}
	}
	wantNav(t, e, "/lists/p-work", sideWithoutCounts...)
	if l := e.Links[len(e.Links)-1]; l.Href != "/lists/p-work/done" || l.Title != "Done in this list" {
		t.Errorf("last link = %+v, want Done in this list", l)
	}
	if len(e.Actions) != 1 || e.Actions[0].Name != "add-task" {
		t.Fatalf("actions = %+v, want add-task only", e.Actions)
	}
	if f := e.Actions[0].Field("list"); f == nil || f.Value != "Work" {
		t.Errorf("list field = %+v, want the value Work", f)
	}
}

func TestNext7KeepsTheDaysFromTodayToSixDaysLater(t *testing.T) {
	env := newCatalogEnv(t) // today is 2030-03-05: the week ends 2030-03-11
	due := func(day string) string { return day + "T00:00:00.000+0000" }
	env.tick.add("d11", "p-work", "In 6 days").DueDate = due("2030-03-11")
	env.tick.add("d12", "p-work", "In 7 days").DueDate = due("2030-03-12")
	env.tick.add("d5", "p-home", "Today").DueDate = due("2030-03-05")
	env.tick.add("d4", "p-home", "Yesterday").DueDate = due("2030-03-04")
	env.tick.add("nodue", "p-home", "No due day")

	e, err := env.c.Get(t.Context(), "/next7")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"next7"}) || e.Title != "Next 7 days" {
		t.Errorf("page = %v %q, want class next7, title Next 7 days", e.Class, e.Title)
	}
	if want := []string{"Today", "In 6 days"}; !slices.Equal(titles(e), want) {
		t.Errorf("rows = %q, want %q", titles(e), want)
	}
	if rowProps(e.Entities[1])["list"] != "Work" {
		t.Errorf("row = %+v, want the list chip Work", e.Entities[1])
	}
	if num(t, e, "open") != "2" {
		t.Errorf("open = %v, want 2", props(t, e)["open"])
	}
	if len(e.Actions) != 1 || e.Actions[0].Name != "add-task" || e.Actions[0].Field("due").Value != nil {
		t.Errorf("actions = %+v, want add-task with no prefill", e.Actions)
	}
	wantNav(t, e, "/next7", sideWithoutCounts...)
}

func TestDoneListsTheTasksOfTheWindowNewestFirst(t *testing.T) {
	env := newCatalogEnv(t) // the clock of the poller is 2030-03-05 12:00; the window starts 2030-02-03 12:00
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.add("t2", "p-work", "Write report")
	env.tick.add("t3", "p-work", "Too old")
	env.tick.add("t4", "p-home", "Still open")
	env.tick.finish("t1", time.Date(2030, 3, 2, 9, 0, 0, 0, time.UTC))
	env.tick.finish("t2", time.Date(2030, 3, 4, 10, 0, 0, 0, time.UTC))
	env.tick.finish("t3", time.Date(2030, 1, 10, 9, 0, 0, 0, time.UTC))

	e, err := env.c.Get(t.Context(), "/done")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"done"}) || e.Title != "Done" {
		t.Errorf("done = %v %q", e.Class, e.Title)
	}
	if want := []string{"Write report", "Pay rent"}; !slices.Equal(titles(e), want) {
		t.Errorf("rows = %q, want %q", titles(e), want)
	}
	for i, want := range []map[string]string{{"done": "2030-03-04", "list": "Work"}, {"done": "2030-03-02", "list": "Home"}} {
		l := e.Entities[i]
		if got := rowProps(l); len(got) != 2 || got["done"] != want["done"] || got["list"] != want["list"] {
			t.Errorf("properties of %q = %v, want %v", l.Title, got, want)
		}
		if len(l.Actions) != 0 {
			t.Errorf("done row %q has actions %+v, want none", l.Title, l.Actions)
		}
	}
	if num(t, e, "count") != "2" {
		t.Errorf("count = %v, want 2", props(t, e)["count"])
	}
	wantNav(t, e, "/done", sideWithoutCounts...)
}

func TestTheDoneTasksOfAListAreOnlyThatListNewestFirst(t *testing.T) {
	env := newCatalogEnv(t)
	for id, list := range map[string]string{"t1": "p-home", "t2": "p-work", "t3": "p-home", "t4": "p-home"} {
		env.tick.add(id, list, "task "+id)
	}
	env.tick.finish("t1", time.Date(2030, 3, 2, 9, 0, 0, 0, time.UTC))
	env.tick.finish("t2", time.Date(2030, 3, 4, 10, 0, 0, 0, time.UTC))
	env.tick.finish("t3", time.Date(2030, 3, 3, 10, 0, 0, 0, time.UTC))
	env.tick.finish("t4", time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)) // out of the window

	e, err := env.c.Get(t.Context(), "/lists/p-home/done")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"done"}) || e.Title != "Done · Home" {
		t.Errorf("page = %v %q, want class done, title Done · Home", e.Class, e.Title)
	}
	if want := []string{"task t3", "task t1"}; !slices.Equal(titles(e), want) {
		t.Errorf("rows = %q, want %q", titles(e), want)
	}
	if got := rowProps(e.Entities[0]); len(got) != 1 || got["done"] != "2030-03-03" {
		t.Errorf("properties = %v, want only done 2030-03-03", got)
	}
	wantNav(t, e, "/lists/p-home/done", sideWithoutCounts...)

	_, err = env.c.Get(t.Context(), "/lists/nope/done")
	wantError(t, err, 404, "not_found", "no such list")
}

func TestTheDoneListsWithoutTheCompletedCallAreEmptyWithANote(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.noCompleted = true
	for _, path := range []string{"/done", "/lists/p-home/done"} {
		e, err := env.c.Get(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if len(e.Entities) != 0 || props(t, e)["note"] != "the server has no list of done tasks" {
			t.Errorf("%s = %+v %v, want no tasks and the note", path, e.Entities, e.Properties)
		}
	}
}

func TestATaskPageHasNoActionAndLinksItsList(t *testing.T) {
	env := newCatalogEnv(t)
	task := env.tick.add("t1", "p-work", "Write report")
	task.Content, task.Priority, task.DueDate = "the notes", 5, "2030-03-09T00:00:00.000+0000"
	e, err := env.c.Get(t.Context(), "/tasks/t1")
	if err != nil {
		t.Fatal(err)
	}
	p := props(t, e)
	if e.Title != "Write report" || p["list"] != "Work" || p["done"] != false || p["due"] != "2030-03-09" ||
		p["priority"] != "high" || p["notes"] != "the notes" {
		t.Errorf("task = %q %v", e.Title, p)
	}
	if len(e.Actions) != 0 {
		t.Errorf("actions = %+v, want none: Complete is the button of the row", e.Actions)
	}
	if l := e.LinkByRel("list"); l == nil || l.Href != "/lists/p-work" || l.Title != "Work" {
		t.Errorf("list link = %+v, want /lists/p-work titled Work", l)
	}
	wantNav(t, e, "/tasks/t1", sideWithoutCounts...)

	// The app has not learned this task: it is not in the open tasks, so it is found among the done ones.
	env.tick.add("t2", "p-home", "Pay rent")
	env.tick.finish("t2", time.Date(2030, 3, 4, 10, 0, 0, 0, time.UTC))
	e, err = env.c.Get(t.Context(), "/tasks/t2")
	if err != nil {
		t.Fatal(err)
	}
	if props(t, e)["done"] != true || len(e.Actions) != 0 {
		t.Errorf("done task = %+v, want done: true and no action", e)
	}
	if l := e.LinkByRel("list"); l == nil || l.Href != "/lists/p-home" {
		t.Errorf("list link = %+v, want /lists/p-home", l)
	}
	if env.tick.callsTo("POST /task/completed") != 1 {
		t.Errorf("the done tasks were read %d times, want 1", env.tick.callsTo("POST /task/completed"))
	}
}

func TestEveryPageHasTheSideColumnAfterSelf(t *testing.T) {
	env := newCatalogEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	for _, path := range []string{"/next7", "/lists/p-home", "/lists/p-home/done", "/done", "/tasks/t1"} {
		e, err := env.c.Get(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		wantNav(t, e, path, sideWithoutCounts...)
	}
	// The answer of the action sync is the home, with the counts.
	e, err := env.c.Do(t.Context(), env.action(t, "sync"), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNav(t, e, "/", "Today /", "Next 7 days /next7", "Home · 1 /lists/p-home", "Work · 0 /lists/p-work", "Done /done")
}
