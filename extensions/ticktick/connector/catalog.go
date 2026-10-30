package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"lifelog-ticktick/ticktick"
	"lifelog/app"
	"lifelog/siren"
)

const (
	inbox  = "Inbox" // the name of the list that TickTick does not report
	dayFmt = "2006-01-02"
)

// catalog holds what the handlers share. The mutex guards project only.
type catalog struct {
	p     *Poller
	lists []ticktick.Project

	mu      sync.Mutex
	project map[string]string // task id -> list id, learned from the lists and from the tasks made here
}

// actions are the three actions of the app. list takes the names of the lists read at the start, and the Inbox.
func actions(lists []ticktick.Project) []siren.Action {
	options := []string{inbox}
	for _, l := range lists {
		options = append(options, l.Name)
	}
	return []siren.Action{
		{Name: "add-task", Method: "POST", Href: "/tasks", Title: "Add a task",
			Description: "Make a task in TickTick. list is the name of a list (the Inbox when absent). due is a day, YYYY-MM-DD.",
			Hint:        "Make a task in TickTick.",
			Fields: []siren.Field{
				{Name: "title", Type: "text", Title: "Title", Required: true},
				{Name: "list", Type: "text", Title: "List", Options: options},
				{Name: "due", Type: "date", Title: "Due day"},
				{Name: "notes", Type: "textarea", Title: "Notes", Rows: 4},
			}},
		{Name: "complete-task", Method: "POST", Href: "/tasks/{id}/complete", Title: "Complete a task",
			Description: "Mark a task of TickTick as done. The next poll writes its Done: entry on the day.",
			Fields:      []siren.Field{{Name: "id", Type: "text", Title: "Task", Required: true, In: "path"}}},
		{Name: "sync", Method: "POST", Href: "/sync", Title: "Sync now",
			Description: "Read the tasks that TickTick shows as done and write each as an entry of its day, once. The app does this every poll interval too."},
	}
}

// Catalog is the handler of the app. Its title is TickTick. It serves the home (Today), the three actions, and the
// resources that the pages link to: GET /next7, /lists/{id}, /lists/{id}/done, /tasks/{id} and /done.
// lists are the lists that TickTick reported at the start.
func Catalog(p *Poller, lists []ticktick.Project) http.Handler {
	c := &catalog{p: p, lists: lists, project: map[string]string{}}
	acts := actions(lists)
	inner := app.Catalog("TickTick", c.home, acts, map[string]func(*http.Request) (*siren.Entity, error){
		"add-task": c.addTask, "complete-task": c.completeTask, "sync": c.sync,
	})
	// app.Catalog routes the actions and the home. A task is a resource that its entities link to, so it has a route here.
	mux := http.NewServeMux()
	mux.Handle("/", inner)
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		e, err := c.showTask(r)
		respond(w, e, err)
	})
	mux.HandleFunc("GET /lists/{id}", func(w http.ResponseWriter, r *http.Request) {
		e, err := c.showList(r)
		respond(w, e, err)
	})
	mux.HandleFunc("GET /next7", func(w http.ResponseWriter, r *http.Request) {
		e, err := c.showNext7(r)
		respond(w, e, err)
	})
	mux.HandleFunc("GET /lists/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		e, err := c.showListDone(r)
		respond(w, e, err)
	})
	mux.HandleFunc("GET /done", func(w http.ResponseWriter, r *http.Request) {
		e, err := c.showDone(r)
		respond(w, e, err)
	})
	return mux
}

// respond answers a GET route: the entity, or the error as the error entity. An error that is not an app.Error is 500.
func respond(w http.ResponseWriter, e *siren.Entity, err error) {
	var ae *app.Error
	switch {
	case errors.As(err, &ae):
		app.Fail(w, ae.Status, ae.Code, ae.Msg)
	case err != nil:
		app.Fail(w, http.StatusInternalServerError, siren.CodeInternal, "internal error")
	default:
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		_ = siren.WriteJSON(w, e, "")
	}
}

