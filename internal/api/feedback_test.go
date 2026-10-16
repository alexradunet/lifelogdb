package api_test

import (
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

func TestFeedbackURLPrivacyUsesStructureNotRandomSubstrings(t *testing.T) {
	opaque := "bad" + strings.Repeat("0", 61)
	if !feedbackLocation("/pages/1?feedback="+opaque, "/pages/1") {
		t.Fatal("valid random hex may contain the substring bad")
	}
	for _, location := range []string{
		"/pages/1?feedback=" + opaque + "&message=bad",
		"/pages/1?feedback=" + opaque + "&body=private%GG",
		"/pages/1?feedback=" + opaque + "&private%GG=value",
		"/pages/1?feedback=" + opaque + "&note=private%",
		"/pages/1?feedback=bad",
		"/pages/Revived?feedback=" + opaque,
	} {
		if feedbackLocation(location, "/pages/1") {
			t.Fatalf("accepted private or malformed URL %s", location)
		}
	}
}

func TestBrowserMutationFeedback(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	target := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Revived target"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, target, "tombstone"), nil))
	rec := submit(h, "/pages", url.Values{"title": {"Feedback page"}, "body": {"[[Revived target]] [[bad/name]]"}}, "")
	if rec.Code != 303 {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	body := browse(t, h, location)
	for _, want := range []string{"Result", `Revived <a href="/pages?title=Revived+target">Revived target</a>.`, "Not linked, not a valid title: <code>[[bad/name]]</code>."} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in redirected feedback", want)
		}
	}
	if strings.Contains(browse(t, h, location), "<h2>Result</h2>") {
		t.Error("receipt replayed")
	}
	page := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Feedback%20page"))
	if !feedbackLocation(location, apitest.Href(page, "self")) {
		t.Errorf("feedback URL must contain only the resource path and opaque receipt: %s", location)
	}
}
