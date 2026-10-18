package api_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
	"lifelog/internal/core"
)

// Browse (docs/plans/117-one-browse-view.md): one list with two filters, the kind and where it looks.

// browseFixture writes through the writer, and returns the ids by title. Hand-checked layout:
//
//	Family:  Beta, Cousins, Ana, photo.jpg, scan.pdf
//	Cousins: Gamma, Weight
//	nowhere: Alpha, Bo, Lakeside, memo.m4a, Mood (the metric every file has)
//
// The day page 2031-05-01 mentions Ana and Lakeside; a day page is never filed (links_endpoint_types, D26).
func browseFixture(t *testing.T, s *core.Store) map[string]int64 {
	t.Helper()
	ctx := t.Context()
	ids := map[string]int64{}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, title := range []string{"Alpha", "Beta", "Gamma"} {
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		check(err)
		ids[title] = id
	}
	for _, title := range []string{"Ana", "Bo"} {
		id, err := s.CreatePerson(ctx, "cli", title, "", "", "")
		check(err)
		ids[title] = id
	}
	id, err := s.CreatePlace(ctx, "cli", "Lakeside")
	check(err)
	ids["Lakeside"] = id
	for i, f := range []core.FileIn{{Title: "photo.jpg", MIME: "image/jpeg", Day: "2031-05-02"}, {Title: "memo.m4a", MIME: "audio/mp4", Day: "2031-05-01"},
		{Title: "scan.pdf", MIME: "application/pdf", Day: "2031-04-01"}} {
		f.SHA256 = strings.Repeat(fmt.Sprintf("%02x", i+1), 32)
		k, err := s.AddFile(ctx, "cli", f)
		check(err)
		ids[f.Title] = k.ID
	}
	_, err = s.CreateMetric(ctx, "cli", core.NewMetric{Name: "Weight", Unit: "kg"})
	check(err)
	for _, title := range []string{"Weight", "Mood"} {
		ids[title], err = s.PageID(ctx, title)
		check(err)
	}
	ids["2031-05-01"], _, err = s.Capture(ctx, "cli", "2031-05-01", "Lunch with [[Ana]] at [[Lakeside]]", nil)
	check(err)
	file := func(category string, titles ...string) {
		t.Helper()
		var list []int64
		for _, title := range titles {
			list = append(list, ids[title])
		}
		f, err := s.FilePages(ctx, "cli", list, category, false)
		check(err)
		ids[category] = f.Category.ID
	}
	file("Family", "Beta", "Ana", "photo.jpg", "scan.pdf")
	file("Cousins", "Gamma", "Weight")
	file("Family", "Cousins")
	return ids
}

// browseRows says the rows of a Browse answer, one "title:field=value;…" each, the fields in the order named (a field
// the row lacks is "-"); and it checks the item links: one a row, in the same order, the class the row's type.
func browseRows(t *testing.T, e *siren.Entity, fields ...string) string {
	t.Helper()
	rows, _ := props(e)["rows"].([]any)
	items := []siren.Link{}
	for _, l := range e.Entities {
		if l.Rel[0] == "item" {
			items = append(items, l)
		}
	}
	if len(items) != len(rows) {
		t.Fatalf("%d rows and %d item links", len(rows), len(items))
	}
	var out []string
	for i, x := range rows {
		r := x.(map[string]any)
		if items[i].Href != fmt.Sprintf("/pages/%v", r["id"]) || items[i].Title != r["title"] || items[i].Class[0] != r["type"] {
			t.Errorf("item %d %+v for row %v", i, items[i], r)
		}
		var vals []string
		for _, f := range fields {
			v, ok := r[f]
			if !ok {
				vals = append(vals, f+"=-")
				continue
			}
			vals = append(vals, fmt.Sprintf("%s=%v", f, v))
		}
		out = append(out, fmt.Sprint(r["title"])+":"+strings.Join(vals, ";"))
	}
	return strings.Join(out, " ")
}