// upstream turns an error of TickTick into the error of the catalog. TickTick's body is not in it.
func (c *catalog) upstream(err error) error {
	err = c.p.check(err)
	var se *ticktick.StatusError
	switch {
	case errors.Is(err, ErrLoginAgain):
		return &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: "TickTick answered 401"}
	case errors.As(err, &se):
		return &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: fmt.Sprintf("TickTick answered %d", se.Status)}
	}
	return err
}

func invalid(msg string) error {
	return &app.Error{Status: http.StatusUnprocessableEntity, Code: siren.CodeInvalid, Msg: msg}
}

func noTask() error {
	return &app.Error{Status: http.StatusNotFound, Code: siren.CodeNotFound, Msg: "no such task"}
}

func noList() error {
	return &app.Error{Status: http.StatusNotFound, Code: siren.CodeNotFound, Msg: "no such list"}
}

func isStatus(err error, status int) bool {
	var se *ticktick.StatusError
	return errors.As(err, &se) && se.Status == status
}

func (c *catalog) remember(listID string, tasks ...ticktick.Task) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range tasks {
		c.project[t.ID] = listID
	}
}

func (c *catalog) hasList(id string) bool {
	return slices.ContainsFunc(c.lists, func(l ticktick.Project) bool { return l.ID == id })
}

func (c *catalog) listName(id string) string {
	for _, l := range c.lists {
		if l.ID == id {
			return l.Name
		}
	}
	return inbox
}

// find reads a task by its id. TickTick wants the list too: the id is looked up where this app learned it, else in
// the open tasks of the lists.
func (c *catalog) find(ctx context.Context, id string) (ticktick.Task, error) {
	c.mu.Lock()
	listID, known := c.project[id]
	c.mu.Unlock()
	if known {
		t, err := c.p.TT.Task(ctx, listID, id)
		if isStatus(err, http.StatusNotFound) {
			return t, noTask()
		}
		return t, c.upstream(err)
	}
	for _, l := range c.lists {
		tasks, err := c.p.TT.ProjectData(ctx, l.ID)
		if err != nil {
			return ticktick.Task{}, c.upstream(err)
		}
		c.remember(l.ID, tasks...)
		for _, t := range tasks {
			if t.ID == id {
				if t.ProjectID == "" {
					t.ProjectID = l.ID
				}
				return t, nil
			}
		}
	}
	// A done task has left its list. TickTick lists it as completed.
	done, err := c.p.TT.Completed(ctx)
	if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
		return ticktick.Task{}, noTask()
	}
	if err != nil {
		return ticktick.Task{}, c.upstream(err)
	}
	for _, t := range done {
		if t.ID == id {
			if t.ProjectID != "" {
				c.remember(t.ProjectID, t)
			}
			return t, nil
		}
	}
	return ticktick.Task{}, noTask()
}

// taskEntity is a task as a page. It has no action: Complete is the round button of the row, and the catalog action
// complete-task stays for the CLI, the MCP and WhatsApp.
func (c *catalog) taskEntity(t ticktick.Task) *siren.Entity {
	props := map[string]any{"id": t.ID, "title": t.Title, "list": c.listName(t.ProjectID), "done": t.Status == 2}
	if t.Content != "" {
		props["notes"] = t.Content
	}
	if due, ok := dueDay(t); ok {
		props["due"] = due
	}
	if p := t.PriorityWord(); p != "" {
		props["priority"] = p
	}
	var extra []siren.Link
	if c.hasList(t.ProjectID) { // the Inbox has no page: TickTick does not list its tasks
		extra = append(extra, siren.Link{Rel: []string{"list"}, Href: listHref(t.ProjectID), Title: c.listName(t.ProjectID)})
	}
	return &siren.Entity{Class: []string{"task"}, Title: t.Title, Properties: props,
		Links: c.links(taskHref(t.ID), t.Title, nil, extra...)}
}

func taskHref(id string) string { return "/tasks/" + url.PathEscape(id) }

func listHref(id string) string { return "/lists/" + url.PathEscape(id) }

