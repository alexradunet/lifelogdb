package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestAnErrorIsAnAlertInWords: the browser draws an error entity as an alert whose first words and tone its code
// picks (docs/plans/092-the-app-shell.md): a stale save is a warning that keeps the text, an owner-only action sent
// by an agent is a refusal, and a missing page shows the message alone.
func TestAnErrorIsAnAlertInWords(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := apitest.Action(t, p, "save-body")
	apitest.Must(t)(c.Do(t.Context(), save, map[string]string{"body": "v2"}))
	var version string
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	stale := submit(h, save.Href, url.Values{"body": {"my draft"}, "version": {version}}, apitest.Href(p, "self")).Body.String()
	for _, want := range []string{`<span class="date">409 · stale version</span>`,
		`<div class="alert" role="alert"><p><strong>Not saved. The page changed since you opened it.</strong> `, ">my draft</textarea>"} {
		if !strings.Contains(stale, want) {
			t.Errorf("a stale save lacks %s", want)
		}
	}

	req := httptest.NewRequest("POST", "/snapshots", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set(api.SourceHeader, "agent:synthetic")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `<div class="alert danger" role="alert"><p><strong>Refused.</strong> `) {
		t.Errorf("an agent's snapshot: %d, no refusal in words", rec.Code)
	}

	if body := browseStatus(t, h, "/pages/999999", http.StatusNotFound); !strings.Contains(body, `<div class="alert" role="alert"><p>no page 999999</p></div>`) {
		t.Errorf("a missing page is not an alert of its message alone")
	}
}

// TestAWriteEndsInAToast: a browser form's result comes back once, as the toast at the top of the page: an open
// dialog in the flow, which Escape (closedby) and its own Close close with no script.
func TestAWriteEndsInAToast(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/"}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/?feedback=") {
		t.Fatalf("capture: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	body := browse(t, h, rec.Header().Get("Location"))
	if !strings.Contains(body, `<main id="main">`+"\n"+`<dialog open closedby="closerequest" class="toast" aria-label="Result"><h2>Result</h2>`) || !strings.Contains(body, "Dana Example") ||
		!strings.Contains(body, `<form method="dialog" class="tiny"><button class="close">Close</button></form></dialog>`) {
		t.Errorf("the capture's result is not the toast at the top of the page, with its Close")
	}
	if strings.Contains(browse(t, h, rec.Header().Get("Location")), `class="toast"`) {
		t.Error("the toast came back on a reload")
	}
}

// TestAnOwnerActionCarriesTheOwnerMark: the catalog's owner flag, not a template, puts the mark on the button.
func TestAnOwnerActionCarriesTheOwnerMark(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	body := browse(t, h, "/")
	if want := `<summary class="button">Take a snapshot <span class="owner">owner</span></summary>`; !strings.Contains(body, want) {
		t.Errorf("Home lacks %s", want)
	}
	if want := `<summary class="button">New metric <span class="owner">owner</span></summary>`; !strings.Contains(browse(t, h, "/metrics"), want) {
		t.Errorf("/metrics lacks %s", want)
	}
	if strings.Contains(body, `New page <span class="owner">`) {
		t.Error("an action that is not the owner's alone carries the owner mark")
	}
}

// TestNoPageShowsARawInstant: a page and a reading say their times in words; the
// exact instant stays only in an attribute (the <time> element's, or a form's value).
func TestNoPageShowsARawInstant(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden project", "body": "Seeds."}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	reading := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.1", "taken_at": fixedToday + "T06:00:00.000Z"}))

	attributes := regexp.MustCompile(`(datetime|title|value)="[^"]*"`)
	instant := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)
	for _, path := range []string{apitest.Href(p, "self"), apitest.Href(reading, "self")} {
		body := browse(t, h, path)
		if raw := instant.FindString(attributes.ReplaceAllString(body, "")); raw != "" {
			t.Errorf("%s shows the raw instant %s", path, raw)
		}
		if !strings.Contains(body, "<time datetime=") {
			t.Errorf("%s says no time in words", path)
		}
	}
}

// TestEveryActionFormSendsToItsAction: a form drawn from an action sends to the action's href with the action's
// method (a box, a row's one-button form, the capture), and every POST form of Home sends to an action of Home.
func TestEveryActionFormSendsToItsAction(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "walk"}))
	body := browse(t, h, "/")
	posts := map[string]bool{}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/")).Actions {
		if a.Method == "POST" {
			posts[a.Href] = true
		}
	}
	for _, name := range []string{"capture", "create-page", "find", "snapshot"} {
		if a := apitest.Action(t, root, name); !strings.Contains(body, `<form method="`+a.Method+`" action="`+a.Href+`"`) {
			t.Errorf("Home has no form of %s: %+v", name, a)
		}
	}
	for _, m := range regexp.MustCompile(`<form method="POST" action="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !posts[m[1]] {
			t.Errorf("a form of Home posts to %s, which is no action of Home", m[1])
		}
	}
}
