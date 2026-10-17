package api_test

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
)

// formRE finds a POST form of a page: its action and what it holds. This application's own markup is regular enough
// to read this way; a form never nests. hiddenRE finds a hidden field of a form: its name and its value. formTagRE
// finds the start tag of each form, with its attributes in any order, and actionRE finds the action in such a tag.
var (
	formRE    = regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>(.*?)</form>`)
	hiddenRE  = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
	formTagRE = regexp.MustCompile(`<form\b[^>]*>`)
	actionRE  = regexp.MustCompile(`\saction="([^"]*)"`)
)

// formSends is what a browser sends with the form of body whose start tag holds mark (id="pick", or its action): the
// value of each hidden input and each text input of the form, then the values that the reader gives (typed) in place
// of these. Each name of typed must be a control of the form, or of a box outside it that joins it by its id
// (form="pick"), else the test fails: a form that draws no control of a field cannot send it.
func formSends(t *testing.T, body, mark string, typed url.Values) url.Values {
	t.Helper()
	m := regexp.MustCompile(`(?s)(<form\b[^>]*` + regexp.QuoteMeta(mark) + `[^>]*>)(.*?)</form>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no form holds %s", mark)
	}
	controls := map[string]bool{}
	for _, n := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(m[2], -1) {
		controls[n[1]] = true
	}
	if id := regexp.MustCompile(`\sid="([^"]+)"`).FindStringSubmatch(m[1]); id != nil {
		for _, n := range regexp.MustCompile(`name="([^"]+)"[^>]*\sform="`+regexp.QuoteMeta(id[1])+`"`).FindAllStringSubmatch(body, -1) {
			controls[n[1]] = true
		}
	}
	sent := url.Values{}
	for _, in := range regexp.MustCompile(`<input type="(?:hidden|text)" name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(m[2], -1) {
		sent.Add(in[1], html.UnescapeString(in[2]))
	}
	for name, values := range typed {
		if !controls[name] {
			t.Fatalf("the form %s has no control %s", mark, name)
		}
		sent[name] = values
	}
	return sent
}

// withoutReceipt is a Location without the feedback receipt a write adds, so a test compares the path it returned to.
func withoutReceipt(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	if len(q["feedback"]) != 1 {
		t.Errorf("Location %q carries no receipt", location)
	}
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// fieldOf is the value of an action's field, "" when it has none.
func fieldOf(a siren.Action, name string) string {
	for _, f := range a.Fields {
		if f.Name == name && f.Value != nil {
			return fmt.Sprint(f.Value)
		}
	}
	return ""
}

// fixedToday is the server's local day in the tests of a day: a Wednesday.
const fixedToday = "2031-05-14"

// dayOf is the local day n days from fixedToday.
func dayOf(n int) string {
	t, _ := time.Parse(time.DateOnly, fixedToday)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// TestABrowserFormReturnsToThePageThatSentIt: a form that names a path on this server returns there with the
// feedback; any other value is ignored and the browser goes to what it changed, as before; a JSON client never
// follows back. Which values are a path on this server is TestBackOf's.
func TestABrowserFormReturnsToThePageThatSentIt(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	for _, tc := range []struct{ back, want string }{
		{"/days?limit=5&offset=10", "/days?limit=5&offset=10"},
		{"//evil.example/", "/days/2026-09-29"},
	} {
		rec := submit(h, "/days/2026-09-29/capture", url.Values{"text": {"a line"}, "back": {tc.back}}, "")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("back %q: %d", tc.back, rec.Code)
		}
		if got := withoutReceipt(t, rec.Header().Get("Location")); got != tc.want {
			t.Errorf("back %q: Location %q, want %q", tc.back, got, tc.want)
		}
	}
	// The feedback of a write that has some goes to the page returned to, with its receipt.
	rec := submit(h, "/pages", url.Values{"title": {"Notes"}, "body": {"[[bad/name]]"}, "back": {"/browse?kind=person"}}, "")
	if loc := rec.Header().Get("Location"); !feedbackLocation(loc, "/browse?kind=person") || !strings.Contains(browse(t, h, loc), "bad/name") {
		t.Errorf("feedback did not follow the return path: %q", loc)
	}
	// A JSON client: the answer is the resource, whatever back says.
	req := httptest.NewRequest("POST", "/days/2026-09-29/capture", strings.NewReader(url.Values{"text": {"json"}, "back": {"/browse?kind=person"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK || out.Header().Get("Location") != "/days/2026-09-29" {
		t.Errorf("a JSON client followed back: %d %q", out.Code, out.Header().Get("Location"))
	}
	if n := len(apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-29")).Properties.(map[string]any)["view"].([]any)); n == 0 {
		t.Error("the writes were not done")
	}
}

func TestBrowserStableNameResolution(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	id, err := s.CreatePerson(context.Background(), "cli", "Old person", "Synthetic Person", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := s.CreatePage(context.Background(), "cli", "Old reference", "See [[Old person]].", "")
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest("POST", fmt.Sprintf("/pages/%d/rename", id), strings.NewReader(url.Values{"title": {"New person"}}.Encode()))
	post.Header.Set("Accept", "text/html")
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != fmt.Sprintf("/pages/%d", id) {
		t.Fatalf("rename HTML: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, href := range []string{fmt.Sprintf("/pages/%d", id), fmt.Sprintf("/pages/%d", ref)} {
		req := httptest.NewRequest("GET", href, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("HTML: %d", rec.Code)
		}
		if href == fmt.Sprintf("/pages/%d", id) && !strings.Contains(rec.Body.String(), "New person") {
			t.Fatal("preferred title absent from HTML")
		}
		if href == fmt.Sprintf("/pages/%d", ref) && !strings.Contains(rec.Body.String(), fmt.Sprintf(`href="/pages/%d"`, id)) {
			t.Fatal("old reference rendered to wrong identity")
		}
	}
}

func TestBrowserFormCarriesRevisionAndRefusesOldClockToken(t *testing.T) {
	_, h, store := apitest.Fresh(t, "cli")
	d := store.DB
	id, _, err := store.CreatePage(context.Background(), "cli", "Browser version", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE entities SET revision=9007199254740993 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", fmt.Sprintf("/pages/%d", id), nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="version" value="9007199254740993"`) {
		t.Fatalf("HTML token missing: status %d", rec.Code)
	}
	var clock string
	if err := d.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	post := func(token, body string) int {
		t.Helper()
		req := httptest.NewRequest("POST", fmt.Sprintf("/pages/%d/body", id), strings.NewReader(url.Values{"version": {token}, "body": {body}}.Encode()))
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		res := rec.Result()
		defer res.Body.Close()
		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			t.Fatal(err)
		}
		return rec.Code
	}
	if got := post("9007199254740993", "committed"); got != http.StatusSeeOther {
		t.Fatalf("form save %d", got)
	}
	for _, token := range []string{"9007199254740993", clock} {
		if got := post(token, "stale [[Forbidden]]"); got != 409 {
			t.Fatalf("stale browser token %q: %d", token, got)
		}
	}
	p, err := store.PageByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "committed" || p.Version != "9007199254740994" {
		t.Fatalf("browser persisted %+v", p)
	}
	if target, err := store.PageID(context.Background(), "Forbidden"); err != nil || target != 0 {
		t.Fatalf("browser stale target %d %v", target, err)
	}
}

