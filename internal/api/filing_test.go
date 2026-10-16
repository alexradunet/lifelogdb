package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
)

// The metrics as cards, and file-pages (docs/plans/112-metrics-as-cards.md): several metrics filed in a category in
// one write, and the cards /metrics draws of them. The parity table of cmd/lifelog runs file-pages on every surface.

// cardsFixture registers Ferritin (read on two days long ago), Weight (read on the two days before fixedToday), Walk
// (a habit) and Steps (never read), and returns the client, the handler and the metric ids by name.
func cardsFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	for _, m := range []map[string]string{{"name": "Ferritin", "unit": "ug/L", "note": "iron stores"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), m))
	}
	record := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics/Ferritin")), "record")
	for _, r := range []map[string]string{{"metric": "Ferritin", "day": "2030-03-01", "value": "40"}, {"metric": "Ferritin", "day": "2030-09-01", "value": "55"},
		{"metric": "Weight", "day": "2031-05-12", "value": "80"}, {"metric": "Weight", "day": "2031-05-13", "value": "79.5"}} {
		apitest.Must(t)(c.Do(ctx, record, r))
	}
	ids := map[string]string{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		ids[m["name"].(string)] = fmt.Sprint(m["id"])
	}
	return ids
}

// groupsOf says the groups of /metrics: "title:names" for each, in order.
func groupsOf(t *testing.T, c *client.Client) string {
	t.Helper()
	var got []string
	for _, g := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["groups"].([]any) {
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
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	if file.Owner || file.Danger || file.Method != "POST" || file.Href != "/pages/file" {
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
	done := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"] + "," + ids["Steps"], "category": "Bloodwork"}))
	p := props(done)
	cat := p["category"].(map[string]any)
	if done.Class[0] != "filed" || cat["title"] != "Bloodwork" || p["created"] != true || p["added"] != json.Number("2") || p["count"] != json.Number("2") ||
		len(done.Entities) != 2 || done.Entities[0].Href != "/pages/"+ids["Ferritin"] || done.Result != "Put 2 in Bloodwork (a new category)." {
		t.Fatalf("the answer: %v %v %v", done.Class, p, done.Result)
	}
	if got, want := apitest.Href(done, "self"), "/pages/"+fmt.Sprint(cat["id"]); got != want {
		t.Errorf("the answer's self is %s, want the category %s", got, want)
	}
	if got, want := groupsOf(t, c), "Bloodwork:Ferritin,Steps Not filed:Mood,Walk,Weight"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}

	// An agent may file: a part-of link is open to it, as link is.
	agent := client.InProcess(h, "agent:test")
	if again := apitest.Must(t)(agent.Do(ctx, file, map[string]string{"ids": ids["Weight"], "category": "Body"})); again.Result != "Put 1 in Body (a new category)." {
		t.Errorf("an agent's filing: %v", again.Result)
	}
	// Move takes Steps out of Bloodwork, and Ferritin stays in it.
	moved := apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Steps"], "category": "Body", "move": "1"}))
	if props(moved)["moved"] != json.Number("1") || moved.Result != "Moved 1 in Body." {
		t.Errorf("the move: %v %v", props(moved), moved.Result)
	}
	if got, want := groupsOf(t, c), "Bloodwork:Ferritin Body:Steps,Weight Not filed:Mood,Walk"; got != want {
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
		if _, err := c.Do(ctx, file, bad.form); apitest.Status(err) != bad.want {
			t.Errorf("%v: %v, want %d", bad.form, err, bad.want)
		}
	}
	if _, err := c.Get(ctx, "/pages?title=Mood+board"); apitest.Status(err) != 404 {
		t.Errorf("a refused filing made its category page: %v", err)
	}
	if got := groupsOf(t, c); !strings.HasSuffix(got, "Not filed:Mood,Walk") {
		t.Errorf("a refused filing filed Mood: %q", got)
	}
}

func TestTheMetricsPageDrawsCards(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	iron := apitest.Must(t)(c.Get(ctx, "/pages?title=Iron"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, iron, "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	// The JSON carries each metric's recent readings, oldest first.
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
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
		`<nav class="kinds jump" aria-label="Groups"><a href="#c-`, `>Body <small>3</small></a>`, `<a href="#not-filed">Not filed <small>2</small></a>`,
		`id="not-filed"`, `<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `, `<a href="/pages/` + idOfSelf(t, apitest.Href(iron, "self")) + `">Open Iron</a>`,
		`<a class="name" href="/metrics/Weight">Weight</a>`, `79.5 <span class="unit">kg</span>`, `<time class="meta" datetime="2031-05-13" title="yesterday · Tue 13 May · 2 readings">yesterday</time>`, `<svg class="trend"`,
		`<div class="reading-tile metric-card stale">`, `title="iron stores">Ferritin</a>`, `<span class="reading none"><span aria-hidden="true">–</span> <span class="unit">n</span></span><span class="line-none" aria-hidden="true"></span>`, `<div class="card-foot"><span class="meta">No reading</span>`,
		`<form id="pick" class="pick-bar" method="POST" action="/pages/file">`, `<input type="hidden" name="back" value="/metrics">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`, `<option value="Iron">Body/Iron</option>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if main := page[strings.Index(page, "<main"):]; strings.Contains(main, "<table") { // the rail's month is a table
		t.Error("the category lens draws a table")
	}
	if strings.Contains(page, `class="pick-on" checked`) {
		t.Error("Select is on with nothing refused")
	}

	// The browser's form: a box per card, back to /metrics with the sentence.
	rec := submit(h, "/pages/file", url.Values{"ids": {ids["Mood"], ids["Steps"]}, "category": {"Daily"}, "move": {"0"}, "back": {"/metrics"}}, "")
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
		t.Fatalf("the form: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, "Put 2 in Daily (a new category).") || !strings.Contains(after, `<a href="#c-`) || strings.Contains(after, `id="not-filed"`) {
		t.Errorf("/metrics after the filing: %.600s", after)
	}
	// A refused form comes back to the page with Select on and the category it sent.
	rec = submit(h, "/pages/file", url.Values{"category": {"Daily"}, "back": {"/metrics"}}, "")
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `<input type="checkbox" class="pick-on" checked>`) ||
		!strings.Contains(body, `value="Daily"`) || !strings.Contains(body, "missing field ids") {
		t.Errorf("no box checked: %d %.600s", rec.Code, body)
	}
}

// A day page is never filed (D26): file-pages that names a day page is a 422 with SQLite's sentence and writes
// nothing, not even for the other ids or the category it would make; a part-of link from a day page is a 422; and a
// day page's link form does not offer part-of.
func TestADayPageIsNeverFiled(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	plain := fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Plans"})))["id"])
	day := apitest.Must(t)(c.Get(ctx, apitest.Href(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "a synthetic entry"})), "page")))
	dayID := fmt.Sprint(props(day)["id"])
	const sentence = "a day page is never filed in a category (D26)"

	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+plain)), "file-pages")
	for _, ids := range []string{dayID, plain + " " + dayID, dayID + "," + plain} {
		_, err := c.Do(ctx, file, map[string]string{"ids": ids, "category": "Garden"})
		if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(err), sentence) {
			t.Errorf("file-pages %s: %v, want a 422 that says %q", ids, err, sentence)
		}
	}
	if _, err := c.Get(ctx, "/pages?title=Garden"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("a refused filing made its category page: %v", err)
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, "/pages/"+plain))); len(got) != 0 {
		t.Errorf("a refused filing filed Plans in %v", got)
	}

	if hasOption(apitest.Action(t, day, "link"), "kind", "part-of") || !hasOption(apitest.Action(t, day, "link"), "kind", "at") {
		t.Errorf("a day page's link offers %+v: at, and never part-of", apitest.Action(t, day, "link").Fields)
	}
	if !hasOption(apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+plain)), "link"), "kind", "part-of") {
		t.Error("a plain page's link does not offer part-of")
	}
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": plain, "category": "Garden"}))
	_, err := c.Do(ctx, apitest.Action(t, day, "link"), map[string]string{"to": "Garden", "kind": "part-of"})
	if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(err), sentence) {
		t.Errorf("link part-of from a day page: %v, want a 422 that says %q", err, sentence)
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, "/pages/"+dayID))); len(got) != 0 {
		t.Errorf("the day page is part-of %v", got)
	}
}

