package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/siren"
)

// remote is a client over an HTTP server that serves h, closed at the end of the test.
func remote(t *testing.T, h http.Handler, source string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return client.Remote(srv.URL, source, "")
}

// fakeClient is a client of a fake API: it serves a fixed catalog (fakeCatalog) and answers the few reads and writes
// that the tests make. The real API is held by the end-to-end test (e2e_test.go).
func fakeClient(t *testing.T) *client.Client {
	return remote(t, http.HandlerFunc(fakeAPI), "agent:test")
}

func props(t *testing.T, e *siren.Entity) map[string]any {
	t.Helper()
	p, ok := e.Properties.(map[string]any)
	if !ok {
		t.Fatalf("the entity %q has no properties", e.Title)
	}
	return p
}

func fakeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	e := siren.Entity{}
	switch {
	case r.URL.Path == "/actions":
		e.Actions = fakeCatalog()
	case strings.HasPrefix(r.URL.Path, "/days/"):
		e.Title = strings.TrimPrefix(r.URL.Path, "/days/")
		e.Properties = map[string]any{"view": map[string]any{"entries": []any{}}}
	case r.URL.Path == "/months/2031-05":
		e.Title = "May 2031"
	case r.URL.Path == "/files" && r.Method == "POST": // add-file: say what arrived
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, header, err := r.FormFile("original")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()
		n, _ := io.Copy(io.Discard, f)
		e.Title = header.Filename
		e.Properties = map[string]any{"size": strconv.FormatInt(n, 10)}
		e.Links = []siren.Link{{Rel: []string{"preview"}, Href: "/pages/1/preview"}}
	}
	json.NewEncoder(w).Encode(e)
}

// fakeCatalog is a small catalog in the shape of GET /actions: public actions of each kind that the tests need (a
// read, a date, a number with options, a file, a path field) and the owner's actions that are never tools.
func fakeCatalog() []siren.Action {
	day := func(title string, in string) siren.Field {
		return siren.Field{Name: "day", Type: "date", Title: title, In: in}
	}
	return []siren.Action{
		{Name: "find", Method: "GET", Href: "/pages", Fields: []siren.Field{{Name: "title", Type: "text", Title: "Title"}}},
		{Name: "browse", Method: "GET", Href: "/browse"},
		{Name: "random-page", Method: "GET", Href: "/pages/random"},
		{Name: "open-day", Method: "GET", Href: "/days/{day}", Fields: []siren.Field{day("Day (default today)", "path")}},
		{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{
			{Name: "day", Type: "date", Title: "Day", In: "path", Required: true}, {Name: "text", Type: "textarea", Title: "Text"},
			{Name: "mood", Type: "number", Title: "Mood", Options: []string{"1", "2", "3", "4", "5"}},
			{Name: "import_key", Type: "text", Title: "Key"}}},
		{Name: "record", Method: "POST", Href: "/measurements", Fields: []siren.Field{
			{Name: "metric", Type: "text", Title: "Metric", Required: true}, day("Day", ""), {Name: "value", Type: "number", Title: "Value", Required: true}}},
		{Name: "revive", Method: "POST", Href: "/pages/{id}/revive", Fields: []siren.Field{{Name: "id", Type: "number", In: "path", Required: true}}},
		{Name: "add-file", Method: "POST", Href: "/files", Fields: []siren.Field{
			{Name: "original", Type: "file", Title: "The file", Required: true},
			day("Its day (default: a photo's EXIF date, else none; any other file: today)", "")}},
		{Name: "create-person", Method: "POST", Href: "/people", Fields: []siren.Field{
			{Name: "title", Type: "text", Title: "Name", Required: true}, {Name: "birth_day", Type: "date", Title: "Born"}, {Name: "death_day", Type: "date", Title: "Died"}}},
		{Name: "register-metric", Owner: true, Method: "POST", Href: "/metrics"},
		{Name: "snapshot", Owner: true, Method: "POST", Href: "/snapshot"},
		{Name: "integrity-check", Owner: true, Method: "GET", Href: "/integrity"},
		{Name: "rebuild-search", Owner: true, Method: "POST", Href: "/rebuild-search"},
		{Name: "resync-links", Owner: true, Method: "POST", Href: "/resync-links"},
	}
}
