package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestEveryActionOpensInABrowser: /actions is the browser's way to every action of the catalog
// (docs/plans/131-the-pareto-pass.md, part 1). No form of it posts to a templated href. An action with path fields is
// a GET form of /actions that names it, and that page draws the action with its href filled in and its form open.
func TestEveryActionOpensInABrowser(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	body := browse(t, h, "/actions")
	if strings.Contains(body, "%7b") || regexp.MustCompile(`action="[^"]*\{`).MatchString(body) {
		t.Fatalf("/actions draws a form that posts to a templated href:\n%s", body)
	}
	// settings has no path field: it opens on its resource, the Settings page, which draws its form (plan 146).
	if page := browse(t, h, "/settings"); !strings.Contains(page, `<form method="POST" action="/settings" autocomplete="off" data-action="settings">`) {
		t.Errorf("the action settings does not open on /settings:\n%s", page)
	}
	values := map[string]string{"id": "7", "day": "2031-05-03"}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		q := url.Values{"new": {a.Name}}
		want := a.Href
		for _, f := range a.Fields {
			if f.In == "path" {
				q.Set(f.Name, values[f.Name])
				want = strings.ReplaceAll(want, "{"+f.Name+"}", values[f.Name])
			}
		}
		if want == a.Href {
			continue
		}
		page := browse(t, h, "/actions?"+q.Encode())
		form := regexp.MustCompile(`<details class="action" open>.*?<form[^>]* action="([^"]*)"`).FindStringSubmatch(page)
		if form == nil || form[1] != want {
			t.Errorf("/actions?%s: the open form posts to %v, want %s", q.Encode(), form, want)
		}
	}
}

// TestAnOpenedActionWritesFromTheBrowser: the action that /actions opens posts to its resource, and the write is done
// there, as from the resource's own page.
func TestAnOpenedActionWritesFromTheBrowser(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Old name"}))
	id := fmt.Sprint(p.Properties.(map[string]any)["id"])
	page := browse(t, h, "/actions?new=rename&id="+id)
	form := regexp.MustCompile(`<form[^>]* action="(/pages/` + id + `/rename)"`).FindStringSubmatch(page)
	if form == nil {
		t.Fatalf("/actions?new=rename&id=%s draws no form of the page's rename:\n%s", id, page)
	}
	if rec := submit(h, form[1], url.Values{"title": {"New name"}}, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d %s", form[1], rec.Code, rec.Body)
	}
	if got := apitest.Must(t)(c.Get(t.Context(), "/pages/"+id)).Title; got != "New name" {
		t.Errorf("the page is titled %q after the rename, want New name", got)
	}
}

// TestTheCatalogCarriesThePlaceholderOfAField: GET /actions says the example of a field as the member placeholder,
// for the CLI, the MCP tools and a browser alike.
func TestTheCatalogCarriesThePlaceholderOfAField(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		if a.Name != "register-metric" {
			continue
		}
		if f := a.Field("name"); f == nil || f.Placeholder != "Weight, Mood, Walk" {
			t.Errorf("register-metric name: %+v", f)
		}
		if f := a.Field("daily"); f == nil || f.Placeholder != "" {
			t.Errorf("register-metric daily: %+v", f)
		}
		return
	}
	t.Error("register-metric is not in the catalog")
}
