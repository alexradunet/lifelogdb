package api_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestThePeriodsAreAList: the recorded periods are drawn as the people and the places are, a row each with its
// boundaries in words, and not as the generic view's properties; an empty list names the way forward.
func TestThePeriodsAreAList(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	if body := browse(t, h, "/periods"); !strings.Contains(body, `<p class="empty">No period yet. New period makes one.</p>`) {
		t.Error("an empty list of periods does not say how to make one")
	}
	root := must(c.Get(t.Context(), "/"))
	cluj := must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Living in Cluj", "start_boundary": "2019-09", "end_boundary": "2023-06"}))
	must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Childhood"}))
	must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Since the move", "start_boundary": "2024-03"}))
	body := browse(t, h, "/periods")
	for _, want := range []string{
		`<a class="entry named" href="` + hrefOf(cluj, "self") + `"><span class="first">Living in Cluj</span><span class="meta">2019-09 to 2023-06</span></a>`,
		`<span class="first">Childhood</span><span class="meta">no boundaries</span>`,
		`<span class="first">Since the move</span><span class="meta">2024-03 to unknown</span>`,
		`<p class="sub">3 on this page</p>`, `>New period</summary>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the periods lack %s", want)
		}
	}
	if strings.Contains(body, `aria-label="Properties"`) {
		t.Error("the periods are drawn by the generic view")
	}
}

// TestTheSessionsAreAList: the recorded sessions are a row each, as the periods are: the reporting day, the kind, the
// length in words or the time basis when there is no length, and under them the evidence of each endpoint as it was
// recorded (an instant in words with its supplied offset, a local clock as it was given, a zone claim marked unverified) and when the session
// or its kind was deleted. Nothing is converted and nothing is left out (D22).
func TestTheSessionsAreAList(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	if body := browse(t, h, "/sessions"); !strings.Contains(body, `<p class="empty">No session yet. New session makes one.</p>`) {
		t.Error("an empty list of sessions does not say how to make one")
	}
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Run"}))
	must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Run", "day": "2031-05-13",
		"start_at": "2031-05-13T16:30:00.000Z", "end_at": "2031-05-13T17:45:00.000Z", "start_offset": "+03:00"}))
	must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Run", "day": "2031-05-12",
		"start_local": "2031-05-11T23:10:00.000", "end_local": "2031-05-12T07:05:00.000", "start_zone_unverified": "Claimed/Zone"}))
	open := must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Run", "day": "2031-05-14", "start_at": "2031-05-14T05:40:00.000Z"}))
	must(c.Do(t.Context(), find(open, "tombstone-session"), nil))

	body := browse(t, h, "/sessions")
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`<a class="entry" href="/sessions/\d+"><span class="date">Tue 13 May</span><span class="first">Run</span><span class="meta">1 h 15 min</span>` +
			`<small class="note"><time datetime="2031-05-13T16:30:00.000Z"[^>]*>[^<]+</time> \(offset &#43;03:00\) to <time datetime="2031-05-13T17:45:00.000Z"[^>]*>[^<]+</time></small></a>`),
		regexp.MustCompile(`<span class="date">Mon 12 May</span><span class="first">Run</span><span class="meta">unresolved</span>` +
			`<small class="note">local 2031-05-11T23:10:00.000 Claimed/Zone \(unverified\) to local 2031-05-12T07:05:00.000</small>`),
		regexp.MustCompile(`<p class="sub">2 on this page<a href="/sessions\?include_deleted=1">Show deleted sessions</a></p>`),
		regexp.MustCompile(`>New session</summary>`),
	} {
		if !want.MatchString(body) {
			t.Errorf("the sessions lack %s", want)
		}
	}
	if strings.Contains(body, `aria-label="Properties"`) || strings.Contains(body, `href="`+hrefOf(open, "self")+`"`) {
		t.Error("the sessions are drawn by the generic view, or show a deleted one")
	}
	all := browse(t, h, "/sessions?include_deleted=1")
	deleted := regexp.MustCompile(`<span class="date">Wed 14 May</span><span class="first">Run <span class="tag deleted">deleted</span></span><span class="meta">missing end</span>` +
		`<small class="note"><time datetime="2031-05-14T05:40:00.000Z"[^>]*>[^<]+</time> to no end · deleted at <time datetime=`)
	if !deleted.MatchString(all) || !strings.Contains(all, `<a href="/sessions">Hide deleted sessions</a>`) {
		t.Error("the deleted session is not shown with its state when asked for")
	}
}

// TestASessionPageIsItsFacts: a session's own page has the head of the other pages (its kind as the title, its day
// and its kind linked), its facts in words on a card, and Edit and Delete in the head, as a task's occurrence has.
func TestASessionPageIsItsFacts(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	kind := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Run"}))
	s := must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Run", "day": "2031-05-13",
		"start_at": "2031-05-13T16:30:00.000Z", "end_at": "2031-05-13T17:45:00.000Z"}))
	body := browse(t, h, hrefOf(s, "self"))
	for _, want := range []string{
		`<span class="kind">session</span><h1>Run</h1>`,
		`<a href="/days/2031-05-13">Tue 13 May</a>`, `<a href="` + hrefOf(kind, "self") + `">Run</a>`, `<a href="/sessions">Every session</a>`,
		`<dt>length</dt><dd>1 h 15 min</dd>`, `<dt>time basis</dt><dd>known UTC interval</dd>`,
		`>Edit session</summary>`, `>Delete session</summary>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the session's page lacks %s", want)
		}
	}
	if strings.Contains(body, `aria-label="Properties"`) || strings.Contains(body, "kind id") {
		t.Error("the session's page is drawn by the generic view")
	}
}

// TestTheGhostPagesSayWhatTheyAre: the ghost pages say what a ghost page is under their title, and an empty list says
// why it is empty, not how to make a page.
func TestTheGhostPagesSayWhatTheyAre(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/ghosts")
	for _, want := range []string{`<p class="sub">Empty pages, older than 30 days, that nothing links to or from.</p>`,
		`<p class="empty">No ghost page: every page has text or a link.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the ghost pages lack %s", want)
		}
	}
}

// TestAReadingsFactsAreOnACard: a reading's page puts its facts on a card, as an occurrence's and a session's do.
func TestAReadingsFactsAreOnACard(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	r := must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.4"}))
	if body := browse(t, h, hrefOf(r, "self")); !strings.Contains(body, `<div class="sheet list"><dl class="facts">`) {
		t.Error("a reading's facts are not on a card")
	}
}
