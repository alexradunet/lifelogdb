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

	"lifelog/internal/api/apitest"
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

// TestDeletePagesAtOnce: tombstone-pages tombstones every id named in one write, or none of them. The lists of Browse
// that offer it are TestBrowseActions'.
func TestDeletePagesAtOnce(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	add := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file")
	var ids []string
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a", "Memo 3.m4a"} {
		e := apitest.Must(t)(c.Do(ctx, add, map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+1), 32), "mime": "audio/mp4"}))
		ids = append(ids, idOfSelf(t, apitest.Href(e, "self")))
	}
	del := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=media")), "tombstone-pages")
	if !del.Danger || len(del.Fields) != 1 || del.Fields[0].Name != "ids" || !del.Fields[0].Required {
		t.Fatalf("the media list's delete: %+v", del)
	}
	deleted := func(id string) bool {
		t.Helper()
		return slices.Contains(apitest.Must(t)(c.Get(ctx, "/pages/"+id)).Class, "deleted")
	}

	// Commas and spaces separate the ids; an id named twice counts once.
	done := apitest.Must(t)(c.Do(ctx, del, map[string]string{"ids": ids[0] + ", " + ids[1] + " " + ids[0]}))
	if done.Class[0] != "tombstoned" || props(done)["count"] != json.Number("2") || len(done.Entities) != 2 ||
		done.Entities[0].Href != "/pages/"+ids[0] || done.Entities[1].Href != "/pages/"+ids[1] || done.Result != "Deleted 2 pages." {
		t.Fatalf("the answer: %+v %v %v", done.Class, props(done), done.Result)
	}
	if !deleted(ids[0]) || !deleted(ids[1]) || deleted(ids[2]) {
		t.Fatalf("after the delete: %v %v %v", deleted(ids[0]), deleted(ids[1]), deleted(ids[2]))
	}
	if rows := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=media")))["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 3.m4a" {
		t.Errorf("the media list after the delete: %v", rows)
	}

	// An id no page has refuses the whole write: the live page named beside it stays live.
	if _, err := c.Do(ctx, del, map[string]string{"ids": ids[2] + " 424242"}); apitest.Status(err) != 404 {
		t.Fatalf("an id no page has: %v", err)
	}
	if deleted(ids[2]) {
		t.Error("a refused delete tombstoned the live page named beside the missing id")
	}
	// A word that is not an id is a 422; which words are ids is TestIdsOf's.
	if _, err := c.Do(ctx, del, map[string]string{"ids": "abc"}); apitest.Status(err) != 422 {
		t.Errorf("ids abc: %v", err)
	}

	// A page tombstoned already is left as it is; one page is said in the singular.
	if again := apitest.Must(t)(c.Do(ctx, del, map[string]string{"ids": ids[0]})); again.Result != "Deleted 1 page." {
		t.Errorf("a second delete: %v", again.Result)
	}
	// Every writer may run it: it is a danger, not the owner's alone.
	agent := client.InProcess(h, "agent:test")
	apitest.Must(t)(agent.Do(ctx, del, map[string]string{"ids": ids[2]}))
	if !deleted(ids[2]) {
		t.Error("an agent's delete did nothing")
	}
	// The browser's form sends the field once per checked box.
	if _, err := c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids[2])), "revive"), nil); err != nil {
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

// TestTheListsDeleteWhatIsChecked follows the browser: the media list of Browse draws a box on each tile for one
// form, the form posts the checked ids and returns to the list with the count, and a post with no box checked comes
// back to the list with the refusal.
func TestTheListsDeleteWhatIsChecked(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	var files []string
	for i, title := range []string{"Scan 1.jpg", "Scan 2.jpg"} {
		e := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+7), 32), "mime": "image/jpeg"}))
		files = append(files, idOfSelf(t, apitest.Href(e, "self")))
	}

	list := browse(t, h, "/browse?kind=media")
	for _, want := range []string{`<form id="pick" class="pick-bar" method="POST" action="/pages/file">`,
		`<input type="hidden" name="back" value="/browse?kind=media">`, `<span class="desc">Check the rows, then type a category or delete them. A new name makes a new category.</span>`,
		`<button class="danger" formaction="/pages/tombstone" formnovalidate>Delete selected</button></div></form>`,
		`<input type="checkbox" name="ids" value="` + files[0] + `" form="pick" aria-label="Select Scan 1.jpg">`, `name="ids" value="` + files[1] + `"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the media list lacks %s", want)
		}
	}
	if strings.Contains(browse(t, h, "/browse?kind=person"), `formaction="/pages/tombstone"`) {
		t.Error("the people list draws Delete")
	}
	// The bar comes after the rows, so a CSS counter can count the checked boxes on its button (plan 108).
	if bar, last := strings.Index(list, `class="pick-bar"`), strings.LastIndex(list, `name="ids"`); bar < last || !strings.Contains(list[bar:], "</form>") {
		t.Error("the bar is not after the rows")
	}
	rec := submit(h, "/pages/tombstone", url.Values{"ids": files, "back": {"/browse?kind=media"}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form: %d %.300s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !feedbackLocation(loc, "/browse?kind=media") {
		t.Fatalf("the form returned to %q", loc)
	}
	after := browse(t, h, loc)
	if !strings.Contains(after, "Deleted 2 pages.") || strings.Contains(after, `href="/pages/`+files[0]+`"`) || strings.Contains(after, `href="/pages/`+files[1]+`"`) {
		t.Errorf("the list after the delete: %.600s", after)
	}
	rec = submit(h, "/pages/tombstone", url.Values{"back": {"/browse?kind=media"}}, "")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) || !strings.Contains(rec.Body.String(), "missing field ids") || !strings.Contains(rec.Body.String(), "<h1>Browse</h1>") {
		t.Errorf("no box checked: %d %.300s", rec.Code, rec.Body.String())
	}

}
