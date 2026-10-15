package api_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// A category page shows what is filed in it, and a new page is filed where it is made
// (docs/plans/115-categories-to-browse.md).

// filedFixture files Alps 2030, Japan 2031 and the person Sam in Travel, and Travel in Life. It returns the page ids
// by title.
func filedFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030"} {
		ids[title] = fmt.Sprint(props(must(c.Do(ctx, find(root, "create-page"), map[string]string{"title": title})))["id"])
	}
	ids["Sam"] = fmt.Sprint(props(must(c.Do(ctx, find(root, "create-person"), map[string]string{"title": "Sam"})))["id"])
	for _, title := range []string{"Japan 2031", "Alps 2030", "Sam"} {
		must(c.Do(ctx, find(must(c.Get(ctx, "/pages/"+ids[title])), "file-pages"), map[string]string{"category": "Travel"}))
	}
	ids["Travel"] = fmt.Sprint(props(must(c.Get(ctx, "/pages?title=Travel")))["id"])
	must(c.Do(ctx, find(must(c.Get(ctx, "/pages/"+ids["Travel"])), "file-pages"), map[string]string{"category": "Life"}))
	ids["Life"] = fmt.Sprint(props(must(c.Get(ctx, "/pages?title=Life")))["id"])
	return ids
}

func TestACategoryPageListsWhatIsFiledInIt(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel, life := ids["Travel"], ids["Life"]

	// The JSON: a sub-category's backlink carries the class category; the rest their type.
	e := must(c.Get(ctx, "/pages/"+life))
	var filed []string
	for _, l := range e.Entities {
		if l.Rel[0] == "backlink" && l.Rel[1] == "part-of" {
			filed = append(filed, l.Title+":"+strings.Join(l.Class, "+"))
		}
	}
	if want := "Travel:page+category"; strings.Join(filed, " ") != want {
		t.Errorf("Life's part-of backlinks: %v, want %s", filed, want)
	}
	filed = nil
	for _, l := range must(c.Get(ctx, "/pages/"+travel)).Entities {
		if l.Rel[0] == "backlink" && l.Rel[1] == "part-of" {
			filed = append(filed, l.Title+":"+strings.Join(l.Class, "+"))
		}
	}
	if want := "Alps 2030:page Japan 2031:page Sam:person"; strings.Join(filed, " ") != want {
		t.Errorf("Travel's part-of backlinks: %v, want %s", filed, want)
	}

	// The page: one section, Filed here, the sub-categories first, a type badge on what is not a page; the margin's
	// Backlinks has no part-of group.
	row := regexp.MustCompile(`<li><a class="entry" href="/pages/(\d+)"><span class="first">([^<]*)</span><span class="meta">(?:<span class="badge">([^<]*)</span>)?</span></a></li>`)
	section := func(body string) string {
		t.Helper()
		i := strings.Index(body, `<section class="group filed" aria-label="Filed here">`)
		if i < 0 {
			t.Fatal("no Filed here section")
		}
		body = body[i:]
		return body[:strings.Index(body, "</section>")]
	}
	var rows []string
	for _, m := range row.FindAllStringSubmatch(section(browse(t, h, "/pages/"+life)), -1) {
		rows = append(rows, m[2]+":"+m[3])
	}
	if want := "Travel:category"; strings.Join(rows, " ") != want {
		t.Errorf("Life's Filed here: %v, want %s", rows, want)
	}
	body := browse(t, h, "/pages/"+travel)
	rows = nil
	for _, m := range row.FindAllStringSubmatch(section(body), -1) {
		rows = append(rows, m[2]+":"+m[3])
	}
	if want := "Alps 2030: Japan 2031: Sam:person"; strings.Join(rows, " ") != want {
		t.Errorf("Travel's Filed here: %v, want %s", rows, want)
	}
	if strings.Contains(body, `<p class="sub">part-of</p>`) {
		t.Error("the margin's Backlinks still lists part-of")
	}
	if !strings.Contains(body, `<h2>Filed here <small>3</small></h2>`) {
		t.Error("Filed here has no count")
	}
	// A page in no category has no section.
	if strings.Contains(browse(t, h, "/pages/"+ids["Japan 2031"]), `aria-label="Filed here"`) {
		t.Error("a page that is no category has a Filed here section")
	}
}

func TestANewPageIsFiledWhereItIsMade(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel := ids["Travel"]

	// The catalog: category is an optional title field of create-page, so every surface has it.
	create := find(must(c.Get(ctx, "/")), "create-page")
	var fields []string
	for _, f := range create.Fields {
		fields = append(fields, f.Name)
	}
	if strings.Join(fields, ",") != "title,body,category" || create.Fields[2].Required || !create.Fields[2].Pages {
		t.Errorf("create-page fields: %+v", create.Fields)
	}

	// A category page offers create-page with its own title filled, and draws it as New page here.
	here := find(must(c.Get(ctx, "/pages/"+travel)), "create-page")
	if here.Name == "" || here.Fields[2].Value != "Travel" {
		t.Fatalf("Travel offers create-page as %+v", here)
	}
	body := browse(t, h, "/pages/"+travel)
	if !regexp.MustCompile(`<form method="POST" action="/pages" data-action="create-page"[^>]*>(?s:.*?)<input type="hidden" name="category" value="Travel">`).MatchString(body) ||
		!strings.Contains(body, `<summary class="button">New page here</summary>`) {
		t.Error("Travel has no New page here form with its title filled")
	}
	if find(must(c.Get(ctx, "/pages/"+ids["Japan 2031"])), "create-page").Name != "" {
		t.Error("a page that is no category offers create-page")
	}

	// The write: the page and its part-of link in one; a missing category is made.
	made := must(c.Do(ctx, here, map[string]string{"title": "Kyoto", "body": "Temples."}))
	if l := must(c.Get(ctx, "/pages/"+travel)).Entities; !titled(l, "Kyoto") {
		t.Error("Kyoto is not filed in Travel")
	}
	var kinds []string
	for _, l := range made.Entities {
		kinds = append(kinds, l.Rel[0]+":"+l.Title)
	}
	if want := "part-of:Travel"; strings.Join(kinds, " ") != want {
		t.Errorf("Kyoto's links: %v, want %s", kinds, want)
	}
	must(c.Do(ctx, create, map[string]string{"title": "Lisbon", "category": "Cities"}))
	if cs := props(must(c.Get(ctx, "/browse")))["categories"].([]any); len(cs) != 3 || cs[0].(map[string]any)["path"] != "Cities" {
		t.Errorf("categories after Lisbon: %v", cs)
	}

	// A day cannot be a category (links_endpoint_types): refused, and no page is made.
	if _, err := c.Do(ctx, create, map[string]string{"title": "Nothing", "category": fixedToday}); !clientStatus(err, http.StatusUnprocessableEntity) {
		t.Fatalf("a day as the category: %v", err)
	}
	if _, err := c.Get(ctx, "/pages?title=Nothing"); !clientStatus(err, http.StatusNotFound) {
		t.Errorf("the page was made although its filing was refused: %v", err)
	}
}

// titled says whether a link of the list has the title.
func titled(links []api.Link, title string) bool {
	for _, l := range links {
		if l.Title == title {
			return true
		}
	}
	return false
}