// links are the links of every page: self, the index, then the side column of the generic view (Today, Next 7 days,
// one link for each list, Done), then the links that only this page has. counts are the open tasks of each list by
// list id. A page that did not read every list passes nil, and the lists show no count.
func (c *catalog) links(href, title string, counts map[string]int, extra ...siren.Link) []siren.Link {
	links := []siren.Link{
		{Rel: []string{"self"}, Href: href, Title: title},
		{Rel: []string{"index"}, Href: "/", Title: "TickTick"},
		{Rel: []string{"nav"}, Href: "/", Title: "Today"},
		{Rel: []string{"nav"}, Href: "/next7", Title: "Next 7 days"},
	}
	for _, l := range c.lists {
		name := l.Name
		if n, ok := counts[l.ID]; ok {
			name = fmt.Sprintf("%s · %d", name, n)
		}
		links = append(links, siren.Link{Rel: []string{"nav"}, Class: []string{"list"}, Href: listHref(l.ID), Title: name})
	}
	links = append(links, siren.Link{Rel: []string{"done"}, Href: "/done", Title: "Done"})
	return append(links, extra...)
}

func (c *catalog) showTask(r *http.Request) (*siren.Entity, error) {
	t, err := c.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return c.taskEntity(t), nil
}

// home is Today: the open tasks due today or before.
func (c *catalog) home(r *http.Request) (*siren.Entity, error) {
	return c.homeEntity(r.Context(), -1)
}

// openTasks reads the open tasks of a list and remembers where they are.
func (c *catalog) openTasks(ctx context.Context, l ticktick.Project) ([]ticktick.Task, error) {
	tasks, err := c.p.TT.ProjectData(ctx, l.ID)
	if err != nil {
		return nil, c.upstream(err)
	}
	c.remember(l.ID, tasks...)
	return tasks, nil
}

// allOpen reads the open tasks of every list, list by list, and counts them by list id.
func (c *catalog) allOpen(ctx context.Context) ([]ticktick.Task, map[string]int, error) {
	var all []ticktick.Task
	counts := map[string]int{}
	for _, l := range c.lists {
		tasks, err := c.openTasks(ctx, l)
		if err != nil {
			return nil, nil, err
		}
		counts[l.ID] = len(tasks)
		all = append(all, tasks...)
	}
	return all, counts, nil
}

// dueDay is the due day of a task, YYYY-MM-DD.
func dueDay(t ticktick.Task) (string, bool) {
	due, ok := t.Due()
	if !ok {
		return "", false
	}
	return due.Format(dayFmt), true
}

// sortOpen sorts open tasks by due day (no due day last), then by priority (high first). A task that ties keeps the
// order of TickTick.
func sortOpen(tasks []ticktick.Task) {
	slices.SortStableFunc(tasks, func(a, b ticktick.Task) int {
		da, oka := dueDay(a)
		db, okb := dueDay(b)
		switch {
		case oka && okb && da != db:
			return strings.Compare(da, db)
		case oka != okb:
			if oka {
				return -1
			}
			return 1
		}
		return b.Priority - a.Priority
	})
}

// taskRow is a task as a row of a list. The chips are its due day, its priority and, when the rows of many lists are
// mixed, its list. An open task has the round button Complete.
func (c *catalog) taskRow(t ticktick.Task, withList bool) siren.Link {
	href := taskHref(t.ID)
	row := siren.Link{Rel: []string{"item"}, Class: []string{"task"}, Href: href, Title: t.Title}
	props := map[string]any{}
	if day, ok := dueDay(t); ok {
		props["due"] = day
	}
	if p := t.PriorityWord(); p != "" {
		props["priority"] = p
	}
	if withList {
		props["list"] = c.listName(t.ProjectID)
	}
	if len(props) > 0 {
		row.Properties = props
	}
	if t.Status != 2 {
		row.Actions = []siren.Action{{Name: "complete-task", Method: "POST", Href: href + "/complete", Title: "Complete"}}
	}
	return row
}

func (c *catalog) rows(tasks []ticktick.Task, withList bool) []siren.Link {
	var rows []siren.Link
	for _, t := range tasks {
		rows = append(rows, c.taskRow(t, withList))
	}
	return rows
}

