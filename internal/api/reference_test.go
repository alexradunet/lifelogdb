package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// TestAPIReferenceMatchesTheCode keeps API.md true: every property table of a class names exactly the paths the
// Go property type carries, and every route's live JSON carries only the top-level keys its table names.
func TestAPIReferenceMatchesTheCode(t *testing.T) {
	documented := readReference(t, filepath.Join("..", "..", "API.md"))
	// The shape behind each section: a Go value whose JSON names, walked, are the paths the table must list.
	shapes := map[string]any{
		"root":                                map[string]any{"today": ""},
		"days, people, places, files, ghosts": map[string]any{"limit": 0, "offset": 0},
		"search":                              map[string]any{"q": "", "hits": []core.Hit{}, "limit": 0, "offset": 0},
		"day":                                 core.Day{},
		"page, person, place, file, period":   core.Page{},
		"measurement":                         core.Measurement{},
		"series":                              map[string]any{"metric": "", "from": "", "to": "", "readings": []core.Reading{}, "habit_periods": []core.Period{}, "categories": []string{}, "scope": "", "session_id": int64(0), "include_deleted": false, "all_history": false},
		"metrics":                             map[string]any{"metrics": []core.Metric{}, "groups": []metricGroup{}},
		"habits":                              map[string]any{"day": "", "habits": []core.HabitState{}, "from": "", "to": "", "completion": []core.Completion{}},
		"sessions":                            map[string]any{"sessions": []core.Session{}, "include_deleted": false, "order": ""},
		"session":                             core.Session{},
		"periods":                             map[string]any{"periods": []core.LifePeriod{}, "day": "", "as_of": "", "membership_basis": "", "include_deleted": false},
		"tasks":                               map[string]any{"tasks": []core.Task{}, "include_deleted": false},
		"task":                                map[string]any{"task": core.Task{}, "from": "", "through": "", "include_deleted": false, "occurrences": []occurrenceView{}},
		"occurrence":                          occurrenceView{},
		"deadlines":                           map[string]any{"from": "", "through": "", "state": "", "include_deleted": false, "occurrences": []occurrenceView{}},
		"integrity":                           core.IntegrityResult{},
		"result":                              core.Result{},
		"login":                               map[string]any{"failed": false, "logged_in": false},
		"result of capture, create-page, save-body": core.Sync{},
		"result of add-file":                        core.Kept{},
		"result of relocate-reading":                core.ScopeRelocation{},
	}
	for section, shape := range shapes {
		want := map[string]bool{}
		walk("", reflect.ValueOf(shape), want)
		got, ok := documented[section]
		if !ok {
			t.Errorf("API.md has no section %q", section)
			continue
		}
		for p := range want {
			if !got[p] {
				t.Errorf("API.md %q lacks %s", section, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("API.md %q documents %s, which the code does not send", section, p)
			}
		}
	}
	for section := range documented {
		if _, ok := shapes[section]; !ok && section != "errors" && section != "import, …" && section != "actions" {
			t.Errorf("API.md section %q is not checked here: add its shape", section)
		}
	}
	if e := documented["errors"]; !e["status"] || !e["code"] || !e["message"] || len(e) != 3 {
		t.Errorf("errors section: %v", e)
	}

	// Live: every route's top-level keys are documented for its section.
	h, c, ids := fixture(t)
	live := []struct{ section, route string }{
		{"root", "/"}, {"days, people, places, files, ghosts", "/days"}, {"days, people, places, files, ghosts", "/people"},
		{"days, people, places, files, ghosts", "/files"}, {"days, people, places, files, ghosts", "/ghosts"}, {"search", "/search?q=word"},
		{"day", "/days/2031-05-01"}, {"page, person, place, file, period", ids["page"]}, {"page, person, place, file, period", ids["person"]},
		{"page, person, place, file, period", ids["place"]}, {"page, person, place, file, period", ids["file"]}, {"page, person, place, file, period", ids["period"]},
		{"measurement", ids["measurement"]}, {"series", "/metrics/Weight"}, {"metrics", "/metrics"}, {"habits", "/habits?day=2031-05-01"},
		{"sessions", "/sessions"}, {"session", ids["session"]}, {"periods", "/periods?day=2031-05-01"}, {"integrity", "/integrity"},
		{"tasks", "/tasks"}, {"task", ids["task"]}, {"occurrence", ids["task"] + "/occurrences/once"}, {"deadlines", "/deadlines?from=2031-05-01&through=2031-05-31"},
	}
	named := func(section, key string) bool { // a list is documented as "name[]"
		return documented[section][key] || documented[section][key+"[]"]
	}
	for _, l := range live {
		props := properties(t, h, "GET", l.route, "")
		for k := range props {
			if !named(l.section, k) {
				t.Errorf("GET %s sends %q, which API.md %q does not name", l.route, k, l.section)
			}
		}
	}
	for k := range properties(t, h, "POST", "/query", "sql=SELECT+1") {
		if !named("result", k) {
			t.Errorf("POST /query sends %q, which API.md \"result\" does not name", k)
		}
	}
	for k := range properties(t, h, "GET", "/pages/999999", "") {
		if !documented["errors"][k] {
			t.Errorf("an error sends %q, which API.md \"errors\" does not name", k)
		}
	}
	_ = c
}

// readReference is the property paths of every "### section" of API.md, from the first column of its tables.
func readReference(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]map[string]bool{}
	row := regexp.MustCompile("^\\| `([^`]+)` \\|")
	section := ""
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if strings.HasPrefix(line, "### ") {
			section = strings.TrimPrefix(line, "### ")
			out[section] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "## Errors") {
			section = "errors"
			out[section] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			section = ""
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil && section != "" {
			if out[section][m[1]] {
				t.Errorf("API.md %q lists %s twice", section, m[1])
			}
			out[section][m[1]] = true
		}
	}
	return out
}

