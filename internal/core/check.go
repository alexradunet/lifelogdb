package core

import "lifelog/internal/text"

// The checks of a surface's input that every write and read shares, so one refusal has one message.

// checkDay refuses a value that is not a local day (lifelog_meta.days); what names the field.
func checkDay(what, s string) error {
	if !IsDay(s) {
		return Invalid("%s %q is not a day (YYYY-MM-DD)", what, s)
	}
	return nil
}

// checkWindow refuses a window that is not two local days in order, both inclusive.
func checkWindow(from, through string) error {
	if !IsDay(from) || !IsDay(through) || from > through {
		return Invalid("from %q and through %q must be days (YYYY-MM-DD), from first", from, through)
	}
	return nil
}

// checkTitle refuses a title the contract does not admit (docs/contract/titles-and-wikilinks.md); what names the field.
func checkTitle(what, title string) error {
	if !text.ValidTitle(title) {
		return Invalid("%s %q is not a valid title (docs/contract/titles-and-wikilinks.md)", what, title)
	}
	return nil
}

// zoneLabel reports a time-zone label as the contract admits it: 1 to 64 characters of A-Z, a-z, 0-9, _, /, + and -.
func zoneLabel(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '/' || r == '+' || r == '-') {
			return false
		}
	}
	return true
}
