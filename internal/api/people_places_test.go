package api_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt: the answer 409 keeps "open it" and adds a link to the
// promote form of that page, with the kind and the fields typed (docs/plans/141-the-access-pass.md, part 2).
func TestANewPersonWithTheTitleOfAPlainPageOffersToPromoteIt(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Sam"}))
	samHref := apitest.Href(sam, "self")
	pier := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Pier"}))
	ana := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana"}))

	post := func(path string, form url.Values) string {
		t.Helper()
		rec := submit(h, path, form, "")
		if rec.Code != http.StatusConflict {
			t.Fatalf("POST %s %v: %d, want 409", path, form, rec.Code)
		}
		return rec.Body.String()
	}
	body := post("/people", url.Values{"title": {"Sam"}, "name": {"Samuel Ortiz"}, "birth_day": {"1980-02-03"}})
	for _, want := range []string{`open it</a>`, `href="` + samHref + `?birth_day=1980-02-03&amp;name=Samuel&#43;Ortiz&amp;new=promote&amp;to=person">make it a person, with what you typed</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the 409 of a person does not say %s:\n%.600s", want, body)
		}
	}
	// The link opens the promote form with the fields filled.
	page := browse(t, h, samHref+"?birth_day=1980-02-03&name=Samuel+Ortiz&new=promote&to=person")
	for _, want := range []string{`value="Samuel Ortiz"`, `value="1980-02-03"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the promote form is not filled: it lacks %s", want)
		}
	}
	// A place takes no full name and no dates: its link carries the kind alone.
	body = post("/places", url.Values{"title": {"Pier"}})
	if want := apitest.Href(pier, "self") + `?new=promote&amp;to=place">make it a place`; !strings.Contains(body, want) {
		t.Errorf("the 409 of a place does not say %s:\n%.600s", want, body)
	}
	// A page that is a person already has nothing to promote: only "open it".
	if body = post("/people", url.Values{"title": {"Ana"}}); strings.Contains(body, "make it a") || !strings.Contains(body, apitest.Href(ana, "self")) {
		t.Errorf("the 409 of a taken person offers a promotion or no link to it:\n%.600s", body)
	}
}

// TestPromoteTakesTheDatesOfAPersonAndRefusesThemForAPlace: through the API, as a client of any surface sends them.
func TestPromoteTakesTheDatesOfAPersonAndRefusesThemForAPlace(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Sam"}))
	pier := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Pier"}))
	if _, err := c.Do(ctx, apitest.Action(t, pier, "promote"), map[string]string{"to": "place", "birth_day": "1980-02-03"}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("a place with a birth day: %v, want 422", err)
	}
	got := apitest.Must(t)(c.Do(ctx, apitest.Action(t, sam, "promote"), map[string]string{"to": "person", "name": "Samuel Ortiz", "birth_day": "1980-02-03"}))
	if person, _ := props(got)["person"].(map[string]any); person["name"] != "Samuel Ortiz" || person["birth_day"] != "1980-02-03" || person["death_day"] != nil {
		t.Errorf("the person is %v", person)
	}
}

