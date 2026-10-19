package view

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api/siren"
)

// TestNavMarksThePageTheBrowserIsOn says one navigation link is current: the one whose path is the page's, else the
// one with the longest path the page is under.
func TestNavMarksThePageTheBrowserIsOn(t *testing.T) {
	for path, want := range map[string]string{
		"/":                "/",
		"/days":            "/days",
		"/days/2026-01-01": "/days",
		"/days/today":      "/days/today",
		"/months/2026-10":  "/days",
		"/years/2026":      "/days",
		"/integrity":       "/integrity",
		"/browse":          "/browse",
		"/metrics":         "/metrics",
		"/metrics/TSH":     "/metrics",
		"/pages/7":         "",
		"/search":          "",
		"/measurements/1":  "",
	} {
		var got string
		for _, it := range (&view{Path: path}).Nav() {
			if it.Current {
				if got != "" {
					t.Errorf("%s marks both %s and %s current", path, got, it.Href)
				}
				got = it.Href
			}
		}
		if got != want {
			t.Errorf("%s marks %q current, want %q", path, got, want)
		}
	}
}

// TestMoreIsMarkedOnAPageOfTheOwner: More is marked on a page of the owner's links at its foot, and on no other page.
// Today's day page marks Today, by the today of the view's words.
func TestMoreIsMarkedOnAPageOfTheOwner(t *testing.T) {
	shell := &Shell{Today: "2026-10-16", Zone: time.UTC}
	for path, want := range map[string]bool{
		"/integrity": true, "/actions": true, "/": false, "/days/2026-10-16": false, "/metrics": false, "/pages/7": false,
	} {
		if got := (&view{Path: path, Shell: shell}).InMore(); got != want {
			t.Errorf("InMore on %s = %v, want %v", path, got, want)
		}
	}
	for _, c := range []struct {
		shell *Shell
		want  string
	}{{shell, "/days/today"}, {nil, "/days"}, {&Shell{Today: "2026-10-15"}, "/days"}} {
		var got string
		for _, it := range (&view{Path: "/days/2026-10-16", Shell: c.shell}).Nav() {
			if it.Current {
				got = it.Href
			}
		}
		if got != c.want {
			t.Errorf("the shell %+v marks %q on /days/2026-10-16, want %q", c.shell, got, c.want)
		}
	}
}

// drawRail draws a page of a shell and returns its rail.
func drawRail(t *testing.T, shell *Shell) string {
	t.Helper()
	u, _ := url.Parse("/anything")
	b, err := Draw(&siren.Entity{Class: []string{"unknown"}, Title: "A page"}, Facts{URL: u, Shell: shell})
	if err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(`(?s)<aside class="rail">.*?</aside>`).FindString(string(b))
}

func ptrBool(b bool) *bool { return &b }

// TestTheRailDrawsTheTodayStrip: two things done and two open: the line counts the done ones, every chip says its
// state, and an open chip leads to the form that does the thing (entry and mood: the capture of today; a habit: the
// reading form of Home, with the habit and the value 1 in the query).
func TestTheRailDrawsTheTodayStrip(t *testing.T) {
	rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC, Strip: &Strip{
		Written: true, Mood: false, Streak: 12,
		Habits: []Habit{{"Walk", nil}, {"No phone", ptrBool(true)}, {"Run & jump", nil}, {"Stretch", ptrBool(false)}},
	}})
	if rail == "" {
		t.Fatal("no rail")
	}
	strip := regexp.MustCompile(`(?s)<section class="rail-today".*?</section>`).FindString(rail)
	if strip == "" {
		t.Fatalf("no strip in the rail:\n%s", rail)
	}
	if !strings.Contains(strip, "2 of 6 done") { // entry and one habit done; the entry, the mood and four habits are counted
		t.Errorf("the line does not say 2 of 6 done:\n%s", strip)
	}
	for _, want := range []string{
		`<a class="chip done" href="/days/today#capture">✓ entry</a>`,
		`<a class="chip" href="/days/today#capture">mood</a>`,
		`<a class="chip" href="/?new=record&amp;metric=Walk&amp;value=1#record">Walk</a>`,
		`<span class="chip done">✓ No phone</span>`,
		`<a class="chip" href="/?new=record&amp;metric=Run&#43;%26&#43;jump&amp;value=1#record">Run &amp; jump</a>`,
		`<span class="chip">Stretch · no</span>`,
		`12 days in a row`,
	} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip lacks %s:\n%s", want, strip)
		}
	}
	// the strip is inside the sheet of More, before the owner's links, so a phone keeps it out of the bottom bar
	more := rail[strings.Index(rail, `<div class="rail-more"`):]
	if i, j := strings.Index(more, `class="rail-today"`), strings.Index(more, `class="rail-foot"`); i < 0 || j < i {
		t.Errorf("the strip is not in More before the owner's links:\n%s", more)
	}
}

// TestTheStripSaysTwoOfTwoAndHidesAShortStreak: the count is over the entry, the mood and the habits; a streak of 0
// or 1 draws no line.
func TestTheStripSaysTwoOfTwoAndHidesAShortStreak(t *testing.T) {
	for _, streak := range []int{0, 1} {
		rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC, Strip: &Strip{Written: true, Mood: true, Streak: streak}})
		if !strings.Contains(rail, "2 of 2 done") || strings.Contains(rail, "in a row") {
			t.Errorf("streak %d:\n%s", streak, rail)
		}
	}
}

// TestNoStripWithoutState: a shell with no state draws the rail with no strip, and a page with no shell no rail.
func TestNoStripWithoutState(t *testing.T) {
	rail := drawRail(t, &Shell{Today: "2026-10-19", Zone: time.UTC})
	if rail == "" || strings.Contains(rail, "rail-today") {
		t.Errorf("a shell with no state:\n%s", rail)
	}
	if rail := drawRail(t, nil); rail != "" {
		t.Errorf("a page with no shell has a rail:\n%s", rail)
	}
}
