package siren

import "testing"

// TestBrowseHrefs: the addresses of the Browse lists, with the query keys in the order that url.Values.Encode gives.
func TestBrowseHrefs(t *testing.T) {
	for _, tc := range []struct{ name, got, want string }{
		{"no filter", BrowseHref(nil), "/browse"},
		{"a kind", BrowseKindHref("person"), "/browse?kind=person"},
		{"the ghosts", GhostsHref(), "/browse?ghosts=1&kind=page"},
		{"a page with its form open", BrowseNewHref("page"), "/browse?kind=page&new=create-page"},
		{"a file with its form open", BrowseNewHref("media"), "/browse?kind=media&new=add-file"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, tc.got, tc.want)
		}
	}
	if got := NewActionOf("metric"); got != "" {
		t.Errorf("a metric is not made on its Browse list, but NewActionOf says %q", got)
	}
}

func TestNewMetricHref(t *testing.T) {
	for _, c := range []struct{ name, got, want string }{
		{"no name", NewMetricHref(""), "/metrics?new=register-metric"},
		{"a name", NewMetricHref("Steps"), "/metrics?name=Steps&new=register-metric"},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
}
