package api_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The category tree of Browse (docs/plans/117-one-browse-view.md, rule 3): the categories by path with a count each,
// beside the list, and Not filed at its end.

func TestBrowseDrawsTheTreeAndTheUnfiledPages(t *testing.T) {
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

	e := must(c.Get(ctx, "/browse?unfiled=1"))
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
	rows := regexp.MustCompile(`<li class="depth-(\d)"><a class="entry" href="/browse\?in=(\w+)"><span class="first">([^<]*)</span><span class="meta">(\d+)</span></a></li>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, r := range rows {
		got = append(got, r[1]+":"+r[3]+"="+r[4])
	}
	// The tree is drawn twice: beside the list, and in the fold a phone shows.
	if want := "0:Life=1 1:Travel=2 0:Life=1 1:Travel=2"; strings.Join(got, " ") != want {
		t.Errorf("tree rows: %v, want %s", got, want)
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
	c, h := freshOn(t, fixedToday)
	e := must(c.Get(t.Context(), "/browse"))
	if cs := props(e)["categories"].([]any); len(cs) != 0 {
		t.Errorf("categories: %v", cs)
	}
	body := browse(t, h, "/browse?unfiled=1")
	tree := regexp.MustCompile(`(?s)<nav class="browse-side panel" aria-label="Categories">(.*?)</nav>`).FindStringSubmatch(body)
	if tree == nil || strings.Count(tree[1], "<li") != 2 || !strings.Contains(tree[1], "Everything") || !strings.Contains(tree[1], "Not filed") {
		t.Errorf("an empty tree is not Everything and Not filed: %v", tree)
	}
	if !strings.Contains(body, `<p class="empty">Everything of this kind is in a category.</p>`) {
		t.Error("an empty Not filed says nothing")
	}
}
