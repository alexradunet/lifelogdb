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
