package api_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/siren"
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
		`<div class="alert" role="alert" tabindex="-1" autofocus><p><strong>Not saved. The page changed since you opened it.</strong> `, ">my draft</textarea>"} {
		if !strings.Contains(stale, want) {
			t.Errorf("a stale save lacks %s", want)
		}
	}

	req := httptest.NewRequest("POST", "/snapshots", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set(siren.SourceHeader, "agent:synthetic")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `<div class="alert danger" role="alert" tabindex="-1" autofocus><p><strong>Refused.</strong> `) {
		t.Errorf("an agent's snapshot: %d, no refusal in words", rec.Code)
	}

	if body := browseStatus(t, h, "/pages/999999", http.StatusNotFound); !strings.Contains(body, `<div class="alert" role="alert" tabindex="-1" autofocus><p>no page 999999</p></div>`) {
		t.Errorf("a missing page is not an alert of its message alone, which takes the focus")
	}
	if body := browseStatus(t, h, "/pages/999999", http.StatusNotFound); strings.Count(body, "autofocus") != 1 {
		t.Errorf("the error page has %d autofocus, want the alert's only", strings.Count(body, "autofocus"))
	}
}

// TestAWriteEndsInAToast: a browser form's result comes back once, as the toast at the top of the page: an open
// dialog in the flow, which Escape (closedby) and its own Close close with no script.
func TestAWriteEndsInAToast(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/days/" + fixedToday}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+fixedToday+"?feedback=") {
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
	body := browse(t, h, "/actions")
	if want := `<summary class="button">Take a snapshot <span class="owner">owner</span></summary>`; !strings.Contains(body, want) {
		t.Errorf("/actions lacks %s", want)
	}
	if m := browse(t, h, "/metrics"); !strings.Contains(m, `<summary class="button primary">`) || !strings.Contains(m, `New metric <span class="owner">owner</span></summary>`) {
		t.Errorf("/metrics lacks the New metric summary with the owner mark")
	}
	if strings.Contains(body, `New page <span class="owner">`) {
		t.Error("an action that is not the owner's alone carries the owner mark")
	}
}

// TestTheMoreOfAPageAndOfAMetricIsAQuietRow: the row of actions that ends a page (the More of a page and of a metric's
// page) is a quiet row, drawn without the slab (docs/plans/129-the-ux-pass.md, part 5).
func TestTheMoreOfAPageAndOfAMetricIsAQuietRow(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden project", "body": "Seeds."}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	more := regexp.MustCompile(`<section class="group" aria-label="More"><h2>More</h2><div class="actions quiet">`)
	for _, path := range []string{apitest.Href(p, "self"), "/metrics/weight"} {
		if body := browse(t, h, path); !more.MatchString(body) {
			t.Errorf("%s does not draw its More as a quiet row", path)
		}
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
// method (a box, a row's one-button form, the capture), and every POST form of the page of today sends to an action of
// that page. Every action that the JSON root offers has its form on /actions, the page of every action, so a browser
// reaches what Home used to offer (docs/plans/129-the-ux-pass.md, part 10).
func TestEveryActionFormSendsToItsAction(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "walk"}))
	day := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	body := browse(t, h, "/days/"+fixedToday)
	posts := map[string]bool{}
	for _, a := range day.Actions {
		if a.Method == "POST" {
			posts[a.Href] = true
		}
	}
	for _, name := range []string{"capture", "record"} {
		if a := apitest.Action(t, day, name); !strings.Contains(body, `<form method="`+a.Method+`" action="`+a.Href+`"`) {
			t.Errorf("the page of today has no form of %s: %+v", name, a)
		}
	}
	for _, m := range regexp.MustCompile(`<form method="POST" action="([^"]*)"`).FindAllStringSubmatch(withoutChat(body), -1) {
		if !posts[m[1]] {
			t.Errorf("a form of the page of today posts to %s, which is no action of that page", m[1])
		}
	}

	every, catalog := browse(t, h, "/actions"), apitest.Must(t)(c.Get(t.Context(), "/actions"))
	for _, offered := range root.Actions {
		a := apitest.Action(t, catalog, offered.Name)
		if strings.Contains(a.Href, "{") { // the day's action (capture): its form is the page of the day, checked above
			continue
		}
		if !strings.Contains(every, `<form method="`+a.Method+`" action="`+html.EscapeString(a.Href)+`"`) {
			t.Errorf("/actions has no form of %s, which the root offers: %+v", offered.Name, a)
		}
	}
}
