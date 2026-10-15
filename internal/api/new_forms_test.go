package api_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// One way to make a thing (docs/plans/114-one-way-to-do-a-thing.md): the rail's New is a menu of the kinds, each
// made on its list, and ?new=<action> opens that action's form on any page.

func TestEveryKindOfTheNewMenuOpensItsForm(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	day := browse(t, h, "/days/"+fixedToday)
	menu := day[strings.Index(day, `<div class="new-menu" id="new-menu" popover>`):]
	menu = menu[:strings.Index(menu, "</nav></div>")]
	for _, kind := range []struct{ title, href, action string }{
		{"Page", "/?new=create-page", "create-page"},
		{"Task", "/tasks?new=create-task", "create-task"},
		{"Person", "/people?new=create-person", "create-person"},
		{"Place", "/places?new=create-place", "create-place"},
		{"Metric", "/metrics?new=register-metric", ""},
		{"Reading", "/metrics", ""},
		{"File", "/files?new=add-file", "add-file"},
		{"Period", "/periods?new=create-period", "create-period"},
		{"Session", "/sessions?new=capture-session", "capture-session"},
	} {
		link := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(strings.ReplaceAll(kind.href, "&", "&amp;")) + `">(<svg[^>]*>.*?</svg>)?` + kind.title + `</a>`)
		if !link.MatchString(menu) {
			t.Errorf("the New menu has no %s at %s", kind.title, kind.href)
			continue
		}
		page := browse(t, h, kind.href)
		switch {
		case kind.action != "":
			open := regexp.MustCompile(`<details class="action" open><summary class="button">[^<]*(<span class="owner">owner</span>)?</summary><div class="action-form">(<p class="desc">[^<]*</p>)?<form method="POST" action="[^"]*" data-action="` + kind.action + `"`)
			if !open.MatchString(page) {
				t.Errorf("%s does not open the form of %s", kind.href, kind.action)
			}
		case kind.title == "Metric":
			if !strings.Contains(page, `id="new-metric" open>`) {
				t.Errorf("%s does not open New metric", kind.href)
			}
		}
	}
	if strings.Count(menu, "<li>") != 9 {
		t.Errorf("the New menu has %d items, want 9", strings.Count(menu, "<li>"))
	}
}

func TestALinkOpensAFormAndFillsItsFields(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	people := browse(t, h, "/people?new=create-person&title=Ana+Popescu&name=Ana")
	form := people[strings.Index(people, `<details class="action" open>`):]
	form = form[:strings.Index(form, "</details>")]
	for _, want := range []string{`data-action="create-person"`, `name="title" value="Ana Popescu"`, `name="name" value="Ana"`, `autofocus`} {
		if !strings.Contains(form, want) {
			t.Errorf("the asked form lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(people, "<details class=\"action\" open>") != 1 {
		t.Error("more than the asked form is open")
	}

	// A name no action has opens nothing, and with no ?new= every form stays closed.
	for _, path := range []string{"/people?new=nothing&title=Ana", "/people"} {
		if body := browse(t, h, path); strings.Contains(body, `<details class="action" open>`) || strings.Contains(body, `value="Ana"`) {
			t.Errorf("%s opens or fills a form", path)
		}
	}

	// Home has New page beside Open a page, and no fold of every create form.
	home := browse(t, h, "/")
	tools := home[strings.Index(home, `<div class="tools">`):]
	tools = tools[:strings.Index(tools, "</header>")]
	if !strings.Contains(tools, `data-action="create-page"`) || !strings.Contains(tools, `data-action="find"`) {
		t.Errorf("Home's header lacks New page or Open a page:\n%s", tools)
	}
	for _, gone := range []string{`<details id="new">`, `data-action="create-person"`, `data-action="add-file"`, `data-action="create-task"`} {
		if strings.Contains(home, gone) {
			t.Errorf("Home still draws %s", gone)
		}
	}
}

func TestTheCatalogSaysOneVerbForOneMeaning(t *testing.T) {
	c, _ := freshOn(t, fixedToday)
	titles := map[string]string{}
	for _, a := range must(c.Get(t.Context(), "/actions")).Actions {
		titles[a.Name] = a.Title
	}
	for name, want := range map[string]string{
		"create-page": "New page", "create-person": "New person", "create-place": "New place", "create-task": "New task",
		"register-metric": "New metric", "add-file": "New file", "create-period": "New period", "capture-session": "New session",
		"record": "Add a reading", "capture-occurrence": "Add an occurrence", "link": "Add a link", "capture": "Add to the day",
		"unlink": "Remove a link", "revive": "Restore", "revive-task": "Restore task", "revive-session": "Restore session",
		"revive-occurrence": "Restore occurrence", "file-pages": "Put in a category",
	} {
		if titles[name] != want {
			t.Errorf("%s is titled %q, want %q", name, titles[name], want)
		}
	}
}

// TestADaysReadingTileAddsAReadingForThatDay: + Add on a day's tile records one value for that day, not today, and
// returns to the day; on today it says Today.
func TestADaysReadingTileAddsAReadingForThatDay(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	must(c.Do(ctx, find(must(c.Get(ctx, "/")), "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	record := find(must(c.Get(ctx, "/metrics/Weight")), "record")
	for _, day := range []string{"2031-05-12", fixedToday} {
		must(c.Do(ctx, record, map[string]string{"metric": "Weight", "day": day, "value": "80"}))
	}

	past := browse(t, h, "/days/2031-05-12")
	for _, want := range []string{`<summary class="button small" aria-label="Add a reading of Weight">+ Add</summary>`,
		`<input type="hidden" name="metric" value="Weight"><input type="hidden" name="day" value="2031-05-12">`, `<span>Mon 12 May, kg</span>`} {
		if !strings.Contains(past, want) {
			t.Errorf("the day's tile lacks %s", want)
		}
	}
	if today := browse(t, h, "/days/"+fixedToday); !strings.Contains(today, `<span>Today, kg</span>`) {
		t.Error("today's tile does not say Today")
	}

	rec := submit(h, "/measurements", url.Values{"metric": {"Weight"}, "day": {"2031-05-12"}, "value": {"79.5"}, "back": {"/days/2031-05-12"}}, "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != "/days/2031-05-12" && !feedbackLocation(loc, "/days/2031-05-12")) {
		t.Fatalf("+ Add on a day: %d %q", rec.Code, loc)
	}
	if after := browse(t, h, "/days/2031-05-12"); !strings.Contains(after, "and 1 more this day") || !strings.Contains(after, ">79.5 <span class=\"unit\">kg</span>") {
		t.Error("the day does not show the added reading on its tile")
	}
}
