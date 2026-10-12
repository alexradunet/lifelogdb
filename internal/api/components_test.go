package api

import (
	"strings"
	"testing"
)

// TestSegmentedIsForAFewShortOptions: a field is drawn as a row of radio buttons only when it has 2 to 5 options,
// none empty and each of 3 characters or fewer; any other field with options is a select.
func TestSegmentedIsForAFewShortOptions(t *testing.T) {
	for _, tc := range []struct {
		options []string
		want    bool
	}{
		{[]string{"1", "2", "3", "4", "5"}, true},
		{[]string{"1", "0"}, true},
		{[]string{"yes", "no"}, true},
		{[]string{"1"}, false},
		{[]string{"1", "2", "3", "4", "5", "6"}, false},
		{[]string{"", "0", "1"}, false},
		{[]string{"open", "done", "skipped"}, false},
		{nil, false},
	} {
		if got := segmented(Field{Name: "f", Type: "text", Options: tc.options}); got != tc.want {
			t.Errorf("segmented(%q) = %v, want %v", tc.options, got, tc.want)
		}
	}
}

// TestAnErrorCodePicksItsWords: the first words and the tone of each error code of API.md.
func TestAnErrorCodePicksItsWords(t *testing.T) {
	for code, want := range map[string]alertView{
		"stale_version":  {"Not saved. The page changed since you opened it.", false},
		"exists":         {"Not created.", false},
		"forbidden":      {"Refused.", true},
		"cross_origin":   {"Refused.", true},
		"token_required": {"Refused.", true},
		"unauthorized":   {"Refused.", true},
		"too_large":      {"Not done.", true},
		"timeout":        {"Not done.", true},
		"internal":       {"Not done.", true},
		"invalid":        {},
		"bad_request":    {},
		"not_found":      {},
		"conflict":       {},
	} {
		if got := alertOf(code); got != want {
			t.Errorf("alertOf(%q) = %+v, want %+v", code, got, want)
		}
	}
	for _, words := range []string{alertOf("stale_version").Words, alertOf("forbidden").Words} {
		if !strings.HasSuffix(words, ".") {
			t.Errorf("%q is not a sentence", words)
		}
	}
}
