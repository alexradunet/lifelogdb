package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"lifelog/internal/client"
)

// idOfSelf is the id at the end of an entity's self link.
func idOfSelf(t *testing.T, href string) string {
	t.Helper()
	href, _, _ = strings.Cut(href, "?")
	id := href[strings.LastIndex(href, "/")+1:]
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		t.Fatalf("self %q ends in no id", href)
	}
	return id
}

// TestDeletePagesAtOnce: tombstone-pages is offered on the files list and on no other list; it tombstones every id
// named in one write, or none of them.
func TestDeletePagesAtOnce(t *testing.T) {
	c, h := fresh(t)
	ctx := t.Context()
	add := find(must(c.Get(ctx, "/")), "add-file")
	var ids []string
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a", "Memo 3.m4a"} {
		e := must(c.Do(ctx, add, map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+1), 32), "mime": "audio/mp4"}))
		ids = append(ids, idOfSelf(t, hrefOf(e, "self")))
	}
	del := find(must(c.Get(ctx, "/files")), "tombstone-pages")
	if del.Name == "" || !del.Danger || len(del.Fields) != 1 || del.Fields[0].Name != "ids" || !del.Fields[0].Required {
		t.Fatalf("the files list's delete: %+v", del)
	}
	for _, list := range []string{"/people", "/places", "/ghosts"} {
		if find(must(c.Get(ctx, list)), "tombstone-pages").Name != "" {
			t.Errorf("%s offers tombstone-pages", list)
		}
	}
	deleted := func(id string) bool {
		t.Helper()
		return slices.Contains(must(c.Get(ctx, "/pages/"+id)).Class, "deleted")
	}

	// Commas and spaces separate the ids; an id named twice counts once.
	done := must(c.Do(ctx, del, map[string]string{"ids": ids[0] + ", " + ids[1] + " " + ids[0]}))
	if done.Class[0] != "tombstoned" || props(done)["count"] != json.Number("2") || props(done)["kind"] != "pages" || len(done.Entities) != 2 ||
		done.Entities[0].Href != "/pages/"+ids[0] || done.Entities[1].Href != "/pages/"+ids[1] || done.Result != "Deleted 2 pages." {
		t.Fatalf("the answer: %+v %v %v", done.Class, props(done), done.Result)
	}
	if !deleted(ids[0]) || !deleted(ids[1]) || deleted(ids[2]) {
		t.Fatalf("after the delete: %v %v %v", deleted(ids[0]), deleted(ids[1]), deleted(ids[2]))
	}
	if rows := props(must(c.Get(ctx, "/files")))["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 3.m4a" {
		t.Errorf("the files list after the delete: %v", rows)
	}

	// An id no page has refuses the whole write: the live page named beside it stays live.
	if _, err := c.Do(ctx, del, map[string]string{"ids": ids[2] + " 424242"}); !clientStatus(err, 404) {
		t.Fatalf("an id no page has: %v", err)
	}
	if deleted(ids[2]) {
		t.Error("a refused delete tombstoned the live page named beside the missing id")
	}
	for _, bad := range []string{"", " , ", "abc", "0", "-1", "1.5"} {
		if _, err := c.Do(ctx, del, map[string]string{"ids": bad}); !clientStatus(err, 422) {
			t.Errorf("ids %q: %v", bad, err)
		}
	}
	many := make([]string, 501)
	for i := range many {
		many[i] = strconv.Itoa(i + 1)
	}
	if _, err := c.Do(ctx, del, map[string]string{"ids": strings.Join(many, " ")}); !clientStatus(err, 422) {
		t.Errorf("501 ids: %v", err)
	}

	// A page tombstoned already is left as it is; one page is said in the singular.
	if again := must(c.Do(ctx, del, map[string]string{"ids": ids[0]})); again.Result != "Deleted 1 page." {
		t.Errorf("a second delete: %v", again.Result)
	}
	// Every writer may run it: it is a danger, not the owner's alone.
	agent := client.InProcess(h, "agent:test")
	must(agent.Do(ctx, del, map[string]string{"ids": ids[2]}))
	if !deleted(ids[2]) {
		t.Error("an agent's delete did nothing")
	}
	// The browser's form sends the field once per checked box.
	if _, err := c.Do(ctx, find(must(c.Get(ctx, "/pages/"+ids[2])), "revive"), nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "/pages/tombstone", strings.NewReader(url.Values{"ids": {ids[1], ids[2]}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !deleted(ids[2]) {
		t.Errorf("ids sent once per box: %d %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteTasksAtOnce: tombstone-tasks is offered on the tasks list; it needs no version, tombstones every task
// named in one write or none, and revive-task undoes each.
func TestDeleteTasksAtOnce(t *testing.T) {
	c, _ := fresh(t)
	ctx := t.Context()
	create := find(must(c.Get(ctx, "/")), "create-task")
	var ids []string
	for _, label := range []string{"Buy seeds", "Water", "Prune"} {
		ids = append(ids, idOfSelf(t, hrefOf(must(c.Do(ctx, create, map[string]string{"label": label})), "self")))
	}
	del := find(must(c.Get(ctx, "/tasks")), "tombstone-tasks")
	if del.Name == "" || !del.Danger || len(del.Fields) != 1 || del.Fields[0].Name != "ids" {
		t.Fatalf("the tasks list's delete: %+v", del)
	}
	labels := func(path string) (live, gone []string) {
		t.Helper()
		for _, x := range props(must(c.Get(ctx, path)))["tasks"].([]any) {
			task := x.(map[string]any)
			if task["deleted_at"] != nil {
				gone = append(gone, task["label"].(string))
			} else {
				live = append(live, task["label"].(string))
			}
		}
		return
	}

	done := must(c.Do(ctx, del, map[string]string{"ids": ids[0] + "," + ids[1]}))
	if done.Class[0] != "tombstoned" || props(done)["count"] != json.Number("2") || props(done)["kind"] != "tasks" ||
		done.Entities[0].Href != "/tasks/"+ids[0] || done.Result != "Deleted 2 tasks." {
		t.Fatalf("the answer: %+v %v %v", done.Class, props(done), done.Result)
	}
	if live, gone := labels("/tasks"); !slices.Equal(live, []string{"Prune"}) || gone != nil {
		t.Errorf("the list after the delete: %v %v", live, gone)
	}
	if live, gone := labels("/tasks?include_deleted=1"); !slices.Equal(live, []string{"Prune"}) || !slices.Equal(gone, []string{"Buy seeds", "Water"}) {
		t.Errorf("the list with the deleted: %v %v", live, gone)
	}

	// An id no task has refuses the whole write: the live task named beside it stays live.
	if _, err := c.Do(ctx, del, map[string]string{"ids": ids[2] + " 424242"}); !clientStatus(err, 404) {
		t.Fatalf("an id no task has: %v", err)
	}
	if live, _ := labels("/tasks"); !slices.Equal(live, []string{"Prune"}) {
		t.Errorf("a refused delete tombstoned the live task: %v", live)
	}
	if _, err := c.Do(ctx, del, map[string]string{"ids": ""}); !clientStatus(err, 422) {
		t.Errorf("no ids: %v", err)
	}
	// A task tombstoned already is left as it is, and revive-task brings one back.
	if again := must(c.Do(ctx, del, map[string]string{"ids": ids[0]})); again.Result != "Deleted 1 task." {
		t.Errorf("a second delete: %v", again.Result)
	}
	gone := must(c.Get(ctx, "/tasks/"+ids[0]))
	if revive := find(gone, "revive-task"); revive.Name == "" {
		t.Fatalf("a deleted task offers %s", names(gone))
	} else {
		must(c.Do(ctx, revive, nil))
	}
	if live, _ := labels("/tasks"); !slices.Equal(live, []string{"Buy seeds", "Prune"}) {
		t.Errorf("after the revive: %v", live)
	}
}

// TestTheListsDeleteWhatIsChecked follows the browser: Files and Tasks draw a box on each row inside one form, the
// form posts the checked ids and returns to the list with the count, and a post with no box checked comes back to
// the list with the refusal.
func TestTheListsDeleteWhatIsChecked(t *testing.T) {
	c, h := fresh(t)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	var files, tasks []string
	for i, title := range []string{"Scan 1.pdf", "Scan 2.pdf"} {
		e := must(c.Do(ctx, find(root, "add-file"), map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+7), 32), "mime": "application/pdf"}))
		files = append(files, idOfSelf(t, hrefOf(e, "self")))
	}
	for _, label := range []string{"Buy seeds", "Water"} {
		tasks = append(tasks, idOfSelf(t, hrefOf(must(c.Do(ctx, find(root, "create-task"), map[string]string{"label": label})), "self")))
	}

	list := browse(t, h, "/files")
	for _, want := range []string{`<form method="POST" action="/pages/tombstone" data-action="tombstone-pages" class="pick-form">`,
		`<input type="hidden" name="back" value="/files">`, `<div class="pick-bar"><span class="desc">Check the rows to delete, then delete them.</span><button class="danger">Delete selected</button></div></form>`,
		`<input type="checkbox" name="ids" value="` + files[0] + `" aria-label="Select Scan 1.pdf">`, `name="ids" value="` + files[1] + `"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the files list lacks %s", want)
		}
	}
	if strings.Contains(browse(t, h, "/people"), `data-action="tombstone-pages"`) {
		t.Error("the people list draws the boxes")
	}
	// The bar comes after the rows, so a CSS counter can count the checked boxes on its button (plan 108).
	if bar, last := strings.Index(list, `class="pick-bar"`), strings.LastIndex(list, `name="ids"`); bar < last || !strings.Contains(list[bar:], "</form>") {
		t.Error("the bar is not after the rows and before the end of the form")
	}
	rec := submit(h, "/pages/tombstone", url.Values{"ids": files, "back": {"/files"}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form: %d %.300s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !feedbackLocation(loc, "/files") {
		t.Fatalf("the form returned to %q", loc)
	}
	after := browse(t, h, loc)
	if !strings.Contains(after, "Deleted 2 pages.") || strings.Contains(after, `href="/pages/`+files[0]+`"`) || strings.Contains(after, `href="/pages/`+files[1]+`"`) {
		t.Errorf("the list after the delete: %.600s", after)
	}
	rec = submit(h, "/pages/tombstone", url.Values{"back": {"/files"}}, "")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) || !strings.Contains(rec.Body.String(), "missing field ids") || !strings.Contains(rec.Body.String(), "<h1>Files</h1>") {
		t.Errorf("no box checked: %d %.300s", rec.Code, rec.Body.String())
	}

	list = browse(t, h, "/tasks")
	for _, want := range []string{`action="/tasks/tombstone" data-action="tombstone-tasks" class="pick-form">`, `<input type="hidden" name="back" value="/tasks">`,
		`<input type="checkbox" name="ids" value="` + tasks[0] + `" aria-label="Select Buy seeds">`, `name="ids" value="` + tasks[1] + `"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the tasks list lacks %s", want)
		}
	}
	rec = submit(h, "/tasks/tombstone", url.Values{"ids": tasks[:1], "back": {"/tasks"}}, "")
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/tasks") {
		t.Fatalf("the tasks form: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	after = browse(t, h, rec.Header().Get("Location"))
	if !strings.Contains(after, "Deleted 1 task.") || strings.Contains(after, `href="/tasks/`+tasks[0]+`"`) || !strings.Contains(after, `href="/tasks/`+tasks[1]+`"`) {
		t.Errorf("the tasks list after the delete: %.600s", after)
	}
	// A deleted task shown has no box to check: its row's box is disabled.
	shown := browse(t, h, "/tasks?include_deleted=1")
	if strings.Contains(shown, `name="ids" value="`+tasks[0]+`"`) || !strings.Contains(shown, `aria-label="Buy seeds is deleted"`) {
		t.Errorf("a deleted task's row: %.600s", shown)
	}
}
