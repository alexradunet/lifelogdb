package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// The metrics as cards, and file-pages (docs/plans/112-metrics-as-cards.md): several metrics filed in a category in
// one write, on every surface, and the cards /metrics draws of them.

// cardsFixture registers Ferritin (read on two days long ago), Weight (read on the two days before fixedToday), Walk
// (a habit) and Steps (never read), and returns the client, the handler and the metric ids by name.
func cardsFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	for _, m := range []map[string]string{{"name": "Ferritin", "unit": "ug/L", "note": "iron stores"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		must(c.Do(ctx, find(root, "register-metric"), m))
	}
	record := find(must(c.Get(ctx, "/metrics/Ferritin")), "record")
	for _, r := range []map[string]string{{"metric": "Ferritin", "day": "2030-03-01", "value": "40"}, {"metric": "Ferritin", "day": "2030-09-01", "value": "55"},
		{"metric": "Weight", "day": "2031-05-12", "value": "80"}, {"metric": "Weight", "day": "2031-05-13", "value": "79.5"}} {
		must(c.Do(ctx, record, r))
	}
	must(c.Do(ctx, find(must(c.Get(ctx, "/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2031-05-01"}))
	ids := map[string]string{}
	for _, x := range props(must(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		ids[m["name"].(string)] = fmt.Sprint(m["id"])
	}
	return ids
}

// groupsOf says the groups of /metrics: "title:names" for each, in order.
func groupsOf(t *testing.T, c *client.Client) string {
	t.Helper()
	var got []string
	for _, g := range props(must(c.Get(t.Context(), "/metrics")))["groups"].([]any) {
		g := g.(map[string]any)
		var ms []string
		for _, m := range g["metrics"].([]any) {
			ms = append(ms, m.(map[string]any)["name"].(string))
		}
		got = append(got, g["title"].(string)+":"+strings.Join(ms, ","))
	}
	return strings.Join(got, " ")
}

func TestFileMetricsAtOnce(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := find(must(c.Get(ctx, "/metrics")), "file-pages")
	if file.Name == "" || file.Owner || file.Danger || file.Method != "POST" || file.Href != "/pages/file" {
		t.Fatalf("/metrics offers file-pages as %+v", file)
	}
	var fields []string
	for _, f := range file.Fields {
		fields = append(fields, f.Name)
	}
	if !slices.Equal(fields, []string{"ids", "category", "move"}) || !file.Fields[0].Required || !file.Fields[1].Required || file.Fields[2].Required {
		t.Errorf("file-pages fields: %+v", file.Fields)
	}

	// Commas separate the ids; a missing category is made, and said so.
	done := must(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"] + "," + ids["Steps"], "category": "Bloodwork"}))
	p := props(done)
	cat := p["category"].(map[string]any)
	if done.Class[0] != "filed" || cat["title"] != "Bloodwork" || p["created"] != true || p["added"] != json.Number("2") || p["count"] != json.Number("2") ||
		len(done.Entities) != 2 || done.Entities[0].Href != "/pages/"+ids["Ferritin"] || done.Result != "Filed 2 in Bloodwork (a new category)." {
		t.Fatalf("the answer: %v %v %v", done.Class, p, done.Result)
	}
	if got, want := groupsOf(t, c), "Habits:Walk Bloodwork:Ferritin,Steps Not filed:Mood,Weight"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}

	// An agent may file: a part-of link is open to it, as link is.
	agent := client.InProcess(h, "agent:test")
	if again := must(agent.Do(ctx, file, map[string]string{"ids": ids["Weight"], "category": "Body"})); again.Result != "Filed 1 in Body (a new category)." {
		t.Errorf("an agent's filing: %v", again.Result)
	}
	// Move takes Steps out of Bloodwork, and Ferritin stays in it.
	moved := must(c.Do(ctx, file, map[string]string{"ids": ids["Steps"], "category": "Body", "move": "1"}))
	if props(moved)["moved"] != json.Number("1") || moved.Result != "Moved 1 in Body." {
		t.Errorf("the move: %v %v", props(moved), moved.Result)
	}
	if got, want := groupsOf(t, c), "Habits:Walk Bloodwork:Ferritin Body:Steps,Weight Not filed:Mood"; got != want {
		t.Errorf("after the move, groups %q, want %q", got, want)
	}

	// Refusals write nothing: an id no page has, no category, no id, a move that is not 0 or 1.
	for _, bad := range []struct {
		form map[string]string
		want int
	}{
		{map[string]string{"ids": ids["Mood"] + " 424242", "category": "Mood board"}, 404},
		{map[string]string{"ids": ids["Mood"]}, 422},
		{map[string]string{"category": "Mood board"}, 422},
		{map[string]string{"ids": ids["Mood"], "category": "Mood board", "move": "yes"}, 422},
	} {
		if _, err := c.Do(ctx, file, bad.form); !clientStatus(err, bad.want) {
			t.Errorf("%v: %v, want %d", bad.form, err, bad.want)
		}
	}
	if _, err := c.Get(ctx, "/pages?title=Mood+board"); !clientStatus(err, 404) {
		t.Errorf("a refused filing made its category page: %v", err)
	}
	if got := groupsOf(t, c); !strings.HasSuffix(got, "Not filed:Mood") {
		t.Errorf("a refused filing filed Mood: %q", got)
	}
}

func TestFilingTransportParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint(remote), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "life.db")
			if err := db.Init(t.Context(), path); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := d.Close(); err != nil {
					t.Error(err)
				}
			})
			h := api.New(&core.Store{DB: d})
			c := client.InProcess(h, "cli")
			if remote {
				server := httptest.NewServer(h)
				t.Cleanup(server.Close)
				c = client.Remote(server.URL, "cli", "")
			}
			ids := cardsFixture(t, c)
			file := find(must(c.Get(t.Context(), "/metrics")), "file-pages")
			must(c.Do(t.Context(), file, map[string]string{"ids": ids["Weight"] + " " + ids["Steps"], "category": "Sport"}))
			if got, want := groupsOf(t, c), "Habits:Walk Sport:Steps,Weight Not filed:Ferritin,Mood"; got != want {
				t.Errorf("groups %q, want %q", got, want)
			}
		})
	}
}