// addTaskWith is the action add-task of the catalog, with the value of one field given.
func (c *catalog) addTaskWith(field, value string) siren.Action {
	add := actions(c.lists)[0]
	for i := range add.Fields {
		if add.Fields[i].Name == field {
			add.Fields[i].Value = value
		}
	}
	return add
}

// homeEntity is Today: the tasks due today or before across the lists, with add-task (due today) and sync.
// written >= 0 adds the count that a sync wrote.
func (c *catalog) homeEntity(ctx context.Context, written int) (*siren.Entity, error) {
	today := c.p.Now().Format(dayFmt)
	names := make([]string, 0, len(c.lists))
	for _, l := range c.lists {
		names = append(names, l.Name)
	}
	tasks, counts, err := c.allOpen(ctx)
	if err != nil {
		return nil, err
	}
	var due []ticktick.Task
	overdue := 0
	for _, t := range tasks {
		day, ok := dueDay(t)
		if !ok || day > today {
			continue
		}
		if day < today {
			overdue++
		}
		due = append(due, t)
	}
	sortOpen(due)
	props := map[string]any{"lists": names, "open": len(tasks), "overdue": overdue}
	if written >= 0 {
		props["written"] = written
	}
	return &siren.Entity{Class: []string{"ticktick"}, Title: "TickTick", Properties: props, Entities: c.rows(due, true),
		Actions: []siren.Action{c.addTaskWith("due", today), actions(c.lists)[2]},
		Links:   c.links("/", "TickTick", counts)}, nil
}

// showNext7 is the open tasks due from today to six days later, across the lists.
func (c *catalog) showNext7(r *http.Request) (*siren.Entity, error) {
	now := c.p.Now()
	first, last := now.Format(dayFmt), now.AddDate(0, 0, 6).Format(dayFmt)
	tasks, _, err := c.allOpen(r.Context())
	if err != nil {
		return nil, err
	}
	var week []ticktick.Task
	for _, t := range tasks {
		if day, ok := dueDay(t); ok && day >= first && day <= last {
			week = append(week, t)
		}
	}
	sortOpen(week)
	return &siren.Entity{Class: []string{"next7"}, Title: "Next 7 days",
		Properties: map[string]any{"open": len(week)}, Entities: c.rows(week, true),
		Actions: []siren.Action{actions(c.lists)[0]},
		Links:   c.links("/next7", "Next 7 days", nil)}, nil
}

// findList is the list of the request, or 404.
func (c *catalog) findList(r *http.Request) (ticktick.Project, error) {
	i := slices.IndexFunc(c.lists, func(l ticktick.Project) bool { return l.ID == r.PathValue("id") })
	if i < 0 {
		return ticktick.Project{}, noList()
	}
	return c.lists[i], nil
}

// showList is the open tasks of one list, with add-task and the list filled in.
func (c *catalog) showList(r *http.Request) (*siren.Entity, error) {
	l, err := c.findList(r)
	if err != nil {
		return nil, err
	}
	tasks, err := c.openTasks(r.Context(), l)
	if err != nil {
		return nil, err
	}
	today := c.p.Now().Format(dayFmt)
	overdue := 0
	for _, t := range tasks {
		if day, ok := dueDay(t); ok && day < today {
			overdue++
		}
	}
	sortOpen(tasks)
	done := siren.Link{Rel: []string{"done"}, Href: listHref(l.ID) + "/done", Title: "Done in this list"}
	return &siren.Entity{Class: []string{"list"}, Title: l.Name,
		Properties: map[string]any{"open": len(tasks), "overdue": overdue}, Entities: c.rows(tasks, false),
		Actions: []siren.Action{c.addTaskWith("list", l.Name)},
		Links:   c.links(listHref(l.ID), l.Name, nil, done)}, nil
}

// finished is a done task and the instant of its completion.
type finished struct {
	t  ticktick.Task
	at time.Time
}