// One way to make a thing (docs/plans/114-one-way-to-do-a-thing.md): the rail's New is a menu of the kinds, each
// made on its list, and ?new=<action> opens that action's form on any page.

func TestEveryKindOfTheNewMenuOpensItsForm(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := browse(t, h, "/days/"+fixedToday)
	menu := day[strings.Index(day, `<div class="new-menu" id="new-menu" popover>`):]
	menu = menu[:strings.Index(menu, "</nav></div>")]
	for _, kind := range []struct{ title, href, action, post string }{ // post: the address the action's form posts to
		{"Page", "/browse?kind=page&new=create-page", "create-page", "/pages"},
		{"Person", "/browse?kind=person&new=create-person", "create-person", "/people"},
		{"Place", "/browse?kind=place&new=create-place", "create-place", "/places"},
		{"Metric", "/metrics?new=register-metric", "", ""},
		{"Reading", "/metrics", "", ""},
		{"File", "/browse?kind=media&new=add-file", "add-file", "/files"},
	} {
		link := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(strings.ReplaceAll(kind.href, "&", "&amp;")) + `">(<svg[^>]*>.*?</svg>)?` + kind.title + `</a>`)
		if !link.MatchString(menu) {
			t.Errorf("the New menu has no %s at %s", kind.title, kind.href)
			continue
		}
		page := browse(t, h, kind.href)
		switch {
		case kind.action != "":
			open := regexp.MustCompile(`<details class="action" open><summary class="button">[^<]*(<span class="owner">owner</span>)?</summary><div class="action-form">(<p class="desc">[^<]*</p>)?<form method="POST" action="` + kind.post + `"`)
			if !open.MatchString(page) {
				t.Errorf("%s does not open the form of %s", kind.href, kind.action)
			}
		case kind.title == "Metric":
			if !strings.Contains(page, `id="new-metric" open>`) {
				t.Errorf("%s does not open New metric", kind.href)
			}
		}
	}
	if strings.Count(menu, "<li>") != 6 {
		t.Errorf("the New menu has %d items, want 6", strings.Count(menu, "<li>"))
	}
}

