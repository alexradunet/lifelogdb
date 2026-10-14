package api_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTitlesAreTheNamesAfterABracket: /titles answers the names of live pages that start with a prefix, in key
// order, with the page each name is of; its limits are refused, not clamped.
func TestTitlesAreTheNamesAfterABracket(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	for _, title := range []string{"Sam Example", "SAMUEL", "Garden project"} {
		must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": title}))
	}
	got := must(c.Get(t.Context(), "/titles?prefix=sa"))
	var names []string
	for _, m := range props(got)["titles"].([]any) {
		names = append(names, m.(map[string]any)["title"].(string))
	}
	if strings.Join(names, ",") != "Sam Example,SAMUEL" || len(got.Entities) != 2 || !strings.HasPrefix(got.Entities[0].Href, "/pages/") {
		t.Errorf("titles: %v %v", names, got.Entities)
	}
	if fmt.Sprint(props(got)["limit"]) != "10" || props(got)["prefix"] != "sa" {
		t.Errorf("titles' prefix and limit: %v", props(got))
	}
	if n := len(props(must(c.Get(t.Context(), "/titles?prefix=s&limit=1")))["titles"].([]any)); n != 1 {
		t.Errorf("limit 1 answered %d", n)
	}
	for _, bad := range []string{"/titles", "/titles?prefix=", "/titles?prefix=%20", "/titles?prefix=s&limit=0", "/titles?prefix=s&limit=21", "/titles?prefix=s&limit=x"} {
		if _, err := c.Get(t.Context(), bad); !clientStatus(err, 422) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// TestRenderIsTheSavedPage: the preview of a body is a page whose prose is the HTML its page shows once saved, it
// writes nothing, and it is a POST like any other: the origin guard and the request limit apply. A form that
// writes a body offers it as Preview, in a new tab, so the form keeps its words (plan 108).
func TestRenderIsTheSavedPage(t *testing.T) {
	c, h := fresh(t)
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
		return fmtRows(must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "query"), map[string]string{"sql": "SELECT CAST(count(*) AS TEXT) || '/' || CAST(max(revision) AS TEXT) FROM entities"})))
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
	page := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Notes", "body": body}))
	saved := browse(t, h, hrefOf(page, "self"))
	if !strings.Contains(saved, `<div class="prose">`+preview+`</div>`) {
		t.Errorf("the preview is not the saved page:\npreview %s\npage %.2000s", preview, saved)
	}
	if !strings.Contains(saved, `<button type="submit" class="quiet" formaction="/render" formmethod="POST" formtarget="_blank">Preview</button><button class="primary">Save</button>`) {
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
	for _, a := range must(c.Get(t.Context(), "/actions")).Actions {
		if strings.HasPrefix(a.Href, "/render") || strings.HasPrefix(a.Href, "/titles") {
			t.Errorf("the catalog names %s: the preview is presentation, and the titles a resource", a.Href)
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
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	for _, tc := range []struct {
		lines, rows int
	}{{1, 12}, {20, 21}, {38, 39}, {39, 40}, {200, 40}} {
		body := strings.Repeat("line\n", tc.lines-1) + "line"
		p := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Lines " + strings.Repeat("x", tc.lines), "body": body}))
		for _, f := range find(p, "save-body").Fields {
			if f.Name == "body" && f.Rows != tc.rows {
				t.Errorf("%d lines: rows %d, want %d", tc.lines, f.Rows, tc.rows)
			}
		}
	}
	p := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Long", "body": "words"}))
	body := browse(t, h, hrefOf(p, "self"))
	if e, b := strings.Index(body, "<summary>Edit</summary>"), strings.Index(body, `<div class="prose">`); e < 0 || b < 0 || e > b {
		t.Errorf("the editor is not before the body: %d, %d", e, b)
	}
}