func TestTheMetricsPageDrawsCards(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := find(must(c.Get(ctx, "/metrics")), "file-pages")
	must(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	must(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	iron := must(c.Get(ctx, "/pages?title=Iron"))
	must(c.Do(ctx, find(iron, "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	// The JSON carries each metric's recent readings, oldest first.
	for _, x := range props(must(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		if m["name"] != "Weight" {
			continue
		}
		r := m["recent"].([]any)
		if len(r) != 2 || r[0].(map[string]any)["day"] != "2031-05-12" || r[1].(map[string]any)["value"] != json.Number("79.5") {
			t.Errorf("Weight's recent readings: %v", r)
		}
	}

	page := browse(t, h, "/metrics")
	for _, want := range []string{
		`<nav class="kinds jump" aria-label="Groups"><a href="#habits">Habits <small>1</small></a><a href="#c-`, `>Body <small>2</small></a>`, `<a href="#not-filed">Not filed <small>2</small></a>`,
		`id="habits"`, `id="not-filed"`, `<h3><a href="/pages/` + idOfSelf(t, hrefOf(iron, "self")) + `">Iron</a> <small>1</small></h3>`,
		`<a class="name" href="/metrics/Weight">Weight</a>`, `79.5 <span class="unit">kg</span>`, `yesterday · Tue 13 May · 2 readings`, `<svg class="trend"`,
		`<div class="reading-tile metric-card stale">`, `title="iron stores">Ferritin</a>`, `No reading yet · n`,
		`<div class="reading-tile metric-card habit-card">`, `<span class="badge">Body</span>`, `class="strip"`, `>Done</button>`,
		`<form id="file-metrics" method="POST" action="/pages/file"`, `<input type="hidden" name="back" value="/metrics">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="file-metrics" aria-label="Select Weight">`, `<option value="Iron">Body/Iron</option>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if main := page[strings.Index(page, "<main"):]; strings.Contains(main, "<table") { // the rail's month is a table
		t.Error("the category lens draws a table")
	}
	if strings.Contains(page, `<details class="file-bar" open>`) {
		t.Error("the file bar is open with nothing refused")
	}

	// The browser's form: a box per card, back to /metrics with the sentence.
	rec := submit(h, "/pages/file", url.Values{"ids": {ids["Mood"], ids["Steps"]}, "category": {"Daily"}, "move": {"0"}, "back": {"/metrics"}}, "")
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
		t.Fatalf("the form: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, "Filed 2 in Daily (a new category).") || !strings.Contains(after, `<a href="#c-`) || strings.Contains(after, `id="not-filed"`) {
		t.Errorf("/metrics after the filing: %.600s", after)
	}
	// A refused form comes back to the page with the bar open and the category it sent.
	rec = submit(h, "/pages/file", url.Values{"category": {"Daily"}, "back": {"/metrics"}}, "")
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `<details class="file-bar" open>`) ||
		!strings.Contains(body, `value="Daily"`) || !strings.Contains(body, "missing field ids") {
		t.Errorf("no box checked: %d %.600s", rec.Code, body)
	}
}