// recentDone is the tasks done in the last firstWindow, newest first. listID limits them to one list; "" is all. It
// returns ticktick.ErrNoCompletedEndpoint when the server has no call for done tasks.
func (c *catalog) recentDone(ctx context.Context, listID string) ([]finished, error) {
	tasks, err := c.p.TT.Completed(ctx)
	if err != nil {
		if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
			return nil, err
		}
		return nil, c.upstream(err)
	}
	since := c.p.Now().Add(-firstWindow)
	var recent []finished
	for _, t := range tasks {
		if at, ok := t.Done(); ok && !at.Before(since) && (listID == "" || t.ProjectID == listID) {
			recent = append(recent, finished{t, at})
		}
	}
	slices.SortStableFunc(recent, func(a, b finished) int { return b.at.Compare(a.at) })
	return recent, nil
}

// doneEntity is a page of done tasks. A server without the call has none, and says so.
func (c *catalog) doneEntity(ctx context.Context, href, title, listID string) (*siren.Entity, error) {
	e := &siren.Entity{Class: []string{"done"}, Title: title, Links: c.links(href, title, nil)}
	recent, err := c.recentDone(ctx, listID)
	if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
		e.Properties = map[string]any{"count": 0, "note": "the server has no list of done tasks"}
		return e, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range recent {
		row := c.taskRow(f.t, listID == "")
		props, _ := row.Properties.(map[string]any)
		if props == nil {
			props = map[string]any{}
		}
		props["done"] = f.at.Format(dayFmt)
		row.Properties = props
		e.Entities = append(e.Entities, row)
	}
	e.Properties = map[string]any{"count": len(recent)}
	return e, nil
}

// showDone is the tasks done in the last firstWindow, newest first.
func (c *catalog) showDone(r *http.Request) (*siren.Entity, error) {
	return c.doneEntity(r.Context(), "/done", "Done", "")
}

// showListDone is the same for one list.
func (c *catalog) showListDone(r *http.Request) (*siren.Entity, error) {
	l, err := c.findList(r)
	if err != nil {
		return nil, err
	}
	return c.doneEntity(r.Context(), listHref(l.ID)+"/done", "Done · "+l.Name, l.ID)
}

func (c *catalog) addTask(r *http.Request) (*siren.Entity, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return nil, invalid("title is required")
	}
	t := ticktick.Task{Title: title, Content: r.FormValue("notes")}
	if name := r.FormValue("list"); name != "" && name != inbox {
		for _, l := range c.lists {
			if l.Name == name {
				t.ProjectID = l.ID
			}
		}
		if t.ProjectID == "" {
			return nil, invalid("list is not a list of TickTick: " + name)
		}
	}
	if due := r.FormValue("due"); due != "" {
		day, err := time.ParseInLocation(dayFmt, due, time.Local)
		if err != nil {
			return nil, invalid("due must be a day, YYYY-MM-DD")
		}
		t.DueDate, t.IsAllDay, t.TimeZone = day.Format(ticktick.TimeLayout), true, time.Local.String()
	}
	made, err := c.p.TT.Create(r.Context(), t)
	if err != nil {
		return nil, c.upstream(err)
	}
	if made.ProjectID == "" {
		made.ProjectID = t.ProjectID
	}
	c.remember(made.ProjectID, made)
	return c.taskEntity(made), nil
}

func (c *catalog) completeTask(r *http.Request) (*siren.Entity, error) {
	t, err := c.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if err := c.p.TT.Complete(r.Context(), t.ProjectID, t.ID); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, noTask()
		}
		return nil, c.upstream(err)
	}
	t.Status = 2
	return c.taskEntity(t), nil
}

// sync polls now. It waits when a poll is in progress (Poller.Poll).
func (c *catalog) sync(r *http.Request) (*siren.Entity, error) {
	n, err := c.p.Poll(r.Context())
	if err != nil {
		var se *ticktick.StatusError
		if errors.Is(err, ErrLoginAgain) || errors.As(err, &se) {
			return nil, c.upstream(err)
		}
		return nil, &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: "the poll failed: " + err.Error()}
	}
	return c.homeEntity(r.Context(), n)
}
