package api

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"text/template/parse"
	"time"
)

// The classes of a page and of a sheet: a class attribute's words, and a class selector's name. sheetComments cuts the
// comments of a sheet, so the test reads its rules and not its prose.
var (
	classAttr     = regexp.MustCompile(`class="([^"]*)"`)
	cssSelector   = regexp.MustCompile(`\.([a-zA-Z_][\w-]*)`)
	sheetComments = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// calledTemplates are the names of the templates that the tree n calls, at any depth.
func calledTemplates(n parse.Node, called map[string]bool) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			calledTemplates(c, called)
		}
	case *parse.TemplateNode:
		called[n.Name] = true
	case *parse.IfNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	case *parse.RangeNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	case *parse.WithNode:
		calledTemplates(n.List, called)
		calledTemplates(n.ElseList, called)
	}
}

// TestTheStylePageCallsEveryComponent: the style page calls the template of each component that has one. What each
// draws is TestTheStylePageRunsTheRealTemplates's, in package api, which has the synthetic entity of the page.
func TestTheStylePageCallsEveryComponent(t *testing.T) {
	called := map[string]bool{}
	calledTemplates(templates.Lookup("styleguide").Tree.Root, called)
	for _, name := range []string{"leaf", "mood", "step", "form", "boxed", "body-editor", "onthisday", "reading-link",
		"add-reading", "metric-cards", "weekdays", "monthbox", "readings", "pager"} {
		if !called[name] {
			t.Errorf("the style page does not call the template %s", name)
		}
	}
}

