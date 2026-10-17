package view

import (
	"testing"
	"time"
)

// TestNavMarksThePageTheBrowserIsOn says one navigation link is current: the one whose path is the page's, else the
// one with the longest path the page is under.
func TestNavMarksThePageTheBrowserIsOn(t *testing.T) {
	for path, want := range map[string]string{
		"/":                "/",
		"/days":            "/months/current",
		"/days/2026-01-01": "/months/current",
		"/days/today":      "/days/today",
		"/months/2026-10":  "/months/current",
		"/weeks/2026-W41":  "/months/current",
		"/years/2026":      "/months/current",
		"/integrity":       "/integrity",
		"/browse":          "/browse",
		"/metrics":         "/metrics",
		"/metrics/TSH":     "/metrics",
		"/pages/7":         "",
		"/search":          "",
		"/import/prepared": "",
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
	}{{shell, "/days/today"}, {nil, "/months/current"}, {&Shell{Today: "2026-10-15"}, "/months/current"}} {
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
