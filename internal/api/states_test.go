package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestAnErrorIsAnAlertInWords: the browser draws an error entity as an alert whose first words and tone its code
// picks (docs/plans/092-the-app-shell.md): a stale save is a warning that keeps the text, an owner-only action sent
// by an agent is a refusal, and a missing page shows the message alone.
func TestAnErrorIsAnAlertInWords(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	must(c.Do(t.Context(), save, map[string]string{"body": "v2"}))
	var version string
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	stale := submit(h, save.Href, url.Values{"body": {"my draft"}, "version": {version}}, href(p, "self")).Body.String()
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

	if body := page(t, h, "/pages/999999"); !strings.Contains(body, `<div class="alert" role="alert"><p>no page 999999</p></div>`) {
		t.Errorf("a missing page is not an alert of its message alone")
	}
}

// TestAWriteEndsInAToast: a browser form's result comes back once, as the toast at the top of the page: an open
// dialog in the flow, which Escape (closedby) and its own Close close with no script.
func TestAWriteEndsInAToast(t *testing.T) {
	_, h := freshOn(t, fixedToday)
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
	_, h := fresh(t)
	body := page(t, h, "/")
	if want := `<summary class="button">Take a snapshot <span class="owner">owner</span></summary>`; !strings.Contains(body, want) {
		t.Errorf("Home lacks %s", want)
	}
	if want := `<summary class="button">New metric <span class="owner">owner</span></summary>`; !strings.Contains(page(t, h, "/metrics"), want) {
		t.Errorf("/metrics lacks %s", want)
	}
	if strings.Contains(body, `New page <span class="owner">`) {
		t.Error("an action that is not the owner's alone carries the owner mark")
	}
}

// TestNoPageShowsARawInstant: a page, a task, an occurrence and a reading say their times in words; the
// exact instant stays only in an attribute (the <time> element's, or a form's value).
func TestNoPageShowsARawInstant(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	p := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Garden project", "body": "Seeds."}))
	task := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Buy seeds", "due_day": dayOf(-1)}))
	taskHref := strings.SplitN(hrefOf(task, "self"), "?", 2)[0]
	occ := must(c.Do(t.Context(), find(must(c.Get(t.Context(), taskHref+"/occurrences/once")), "edit-occurrence"), map[string]string{"state": "done", "completed_at": "now"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	reading := must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.1", "taken_at": fixedToday + "T06:00:00.000Z"}))

	attributes := regexp.MustCompile(`(datetime|title|value)="[^"]*"`)
	instant := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)
	for _, path := range []string{href(p, "self"), taskHref, hrefOf(occ, "self"), href(reading, "self")} {
		body := browse(t, h, path)
		if raw := instant.FindString(attributes.ReplaceAllString(body, "")); raw != "" {
			t.Errorf("%s shows the raw instant %s", path, raw)
		}
		if !strings.Contains(body, "<time datetime=") {
			t.Errorf("%s says no time in words", path)
		}
	}
}

// TestEveryActionFormCarriesItsName: a form drawn from an action carries the action's catalog name, so the command
// list (docs/plans/094-the-command-list.md) opens the form the server drew: a box, a row's one-button form, the capture.
func TestEveryActionFormCarriesItsName(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Book the dentist", "due_day": dayOf(-1)}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "walk"}))
	body := browse(t, h, "/")
	for _, name := range []string{"capture", "create-page", "find", "snapshot", "edit-occurrence"} {
		if !strings.Contains(body, `data-action="`+name+`"`) {
			t.Errorf("Home has no form named %s", name)
		}
	}
	for _, tag := range regexp.MustCompile(`<form method="POST"[^>]*>`).FindAllString(body, -1) {
		if !strings.Contains(tag, ` data-action="`) {
			t.Errorf("a form has no action's name: %s", tag)
		}
	}
}
