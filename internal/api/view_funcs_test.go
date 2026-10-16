package api

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestNumWritesNumbersInFull keeps an id a path can carry: the JSON client reads every number as a float64.
func TestNumWritesNumbersInFull(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{1000000.0, "1000000"}, {123456789012.0, "123456789012"}, {71.1, "71.1"}, {0.0000001, "0.0000001"},
		{-2.5, "-2.5"}, {nil, ""}, {"text", "text"}, {int64(7), "7"}, {true, "true"},
	} {
		if got := num(c.v); got != c.want {
			t.Errorf("num(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestAPageThatCannotBeDrawnNamesNoCause: when a page cannot be drawn (its properties or its result do not encode as
// JSON, or its template fails on them), the browser gets a 500 that says no word of the internal cause. The server's
// log has the cause one time, with the pattern of the route and not the path.
func TestAPageThatCannotBeDrawnNamesNoCause(t *testing.T) {
	for name, e := range map[string]*Entity{
		"properties that are not JSON": {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": math.NaN()}},
		"a result that is not JSON":    {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": 1}, Result: map[string]any{"value": math.Inf(1)}},
		"a template that fails":        {Class: []string{"error"}, Title: "Error", Properties: "not an object"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/things/1", nil)
			req.Pattern = "GET /things/{id}" // as the routes set it
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			renderHTML(rec, req, http.StatusOK, e, nil)
			body := rec.Body.String()
			if n := strings.Count(logged.String(), "cannot be drawn"); n != 1 || !strings.Contains(logged.String(), "GET /things/{id}") ||
				strings.Contains(logged.String(), "/things/1") {
				t.Errorf("the log does not have the cause one time with the route's pattern, or names the path: %q", logged.String())
			}
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status %d, want 500", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "</html>") {
				t.Errorf("the answer is not a page: %q %.200s", rec.Header().Get("Content-Type"), body)
			}
			for _, cause := range []string{"template:", "executing", "evaluate", "json:", "unsupported value"} {
				if strings.Contains(body, cause) {
					t.Errorf("the page names the cause (%q): %.300s", cause, body)
				}
			}
		})
	}
}

// TestAnAnswerThatJSONCannotCarryIsAFault: when an entity has a value that JSON cannot carry (a NaN, an infinity), a
// program gets 500 and the error entity with the general message, not 200 and a cut body. The server's log has the
// cause one time, with the pattern of the route and not the path.
func TestAnAnswerThatJSONCannotCarryIsAFault(t *testing.T) {
	for name, e := range map[string]*Entity{
		"properties that are not JSON": {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": math.NaN()}},
		"a result that is not JSON":    {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": 1}, Result: map[string]any{"value": math.Inf(1)}},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/things/1", nil)
			req.Pattern = "GET /things/{id}" // as the routes set it
			req.Header.Set("Accept", "application/vnd.siren+json")
			rec := httptest.NewRecorder()
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			writeEntity(rec, req, http.StatusOK, e)
			if n := strings.Count(logged.String(), "cannot be encoded"); n != 1 || !strings.Contains(logged.String(), "GET /things/{id}") ||
				strings.Contains(logged.String(), "/things/1") {
				t.Errorf("the log does not have the cause one time with the route's pattern, or names the path: %q", logged.String())
			}
			var got struct {
				Class      []string
				Properties struct {
					Status        int
					Code, Message string
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("the body is not JSON: %v: %.300s", err, rec.Body.String())
			}
			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/vnd.siren+json" ||
				!slices.Equal(got.Class, []string{"error"}) || got.Properties.Status != http.StatusInternalServerError ||
				got.Properties.Code != "internal" || got.Properties.Message != "internal error" {
				t.Errorf("status %d %q, want 500 and the error entity with the general message: %.300s",
					rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
}

// TestHereDropsTheReceipt: a form returns to the page it is on, but never with the receipt that page already took.
func TestHereDropsTheReceipt(t *testing.T) {
	for raw, want := range map[string]string{
		"/days?feedback=abc&limit=5": "/days?limit=5",
		"/pages/4?feedback=abc":      "/pages/4",
		"/":                          "/",
		"/pages/Caf%C3%A9":           "/pages/Caf%C3%A9",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := hereOf(u); got != want {
			t.Errorf("hereOf(%s) = %s, want %s", raw, got, want)
		}
	}
}

// TestWeeksStartOnMonday lays months out by hand-checked first weekdays: a month that starts on a Monday, one that
// starts on a Sunday, and a February of four whole weeks.
func TestWeeksStartOnMonday(t *testing.T) {
	month := func(first string, n int) []any {
		t0, _ := time.Parse(time.DateOnly, first)
		var out []any
		for i := range n {
			out = append(out, map[string]any{"day": t0.AddDate(0, 0, i).Format(time.DateOnly)})
		}
		return out
	}
	show := func(ws [][]any) string {
		var rows []string
		for _, w := range ws {
			var cells []string
			for _, d := range w {
				if d == nil {
					cells = append(cells, "--")
				} else {
					cells = append(cells, d.(map[string]any)["day"].(string)[8:])
				}
			}
			rows = append(rows, strings.Join(cells, " "))
		}
		return strings.Join(rows, " / ")
	}
	for _, c := range []struct {
		first string
		n     int
		want  string
	}{
		{"2026-06-01", 30, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28 / 29 30 -- -- -- -- --"},
		{"2026-11-01", 30, "-- -- -- -- -- -- 01 / 02 03 04 05 06 07 08 / 09 10 11 12 13 14 15 / 16 17 18 19 20 21 22 / 23 24 25 26 27 28 29 / 30 -- -- -- -- -- --"},
		{"2027-02-01", 28, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28"},
	} {
		if got := show(weeks(month(c.first, c.n))); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.first, got, c.want)
		}
	}
	if weeks([]any{}) != nil {
		t.Error("no day makes weeks")
	}
}
