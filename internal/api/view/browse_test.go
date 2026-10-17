package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api/siren"
)

// The folds of Browse (docs/plans/118-fold-and-find.md, rules 1 to 3), on props written by hand in the shape a
// template gets them.

// browseViewOf is a view of GET /browse with these props, and the tree's links in the order of its categories.
func browseViewOf(props map[string]any) *view {
	e := &siren.Entity{Class: []string{"browse"}}
	cats, _ := props["categories"].([]any)
	for _, x := range cats {
		c := x.(map[string]any)
		e.Entities = append(e.Entities, siren.Link{Rel: []string{"category"}, Href: "/browse?in=" + c["title"].(string)})
	}
	return &view{Entity: e, Props: props}
}

func category(id float64, path string, count float64) map[string]any {
	return map[string]any{"id": id, "title": path[strings.LastIndex(path, "/")+1:], "path": path, "count": count}
}

func pageRows(titles ...string) []any {
	var out []any
	for i, t := range titles {
		out = append(out, map[string]any{"id": float64(100 + i), "title": t, "kind": "page"})
	}
	return out
}

func numbered(n int, prefix string) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("%s %03d", prefix, i))
	}
	return out
}

func TestTheTreeFoldsOpenOnThePath(t *testing.T) {
	cats := []any{category(1, "Family", 3), category(2, "Family/Cousins", 2), category(3, "Family/Cousins/Second", 1),
		category(4, "Family/Grandparents", 1), category(5, "Work", 4)}
	tree := browseViewOf(map[string]any{"categories": cats, "in": map[string]any{"id": float64(2)}}).BrowseTree()
	var got []string
	var walk func(ns []*treeNode, depth int)
	walk = func(ns []*treeNode, depth int) {
		for _, n := range ns {
			got = append(got, fmt.Sprintf("%d:%s=%s open=%v current=%v %s", depth, n.Title, n.Count, n.Open, n.Current, n.Href))
			walk(n.Kids, depth+1)
		}
	}
	walk(tree, 0)
	want := []string{
		"0:Family=3 open=true current=false /browse?in=Family",
		"1:Cousins=2 open=true current=true /browse?in=Cousins",
		"2:Second=1 open=false current=false /browse?in=Second",
		"1:Grandparents=1 open=false current=false /browse?in=Grandparents",
		"0:Work=4 open=false current=false /browse?in=Work",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if tree := browseViewOf(map[string]any{"categories": cats}).BrowseTree(); tree[0].Open || tree[1].Open {
		t.Error("with no category in the list, a fold is open")
	}
	if tree := browseViewOf(map[string]any{}).BrowseTree(); len(tree) != 0 {
		t.Errorf("an empty tree has branches: %v", tree)
	}
}

func TestTheTopCategoriesAreFolders(t *testing.T) {
	cats := []any{category(1, "Family", 3), category(2, "Family/Cousins", 2), category(5, "Work", 4)}
	rows := pageRows("Alpha", "Cousins", "Family", "Work", "Zeta")
	for i, id := range map[int]float64{1: 2, 2: 1, 3: 5} { // Cousins, Family and Work are the categories
		rows[i].(map[string]any)["id"], rows[i].(map[string]any)["category"] = id, true
	}
	l := browseViewOf(map[string]any{"kind": "page", "categories": cats, "rows": rows}).BrowseList()
	var folders, titles []string
	for _, f := range l.Folders {
		folders = append(folders, fmt.Sprintf("%v=%v %v", f["title"], f["count"], f["href"]))
	}
	for _, r := range l.Rows {
		titles = append(titles, r.(map[string]any)["title"].(string))
	}
	if got := strings.Join(folders, ", "); got != "Family=3 /browse?in=Family, Work=4 /browse?in=Work" {
		t.Errorf("folders: %s", got)
	}
	if got := strings.Join(titles, " "); got != "Alpha Zeta" {
		t.Errorf("rows: %s, want the pages that are not categories", got)
	}

	// Inside a category, the categories among its rows are the folders; Not filed and the ghosts have none.
	in := browseViewOf(map[string]any{"kind": "all", "categories": cats, "rows": rows[:2], "in": map[string]any{"id": float64(1)}}).BrowseList()
	if len(in.Folders) != 1 || in.Folders[0]["title"] != "Cousins" || in.Folders[0]["count"] != "2" || len(in.Rows) != 1 {
		t.Errorf("inside Family: folders %v, rows %v", in.Folders, in.Rows)
	}
	for _, props := range []map[string]any{{"kind": "page", "unfiled": true}, {"kind": "page", "ghosts": true}} {
		props["categories"], props["rows"] = cats, pageRows("Alpha")
		if l := browseViewOf(props).BrowseList(); len(l.Folders) != 0 || len(l.Rows) != 1 {
			t.Errorf("%v: folders %v, rows %v", props, l.Folders, l.Rows)
		}
	}
}

func TestALongListIsInLetterGroups(t *testing.T) {
	list := func(kind string, titles []string, more map[string]any) browseList {
		props := map[string]any{"kind": kind, "rows": pageRows(titles...)}
		for k, v := range more {
			props[k] = v
		}
		return browseViewOf(props).BrowseList()
	}
	if l := list("page", numbered(30, "Page"), nil); l.Letters != nil || len(l.Rows) != 30 {
		t.Errorf("30 rows are grouped: %d groups", len(l.Letters))
	}

	titles := append([]string{"2026 plans", "#tag", "ana", "Ana B", "ăla"}, numbered(26, "Bee")...)
	l := list("person", titles, nil)
	var got []string
	for _, g := range l.Letters {
		got = append(got, fmt.Sprintf("%s=%d %s", g.Letter, len(g.Rows), g.Anchor))
	}
	if want := "#=2 letter-1, A=2 letter-2, Ă=1 letter-3, B=26 letter-4"; strings.Join(got, ", ") != want || l.Rows != nil || l.Closed {
		t.Errorf("groups: %s (closed %v), want %s, open", strings.Join(got, ", "), l.Closed, want)
	}
	if l := list("metric", numbered(100, "M"), nil); len(l.Letters) != 1 || l.Closed {
		t.Errorf("100 rows: %d groups, closed %v; want one open group", len(l.Letters), l.Closed)
	}
	if l := list("page", numbered(101, "M"), nil); !l.Closed {
		t.Error("101 rows start open")
	}
	// The lists that are not in title order are not grouped.
	for _, more := range []map[string]any{{"ghosts": true}, {"kind": "media"}, {"kind": "document"}} {
		kind, _ := more["kind"].(string)
		if kind == "" {
			kind = "page"
		}
		if l := list(kind, numbered(40, "X"), more); l.Letters != nil || len(l.Rows) != 40 {
			t.Errorf("%v is grouped", more)
		}
	}
}

// TestTheSitesOfCountSayZeroTheirOwnWay: a site that calls Count says 0 in its own words, and every other number
// with Count. The calendar's items and a year's summary are the tables of TestACalendarItemInWords and
// TestAYearsSummary.
func TestTheSitesOfCountSayZeroTheirOwnWay(t *testing.T) {
	v := &view{Shell: &Shell{Today: "2026-10-16", Zone: time.UTC}}
	for _, c := range []struct {
		n               float64
		media, mentions string
	}{
		{0, "no pictures", "not mentioned yet"},
		{1, "1 picture", "mentioned on 1 page, last Thu 1 Oct"},
		{2, "2 pictures", "mentioned on 2 pages, last Thu 1 Oct"},
		{1234567, "1234567 pictures", "mentioned on 1234567 pages, last Thu 1 Oct"},
	} {
		if got := v.MediaCount(c.n, "picture", "pictures"); got != c.media {
			t.Errorf("MediaCount(%g) = %q, want %q", c.n, got, c.media)
		}
		if got := v.Mentions(c.n, "2026-10-01"); got != c.mentions {
			t.Errorf("Mentions(%g) = %q, want %q", c.n, got, c.mentions)
		}
	}
}
