package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestSetPictureFromThePersonPage: a person's page offers set-picture, and a place's page and the lists do not. A
// browser sends a new photo from the page and comes back to it; the photo is the person's picture in People, kept
// with a title of the person and today. A client names a kept picture by its title instead, as the CLI and an agent
// do.
func TestSetPictureFromThePersonPage(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	ana := must(c.Do(ctx, find(root, "create-person"), map[string]string{"title": "Ana"}))
	must(c.Do(ctx, find(ana, "save-body"), map[string]string{"body": "Met at school."}))
	lake := must(c.Do(ctx, find(root, "create-place"), map[string]string{"title": "Lake"}))
	for _, e := range []*api.Entity{lake, must(c.Get(ctx, "/browse?kind=person")), must(c.Get(ctx, "/browse?kind=place"))} {
		if hasAction(e, "set-picture") {
			t.Errorf("%s offers set-picture", e.Title)
		}
	}
	if body := page(t, h, "/browse?kind=person"); strings.Contains(body, "/picture\"") {
		t.Errorf("the people sheet draws a form to set a picture")
	}
	set := pictureAction(t, must(c.Get(ctx, href(ana, "self"))), href(ana, "self")+"/picture")
	body := page(t, h, href(ana, "self"))
	for _, s := range []string{`action="` + set.Href + `"`, `enctype="multipart/form-data"`} {
		if !strings.Contains(body, s) {
			t.Errorf("the person's page lacks %q", s)
		}
	}

	jpg, _ := jpegFile(t, "ana.jpg", 40, 30)
	rec := postPhoto(t, h, set.Href, jpg, href(ana, "self"))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), href(ana, "self")) {
		t.Fatalf("POST %s: %d to %q %.300s", set.Href, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	title := "Ana " + fixedToday + ".jpg"
	if got := str(props(must(c.Get(ctx, href(ana, "self"))))["body"]); got != "![["+title+"]]\n\nMet at school." {
		t.Errorf("Ana's body = %q", got)
	}
	rows, _ := props(must(c.Get(ctx, "/browse?kind=person")))["rows"].([]any)
	if len(rows) != 1 || !strings.HasSuffix(str(rows[0].(map[string]any)["picture"]), "/preview") {
		t.Fatalf("rows = %v, want Ana with a picture", rows)
	}
	first := str(rows[0].(map[string]any)["picture"])

	other, _ := jpegFile(t, "other.jpg", 30, 40)
	kept := must(c.DoFiles(ctx, find(root, "add-file"), map[string]string{"title": "Other.jpg"}, map[string]string{"original": other}))
	must(c.Do(ctx, set, map[string]string{"title": "Other.jpg"}))
	rows, _ = props(must(c.Get(ctx, "/browse?kind=person")))["rows"].([]any)
	if got := str(rows[0].(map[string]any)["picture"]); got != hrefOf(kept, "preview") || got == first {
		t.Errorf("Ana's picture = %q, want %q", got, hrefOf(kept, "preview"))
	}
	if _, err := c.Do(ctx, set, map[string]string{"title": "Nothing.jpg"}); err == nil {
		t.Errorf("a title that is no kept picture was taken")
	}
}

func hasAction(e *api.Entity, name string) bool {
	for _, a := range e.Actions {
		if a.Name == name {
			return true
		}
	}
	return false
}

func pictureAction(t *testing.T, e *api.Entity, href string) api.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == "set-picture" && a.Href == href {
			return a
		}
	}
	t.Fatalf("no set-picture to %s among %+v", href, e.Actions)
	return api.Action{}
}

// postPhoto sends a photo as a browser's form does.
func postPhoto(t *testing.T, h http.Handler, path, file, back string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("back", back)
	mw.WriteField("title", "")
	p, err := mw.CreateFormFile("original", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(p, f); err != nil {
		t.Fatal(err)
	}
	mw.CreateFormFile("preview", "") // the folded preview input, left empty
	mw.Close()
	req := httptest.NewRequest("POST", path, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