func TestBrowseListsEachKindWithItsRowAndOrder(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	ids := browseFixture(t, s)
	ctx := t.Context()
	all := []string{"type", "kind", "category", "mentions", "last_day", "picture", "mime", "file_kind", "day", "unit"}
	for _, tc := range []struct{ path, want string }{
		{"/browse", "Alpha:type=page;kind=page;category=-;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=- " +
			"Beta:type=page;kind=page;category=-;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=- " +
			"Cousins:type=page;kind=page;category=true;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=- " +
			"Family:type=page;kind=page;category=true;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=- " +
			"Gamma:type=page;kind=page;category=-;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=-"},
		{"/browse?kind=person", "Ana:type=person;kind=person;category=-;mentions=1;last_day=2031-05-01;picture=-;mime=-;file_kind=-;day=-;unit=- " +
			"Bo:type=person;kind=person;category=-;mentions=0;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=-"},
		{"/browse?kind=place", "Lakeside:type=place;kind=place;category=-;mentions=1;last_day=2031-05-01;picture=-;mime=-;file_kind=-;day=-;unit=-"},
		{"/browse?kind=media", "photo.jpg:type=file;kind=media;category=-;mentions=0;last_day=-;picture=-;mime=image/jpeg;file_kind=image;day=2031-05-02;unit=- " +
			"memo.m4a:type=file;kind=media;category=-;mentions=0;last_day=-;picture=-;mime=audio/mp4;file_kind=audio;day=2031-05-01;unit=-"},
		{"/browse?kind=media&media=audio", "memo.m4a:type=file;kind=media;category=-;mentions=0;last_day=-;picture=-;mime=audio/mp4;file_kind=audio;day=2031-05-01;unit=-"},
		{"/browse?kind=document", "scan.pdf:type=file;kind=document;category=-;mentions=0;last_day=-;picture=-;mime=application/pdf;file_kind=application;day=2031-04-01;unit=-"},
		{"/browse?kind=metric", "Mood:type=metric;kind=metric;category=-;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=1-5 " +
			"Weight:type=metric;kind=metric;category=-;mentions=-;last_day=-;picture=-;mime=-;file_kind=-;day=-;unit=kg"},
	} {
		e := apitest.Must(t)(c.Get(ctx, tc.path))
		if e.Class[0] != "browse" || e.Title != "Browse" {
			t.Fatalf("%s: %v %q", tc.path, e.Class, e.Title)
		}
		if got := browseRows(t, e, all...); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.path, got, tc.want)
		}
	}
	// The newest day comes first among the media: the first row is the photo, by its id.
	if got := props(apitest.Must(t)(c.Get(ctx, "/browse?kind=media")))["rows"].([]any)[0].(map[string]any)["id"]; fmt.Sprint(got) != fmt.Sprint(ids["photo.jpg"]) {
		t.Errorf("the first media row is %v, want photo.jpg %d", got, ids["photo.jpg"])
	}
}

// TestBrowseRowsNameTheirPicture: a file kept with a preview and a person whose picture it is name the preview, and
// the page draws it in the tile and in the face.
func TestBrowseRowsNameTheirPicture(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	ids := browseFixture(t, s)
	ctx := t.Context()
	path, sum := jpegFile(t, "lake.jpg", 40, 30)
	pic, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddFile(ctx, "cli", core.FileIn{Title: "lake.jpg", MIME: "image/jpeg", SHA256: sum, Day: "2031-06-01", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoverOf(ctx, "cli", ids["Bo"], "lake.jpg"); err != nil {
		t.Fatal(err)
	}
	preview := fmt.Sprintf("/pages/%d/preview", k.ID)
	if got, want := browseRows(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=media")), "picture"), "lake.jpg:picture="+preview+" photo.jpg:picture=- memo.m4a:picture=-"; got != want {
		t.Errorf("media:\n got %s\nwant %s", got, want)
	}
	if got, want := browseRows(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=person")), "picture"), "Ana:picture=- Bo:picture="+preview; got != want {
		t.Errorf("people:\n got %s\nwant %s", got, want)
	}
	if body := browse(t, h, "/browse?kind=media"); !strings.Contains(body, `<img src="`+preview+`" alt="" loading="lazy"><span class="first">lake.jpg</span>`) {
		t.Error("the media tile does not draw the picture")
	}
	if body := browse(t, h, "/browse?kind=person"); !strings.Contains(body, fmt.Sprintf(`<a class="entry named face" href="/pages/%d"><img class="face-picture" src="%s" alt="" loading="lazy">`, ids["Bo"], preview)) {
		t.Error("the face does not draw the picture")
	}
}

func TestBrowseInACategoryListsOnlyWhatIsFiledDirectly(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	ids := browseFixture(t, s)
	ctx := t.Context()
	family := apitest.Must(t)(c.Get(ctx, "/browse?in=Family"))
	p := props(family)
	in := p["in"].(map[string]any)
	if p["kind"] != "all" || in["title"] != "Family" || in["path"] != "Family" || fmt.Sprint(in["id"]) != fmt.Sprint(ids["Family"]) {
		t.Errorf("in Family: kind %v, in %v", p["kind"], in)
	}
	// The categories first, then by title; Gamma is in Cousins, not in Family; the day page is in no list.
	if got, want := browseRows(t, family, "kind", "category", "file_kind"),
		"Cousins:kind=page;category=true;file_kind=- Ana:kind=person;category=-;file_kind=- Beta:kind=page;category=-;file_kind=- "+
			"photo.jpg:kind=media;category=-;file_kind=image scan.pdf:kind=document;category=-;file_kind=application"; got != want {
		t.Errorf("All in Family:\n got %s\nwant %s", got, want)
	}
	if got := browseRows(t, apitest.Must(t)(c.Get(ctx, "/browse?in=Family&kind=page")), "category"); got != "Beta:category=- Cousins:category=true" {
		t.Errorf("pages in Family: %s", got)
	}
	cousins := apitest.Must(t)(c.Get(ctx, "/browse?in=cousins&kind=metric")) // a title in any case names the category
	if got := browseRows(t, cousins, "unit"); got != "Weight:unit=kg" {
		t.Errorf("metrics in Cousins: %s", got)
	}
	if in := props(cousins)["in"].(map[string]any); in["title"] != "Cousins" || in["path"] != "Family/Cousins" {
		t.Errorf("in Cousins: %v", in)
	}
	// A page with nothing filed in it gives an empty list, and no tree path of its own.
	alpha := apitest.Must(t)(c.Get(ctx, "/browse?in=Alpha"))
	if rows := props(alpha)["rows"].([]any); len(rows) != 0 {
		t.Errorf("in Alpha: %v", rows)
	}
	if in := props(alpha)["in"].(map[string]any); in["path"] != "Alpha" {
		t.Errorf("in Alpha: %v", in)
	}
}

func TestBrowseNotFiledFollowsTheKind(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	for _, tc := range []struct{ path, want string }{
		{"/browse?unfiled=1", "Alpha:"},
		{"/browse?kind=person&unfiled=1", "Bo:"},
		{"/browse?kind=place&unfiled=1", "Lakeside:"},
		{"/browse?kind=media&unfiled=1", "memo.m4a:"},
		{"/browse?kind=document&unfiled=1", ""},
		{"/browse?kind=metric&unfiled=1", "Mood:"},
	} {
		e := apitest.Must(t)(c.Get(ctx, tc.path))
		if got := browseRows(t, e); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.path, got, tc.want)
		}
		if props(e)["unfiled"] != true {
			t.Errorf("%s: unfiled is %v", tc.path, props(e)["unfiled"])
		}
	}
	// For pages, Not filed is the list Categories showed: the plain pages in no category that are not categories
	// themselves (Family and Cousins are), and never a day page (a day page is filed nowhere: the Calendar has the
	// days).
	if _, _, err := s.Capture(ctx, "cli", "2031-05-03", "a day", nil); err != nil {
		t.Fatal(err)
	}
	if got := browseRows(t, apitest.Must(t)(c.Get(ctx, "/browse?unfiled=1")), "category"); got != "Alpha:category=-" {
		t.Errorf("pages not filed: %s", got)
	}
}

