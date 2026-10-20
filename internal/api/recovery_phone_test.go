package api_test

import (
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestBrowseCountsTheDeletedOnEveryKind: the tombstones are one list of every kind, so its link and its count are in
// the category tree, and every kind's list draws them, not only the list of pages.
func TestBrowseCountsTheDeletedOnEveryKind(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	e := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "Old idea"}))
	id := idOfSelf(t, apitest.Href(e, "self"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse")), "tombstone-pages"), map[string]string{"ids": id}))
	want := `<li><a class="entry" href="/browse?deleted=1"><span class="first">Deleted</span><span class="meta">1</span></a></li>`
	for _, path := range []string{"/browse", "/browse?kind=person", "/browse?kind=media", "/browse?kind=metric", "/browse?unfiled=1"} {
		if body := browse(t, h, path); !strings.Contains(body, want) {
			t.Errorf("%s lacks %s", path, want)
		}
	}
	if body := browse(t, h, "/browse?deleted=1"); !strings.Contains(body, `<a class="entry" href="/browse?deleted=1" aria-current="page">`) {
		t.Error("the deleted list does not mark its own link")
	}
}

// TestNewCategoryOpensTheSelectBar: a page becomes a category when something is filed in it, so the link of the tree
// opens the Select bar with the words that say so, not the New page form.
func TestNewCategoryOpensTheSelectBar(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "create-page"), map[string]string{"title": "Loose note"}))
	body := browse(t, h, "/browse?kind=page&new=file-pages")
	for _, want := range []string{`<a href="/browse?kind=page&amp;new=file-pages">New category</a>`,
		`<input type="checkbox" class="pick-on" checked>`,
		`<span class="desc">Check what goes in it, then type its name. A new name makes a new category.</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the list lacks %s", want)
		}
	}
	if plain := browse(t, h, "/browse?kind=page"); strings.Contains(plain, "then type its name") || strings.Contains(plain, `class="pick-on" checked`) {
		t.Error("the Select bar is open without being asked")
	}
}

// TestASearchWithNoHitOffersTheNewPage: the link makes a page titled with the words searched, unless they hold search
// syntax (a * or a quote), which no title can keep.
func TestASearchWithNoHitOffersTheNewPage(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	body := browse(t, h, "/search?q="+url.QueryEscape("Zebra crossing"))
	if want := `<a href="/browse?kind=page&amp;new=create-page&amp;title=Zebra&#43;crossing">Make a page titled ‘Zebra crossing’</a>`; !strings.Contains(body, want) {
		t.Errorf("the search lacks %s", want)
	}
	for _, q := range []string{"zebra*", `"zebra crossing"`} {
		if body := browse(t, h, "/search?q="+url.QueryEscape(q)); !strings.Contains(body, "No page matches.") || strings.Contains(body, "Make a page titled") {
			t.Errorf("q=%s: the link is drawn for search syntax, or the empty words are not", q)
		}
	}
	if form := browse(t, h, "/browse?kind=page&new=create-page&title=Zebra+crossing"); !strings.Contains(form, `name="title" value="Zebra crossing"`) {
		t.Error("the New page form does not hold the title")
	}
}

// TestTheTodayStripIsAtTheHeadOfTodayOnAPhone: the strip is drawn in the More sheet and also in the page of today, in a
// block that app.css shows on a narrow screen only. Another day does not draw it, and the entry has no autofocus.
func TestTheTodayStripIsAtTheHeadOfTodayOnAPhone(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	today := browse(t, h, "/days/"+fixedToday)
	if n := strings.Count(today, `<section class="rail-today"`); n != 2 || !strings.Contains(today, `<div class="today-inline"><section class="rail-today"`) {
		t.Errorf("the strip is drawn %d times, or not in the page of today", n)
	}
	if other := browse(t, h, "/days/2031-05-10"); strings.Count(other, `<section class="rail-today"`) != 1 || strings.Contains(other, `<div class="today-inline">`) {
		t.Error("another day draws the strip in its page")
	}
	if strings.Contains(today, `accesskey="c" autofocus`) {
		t.Error("the entry of today takes the focus: a phone opens scrolled past the date")
	}
}
