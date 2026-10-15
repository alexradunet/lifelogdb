package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// journalFixture is a small life around fixedToday: a mood; two earlier day pages, one written as a mood alone.
func journalFixture(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h := freshOn(t, fixedToday)
	capture := func(day string, v map[string]string) {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture(dayOf(-2), map[string]string{"text": "## Morning\nRan with [[Sam]] by the river."})
	capture(dayOf(-1), map[string]string{"mood": "2"})
	capture(fixedToday, map[string]string{"text": "Quiet day.", "mood": "4"})
	capture(fixedToday, map[string]string{"mood": "3"})
	return c, h
}

// actionsNamed are the hrefs of an entity's actions of that name.
func actionsNamed(e *api.Entity, name string) []string {
	var out []string
	for _, a := range e.Actions {
		if a.Name == name {
			out = append(out, a.Href)
		}
	}
	return out
}

func TestTheDayCarriesItsMood(t *testing.T) {
	c, h := journalFixture(t)

	today := must(c.Get(t.Context(), "/days/"+fixedToday))
	p := props(today)
	if fmt.Sprint(p["mood"]) != "[4 3]" || p["today"] != fixedToday {
		t.Errorf("today's mood %v, today %v", p["mood"], p["today"])
	}
	save := find(today, "save-body")
	page := must(c.Get(t.Context(), hrefOf(today, "page")))
	if save.Href != hrefOf(today, "page")+"/body" || fieldOf(save, "version") != fmt.Sprint(props(page)["version"]) ||
		fieldOf(save, "body") != props(page)["body"] {
		t.Errorf("the day's save-body: %+v, page %v", save, props(page))
	}

	// A past day with nothing on it.
	past := props(must(c.Get(t.Context(), "/days/"+dayOf(-20))))
	if fmt.Sprint(past["mood"]) != "[]" {
		t.Errorf("a day with no mood: %v", past["mood"])
	}
	quiet := must(c.Get(t.Context(), "/days/"+dayOf(-5)))
	if find(quiet, "save-body").Name != "" {
		t.Errorf("a quiet day: save-body %v", find(quiet, "save-body").Href)
	}

	// The browser.
	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{`<span class="mood" title="Mood">mood 4, 3</span>`, "<summary>Edit the day page</summary>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the day view lacks %q", want)
		}
	}
	if strings.Contains(body, `<th scope="col">scope</th>`) {
		t.Error("the readings show a scope column")
	}
}

func TestTheRootIsTodayAtAGlance(t *testing.T) {
	c, h := journalFixture(t)
	root := must(c.Get(t.Context(), "/"))
	p := props(root)
	var recent []string
	for _, r := range p["recent"].([]any) {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> Morning"; got != want {
		t.Errorf("recent:\n got %s\nwant %s", got, want)
	}

	body := browse(t, h, "/")
	for _, want := range []string{"<h2>Last entries</h2>", "Quiet day."} {
		if !strings.Contains(body, want) {
			t.Errorf("the root lacks %q", want)
		}
	}
}