// The Metrics board folds, and ?view=list draws the same metrics as rows (docs/plans/118-fold-and-find.md, rules 4
// and 5).
func TestTheMetricsBoardFoldsAndHasAList(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages?title=Iron")), "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	cards := browse(t, h, "/metrics")
	for _, want := range []string{
		`<nav class="kinds view-switch" aria-label="View"><a href="/metrics" aria-current="true">Cards</a><a href="/metrics?view=list">List</a></nav>`,
		// Body holds Weight and Walk, and Ferritin under Iron: its newest reading is Weight's, yesterday.
		`<details class="group metric-group" id="c-`, ` open><summary><h2>Body <small>3</small></h2><span class="meta">newest yesterday · Tue 13 May</span></summary>`,
		`<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `,
		// Mood and Steps have no reading: the line of Not filed says no day.
		`<details class="group metric-group" id="not-filed" open><summary><h2>Not filed <small>2</small></h2></summary>`,
	} {
		if !strings.Contains(cards, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	list := browse(t, h, "/metrics?view=list")
	for _, want := range []string{
		`<a href="/metrics">Cards</a><a href="/metrics?view=list" aria-current="true">List</a>`,
		`<ul class="plain metric-rows"><li class="metric-row pick">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`,
		`<a class="name" href="/metrics/Weight">Weight</a>`,
		`79.5 <span class="unit">kg</span></a><span class="meta">yesterday · Tue 13 May</span><span class="line"><svg class="trend"`,
		`<li class="metric-row pick stale">`, `<span class="meta none">No reading yet · n</span>`,
		`<input type="hidden" name="back" value="/metrics?view=list">`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("/metrics?view=list lacks %s", want)
		}
	}
	// Each + Add of a row and the pick bar return to the list.
	if strings.Contains(list, `class="reading-tile metric-card`) || strings.Count(list, `<input type="hidden" name="back" value="/metrics?view=list">`) != 6 {
		t.Error("the list draws a card, or a form of it does not return to the list")
	}
	// The tools of a group are its foot: after its cards, and in Iron after Ferritin (docs/plans/119-the-finish.md, rule 3).
	for _, page := range []string{cards, list} {
		iron := page[strings.Index(page, `<h3>Iron`):]
		if ferritin, tools := strings.Index(iron, `>Ferritin</a>`), strings.Index(iron, `<div class="group-tools">`); ferritin < 0 || tools < ferritin {
			t.Errorf("the tools of Iron come before its cards: Ferritin at %d, the tools at %d", ferritin, tools)
		}
	}
	names := regexp.MustCompile(`<a class="name" href="/metrics/(\w+)"`)
	if a, b := names.FindAllString(cards, -1), names.FindAllString(list, -1); len(a) != 5 || strings.Join(a, " ") != strings.Join(b, " ") {
		t.Errorf("the cards %v and the rows %v are not the same metrics", a, b)
	}

	// + Add on a row returns to the list.
	rec := submit(h, "/measurements", url.Values{"metric": {"Steps"}, "day": {fixedToday}, "value": {"9000"}, "back": {"/metrics?view=list"}}, "")
	if loc, err := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || err != nil || loc.Path != "/metrics" || loc.Query().Get("view") != "list" {
		t.Fatalf("+ Add on the list: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, `9000 <span class="unit">n</span></a><span class="meta">today</span>`) {
		t.Error("the list after + Add lacks the reading")
	}
}
