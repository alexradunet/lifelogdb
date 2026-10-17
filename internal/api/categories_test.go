package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
)

// The categories (D26). A category page shows what is filed in it and opens in Browse, and a new page is filed where
// it is made (docs/plans/115-categories-to-browse.md). file-pages files several pages in one write
// (docs/plans/112-metrics-as-cards.md), and never a day page.

// filedFixture files Alps 2030, Japan 2031 and the person Sam in Travel, and Travel in Life. It returns the page ids
// by title.
func filedFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030"} {
		ids[title] = fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": title})))["id"])
	}
	ids["Sam"] = fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam"})))["id"])
	for _, title := range []string{"Japan 2031", "Alps 2030", "Sam"} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids[title])), "file-pages"), map[string]string{"category": "Travel"}))
	}
	ids["Travel"] = fmt.Sprint(props(apitest.Must(t)(c.Get(ctx, "/pages?title=Travel")))["id"])
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Travel"])), "file-pages"), map[string]string{"category": "Life"}))
	ids["Life"] = fmt.Sprint(props(apitest.Must(t)(c.Get(ctx, "/pages?title=Life")))["id"])
	return ids
}

func TestACategoryPageListsWhatIsFiledInIt(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel, life := ids["Travel"], ids["Life"]

	// The JSON: a sub-category's backlink carries the class category; the rest their type.
	e := apitest.Must(t)(c.Get(ctx, "/pages/"+life))
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
	for _, l := range apitest.Must(t)(c.Get(ctx, "/pages/"+travel)).Entities {
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
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	travel := ids["Travel"]

	// The catalog: category is an optional title field of create-page, so every surface has it.
	create := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page")
	var fields []string
	for _, f := range create.Fields {
		fields = append(fields, f.Name)
	}
	if strings.Join(fields, ",") != "title,body,category" || create.Fields[2].Required || !create.Fields[2].Pages {
		t.Errorf("create-page fields: %+v", create.Fields)
	}

	// A category page offers create-page with its own title filled, and draws it as New page here.
	here := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+travel)), "create-page")
	if here.Fields[2].Value != "Travel" {
		t.Fatalf("Travel offers create-page as %+v", here)
	}
	body := browse(t, h, "/pages/"+travel)
	if !regexp.MustCompile(`<form method="POST" action="/pages"[^>]*>(?s:.*?)<input type="hidden" name="category" value="Travel">`).MatchString(body) ||
		!strings.Contains(body, `<summary class="button">New page here</summary>`) {
		t.Error("Travel has no New page here form with its title filled")
	}
	if hasAction(apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Japan 2031"])), "create-page") {
		t.Error("a page that is no category offers create-page")
	}

	// The write: the page and its part-of link in one; a missing category is made.
	made := apitest.Must(t)(c.Do(ctx, here, map[string]string{"title": "Kyoto", "body": "Temples."}))
	if l := apitest.Must(t)(c.Get(ctx, "/pages/"+travel)).Entities; !titled(l, "Kyoto") {
		t.Error("Kyoto is not filed in Travel")
	}
	var kinds []string
	for _, l := range made.Entities {
		kinds = append(kinds, l.Rel[0]+":"+l.Title)
	}
	if want := "part-of:Travel"; strings.Join(kinds, " ") != want {
		t.Errorf("Kyoto's links: %v, want %s", kinds, want)
	}
	apitest.Must(t)(c.Do(ctx, create, map[string]string{"title": "Lisbon", "category": "Cities"}))
	if cs := props(apitest.Must(t)(c.Get(ctx, "/browse")))["categories"].([]any); len(cs) != 3 || cs[0].(map[string]any)["path"] != "Cities" {
		t.Errorf("categories after Lisbon: %v", cs)
	}

	// A day cannot be a category (links_endpoint_types): refused, and no page is made.
	if _, err := c.Do(ctx, create, map[string]string{"title": "Nothing", "category": fixedToday}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Fatalf("a day as the category: %v", err)
	}
	if _, err := c.Get(ctx, "/pages?title=Nothing"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the page was made although its filing was refused: %v", err)
	}
}

// titled says whether a link of the list has the title.
func titled(links []siren.Link, title string) bool {
	for _, l := range links {
		if l.Title == title {
			return true
		}
	}
	return false
}