// walk adds the JSON paths of v: a struct's fields by their json names (embedded ones flattened), an object as
// "name" and "name.field", a list of objects as "name[]" and "name[].field", anything else as a leaf.
func walk(prefix string, v reflect.Value, out map[string]bool) {
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, k := range keys {
			add(prefix+k.String(), v.MapIndex(k).Elem(), out)
		}
	case reflect.Struct:
		walkType(prefix, t, out)
	}
}

func add(path string, v reflect.Value, out map[string]bool) {
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t.Kind() == reflect.Struct:
		out[path] = true
		walkType(path+".", t, out)
	case t.Kind() == reflect.Slice && elemStruct(t) != nil:
		out[path+"[]"] = true
		walkType(path+"[].", elemStruct(t), out)
	default:
		out[path] = true
	}
}

func elemStruct(t reflect.Type) reflect.Type {
	e := t.Elem()
	for e.Kind() == reflect.Pointer {
		e = e.Elem()
	}
	if e.Kind() == reflect.Struct {
		return e
	}
	return nil
}

func walkType(prefix string, t reflect.Type, out map[string]bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			walkType(prefix, f.Type, out)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		add(prefix+name, reflect.New(f.Type).Elem(), out)
	}
}

// fixture is a database with one of everything the routes show, and the hrefs of the pages made.
func fixture(t *testing.T) (http.Handler, *core.Store, map[string]string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	ctx := t.Context()
	ids := map[string]string{}
	personID, err := s.CreatePerson(ctx, "cli", "Sam", "Sam Synthetic", "2001-01-01", "")
	if err != nil {
		t.Fatal(err)
	}
	ids["person"] = fmt.Sprintf("/pages/%d", personID)
	placeID, err := s.CreatePlace(ctx, "cli", "Lakeside")
	if err != nil {
		t.Fatal(err)
	}
	ids["place"] = fmt.Sprintf("/pages/%d", placeID)
	pageID, _, err := s.CreatePage(ctx, "cli", "A note", "word [[Sam]] #tag")
	if err != nil {
		t.Fatal(err)
	}
	ids["page"] = fmt.Sprintf("/pages/%d", pageID)
	if _, _, err := s.Capture(ctx, "cli", "2031-05-01", "word at [[Lakeside]]", nil); err != nil {
		t.Fatal(err)
	}
	kept, err := s.AddFile(ctx, "cli", core.FileIn{Title: "memo.txt", SHA256: strings.Repeat("b", 64), MIME: "text/plain", Body: "word", Day: "2031-05-01"})
	if err != nil {
		t.Fatal(err)
	}
	ids["file"] = fmt.Sprintf("/pages/%d", kept.ID)
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", "morning"); err != nil {
		t.Fatal(err)
	}
	mid, err := s.Record(ctx, "cli", core.Reading{Metric: "Weight", Day: "2031-05-01", Value: 70})
	if err != nil {
		t.Fatal(err)
	}
	ids["measurement"] = fmt.Sprintf("/measurements/%d", mid)
	if _, err := s.RegisterMetric(ctx, "cli", "Walk", "", "1 = done"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Walk", "2031-04-01", ""); err != nil {
		t.Fatal(err)
	}
	h := New(s, nil)
	req := httptest.NewRequest("POST", "/periods", strings.NewReader("title=School&start_boundary=2030-01-01&end_boundary=2032-01-01"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create-period: %d %.300s", rec.Code, rec.Body.String())
	}
	ids["period"] = rec.Header().Get("Location")
	req = httptest.NewRequest("POST", "/sessions", strings.NewReader("kind=A+note&day=2031-05-01&start_at=2031-05-01T08:00:00.000Z&end_at=2031-05-01T09:00:00.000Z"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("capture-session: %d %.300s", rec.Code, rec.Body.String())
	}
	ids["session"] = rec.Header().Get("Location")
	req = httptest.NewRequest("POST", "/tasks", strings.NewReader("label=Plant+beans&project=A+note&due_day=2031-05-02"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create-task: %d %.300s", rec.Code, rec.Body.String())
	}
	ids["task"] = strings.SplitN(rec.Header().Get("Location"), "?", 2)[0]
	return h, s, ids
}

// properties is the top-level property keys of a route's JSON answer.
func properties(t *testing.T, h http.Handler, method, route, body string) map[string]bool {
	t.Helper()
	req := httptest.NewRequest(method, route, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var e struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("%s %s: %d %v %.200s", method, route, rec.Code, err, rec.Body.String())
	}
	if rec.Code >= 300 && route != "/pages/999999" {
		t.Fatalf("%s %s: %d %.300s", method, route, rec.Code, rec.Body.String())
	}
	out := map[string]bool{}
	for k := range e.Properties {
		out[k] = true
	}
	return out
}
