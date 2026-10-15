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

// TestDeletePagesAtOnce: tombstone-pages is offered on the media list and on no other list; it tombstones every id
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
	del := find(must(c.Get(ctx, "/media")), "tombstone-pages")
	if del.Name == "" || !del.Danger || len(del.Fields) != 1 || del.Fields[0].Name != "ids" || !del.Fields[0].Required {
		t.Fatalf("the media list's delete: %+v", del)
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
	if rows := props(must(c.Get(ctx, "/media")))["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 3.m4a" {
		t.Errorf("the media list after the delete: %v", rows)
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

// TestTheListsDeleteWhatIsChecked follows the browser: Media draws a box on each tile inside one form, the
// form posts the checked ids and returns to the list with the count, and a post with no box checked comes back to
// the list with the refusal.
func TestTheListsDeleteWhatIsChecked(t *testing.T) {
	c, h := fresh(t)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	var files []string
	for i, title := range []string{"Scan 1.jpg", "Scan 2.jpg"} {
		e := must(c.Do(ctx, find(root, "add-file"), map[string]string{"title": title, "sha256": strings.Repeat(fmt.Sprintf("%02x", i+7), 32), "mime": "image/jpeg"}))
		files = append(files, idOfSelf(t, hrefOf(e, "self")))
	}

	list := browse(t, h, "/media")
	for _, want := range []string{`<form id="pick" class="pick-bar" method="POST" action="/pages/tombstone" data-action="tombstone-pages">`,
		`<input type="hidden" name="back" value="/media">`, `<span class="desc">Check the rows, then delete them.</span>`, `<button class="danger" formaction="/pages/tombstone" formnovalidate>Delete selected</button></div></form>`,
		`<input type="checkbox" name="ids" value="` + files[0] + `" form="pick" aria-label="Select Scan 1.jpg">`, `name="ids" value="` + files[1] + `"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the media list lacks %s", want)
		}
	}
	if strings.Contains(browse(t, h, "/people"), `data-action="tombstone-pages"`) {
		t.Error("the people list draws the boxes")
	}
	// The bar comes after the rows, so a CSS counter can count the checked boxes on its button (plan 108).
	if bar, last := strings.Index(list, `class="pick-bar"`), strings.LastIndex(list, `name="ids"`); bar < last || !strings.Contains(list[bar:], "</form>") {
		t.Error("the bar is not after the rows")
	}
	rec := submit(h, "/pages/tombstone", url.Values{"ids": files, "back": {"/media"}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form: %d %.300s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !feedbackLocation(loc, "/media") {
		t.Fatalf("the form returned to %q", loc)
	}
	after := browse(t, h, loc)
	if !strings.Contains(after, "Deleted 2 pages.") || strings.Contains(after, `href="/pages/`+files[0]+`"`) || strings.Contains(after, `href="/pages/`+files[1]+`"`) {
		t.Errorf("the list after the delete: %.600s", after)
	}
	rec = submit(h, "/pages/tombstone", url.Values{"back": {"/media"}}, "")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `role="alert"`) || !strings.Contains(rec.Body.String(), "missing field ids") || !strings.Contains(rec.Body.String(), "<h1>Media</h1>") {
		t.Errorf("no box checked: %d %.300s", rec.Code, rec.Body.String())
	}

}
