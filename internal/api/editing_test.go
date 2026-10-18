package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestRenderIsTheSavedPage: the preview of a body is a page whose prose is the HTML its page shows once saved, it
// writes nothing, and it is a POST like any other: the origin guard and the request limit apply. A form that
// writes a body offers it as Preview, in a new tab, so the form keeps its words (plan 108).
func TestRenderIsTheSavedPage(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	body := "# Notes\n\nRan with [[Sam]] #running, <b>raw</b> and ![[IMG_0001.jpg]].\n\n- one\n- two"
	post := func(form url.Values, header map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/render", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		for k, v := range header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rows := func() string {
		return scalar(t, s, "SELECT CAST(count(*) AS TEXT) || '/' || CAST(max(revision) AS TEXT) FROM entities")
	}
	before := rows()
	rec := post(url.Values{"body": {body}}, map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"})
	prose := regexp.MustCompile(`(?s)<div class="sheet"><div class="prose">(.*?)</div></div>\n`).FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || prose == nil ||
		strings.Contains(prose[1], "<b>raw</b>") || !strings.Contains(rec.Body.String(), "<h1>Preview</h1>") || strings.Contains(rec.Body.String(), `class="rail"`) {
		t.Fatalf("render: %d %q %.600s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	preview := prose[1]
	if after := rows(); after != before {
		t.Fatalf("render wrote: %s, then %s", before, after)
	}
	page := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Notes", "body": body}))
	saved := browse(t, h, apitest.Href(page, "self"))
	if !strings.Contains(saved, `<div class="prose">`+preview+`</div>`) {
		t.Errorf("the preview is not the saved page:\npreview %s\npage %.2000s", preview, saved)
	}
	if !strings.Contains(saved, `<button type="submit" class="quiet" formaction="/render" formmethod="POST" formenctype="application/x-www-form-urlencoded" formtarget="_blank">Preview</button><button class="primary">Save</button>`) {
		t.Error("the page's editor offers no Preview before Save")
	}
	if text := post(url.Values{"text": {"A *capture*"}}, map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"}); !strings.Contains(text.Body.String(), "<em>capture</em>") {
		t.Error("a capture's text is not previewed")
	}
	if rec := post(url.Values{"body": {"x"}}, map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("a cross-origin render: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/render?body=x", nil)
	get := httptest.NewRecorder()
	h.ServeHTTP(get, req)
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /render: %d", get.Code)
	}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		if strings.HasPrefix(a.Href, "/render") {
			t.Errorf("the catalog names %s: the preview is presentation", a.Href)
		}
	}
	big := httptest.NewRequest("POST", "/render", io.MultiReader(strings.NewReader("body="), strings.NewReader(strings.Repeat("a", 20<<20))))
	big.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tooLarge := httptest.NewRecorder()
	h.ServeHTTP(tooLarge, big)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over the limit: %d", tooLarge.Code)
	}
}

// TestTheEditorIsTallAsItsBodyAndFirst: a page's save-body textarea grows with its body, 12 to 40 lines, and the
// page draws its editor before its body.
func TestTheEditorIsTallAsItsBodyAndFirst(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, tc := range []struct {
		lines, rows int
	}{{1, 12}, {20, 21}, {38, 39}, {39, 40}, {200, 40}} {
		body := strings.Repeat("line\n", tc.lines-1) + "line"
		p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Lines " + strings.Repeat("x", tc.lines), "body": body}))
		for _, f := range apitest.Action(t, p, "save-body").Fields {
			if f.Name == "body" && f.Rows != tc.rows {
				t.Errorf("%d lines: rows %d, want %d", tc.lines, f.Rows, tc.rows)
			}
		}
	}
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Long", "body": "words"}))
	body := browse(t, h, apitest.Href(p, "self"))
	if e, b := strings.Index(body, "<summary>Edit</summary>"), strings.Index(body, `<div class="prose">`); e < 0 || b < 0 || e > b {
		t.Errorf("the editor is not before the body: %d, %d", e, b)
	}
}

// TestTheBrowserMarksTheBodyOfASaveRequired: save-body refuses an empty body (the catalog marks the field required),
// so the browser marks its text area required and does not send an empty body.
func TestTheBrowserMarksTheBodyOfASaveRequired(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Notes", "body": "words"}))
	area := regexp.MustCompile(`<textarea name="body"[^>]*>`).FindString(browse(t, h, apitest.Href(p, "self")))
	if area != `<textarea name="body" rows="12" required>` {
		t.Errorf("the text area of save-body: %q, want it required", area)
	}
}