// TestACategoryPageOpensInBrowse: Filed here links the category's list in Browse, its title escaped in the query.
func TestACategoryPageOpensInBrowse(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := filedFixture(t, c)
	if body := browse(t, h, "/pages/"+ids["Travel"]); !strings.Contains(body, `<h2>Filed here <small>3</small></h2><a href="/browse?in=Travel">Open in Browse</a>`) {
		t.Error("Travel's Filed here has no link to Browse")
	}
	note := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "A note"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, note, "file-pages"), map[string]string{"category": "Rock & roll 2031"}))
	cat := apitest.Must(t)(c.Get(ctx, "/pages?title="+url.QueryEscape("Rock & roll 2031")))
	body := browse(t, h, apitest.Href(cat, "self"))
	m := regexp.MustCompile(`<a href="(/browse\?in=[^"]*)">Open in Browse</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the category page has no link to Browse")
	}
	u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	if err != nil || u.Query().Get("in") != "Rock & roll 2031" || len(u.Query()) != 1 {
		t.Fatalf("the link %s does not name the category: %v", m[1], err)
	}
	if got := browseRows(t, apitest.Must(t)(c.Get(ctx, u.String()))); got != "A note:" {
		t.Errorf("Browse in the category: %s", got)
	}
	// A page with nothing filed in it has no Filed here, and no link.
	if strings.Contains(browse(t, h, apitest.Href(note, "self")), "Open in Browse") {
		t.Error("a page with nothing filed in it links Browse")
	}
}

// A new person, place or file is filed where it is made (docs/plans/117-one-browse-view.md, rule 6).

// partOfLinks are the titles the entity links part-of, in order.
func partOfLinks(e *siren.Entity) []string {
	var out []string
	for _, l := range e.Entities {
		if len(l.Rel) == 1 && l.Rel[0] == "part-of" {
			out = append(out, l.Title)
		}
	}
	return out
}

func TestTheMakersTakeACategory(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, name := range []string{"create-page", "create-person", "create-place", "add-file"} {
		a := apitest.Action(t, root, name)
		found := false
		for _, f := range a.Fields {
			if f.Name == "category" {
				found = true
				if f.Required || !f.Pages || f.Title != "Category (title)" {
					t.Errorf("%s: category is %+v, want an optional page title", name, f)
				}
			}
		}
		if !found {
			t.Errorf("%s has no field category", name)
		}
	}
}

func TestANewThingIsFiledWhereItIsMadeOverHTTP(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))

	person := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana", "name": "Ana Example", "category": "Family"}))
	if got := partOfLinks(person); strings.Join(got, ",") != "Family" {
		t.Errorf("Ana is part-of %v, want Family", got)
	}
	place := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-place"), map[string]string{"title": "Lakeside", "category": "family"}))
	if got := partOfLinks(place); strings.Join(got, ",") != "Family" {
		t.Errorf("Lakeside is part-of %v, want Family (the category that exists, by any case)", got)
	}
	file := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "scan.pdf", "sha256": strings.Repeat("ab", 32),
		"mime": "application/pdf", "day": "2031-05-01", "category": "Papers"}))
	if got := partOfLinks(file); strings.Join(got, ",") != "Papers" {
		t.Errorf("scan.pdf is part-of %v, want Papers", got)
	}
	if f, _ := file.Result.(map[string]any)["filed"].(map[string]any); f["created"] != true || f["added"] != json.Number("1") {
		t.Errorf("add-file's result filed = %v, want Papers made and 1 link added", f)
	}

	// The browser sends the original as a multipart form: the category is a field of it too.
	path := filepath.Join(t.TempDir(), "memo.txt")
	if err := os.WriteFile(path, []byte("a synthetic memo"), 0o644); err != nil {
		t.Fatal(err)
	}
	memo := apitest.Must(t)(c.DoFiles(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "memo.txt", "category": "Papers"}, map[string]string{"original": path}))
	if got := partOfLinks(memo); strings.Join(got, ",") != "Papers" {
		t.Errorf("memo.txt (multipart) is part-of %v, want Papers", got)
	}

	// The tree: Family with Ana and Lakeside, Papers with the two files.
	tree := map[string]any{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/browse")))["categories"].([]any) {
		cat := x.(map[string]any)
		tree[cat["title"].(string)] = cat["count"]
	}
	if fmt.Sprint(tree) != "map[Family:2 Papers:2]" {
		t.Errorf("the tree: %v", tree)
	}
}

// A day is never a category: 422, and neither the thing nor its link is written. A day with no page yet is refused by
// entities_day_page, and no day page is written; a day page that exists is refused by links_endpoint_types (D26).
func TestADayAsTheCategoryWritesNothingOverHTTP(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint("day page exists ", exists), func(t *testing.T) {
			c, _, _ := apitest.Fresh(t, "cli")
			ctx := t.Context()
			root := apitest.Must(t)(c.Get(ctx, "/"))
			if exists {
				apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/days/2031-05-02")), "capture"), map[string]string{"text": "a synthetic entry"}))
			}
			for _, tc := range []struct {
				action string
				values map[string]string
			}{
				{"create-person", map[string]string{"title": "Nobody"}},
				{"create-place", map[string]string{"title": "Nobody"}},
				{"add-file", map[string]string{"title": "Nobody", "sha256": strings.Repeat("cd", 32), "mime": "application/pdf"}},
			} {
				tc.values["category"] = "2031-05-02"
				if _, err := c.Do(ctx, apitest.Action(t, root, tc.action), tc.values); apitest.Status(err) != http.StatusUnprocessableEntity {
					t.Errorf("%s with a day as the category: %v, want 422", tc.action, err)
				}
				if _, err := c.Get(ctx, "/pages?title=Nobody"); apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: Nobody was written although the write was refused: %v", tc.action, err)
				}
				_, err := c.Get(ctx, "/pages?title=2031-05-02")
				if !exists && apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: the day page was written although the write was refused: %v", tc.action, err)
				}
				if exists && err != nil {
					t.Errorf("%s: the day page that exists: %v", tc.action, err)
				}
			}
		})
	}
}

func TestAddFileFilesTheFileThatExistsAndADryRunWritesNothing(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	add := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file")
	sent := map[string]string{"title": "scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf", "day": "2031-05-01"}
	kept := apitest.Must(t)(c.Do(ctx, add, sent))
	if got := partOfLinks(kept); len(got) != 0 {
		t.Fatalf("no category sent, but part-of %v", got)
	}

	// A dry run with a category: it says it would file the page that exists, and writes nothing.
	sent["category"], sent["dry_run"] = "Papers", "1"
	dry := apitest.Must(t)(c.Do(ctx, add, sent))
	r, _ := dry.Result.(map[string]any)
	f, _ := r["filed"].(map[string]any)
	cat, _ := f["category"].(map[string]any)
	if dry.Class[1] != "dry-run" || r["existing"] != true || f["created"] != true || f["added"] != json.Number("1") || cat["title"] != "Papers" || cat["id"] != json.Number("0") {
		t.Errorf("the dry run: %v %v, want the page that exists, filed in Papers it would make", dry.Class, r)
	}
	if _, err := c.Get(ctx, "/pages?title=Papers"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the dry run made Papers: %v", err)
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, fmt.Sprintf("/pages/%v", props(kept)["id"])))); len(got) != 0 {
		t.Errorf("the dry run filed the page in %v", got)
	}

	// The write: the same original again files its page.
	delete(sent, "dry_run")
	sent["title"] = "another title.pdf"
	again := apitest.Must(t)(c.Do(ctx, add, sent))
	if props(again)["id"] != props(kept)["id"] || strings.Join(partOfLinks(again), ",") != "Papers" {
		t.Errorf("the same original again: page %v part-of %v, want page %v part-of Papers", props(again)["id"], partOfLinks(again), props(kept)["id"])
	}
}

// Browse inside a category fills category in the New form of each kind; outside one the field is empty.
func TestBrowseNewFormsInACategoryCarryIt(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	for _, tc := range []struct{ kind, action string }{
		{"page", "create-page"}, {"person", "create-person"}, {"place", "create-place"}, {"media", "add-file"}, {"document", "add-file"},
	} {
		made := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=family&kind="+tc.kind)), tc.action)
		if v := fieldOf(made, "category"); v != "Family" {
			t.Errorf("%s in Family: %s has category %q, want Family", tc.kind, tc.action, v)
		}
		if v := fieldOf(apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind="+tc.kind)), tc.action), "category"); v != "" {
			t.Errorf("%s everywhere: %s has category %q, want none", tc.kind, tc.action, v)
		}
		body := browse(t, h, "/browse?in=Family&kind="+tc.kind)
		form := regexp.MustCompile(`(?s)<form method="POST" action="` + regexp.QuoteMeta(made.Href) + `"[ >].*?</form>`).FindString(body)
		if !regexp.MustCompile(`<input[^>]*name="category"[^>]*value="Family"`).MatchString(form) {
			t.Errorf("%s in Family: the page's %s form has no category Family: %s", tc.kind, tc.action, form)
		}
	}

	// The form, sent as it is filled, files the new person in Family.
	ana := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=Family&kind=person")), "create-person"), map[string]string{"title": "Cy"}))
	if got := partOfLinks(ana); strings.Join(got, ",") != "Family" {
		t.Errorf("Cy made from Browse in Family is part-of %v", got)
	}
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