func TestALinkOpensAFormAndFillsItsFields(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	people := browse(t, h, "/browse?kind=person&new=create-person&title=Ana+Popescu&name=Ana")
	form := people[strings.Index(people, `<details class="action" open>`):]
	form = form[:strings.Index(form, "</details>")]
	for _, want := range []string{`<form method="POST" action="/people"`, `name="title" value="Ana Popescu"`, `name="name" value="Ana"`, `autofocus`} {
		if !strings.Contains(form, want) {
			t.Errorf("the asked form lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(people, "<details class=\"action\" open>") != 1 {
		t.Error("more than the asked form is open")
	}

	// A name no action has opens nothing, and with no ?new= every form stays closed.
	for _, path := range []string{"/browse?kind=person&new=nothing&title=Ana", "/browse?kind=person"} {
		if body := browse(t, h, path); strings.Contains(body, `<details class="action" open>`) || strings.Contains(body, `value="Ana"`) {
			t.Errorf("%s opens or fills a form", path)
		}
	}

	// Home is the page of today (docs/plans/129-the-ux-pass.md, part 10): New page is in the New menu of the rail, and
	// the page has no fold of every create form.
	home := browse(t, h, "/days/"+fixedToday)
	if strings.Contains(home, `<details id="new">`) {
		t.Error(`Home still draws <details id="new">`)
	}
	// No form on Home makes a person or keeps a file, whatever the order of the attributes of its tag.
	for _, tag := range formTagRE.FindAllString(home, -1) {
		if m := actionRE.FindStringSubmatch(tag); m != nil && (m[1] == "/people" || m[1] == "/files") {
			t.Errorf("Home still draws the form %s", tag)
		}
	}
}

func TestTheCatalogSaysOneVerbForOneMeaning(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	titles := map[string]string{}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		titles[a.Name] = a.Title
	}
	for name, want := range map[string]string{
		"create-page": "New page", "create-person": "New person", "create-place": "New place",
		"register-metric": "New metric", "add-file": "New file",
		"record": "Add a reading", "link": "Add a link", "capture": "Add to the day",
		"unlink": "Remove a link", "revive": "Restore", "file-pages": "Put in a category",
	} {
		if titles[name] != want {
			t.Errorf("%s is titled %q, want %q", name, titles[name], want)
		}
	}
}

// TestADaysReadingTileAddsAReadingForThatDay: + Add on a day's tile records one value for that day, not today, and
// returns to the day; on today it says Today.
func TestADaysReadingTileAddsAReadingForThatDay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	record := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics/Weight")), "record")
	for _, day := range []string{"2031-05-12", fixedToday} {
		apitest.Must(t)(c.Do(ctx, record, map[string]string{"metric": "Weight", "day": day, "value": "80"}))
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

// TestAPagesCategoriesAreBadges: a page shows its categories as badges; + Add puts it in one (a new name makes it),
// and x takes it out of that one only. + Add is the form of file-pages: it sends this page's id and move 0, and its
// category offers the titles of the categories in a datalist. A day page has none.
func TestAPagesCategoriesAreBadges(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Seeds"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages?title=Seeds")), "file-pages"), map[string]string{"category": "Projects"}))
	garden := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden", "body": "See [[Seeds]]."}))
	self := apitest.Href(garden, "self")
	id := self[strings.LastIndex(self, "/")+1:]

	before := browse(t, h, self)
	for _, want := range []string{`<div class="categories" role="group" aria-label="Categories"><span class="label">Categories</span>`, `<span class="meta">none</span>`,
		`<form method="POST" action="/pages/file" class="inline">` + "\n" + `<input type="hidden" name="ids" value="` + id + `"><input type="hidden" name="move" value="0">`,
		`<input type="text" name="category" value="" required list="file-pages-category" placeholder="Bloodwork, Sport, Financial"><datalist id="file-pages-category"><option value="Projects"></datalist>`} {
		if !strings.Contains(before, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if strings.Contains(before, `<option value="Garden">`) {
		t.Error("a page is offered as its own category")
	}

	for _, cat := range []string{"Projects", "Outdoors"} {
		rec := submit(h, "/pages/file", formSends(t, before, `action="/pages/file"`, url.Values{"category": {cat}}), "")
		if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), self) {
			t.Fatalf("+ Add %s: %d %q", cat, rec.Code, rec.Header().Get("Location"))
		}
	}
	after := browse(t, h, self)
	for _, cat := range []string{"Projects", "Outdoors"} {
		if !regexp.MustCompile(`<span class="badge category"><a href="/pages/\d+">` + cat + `</a><form class="tiny" method="POST" action="` + self + `/unlink"`).MatchString(after) {
			t.Errorf("the page has no badge for %s", cat)
		}
	}

	rec := submit(h, self+"/unlink", url.Values{"to": {"Outdoors"}, "kind": {"part-of"}, "back": {self}}, "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != self && !feedbackLocation(loc, self)) {
		t.Fatalf("x: %d %q", rec.Code, loc)
	}
	links := map[string]string{}
	for _, l := range props(apitest.Must(t)(c.Get(ctx, self)))["links"].([]any) {
		l := l.(map[string]any)
		links[l["title"].(string)] = l["kind"].(string)
	}
	if len(links) != 2 || links["Projects"] != "part-of" || links["Seeds"] != "wikilink" {
		t.Errorf("after x the links are %v, want Projects part-of and Seeds wikilink", links)
	}

	day := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "Dug the beds."}))
	if body := browse(t, h, apitest.Href(day, "page")); strings.Contains(body, `aria-label="Categories"`) {
		t.Error("a day page shows categories")
	}
}