func TestBrowseCountsEachChipInTheScopeOfTheList(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	counts := func(path string) string {
		t.Helper()
		m := props(apitest.Must(t)(c.Get(ctx, path)))["counts"].(map[string]any)
		var out []string
		for _, k := range []string{"page", "person", "place", "media", "document", "metric", "all", "ghosts", "deleted"} {
			if v, ok := m[k]; ok {
				out = append(out, fmt.Sprintf("%s=%v", k, v))
			}
		}
		if len(out) != len(m) {
			t.Errorf("%s: counts %v has other keys", path, m)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct{ path, want string }{
		{"/browse", "page=5 person=2 place=1 media=2 document=1 metric=2 ghosts=0 deleted=0"},
		{"/browse?kind=media", "page=5 person=2 place=1 media=2 document=1 metric=2 ghosts=0 deleted=0"},
		{"/browse?in=Family", "page=2 person=1 place=0 media=1 document=1 metric=0 all=5 ghosts=0 deleted=0"},
		{"/browse?in=Cousins&kind=page", "page=1 person=0 place=0 media=0 document=0 metric=1 all=2 ghosts=0 deleted=0"},
		{"/browse?unfiled=1&kind=person", "page=1 person=1 place=1 media=1 document=0 metric=1 ghosts=0 deleted=0"},
	} {
		if got := counts(tc.path); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.path, got, tc.want)
		}
	}
	// The chips keep where the list looks; All is a chip inside a category only.
	chips := func(e *siren.Entity) string {
		var out []string
		for _, l := range e.Links {
			if l.Rel[0] == "kind" {
				out = append(out, l.Class[0]+"="+l.Href)
			}
		}
		return strings.Join(out, " ")
	}
	if got, want := chips(apitest.Must(t)(c.Get(ctx, "/browse?unfiled=1&kind=person"))), "page=/browse?kind=page&unfiled=1 person=/browse?kind=person&unfiled=1 "+
		"place=/browse?kind=place&unfiled=1 media=/browse?kind=media&unfiled=1 document=/browse?kind=document&unfiled=1 metric=/browse?kind=metric&unfiled=1"; got != want {
		t.Errorf("chips not filed:\n got %s\nwant %s", got, want)
	}
	// Inside a category All is the default, and its chip comes first.
	if got := chips(apitest.Must(t)(c.Get(ctx, "/browse?in=Family&kind=media&media=image"))); !strings.HasPrefix(got, "all=/browse?in=Family&kind=all page=/browse?in=Family&kind=page ") ||
		!strings.HasSuffix(got, " metric=/browse?in=Family&kind=metric") || !strings.Contains(got, "media=/browse?in=Family&kind=media ") {
		t.Errorf("chips in Family: %s", got)
	}
	// The ends of the tree keep a kind the URL names; without one, a branch opens All in that category.
	e := apitest.Must(t)(c.Get(ctx, "/browse?kind=person"))
	if apitest.Href(e, "everything") != "/browse?kind=person" || apitest.Href(e, "unfiled") != "/browse?kind=person&unfiled=1" {
		t.Errorf("tree ends: %s %s", apitest.Href(e, "everything"), apitest.Href(e, "unfiled"))
	}
	var branches []string
	for _, l := range e.Entities {
		if l.Rel[0] == "category" {
			branches = append(branches, l.Title+"="+l.Href)
		}
	}
	if got := strings.Join(branches, " "); got != "Family=/browse?in=Family&kind=person Cousins=/browse?in=Cousins&kind=person" {
		t.Errorf("branches by path: %s", got)
	}
	if e := apitest.Must(t)(c.Get(ctx, "/browse")); apitest.Href(e, "everything") != "/browse" || apitest.Href(e, "unfiled") != "/browse?unfiled=1" ||
		!slices.ContainsFunc(e.Entities, func(l siren.Link) bool { return l.Href == "/browse?in=Family" }) {
		t.Errorf("tree with no kind named: %s %s %v", apitest.Href(e, "everything"), apitest.Href(e, "unfiled"), e.Entities)
	}
}