// TestTheStylePageRunsTheRealTemplates: the components of the style page draw the synthetic entity of the page
// (styleEntity) with the real templates: the mark of each one, checked by hand, is in the page, and so is each
// component that a page draws inline. The page draws no class that base.css or app.css does not define, but num: the
// readings template marks the column of the values with it, and no sheet styles it. The page is drawn with a shell
// whose today is Friday 16 October 2026, in UTC. That the page calls each component is
// TestTheStylePageCallsEveryComponent's, in the tests of the view.
func TestTheStylePageRunsTheRealTemplates(t *testing.T) {
	u, err := url.Parse("/styleguide")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Draw(styleEntity(), Facts{URL: u, Shell: &Shell{Today: "2026-10-16", Zone: time.UTC}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	body := page[strings.LastIndex(page, "</style>"):] // the markup after the two sheets
	for _, c := range []struct{ what, want string }{
		{"leaf", `<a class="leaf" href="/months/2026-10" aria-label="Oct 2026, the calendar of the month"><span class="leaf-month">Oct 2026</span><span class="leaf-day">12</span></a>`},
		{"mood", `<p class="specimen-title">Monday <span class="mood" title="Mood">mood 4</span></p>`},
		{"step", `<a class="button step" accesskey="[" href="/styleguide" rel="prev" aria-label="Previous day, Sun 11 Oct"><span aria-hidden="true">‹</span><span class="step-day">Sun 11 Oct</span></a>`},
		{"form of capture", `<textarea name="text" rows="4" placeholder="What happened? Who was there?"></textarea>`},
		{"form, a segmented field", `<fieldset class="segmented"><legend>Mood 1-5</legend>`},
		{"form, a select", `<select name="kind"><option></option><option>page</option>`},
		{"form, a flag", `<label><input type="checkbox" name="unfiled" value="1"><span>Only what is in no category</span></label>`},
		{"form, folded fields", `<details class="more-fields"><summary>3 more fields</summary>`},
		{"boxed", `<details class="action"><summary class="button">New person</summary>`},
		{"form, a field of page titles", `<input type="text" name="category" value="" list="create-person-category"><datalist id="create-person-category"><option value="Bloodwork"><option value="Sport"></datalist>`},
		{"body-editor", "<details><summary>Edit the day page</summary>\n<small class=\"desc\">CommonMark."},
		{"body-editor, its form", `<textarea name="body" rows="18" required>A walk in Lisbon</textarea>`},
		{"onthisday", `<a class="entry" href="/days/2025-10-12"><span class="date">Sun 12 Oct 2025</span><span class="first">A walk with Sam</span><span class="meta">mood 4</span></a>`},
		{"a tile of a day", `<div class="readings"><div class="reading-tile"><a class="name" href="/styleguide">weight</a>`},
		{"reading-link of a tile", `<a class="reading" href="/measurements/1">71 <span class="unit">kg</span></a>`},
		{"the trend of a tile", `aria-label="weight from 71.4 kg Tue 6 Oct to 71 kg 4 days ago · Mon 12 Oct"`},
		{"the more of a tile", `<span class="meta">and 1 more this day</span>`},
		{"add-reading", `<summary class="button small" aria-label="Add a reading of weight">+ Add</summary>`},
		{"a section of metrics", `<h2>Bloodwork <small>3</small></h2><span class="meta">newest Thu 1 Oct</span>`},
		{"metric-cards, a card", `<a class="reading" href="/measurements/14">38 <span class="unit">ng/mL</span></a>`},
		{"metric-cards, a stale card", `<div class="reading-tile metric-card stale">`},
		{"metric-cards, a card with no reading", `<span class="reading none"><span aria-hidden="true">–</span> <span class="unit">uIU/mL</span></span>`},
		{"weekdays", `<th scope="col" abbr="Monday">Mon</th>`},
		{"monthbox, today", `<a href="/days/2026-10-13" aria-current="date" aria-label="Tue 13 Oct">13</a>`},
		{"monthbox, a written day", `<a href="/days/2026-10-12" class="written" aria-label="Mon 12 Oct, written">12</a>`},
		{"monthbox, its count", `<span class="meta">3 written</span>`},
		{"the chart", `<svg class="chart" viewBox="0 0 640 200" role="img" aria-label="4 readings from Mon 20 Oct 2025 to Mon 12 Oct 2026">`},
		{"a fold of a year", `<details class="readings-year" open><summary><span class="year-name">2026</span> <span class="meta">3 readings · 70 to 71.5 kg</span></summary>`},
		{"readings", `<td class="num"><a href="/measurements/34">71</a></td><td><time datetime="2026-10-12T06:48:00.000Z" title="2026-10-12T06:48:00.000Z">Mon 12 Oct, 06:48</time></td>`},
		{"pager", `<nav class="pager" aria-label="Pages"><a href="/styleguide" rel="prev">← Previous</a><a href="/styleguide" rel="next">Next →</a></nav>`},
		{"the facts", `<dl class="facts"><dt>created at</dt>`},
		{"the journal", `<div class="prose journal-text"><h3>A walk in Lisbon</h3>`},
		{"the sheet and its margin", `<div class="sheet-margin">`},
		{"the margin", `<div class="margin">`},
		{"capture", `<section class="capture" aria-label="Capture">`},
		{"the loud button", `<button class="primary">The loud button</button>`},
		{"a danger", `<button class="danger">A danger</button>`},
		{"the wood slab of New", `<a class="button primary rail-new" href="/styleguide">New</a>`},
		{"a badge", `<span class="badge">edited</span>`},
		{"the owner's mark", `<span class="owner">owner</span>`},
		{"an alert", `<div class="alert danger">`},
		{"the toast", `<dialog open closedby="closerequest" class="toast" aria-label="Result">`},
		{"the search dialog", `<dialog class="palette" open aria-label="Search">`},
		{"the calendar", `<table class="calendar">`},
		{"the month", `<table class="month">`},
		{"add a reading", `<section class="record" aria-label="Add a reading">`},
		{"the empty state", `<p class="empty">`},
	} {
		if !strings.Contains(body, c.want) {
			t.Errorf("the style page does not show %s: %s", c.what, c.want)
		}
	}

	defined := map[string]bool{}
	for _, m := range cssSelector.FindAllStringSubmatch(sheetComments.ReplaceAllString(string(MustFile("base.css"))+string(MustFile("app.css")), ""), -1) {
		defined[m[1]] = true
	}
	if defined["num"] {
		t.Error("a sheet defines the class num now: it is no longer an exception here")
	}
	drawn := map[string]bool{}
	for _, m := range classAttr.FindAllStringSubmatch(body, -1) {
		for _, c := range strings.Fields(m[1]) {
			if !defined[c] && c != "num" && !drawn[c] {
				t.Errorf("the style page draws the class %s, which no sheet defines", c)
			}
			drawn[c] = true
		}
	}
}