// TestEditPersonThroughTheAPIAndTheBrowser: the person's page offers the action with the current values, a field left
// out stays, an empty date is removed, and a page that is not a person is refused.
func TestEditPersonThroughTheAPIAndTheBrowser(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz", "birth_day": "1980-02-03"}))
	self := apitest.Href(sam, "self")

	act := apitest.Action(t, sam, "edit-person")
	if act.Href != self+"/person" || act.Title != "Edit name and dates" {
		t.Errorf("the action is %s %q", act.Href, act.Title)
	}
	for _, f := range act.Fields {
		if want := map[string]any{"name": "Samuel Ortiz", "birth_day": "1980-02-03", "death_day": ""}[f.Name]; f.Value != want {
			t.Errorf("the field %s offers %v, want %v", f.Name, f.Value, want)
		}
	}
	page := browse(t, h, self)
	for _, want := range []string{`Edit name and dates`, `value="Samuel Ortiz"`, `value="1980-02-03"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the person's page lacks %s", want)
		}
	}
	// A form of the browser sends every field; an empty date removes it.
	rec := submit(h, act.Href, url.Values{"name": {"Samuel A. Ortiz"}, "birth_day": {""}, "death_day": {"2020-01-01"}}, self)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the form answered %d, want 303: %.300s", rec.Code, rec.Body.String())
	}
	person := func() map[string]any {
		t.Helper()
		p, _ := props(apitest.Must(t)(c.Get(ctx, self)))["person"].(map[string]any)
		return p
	}
	if p := person(); p["name"] != "Samuel A. Ortiz" || p["birth_day"] != nil || p["death_day"] != "2020-01-01" {
		t.Errorf("the person is %v, want a new name, no birth day and a death day", p)
	}
	// A request that names the name alone leaves the dates. The templated action of /actions has no values filled in.
	catalog := apitest.Must(t)(c.Get(ctx, "/actions"))
	edit := apitest.Action(t, catalog, "edit-person")
	id := strings.TrimPrefix(self, "/pages/")
	apitest.Must(t)(c.Do(ctx, edit, map[string]string{"id": id, "name": "Sam O."}))
	if p := person(); p["name"] != "Sam O." || p["death_day"] != "2020-01-01" {
		t.Errorf("the person is %v, want only the name changed", p)
	}
	if _, err := c.Do(ctx, edit, map[string]string{"id": id, "birth_day": "2021-01-01"}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("a birth after the death: %v, want 422", err)
	}
	if _, err := c.Do(ctx, edit, map[string]string{"id": id, "name": ""}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("an empty name: %v, want 422", err)
	}
	// A page that is not a person offers no such action, and the route refuses it.
	plain := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Plain"}))
	for _, a := range plain.Actions {
		if a.Name == "edit-person" {
			t.Error("a plain page offers edit-person")
		}
	}
	if rec := submit(h, apitest.Href(plain, "self")+"/person", url.Values{"name": {"X"}}, ""); rec.Code != http.StatusConflict {
		t.Errorf("edit-person on a plain page: %d, want 409", rec.Code)
	}
}

// TestTheWhereOfADayLinksAPlaceAndRemovesIt: Where is always drawn; "I was at a place" leads to the link form of the day
// page with the kind filled and the places as options, and the x of a place sends unlink for it.
func TestTheWhereOfADayLinksAPlaceAndRemovesIt(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-place"), map[string]string{"title": "Lisbon"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-place"), map[string]string{"title": "Porto"}))

	// A day with no day page: Where says what to do first, and draws no link form.
	empty := browse(t, h, "/days/2026-03-01")
	if !strings.Contains(empty, `aria-label="Where"`) || !strings.Contains(empty, "Write something in the journal first") || strings.Contains(empty, "I was at a place") {
		t.Errorf("the Where of a day with no page:\n%s", section(empty, "Where"))
	}

	catalog := apitest.Must(t)(c.Get(ctx, "/actions"))
	day := apitest.Must(t)(c.Do(ctx, apitest.Action(t, catalog, "capture"), map[string]string{"day": "2026-03-02", "text": "Walked."}))
	dayPage := apitest.Href(day, "page")
	where := section(browse(t, h, "/days/2026-03-02"), "Where")
	if want := `<a href="` + dayPage + `?new=link&amp;kind=at">I was at a place</a>`; !strings.Contains(where, want) || !strings.Contains(where, "No place on this day") {
		t.Errorf("the Where of a day with a page and no place:\n%s", where)
	}
	// The link opens the day page's link form: kind filled, places offered.
	form := browse(t, h, dayPage+"?new=link&kind=at")
	for _, want := range []string{`<option value="Lisbon">`, `<option value="Porto">`, `<option selected>at</option>`} {
		if !strings.Contains(form, want) {
			t.Errorf("the link form of the day page lacks %s", want)
		}
	}
	link := apitest.Action(t, apitest.Must(t)(c.Get(ctx, dayPage)), "link")
	if _, err := c.Do(ctx, link, map[string]string{"to": "Lisbon", "kind": "at"}); err != nil {
		t.Fatal(err)
	}
	where = section(browse(t, h, "/days/2026-03-02"), "Where")
	unlink := regexp.MustCompile(`<form class="tiny" method="POST" action="` + regexp.QuoteMeta(dayPage) + `/unlink">.*name="kind" value="at">.*name="to" value="Lisbon"><button class="remove" aria-label="Remove Lisbon">`)
	if !unlink.MatchString(where) {
		t.Errorf("the place has no x that sends unlink:\n%s", where)
	}
	rec := submit(h, dayPage+"/unlink", url.Values{"kind": {"at"}, "to": {"Lisbon"}, "back": {"/days/2026-03-02"}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unlink answered %d: %.300s", rec.Code, rec.Body.String())
	}
	if where = section(browse(t, h, "/days/2026-03-02"), "Where"); strings.Contains(where, "Lisbon") {
		t.Errorf("the place is still in Where:\n%s", where)
	}
}

// section is the part of a page from the section with that label to its end tag.
func section(page, label string) string {
	start := strings.Index(page, `aria-label="`+label+`"`)
	if start < 0 {
		return ""
	}
	end := strings.Index(page[start:], "</section>")
	if end < 0 {
		return page[start:]
	}
	return page[start : start+end]
}