// oldGhosts inserts empty unlinked pages with an old created_at by direct SQL: bulk setup, as the core hygiene test
// does, since ghost_pages wants them older than 30 days and created_at never changes.
func oldGhosts(t *testing.T, s *core.Store, titles ...string) {
	t.Helper()
	err := s.DB.Write(t.Context(), func(tx *sql.Tx) error {
		for i, title := range titles {
			var id int64
			at := fmt.Sprintf("2000-01-0%dT00:00:00.000Z", i+1)
			key := strings.ToLower(title)
			if err := tx.QueryRow(`INSERT INTO entities(entity_type, preferred_name_key, body, created_at, updated_at, source)
			    VALUES ('page', ?, '', ?, ?, 'cli') RETURNING id`, key, at, at).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, title, key); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBrowseGhostsAreOldestFirstAndOfferDelete(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	ctx := t.Context()
	if _, _, err := s.CreatePage(ctx, "cli", "Fresh", "", ""); err != nil { // empty, but written now: not a ghost
		t.Fatal(err)
	}
	oldGhosts(t, s, "Zeta ghost", "Alpha ghost")
	e := apitest.Must(t)(c.Get(ctx, "/browse?ghosts=1"))
	if got := browseRows(t, e, "kind"); got != "Zeta ghost:kind=page Alpha ghost:kind=page" {
		t.Errorf("ghost rows: %s", got)
	}
	if p := props(e); p["ghosts"] != true || p["kind"] != "page" || fmt.Sprint(p["counts"].(map[string]any)["ghosts"]) != "2" {
		t.Errorf("ghost properties: %v", p)
	}
	var ids []string
	for _, r := range props(e)["rows"].([]any) {
		ids = append(ids, fmt.Sprint(r.(map[string]any)["id"]))
	}
	done := apitest.Must(t)(c.Do(ctx, apitest.Action(t, e, "tombstone-pages"), map[string]string{"ids": strings.Join(ids, " ")}))
	if done.Result != "Deleted 2 pages." {
		t.Errorf("the delete: %v", done.Result)
	}
	if rows := props(apitest.Must(t)(c.Get(ctx, "/browse?ghosts=1")))["rows"].([]any); len(rows) != 0 {
		t.Errorf("ghosts after the delete: %v", rows)
	}
}

func TestBrowseRefusesACombinationWithNoMeaning(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	ids := browseFixture(t, s)
	ctx := t.Context()
	// Which filters mean nothing is TestBrowseQueryOf's; GET /browse refuses them.
	if _, err := c.Get(ctx, "/browse?kind=all"); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("/browse?kind=all: %v, want 422", err)
	}
	if _, err := c.Get(ctx, "/browse?in=Nobody+at+all"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("a title of nothing: %v, want 404", err)
	}
	// Only a plain page can be a category (link_kinds, links_endpoint_types): a person, a place, a file, a metric and a
	// day are refused, not given an empty list.
	for _, title := range []string{"Ana", "Lakeside", "photo.jpg", "Mood", "2031-05-01"} {
		if _, err := c.Get(ctx, "/browse?in="+url.QueryEscape(title)); apitest.Status(err) != http.StatusUnprocessableEntity {
			t.Errorf("in=%s: %v, want 422", title, err)
		}
	}
	// A tombstoned category is not in the tree: it is a conflict, as every tombstone is.
	if err := s.Tombstone(ctx, "cli", ids["Cousins"]); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "/browse?in=Cousins"); apitest.Status(err) != http.StatusConflict {
		t.Errorf("in a tombstoned category: %v, want 409", err)
	}
	// What was filed only there is in no category now (Weight; Gamma is a page filed only there too).
	if got := browseRows(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=metric&unfiled=1"))); got != "Mood: Weight:" {
		t.Errorf("metrics not filed after the tombstone: %s", got)
	}
}

func TestBrowsePagingKeepsTheFilters(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	first := apitest.Must(t)(c.Get(ctx, "/browse?in=Family&limit=2"))
	if got := browseRows(t, first); got != "Cousins: Ana:" {
		t.Errorf("the first page: %s", got)
	}
	next := apitest.Href(first, "next")
	u, err := url.Parse(next)
	if err != nil {
		t.Fatal(err)
	}
	if q := u.Query(); u.Path != "/browse" || q.Get("in") != "Family" || q.Get("limit") != "2" || q.Get("offset") != "2" || q.Has("kind") {
		t.Errorf("next: %s", next)
	}
	second := apitest.Must(t)(c.Get(ctx, next))
	if got := browseRows(t, second); got != "Beta: photo.jpg:" {
		t.Errorf("the second page: %s", got)
	}
	if prev := apitest.Href(second, "prev"); prev != "/browse?in=Family&limit=2&offset=0" {
		t.Errorf("prev: %s", prev)
	}
	last := apitest.Must(t)(c.Get(ctx, apitest.Href(second, "next")))
	if got := browseRows(t, last); got != "scan.pdf:" || apitest.Href(last, "next") != "" {
		t.Errorf("the last page: %s, next %q", got, apitest.Href(last, "next"))
	}
	media := apitest.Must(t)(c.Get(ctx, "/browse?kind=media&media=image&unfiled=1&limit=1&offset=1"))
	if prev := apitest.Href(media, "prev"); prev != "/browse?kind=media&limit=1&media=image&offset=0&unfiled=1" {
		t.Errorf("prev with every filter: %s", prev)
	}
	if self := apitest.Href(media, "self"); self != "/browse?kind=media&media=image&unfiled=1" {
		t.Errorf("self: %s", self)
	}
}

// TestBrowseOffersTheNewAndThePickBarOfItsKind: the actions of each list are TestBrowseActions'; GET /browse gives
// them the category that the URL names and the categories of the file.
func TestBrowseOffersTheNewAndThePickBarOfItsKind(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	// Inside a category the New page form files the page there; the URL names the category in lower case.
	if v := fieldOf(apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=family&kind=page")), "create-page"), "category"); v != "Family" {
		t.Errorf("New page in Family has category %q", v)
	}
	// The file field takes a file of any kind on both lists: New file opens the media list, and a document kept
	// there goes to the documents.
	accept := func(path string) string {
		a := apitest.Action(t, apitest.Must(t)(c.Get(ctx, path)), "add-file")
		for _, f := range a.Fields {
			if f.Name == "original" {
				return f.Accept
			}
		}
		return "?"
	}
	if got := accept("/browse?kind=media"); got != "" {
		t.Errorf("media accepts %q", got)
	}
	if got := accept("/browse?kind=document"); got != "" {
		t.Errorf("documents accept %q", got)
	}
	// file-pages offers the categories by title.
	if a := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind=person")), "file-pages"); !hasOption(a, "category", "Family") || !hasOption(a, "category", "Cousins") {
		t.Errorf("file-pages options: %+v", a.Fields)
	}
}