// TestEveryListSelectsTheSameWay: the lists of Browse and the Metrics board draw one pick bar, after their boxes,
// with Select and a button for each action the list offers: the form of file-pages (id pick), and Delete pages as its
// second button; a list of metrics offers no Delete of several.
func TestEveryListSelectsTheSameWay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	for _, title := range []string{"Scan.jpg", "Notes.pdf"} {
		mime := map[string]string{"Scan.jpg": "image/jpeg", "Notes.pdf": "application/pdf"}[title]
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": title, "sha256": strings.Repeat(map[string]string{"Scan.jpg": "ab", "Notes.pdf": "cd"}[title], 32), "mime": mime}))
	}
	for _, list := range []struct{ path, put, del string }{
		{"/browse?kind=media", "/pages/file", "/pages/tombstone"}, {"/browse?kind=document", "/pages/file", "/pages/tombstone"},
		{"/browse?kind=metric", "/pages/file", ""}, {"/metrics", "/pages/file", ""},
	} {
		body := browse(t, h, list.path)
		if strings.Count(body, `id="pick"`) != 1 || !strings.Contains(body, `<label class="pick-toggle button small"><input type="checkbox" class="pick-on">Select</label>`) {
			t.Errorf("%s has no one pick bar with Select", list.path)
			continue
		}
		if bar, last := strings.Index(body, `id="pick"`), strings.LastIndex(body, `form="pick"`); last < 0 || bar < last {
			t.Errorf("%s: the bar is not after its boxes", list.path)
		}
		put := strings.Contains(body, `<form id="pick" method="POST" action="`+list.put+`" class="inline">`) &&
			strings.Contains(body, `<button class="primary">Put in a category</button>`)
		del := strings.Contains(body, `<button class="danger" formaction="`+list.del+`" formnovalidate>Delete pages</button></form>`)
		if put != (list.put != "") || del != (list.del != "") {
			t.Errorf("%s: Put in a category %v, Delete %v; want %v, %v", list.path, put, del, list.put != "", list.del != "")
		}
	}
}
