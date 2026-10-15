package api_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The Categories view (docs/plans/115-categories-to-browse.md): the category tree with a count each, then the
// plain pages filed nowhere.

func TestTheCategoriesViewDrawsTheTreeAndTheUnfiledPages(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	ids := map[string]string{}
	for _, title := range []string{"Japan 2031", "Alps 2030", "Loose note", "Gone"} {
		ids[title] = fmt.Sprint(props(must(c.Do(ctx, find(root, "create-page"), map[string]string{"title": title})))["id"])
	}
	must(c.Do(ctx, find(root, "capture"), map[string]string{"text": "a day"}))
	file := func(id, category string) {
		t.Helper()
		must(c.Do(ctx, find(must(c.Get(ctx, "/pages/"+id)), "file-pages"), map[string]string{"category": category}))
	}
	file(ids["Japan 2031"], "Travel")
	file(ids["Alps 2030"], "Travel")
	file(ids["Gone"], "Travel")
	travel := fmt.Sprint(props(must(c.Get(ctx, "/pages?title=Travel")))["id"])
	file(travel, "Life")
	must(c.Do(ctx, find(must(c.Get(ctx, "/pages/"+ids["Gone"])), "tombstone"), nil))

	e := must(c.Get(ctx, "/categories"))
	if e.Title != "Categories" || e.Class[0] != "categories" {
		t.Fatalf("GET /categories: %s %v", e.Title, e.Class)
	}
	var tree []string
	for _, x := range props(e)["categories"].([]any) {
		c := x.(map[string]any)
		tree = append(tree, fmt.Sprintf("%s=%v", c["path"], c["count"]))
	}
	// Life holds Travel; Travel holds the two live pages, not the tombstone.
	if want := "Life=1 Life/Travel=2"; strings.Join(tree, " ") != want {
		t.Errorf("tree: %v, want %s", tree, want)
	}
	var unfiled []string
	for _, x := range props(e)["unfiled"].([]any) {
		unfiled = append(unfiled, x.(map[string]any)["title"].(string))
	}
	// Not a category (Life), not a filed page, not a day page, not a tombstone.
	if want := []string{"Loose note"}; strings.Join(unfiled, ",") != strings.Join(want, ",") {
		t.Errorf("unfiled: %v, want %v", unfiled, want)
	}
	var links []string
	for _, l := range e.Entities {
		links = append(links, strings.Join(l.Rel, "+")+":"+l.Title)
	}
	if want := "category:Life category:Travel item:Loose note"; strings.Join(links, " ") != want {
		t.Errorf("links: %v, want %s", links, want)
	}

	body := browse(t, h, "/categories")
	if !strings.Contains(body, `<a href="/categories" aria-current="page">`) {
		t.Error("the sidebar does not mark Categories as current")
	}
	rows := regexp.MustCompile(`<li class="depth-(\d)"><a class="entry" href="/pages/(\d+)"><span class="first">([^<]*)</span><span class="meta">(\d+)</span></a></li>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, r := range rows {
		got = append(got, r[1]+":"+r[3]+"="+r[4])
	}
	if want := "0:Life=1 1:Travel=2"; strings.Join(got, " ") != want {
		t.Errorf("tree rows: %v, want %s", got, want)
	}
	if !strings.Contains(body, `<h2>Not filed</h2>`) || !strings.Contains(body, `<a href="/pages/`+ids["Loose note"]+`">Loose note</a>`) {
		t.Error("Not filed does not list the loose page")
	}
	if strings.Contains(body, `">Gone</a>`) || strings.Contains(body, `">Japan 2031</a>`) {
		t.Error("Not filed lists a filed page or a tombstone")
	}
	if !strings.Contains(browse(t, h, "/"), `href="/categories"`) {
		t.Error("Home has no link to the categories")
	}
}

func TestTheCategoriesViewWithNoCategory(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	e := must(c.Get(t.Context(), "/categories"))
	if cs := props(e)["categories"].([]any); len(cs) != 0 {
		t.Errorf("categories: %v", cs)
	}
	if body := browse(t, h, "/categories"); !strings.Contains(body, "No category yet") {
		t.Error("an empty tree says nothing")
	}
}
