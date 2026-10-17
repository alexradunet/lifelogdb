package text

import (
	"regexp"
	"time"
)

// The plain forms that the writer and its view both read: a day and an instant (lifelog_meta.days and .instants), the
// markers that a plain-text line cuts, and the marks of a match in a snippet of the search.

// ParseDay parses a YYYY-MM-DD local day that round-trips (date(x) IS x). For anything else ok is false and the
// time is zero.
func ParseDay(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || t.Format(time.DateOnly) != s {
		return time.Time{}, false
	}
	return t, true
}

// IsDay reports a YYYY-MM-DD local day that round-trips (date(x) IS x).
func IsDay(s string) bool {
	_, ok := ParseDay(s)
	return ok
}

// instantLayout is the layout of a lifelog_meta.instants instant: UTC, milliseconds, Z.
const instantLayout = "2006-01-02T15:04:05.000Z"

// IsInstant reports a lifelog_meta.instants instant.
func IsInstant(s string) bool {
	t, err := time.Parse(instantLayout, s)
	return err == nil && t.Format(instantLayout) == s
}

// BlockMark is the markers at the start of a line that a plain-text view cuts: headings, quotes, list items, check
// boxes ([ ] and [x]). A marker is cut only when a space follows it, so the "#" of a tag and the "-" of a number stay.
var BlockMark = regexp.MustCompile(`^(?:#{1,6}\s+|>\s*|[-*+]\s+)*(?:\[[ xX]\]\s+)?`)

// SnippetOpen and SnippetClose mark a match in a core.Hit's snippet. They are not brackets, because a body's own text
// holds [[wikilinks]] and check boxes, and a mark must never be confused with the text.
const (
	SnippetOpen  = "⟦"
	SnippetClose = "⟧"
)
