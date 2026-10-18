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
