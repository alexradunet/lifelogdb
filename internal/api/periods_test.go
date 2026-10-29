package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// The period pages in the browser (D30). Today is Wednesday 2031-05-14, in ISO week 2031-W20. A period page is a plain
// page with a key as its title, made with create-page.

// mainOf is the part of a page between its main tags: what a failure of a test needs to show.
func mainOf(page string) string {
	_, rest, _ := strings.Cut(page, `<main id="main">`)
	rest, _, _ = strings.Cut(rest, "</main>")
	return rest
}

func TestAWeekListsItsSevenDaysAndItsPage(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-W20", "body": "Finish the report"}))

	page := browse(t, h, "/weeks/2031-W20")
	if strings.Contains(page, "no value") {
		t.Errorf("the week draws a missing value:\n%s", mainOf(page))
	}
	if !strings.Contains(page, "<h1>Week 20 of 2031</h1>") {
		t.Errorf("the title of the week:\n%s", mainOf(page))
	}
	for d := 12; d <= 18; d++ {
		if want := fmt.Sprintf(`href="/days/2031-05-%02d"`, d); !strings.Contains(page, want) {
			t.Errorf("the week does not list %s", want)
		}
	}
	if strings.Contains(page, `href="/days/2031-05-11"`) || strings.Contains(page, `href="/days/2031-05-19"`) {
		t.Error("the week lists a day outside it")
	}
	for _, want := range []string{`aria-label="The page of the period"`, `<h2>The week page</h2>`, "Finish the report", `href="/weeks/2031-W19" rel="prev"`, `aria-current="page">Week</a>`} {
		if !strings.Contains(page, want) {
			t.Errorf("the week does not carry %s", want)
		}
	}
	if strings.Contains(page, "has no page yet") {
		t.Error("a week with a page offers to make it")
	}

	req := httptest.NewRequest("GET", "/weeks/today", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound || rec.Header().Get("Location") != "/weeks/2031-W20" {
		t.Errorf("/weeks/today: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, bad := range []string{"/weeks/2031-W53", "/weeks/2031-W00", "/weeks/2031-W5", "/weeks/x"} {
		if _, err := c.Get(t.Context(), bad); apitest.Status(err) != 422 {
			t.Errorf("GET %s: %v, want 422", bad, err)
		}
	}
}

func TestAMonthAndAYearShowTheirPages(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	if page := browse(t, h, "/months/2031-05"); strings.Contains(page, "<h2>The month page</h2>") || !strings.Contains(page, "This month has no page yet") {
		t.Errorf("a month with no page:\n%s", mainOf(page))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-05", "body": "Plan the month"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031", "body": "Run a marathon"}))
	month := browse(t, h, "/months/2031-05")
	for _, want := range []string{`aria-label="The page of the period"`, "Plan the month"} {
		if !strings.Contains(month, want) {
			t.Errorf("the month does not carry %s", want)
		}
	}
	if strings.Index(month, "Plan the month") > strings.Index(month, `<table class="calendar">`) {
		t.Error("the page of the month is below its calendar")
	}
	year := browse(t, h, "/years/2031")
	for _, want := range []string{"Run a marathon", `aria-label="Quarters"`, `href="/actions?new=create-page&amp;title=2031-Q4"`} {
		if !strings.Contains(year, want) {
			t.Errorf("the year does not carry %s", want)
		}
	}
	// The link of a quarter with no page opens the form of create-page, with the quarter as the title.
	opened := browse(t, h, "/actions?new=create-page&title=2031-Q4")
	if !strings.Contains(opened, `<details class="action" open>`) || !strings.Contains(opened, `name="title" value="2031-Q4"`) {
		t.Errorf("?new=create-page&title=2031-Q4 does not open the form for the quarter:\n%s", mainOf(opened))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-Q4", "body": "Ship it"}))
	if after := browse(t, h, "/years/2031"); !strings.Contains(after, `<a href="/pages/`) || strings.Contains(after, `title=2031-Q4"`) {
		t.Errorf("a quarter with a page still links its opener:\n%s", after)
	}
}

func TestAPeriodPageNamesItsKindAndItsView(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for key, want := range map[string][]string{
		"2031-W20": {`<span class="kind">week page</span>`, `<a href="/weeks/2031-W20">The week view</a>`},
		"2031-05":  {`<span class="kind">month page</span>`, `<a href="/months/2031-05">The month view</a>`},
		"2031":     {`<span class="kind">year page</span>`, `<a href="/years/2031">The year view</a>`},
		"2031-Q4":  {`<span class="kind">quarter page</span>`, `<a href="/months/2031-10">October</a>`, `<a href="/months/2031-12">December</a>`},
	} {
		e := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": key, "body": "Aim " + key}))
		page := browse(t, h, fmt.Sprint("/pages/", props(e)["id"]))
		want = append(want, `Aim `+key)
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("the page %s does not carry %s", key, w)
			}
		}
	}
}

// TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist: a create-page of a week key makes the period page
// that the week view then shows. The file refuses a week that the year does not have, by the name of its constraint.
func TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	week := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-W43"}))
	if p := props(week); week.Title != "2031-W43" || p["period"] != "week" {
		t.Errorf("the week page: %q %v", week.Title, p["period"])
	}
	if page := browse(t, h, "/weeks/2031-W43"); !strings.Contains(page, `<h2>The week page</h2>`) || strings.Contains(page, "has no page yet") {
		t.Errorf("the week view does not show the page that create-page made:\n%s", mainOf(page))
	}
	_, err := c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2027-W53"})
	if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("create-page of 2027-W53: %v, want 422 naming entities_period_exists", err)
	}
}

// TestAPeriodViewWithoutAPageOffersCreatePage: a week, a month and a year with no page offer create-page with the key
// as the title, and draw the form with the title fixed. With a page they offer save-body and not create-page.
func TestAPeriodViewWithoutAPageOffersCreatePage(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for path, key := range map[string]string{"/weeks/2031-W20": "2031-W20", "/months/2031-05": "2031-05", "/years/2031": "2031"} {
		e := apitest.Must(t)(c.Get(t.Context(), path))
		if names(e) != "create-page" {
			t.Errorf("%s offers %s, want create-page", path, names(e))
			continue
		}
		var title any
		for _, f := range apitest.Action(t, e, "create-page").Fields {
			if f.Name == "title" {
				title = f.Value
			}
		}
		if title != key {
			t.Errorf("%s: the title of create-page is %v, want %s", path, title, key)
		}
		page := browse(t, h, path)
		for _, want := range []string{"has no page yet", `<input type="hidden" name="title" value="` + key + `">`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s does not carry %s:\n%s", path, want, mainOf(page))
			}
		}
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": key}))
		if after := apitest.Must(t)(c.Get(t.Context(), path)); names(after) != "save-body" {
			t.Errorf("%s with a page offers %s, want save-body", path, names(after))
		}
	}
}
