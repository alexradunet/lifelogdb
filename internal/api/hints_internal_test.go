package api

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheFormsHintsAreInPlainWords: each action that a person meets in a form has a hint. A hint holds no status
// code and no code style, which are the words of the API text (Description).
func TestTheFormsHintsAreInPlainWords(t *testing.T) {
	names := strings.Fields(`plan tick carry schedule record correct retract tombstone-pages rename add-file capture link
		unlink file-pages set-cover promote edit-person edit-metric create-person create-place create-page register-metric`)
	status := regexp.MustCompile(`\b[1-5][0-9][0-9]\b`)
	for _, n := range names {
		h := specOf(n).Hint
		if h == "" {
			t.Errorf("%s has no hint", n)
		}
		if strings.Contains(h, "`") || status.MatchString(h) {
			t.Errorf("%s: the hint %q holds a status code or code style", n, h)
		}
	}
}
