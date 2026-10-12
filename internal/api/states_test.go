package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestAnErrorIsAnAlertInWords: the browser draws an error entity as an alert whose first words and tone its code
// picks (docs/plans/092-the-app-shell.md): a stale save is a warning that keeps the text, an owner-only action sent
// by an agent is a refusal, and a missing page shows the message alone.
func TestAnErrorIsAnAlertInWords(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(find(must(c.Get("/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	must(c.Do(save, map[string]string{"body": "v2"}))
	var version string
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	stale := submit(h, save.Href, url.Values{"body": {"my draft"}, "version": {version}}, href(p, "self")).Body.String()
	for _, want := range []string{`<span class="date">409 · stale_version</span>`,
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

// TestAWriteEndsInAToast: a browser form's result comes back once, as the toast at the top of the page.
func TestAWriteEndsInAToast(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/"}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/?feedback=") {
		t.Fatalf("capture: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	body := browse(t, h, rec.Header().Get("Location"))
	if !strings.Contains(body, `<main id="main">`+"\n"+`<aside class="toast" role="status"><h2>Result</h2>`) || !strings.Contains(body, "Dana Example") {
		t.Errorf("the capture's result is not the toast at the top of the page")
	}
	if strings.Contains(browse(t, h, rec.Header().Get("Location")), `class="toast"`) {
		t.Error("the toast came back on a reload")
	}
}

// TestAnOwnerActionCarriesTheOwnerMark: the catalog's owner flag, not a template, puts the mark on the button.
func TestAnOwnerActionCarriesTheOwnerMark(t *testing.T) {
	_, h := fresh(t)
	body := page(t, h, "/")
	for _, want := range []string{`<summary class="button">Take a snapshot <span class="owner">owner</span></summary>`,
		`<summary class="button">New metric <span class="owner">owner</span></summary>`} {
		if !strings.Contains(body, want) {
			t.Errorf("Home lacks %s", want)
		}
	}
	if strings.Contains(body, `New page <span class="owner">`) {
		t.Error("an action that is not the owner's alone carries the owner mark")
	}
}