// TestTheCatalogsBrowseGivesTheRowsToAnAgent: the catalog action browse is a read of /browse that is not the owner's
// and not a danger, and it gives the owner and an agent the same rows.
func TestTheCatalogsBrowseGivesTheRowsToAnAgent(t *testing.T) {
	want := "Ana:mentions=1 Bo:mentions=0"
	_, h, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	for _, source := range []string{"cli", "agent:browse"} {
		c := client.InProcess(h, source)
		actions, err := c.Catalog(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(actions, func(a siren.Action) bool { return a.Name == "browse" })
		if i < 0 || actions[i].Owner || actions[i].Danger || actions[i].Method != "GET" || actions[i].Href != "/browse" {
			t.Fatalf("%s: the catalog's browse: %+v", source, actions)
		}
		e, err := c.Do(t.Context(), actions[i], map[string]string{"kind": "person"})
		if err != nil {
			t.Fatal(err)
		}
		if got := browseRows(t, e, "mentions"); got != want {
			t.Errorf("%s: %s, want %s", source, got, want)
		}
	}
}

func TestTheBrowsePageDrawsTheTreeTheChipsAndTheRows(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	ids := browseFixture(t, s)
	family := browse(t, h, "/browse?in=Family")
	for _, want := range []string{
		`<h1>Browse</h1><p class="sub"><span>In Family</span>`,
		`<details class="browse-tree panel"><summary>Categories <span class="meta">In Family</span></summary><nav aria-label="Categories">`,
		`<a class="entry" href="/browse"><span class="first">Everything</span>`,
		// The tree folds (docs/plans/118-fold-and-find.md, rule 1): Cousins is in the fold of Family, open, since the
		// list is on Family.
		`<li class="branch"><a class="entry" href="/browse?in=Family" aria-current="page"><span class="first">Family</span><span class="meta">5</span></a><details open><summary><span class="sr">Categories in Family</span></summary><ul class="plain rows tree"><li><a class="entry" href="/browse?in=Cousins"><span class="first">Cousins</span><span class="meta">2</span></a></li>`,
		`<a class="entry" href="/browse?unfiled=1"><span class="first">Not filed</span>`,
		`<nav class="kinds chips" aria-label="Kind"><a href="/browse?in=Family&amp;kind=all" aria-current="page">All <small>5</small></a><a href="/browse?in=Family&amp;kind=page">Pages <small>2</small></a>`,
		// The category under Family is a row with its badge.
		`<span class="first">Cousins</span><span class="meta"><span class="badge category">category</span></span>`,
		`<span class="first">Ana</span><span class="meta"><span class="badge">person</span></span>`,
	} {
		if !strings.Contains(family, want) {
			t.Errorf("/browse?in=Family lacks %s", want)
		}
	}
	if strings.Contains(family, `id="pick"`) {
		t.Error("All in a category draws a pick bar")
	}
	if cousins := browse(t, h, "/browse?in=Cousins&kind=page"); !strings.Contains(cousins, "<span>In Family › Cousins</span>") ||
		!strings.Contains(cousins, `<a href="/browse?in=Cousins&amp;kind=page" aria-current="page">Pages <small>1</small></a>`) {
		t.Errorf("/browse?in=Cousins&kind=page lacks its path or its chip")
	} else if !strings.Contains(cousins, `<details open><summary><span class="sr">Categories in Family</span>`) {
		t.Error("the fold of Family is not open on the path to Cousins")
	}

	people := browse(t, h, "/browse?kind=person")
	for _, want := range []string{
		fmt.Sprintf(`<a class="entry named face" href="/pages/%d"><span class="face-picture" aria-hidden="true">`, ids["Ana"]),
		`<span class="first">Ana</span><span class="meta">mentioned on 1 page, last `, `<span class="meta">not mentioned yet</span>`,
		`<form id="pick" method="POST" action="/pages/file" class="inline">`,
		fmt.Sprintf(`<input type="checkbox" name="ids" value="%d" form="pick" aria-label="Select Ana">`, ids["Ana"]),
		`<datalist id="pick-category"><option value="Family"><option value="Cousins"></datalist>`, `<a href="/browse?kind=person" aria-current="page">People <small>2</small></a>`,
		`<a class="entry" href="/browse?kind=person" aria-current="page"><span class="first">Everything</span>`,
	} {
		if !strings.Contains(people, want) {
			t.Errorf("/browse?kind=person lacks %s", want)
		}
	}
	if strings.Contains(people, `formaction="/pages/tombstone"`) || strings.Contains(people, "delete them") {
		t.Error("the people list offers Delete")
	}

	media := browse(t, h, "/browse?kind=media")
	for _, want := range []string{`<ul class="plain tiles">`, `<li class="tile pick">`, `<span class="first">memo.m4a</span><span class="meta">recording · `,
		`<nav class="kinds" aria-label="Media kind"><a href="/browse?kind=media" aria-current="page">All media</a><a href="/browse?kind=media&amp;media=image">Pictures</a>`,
		`formaction="/pages/tombstone"`} {
		if !strings.Contains(media, want) {
			t.Errorf("/browse?kind=media lacks %s", want)
		}
	}
	if strings.Contains(media, `accept="`) {
		t.Error("the media list limits the kinds of file it keeps")
	}
	if docs := browse(t, h, "/browse?kind=document"); !strings.Contains(docs, `<span class="first">scan.pdf</span><span class="meta">application/pdf · `) {
		t.Error("the documents lack their row")
	}
	if metrics := browse(t, h, "/browse?kind=metric"); !strings.Contains(metrics, `<span class="first">Weight</span><span class="meta">kg</span>`) ||
		!strings.Contains(metrics, `<span class="first">Mood</span><span class="meta">of 5</span>`) || !strings.Contains(metrics, `<a href="/metrics">The values are on the Metrics board</a>`) {
		t.Errorf("the metrics lack their rows or the way to the board: %s", metrics[strings.Index(metrics, "<main"):])
	}
	pages := browse(t, h, "/browse")
	// On Pages in Everything a category is a row with its box and its badge; the fold of Family is closed.
	for _, want := range []string{`<a href="/browse?ghosts=1&amp;kind=page">Ghost pages 0</a>`,
		fmt.Sprintf(`<li class="pick"><label class="pick-box"><input type="checkbox" name="ids" value="%d" form="pick" aria-label="Select Family"></label>`, ids["Family"]),
		`<span class="first">Family</span><span class="meta"><span class="badge category">category</span></span>`,
		`<details><summary><span class="sr">Categories in Family</span>`,
		`formaction="/pages/tombstone"`, "type a category or delete them"} {
		if !strings.Contains(pages, want) {
			t.Errorf("/browse lacks %s", want)
		}
	}
	// The form of the catalog's browse, on /actions: an optional select starts empty, so the form sends only what the
	// reader picks, and the empty values it sends are the defaults.
	actions := browse(t, h, "/actions")
	for _, want := range []string{`<form method="GET" action="/browse">`, `<select name="kind"><option></option><option>page</option>`,
		`<select name="media"><option></option><option>image</option>`} {
		if !strings.Contains(actions, want) {
			t.Errorf("/actions lacks %s", want)
		}
	}
	if blank := browse(t, h, "/browse?kind=&in=&media=&limit=&offset="); !strings.Contains(blank, `<a href="/browse?kind=page" aria-current="page">Pages <small>5</small></a>`) {
		t.Error("the blank form of browse does not give the pages")
	}
	// The ghost list looks at everything: the tree marks Everything.
	if ghosts := browse(t, h, "/browse?ghosts=1&kind=page"); !strings.Contains(ghosts, `<a class="entry" href="/browse?kind=page" aria-current="page"><span class="first">Everything</span>`) {
		t.Error("the ghost list marks no branch of the tree")
	}
	for path, want := range map[string]string{
		"/browse?ghosts=1":                "No ghost page: every page has text or a link.",
		"/browse?kind=document&unfiled=1": "Everything of this kind is in a category.",
		"/browse?in=Cousins&kind=person":  "Nothing of this kind is filed in Cousins.",
		"/browse?kind=place&in=Alpha":     "Nothing of this kind is filed in Alpha.",
	} {
		if body := browse(t, h, path); !strings.Contains(body, `<p class="empty">`+want+`</p>`) {
			t.Errorf("%s lacks its empty state %q", path, want)
		}
	}
}

// The category tree of Browse (docs/plans/117-one-browse-view.md, rule 3): the categories by path with a count each,
// beside the list, and Not filed at its end.

func TestBrowseDrawsTheTreeAndTheUnfiledPages(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030", "Loose note", "Gone"} {
		ids[title] = fmt.Sprint(props(apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": title})))["id"])
	}
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "a day"}))
	file := func(id, category string) {
		t.Helper()
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+id)), "file-pages"), map[string]string{"category": category}))
	}
	file(ids["Japan 2031"], "Travel")
	file(ids["Alps 2030"], "Travel")
	file(ids["Gone"], "Travel")
	travel := fmt.Sprint(props(apitest.Must(t)(c.Get(ctx, "/pages?title=Travel")))["id"])
	file(travel, "Life")
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages/"+ids["Gone"])), "tombstone-pages"), nil))

	e := apitest.Must(t)(c.Get(ctx, "/browse?unfiled=1"))
	var tree []string
	for _, x := range props(e)["categories"].([]any) {
		c := x.(map[string]any)
		tree = append(tree, fmt.Sprintf("%s=%v", c["path"], c["count"]))
	}
	// Life holds Travel; Travel holds the two live pages, not the tombstone.
	if want := "Life=1 Life/Travel=2"; strings.Join(tree, " ") != want {
		t.Errorf("tree: %v, want %s", tree, want)
	}
	// Not filed: not a category (Life), not a filed page, not a day page, not a tombstone.
	var links []string
	for _, l := range e.Entities {
		links = append(links, strings.Join(l.Rel, "+")+":"+l.Title)
	}
	if want := "item:Loose note category:Life category:Travel"; strings.Join(links, " ") != want {
		t.Errorf("links: %v, want %s", links, want)
	}

	body := browse(t, h, "/browse?unfiled=1")
	rows := regexp.MustCompile(`<li(?: class="branch")?><a class="entry" href="/browse\?in=(\w+)"><span class="first">([^<]*)</span><span class="meta">(\d+)</span></a>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, r := range rows {
		got = append(got, r[2]+"="+r[3])
	}
	// The tree is drawn once: a fold that a wide column holds open.
	if want := "Life=1 Travel=2"; strings.Join(got, " ") != want {
		t.Errorf("tree rows: %v, want %s", got, want)
	}
	// Travel is in the fold of Life (docs/plans/118-fold-and-find.md, rule 1), closed: the list is not on either.
	if want := `<li class="branch"><a class="entry" href="/browse?in=Life"><span class="first">Life</span><span class="meta">1</span></a><details><summary><span class="sr">Categories in Life</span></summary><ul class="plain rows tree"><li><a class="entry" href="/browse?in=Travel">`; strings.Count(body, want) != 1 {
		t.Errorf("the tree does not hold Travel in the closed fold of Life")
	}
	if !strings.Contains(body, `<a class="entry" href="/browse?unfiled=1" aria-current="page"><span class="first">Not filed</span>`) ||
		!strings.Contains(body, `href="/pages/`+ids["Loose note"]+`"><span class="first">Loose note</span>`) {
		t.Error("Not filed is not marked, or does not list the loose page")
	}
	if strings.Contains(body, `<span class="first">Gone</span>`) || strings.Contains(body, `<span class="first">Japan 2031</span>`) {
		t.Error("Not filed lists a filed page or a tombstone")
	}
}

func TestBrowseWithNoCategory(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	e := apitest.Must(t)(c.Get(t.Context(), "/browse"))
	if cs := props(e)["categories"].([]any); len(cs) != 0 {
		t.Errorf("categories: %v", cs)
	}
	body := browse(t, h, "/browse?unfiled=1")
	tree := regexp.MustCompile(`(?s)<nav aria-label="Categories">(.*?)</nav>`).FindStringSubmatch(body)
	if tree == nil || strings.Count(tree[1], "<li") != 2 || !strings.Contains(tree[1], "Everything") || !strings.Contains(tree[1], "Not filed") {
		t.Errorf("an empty tree is not Everything and Not filed: %v", tree)
	}
	if !strings.Contains(body, `<p class="empty">Everything of this kind is in a category.</p>`) {
		t.Error("an empty Not filed says nothing")
	}
}

// TestTheGhostPagesSayWhatTheyAre: the ghost list of Browse says what a ghost page is under its title, and an empty
// list says why it is empty, not how to make a page.
func TestTheGhostPagesSayWhatTheyAre(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	body := browse(t, h, "/browse?ghosts=1")
	for _, want := range []string{`<p class="sub"><span>Ghost pages</span><span>Empty pages, older than 30 days, that nothing links to or from.</span></p>`,
		`<p class="empty">No ghost page: every page has text or a link.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the ghost pages lack %s", want)
		}
	}
}

