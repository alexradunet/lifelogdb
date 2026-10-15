package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The navigation of Browse (docs/plans/117-one-browse-view.md, rules 7 and 8): the old lists answer 308 to Browse,
// Home links Browse, the sidebar has Metrics and Browse beside Today and Calendar, and a category page opens itself
// in Browse.

func TestTheOldListsAnswer308ToBrowse(t *testing.T) {
	c, h := fresh(t)
	for _, tc := range []struct{ path, location string }{
		{"/people", "/browse?kind=person"},
		{"/places", "/browse?kind=place"},
		{"/media", "/browse?kind=media"},
		{"/media?kind=video", "/browse?kind=media&media=video"},
		{"/files", "/browse?kind=document"},
		{"/ghosts", "/browse?ghosts=1&kind=page"},
		{"/categories", "/browse"},
		{"/people?limit=3&offset=6", "/browse?kind=person&limit=3&offset=6"},
		{"/categories?offset=200", "/browse?offset=200"},
		{"/media?kind=image&limit=10&offset=10", "/browse?kind=media&limit=10&media=image&offset=10"},
		{"/places?new=create-place", "/browse?kind=place&new=create-place"},
		{"/files?new=add-file&title=Scan", "/browse?kind=document&new=add-file"}, // only limit, offset and new are kept
	} {
		for _, accept := range []string{"application/vnd.siren+json", "text/html"} {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Accept", accept)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != tc.location {
				t.Errorf("GET %s (%s): %d to %q, want 308 to %q", tc.path, accept, rec.Code, rec.Header().Get("Location"), tc.location)
			}
		}
	}
	// A client follows the redirect, and gets Browse.
	if e := must(c.Get(t.Context(), "/people")); e.Class[0] != "browse" || props(e)["kind"] != "person" {
		t.Errorf("GET /people followed: %v %v", e.Class, props(e)["kind"])
	}
	// The POST routes of the same paths do not change.
	for _, tc := range []struct{ path, form string }{
		{"/people", "title=Ana"}, {"/places", "title=Lake"}, {"/files", "title=Scan.pdf&mime=application/pdf&sha256=" + strings.Repeat("ab", 32)},
	} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Location"), "/pages/") {
			t.Errorf("POST %s: %d %q", tc.path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestHomeLinksTheListsOfBrowse(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	for rel, want := range map[string]string{
		"browse": "/browse", "categories": "/browse", "people": "/browse?kind=person", "places": "/browse?kind=place",
		"media": "/browse?kind=media", "files": "/browse?kind=document", "ghosts": "/browse?ghosts=1&kind=page", "metrics": "/metrics",
	} {
		if got := hrefOf(root, rel); got != want {
			t.Errorf("Home's %s is %q, want %q", rel, got, want)
		}
	}
	// Every link of Home is served: no link answers a redirect or an error.
	for _, l := range root.Links {
		req := httptest.NewRequest("GET", l.Href, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && !(l.Href == "/days/today" || strings.HasSuffix(l.Href, "/current")) {
			t.Errorf("Home's %s (%s): %d", l.Rel[0], l.Href, rec.Code)
		}
	}
}

// TestTheSidebarHasMetricsAndBrowse: the sections of every day are Today, Calendar, Metrics and Browse, in the rail
// of a desktop and in the bottom bar of a phone (one list, which the sheet does not repeat); there is no group of
// the record. Browse is marked on every list of Browse, Metrics on the board and under it.
func TestTheSidebarHasMetricsAndBrowse(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	sections := regexp.MustCompile(`(?s)<nav aria-label="Sections">(.*?)</nav>`)
	entry := regexp.MustCompile(`<a href="([^"]+)"( aria-current="page")?[^>]*><svg class="icon"[^>]*>.*?</svg>([^<]+)</a>`)
	for path, current := range map[string]string{
		"/browse": "Browse", "/browse?kind=person": "Browse", "/browse?ghosts=1": "Browse", "/metrics": "Metrics",
		"/metrics/Weight": "Metrics", "/days/" + fixedToday: "Today", "/months/2031-05": "Calendar", "/search": "",
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		bar := sections.FindStringSubmatch(body)
		if bar == nil {
			t.Errorf("%s: no sections:\n%s", path, sidebar(body))
			continue
		}
		var got, marked []string
		for _, m := range entry.FindAllStringSubmatch(bar[1], -1) {
			got = append(got, m[3]+"="+m[1])
			if m[2] != "" {
				marked = append(marked, m[3])
			}
		}
		if want := "Today=/days/today Calendar=/months/current Metrics=/metrics Browse=/browse"; strings.Join(got, " ") != want {
			t.Errorf("%s: the sections are %v, want %s", path, got, want)
		}
		if strings.Join(marked, " ") != current {
			t.Errorf("%s marks %v, want %q", path, marked, current)
		}
		if strings.Contains(body, `aria-label="The record"`) || strings.Count(sidebar(body), `href="/browse"`) != 1 {
			t.Errorf("%s: the rail has a group of the record, or a second link to Browse", path)
		}
	}
}

// TestACategoryPageOpensInBrowse: Filed here links the category's list in Browse, its title escaped in the query.
func TestACategoryPageOpensInBrowse(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	if body := browse(t, h, "/pages/"+ids["Travel"]); !strings.Contains(body, `<h2>Filed here <small>3</small></h2><a href="/browse?in=Travel">Open in Browse</a>`) {
		t.Error("Travel's Filed here has no link to Browse")
	}
	note := must(c.Do(ctx, find(must(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "A note"}))
	must(c.Do(ctx, find(note, "file-pages"), map[string]string{"category": "Rock & roll 2031"}))
	cat := must(c.Get(ctx, "/pages?title="+url.QueryEscape("Rock & roll 2031")))
	body := browse(t, h, href(cat, "self"))
	m := regexp.MustCompile(`<a href="(/browse\?in=[^"]*)">Open in Browse</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the category page has no link to Browse")
	}
	u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	if err != nil || u.Query().Get("in") != "Rock & roll 2031" || len(u.Query()) != 1 {
		t.Fatalf("the link %s does not name the category: %v", m[1], err)
	}
	if got := browseRows(t, must(c.Get(ctx, u.String()))); got != "A note:" {
		t.Errorf("Browse in the category: %s", got)
	}
	// A page with nothing filed in it has no Filed here, and no link.
	if strings.Contains(browse(t, h, href(note, "self")), "Open in Browse") {
		t.Error("a page with nothing filed in it links Browse")
	}
}