// The navigation of Browse (docs/plans/117-one-browse-view.md, rules 7 and 8): the old lists answer 308 to Browse,
// and Home links the lists of Browse. The sidebar is TestTheSidebarHasMetricsAndBrowse's, and a category page that
// opens itself in Browse is TestACategoryPageOpensInBrowse's.

func TestHomeLinksTheListsOfBrowse(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for rel, want := range map[string]string{
		"browse": "/browse", "categories": "/browse", "people": "/browse?kind=person", "places": "/browse?kind=place",
		"media": "/browse?kind=media", "files": "/browse?kind=document", "ghosts": "/browse?ghosts=1&kind=page", "metrics": "/metrics",
	} {
		if got := apitest.Href(root, rel); got != want {
			t.Errorf("Home's %s is %q, want %q", rel, got, want)
		}
	}
	// Every link of Home is served: no link answers a redirect or an error.
	for _, l := range root.Links {
		req := httptest.NewRequest("GET", l.Href, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && !(l.Href == "/days/today" || strings.HasSuffix(l.Href, "/current")) {
			t.Errorf("Home's %s (%s): %d", l.Rel[0], l.Href, rec.Code)
		}
	}
}

// TestBrowseListsTheDeletedSoTheyCanBeRestored: a tombstone is in Browse's list of the deleted, with its type, and
// its row opens its page, which offers revive (docs/plans/131-the-pareto-pass.md, part 1). The list of pages counts
// and links it; a live page is not in it.
func TestBrowseListsTheDeletedSoTheyCanBeRestored(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	gone := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Gone"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Kept"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, gone, "tombstone-pages"), nil))

	pages := apitest.Must(t)(c.Get(ctx, "/browse"))
	if n := pages.Properties.(map[string]any)["counts"].(map[string]any)["deleted"]; fmt.Sprint(n) != "1" {
		t.Errorf("counts.deleted = %v, want 1", n)
	}
	deleted := apitest.Must(t)(c.Get(ctx, apitest.Href(pages, "deleted")))
	if got := browseRows(t, deleted, "type"); got != "Gone:type=page" {
		t.Errorf("the deleted list: %q, want Gone:type=page", got)
	}
	if len(deleted.Actions) != 0 {
		t.Errorf("the deleted list offers %v, want no action", deleted.Actions)
	}
	page := apitest.Must(t)(c.Get(ctx, deleted.Entities[0].Href))
	apitest.Action(t, page, "revive")
	body := browse(t, h, "/browse?deleted=1")
	if !strings.Contains(body, `href="`+deleted.Entities[0].Href+`"`) {
		t.Errorf("the page of the deleted list does not link Gone:\n%s", body)
	}
	if _, err := c.Get(ctx, "/browse?deleted=1&kind=page"); apitest.Status(err) != 422 {
		t.Errorf("deleted with a kind: %v, want 422", err)
	}
}
