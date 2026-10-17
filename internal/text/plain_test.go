package text

import (
	"testing"
	"time"
)

// TestParseDayReadsOnlyADayThatRoundTrips: ParseDay gives the time of a YYYY-MM-DD day (lifelog_meta.days), and a
// zero time and false for anything else. A string that time.Parse reads but that does not round-trip is not a day:
// "+026-10-05" is read as the year 26. IsDay agrees with ParseDay.
func TestParseDayReadsOnlyADayThatRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		s    string
		ok   bool
		want time.Time
	}{
		{"2026-10-05", true, time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)},
		{"0000-01-01", true, time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31", true, time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)},
		{"2028-02-29", true, time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)},
		{"2026-02-29", false, time.Time{}},
		{"2026-10-5", false, time.Time{}},
		{"+026-10-05", false, time.Time{}},
		{"-001-12-26", false, time.Time{}},
		{"-0001-12-26", false, time.Time{}},
		{"2026-10-05T00:00:00Z", false, time.Time{}},
		{" 2026-10-05", false, time.Time{}},
		{"", false, time.Time{}},
	} {
		got, ok := ParseDay(tc.s)
		if !got.Equal(tc.want) || ok != tc.ok || IsDay(tc.s) != tc.ok {
			t.Errorf("ParseDay(%q) = %v, %v (IsDay %v), want %v, %v", tc.s, got, ok, IsDay(tc.s), tc.want, tc.ok)
		}
	}
}

// TestPlainKeepsTheWordsAndDropsTheMarks: Plain gives the words a reader sees. It keeps the text of emphasis, strong,
// code spans and link labels. It drops images, embeds, raw HTML and link destinations. It decodes escapes and
// entities, shows a wikilink as its title or its alias, and joins lines and blocks with one space.
func TestPlainKeepsTheWordsAndDropsTheMarks(t *testing.T) {
	for _, tc := range []struct{ name, md, want string }{
		{"empty", "", ""},
		{"plain words", "Café ünïcode", "Café ünïcode"},
		{"emphasis", "Read *Piranesi* today", "Read Piranesi today"},
		{"strong and underscore", "**Very** good and _nice_", "Very good and nice"},
		{"nested", "***both*** and **a *b* c**", "both and a b c"},
		{"code span", "ran `5 km` fast", "ran 5 km fast"},
		{"link label", "see [the lake](https://x.example/a_b) now", "see the lake now"},
		{"link with a title", `see [the lake](/a "Lake") now`, "see the lake now"},
		{"autolink", "mail <a@b.example> now", "mail now"},
		{"image", "a ![a lake](lake.png) b", "a b"},
		{"raw html", "a <b>bold</b> <span>x</span> c", "a bold x c"},
		{"wikilink", "Ran with [[Sam]] #running", "Ran with Sam #running"},
		{"wikilink with alias", "Ran with [[Sam Smith|Sam]].", "Ran with Sam."},
		{"wikilink in emphasis", "Ran with *[[Sam]]*", "Ran with Sam"},
		{"wikilink in a link label", "[see [[Diet]]](http://x)", "see Diet"},
		{"embed", "![[IMG_0001.jpg]] by the lake", "by the lake"},
		{"embed with a label", "a ![[B.jpg|the lake]] b", "a b"},
		{"only an embed", "![[A.jpg]]", ""},
		{"only an image", "![alt](a.png)", ""},
		{"escapes", `a \*star\* and \[x\]`, "a *star* and [x]"},
		{"entities", "fish &amp; chips &#35;1", "fish & chips #1"},
		{"a tag stays", "#running 5k", "#running 5k"},
		{"a number stays", "-5 kg today", "-5 kg today"},
		{"a literal star stays", "5 * 3 = 15", "5 * 3 = 15"},
		{"intraword underscore stays", "see file_name.txt", "see file_name.txt"},
		{"soft and hard breaks", "one\ntwo  \nthree", "one two three"},
		{"blocks", "# Title\n\nA *para*.\n\n- item", "Title A para. item"},
		{"a thematic break", "---", ""},
		{"a code block", "```\ncode\n```", ""},
		{"spaces", "  a   b\t c  ", "a b c"},
	} {
		if got := Plain(tc.md); got != tc.want {
			t.Errorf("%s: Plain(%q) = %q, want %q", tc.name, tc.md, got, tc.want)
		}
	}
}

// TestCutInlineMarksIsForAFragment: a snippet is a piece of a body, so goldmark must not parse it. CutInlineMarks
// cuts the marks of emphasis and code and keeps the label of a link, and it leaves a mark-like sign inside a word.
func TestCutInlineMarksIsForAFragment(t *testing.T) {
	for src, want := range map[string]string{
		"":                               "",
		"plain words":                    "plain words",
		"Read *Piranesi* and **more**":   "Read Piranesi and more",
		"_nice_ and __very__ good":       "nice and very good",
		"a `code` span":                  "a code span",
		"a [link](x) and [x] in a line":  "a link and [x] in a line",
		`a [link](/x "t") b`:             "a link b",
		"an ![image](x.png) here":        "an image here",
		"see file_name.txt and snake_ok": "see file_name.txt and snake_ok",
		"_⟦soil⟧_ here":                  "⟦soil⟧ here",
		"5 * 3":                          "5  3",
		"a [[Ana]] and #tag":             "a [[Ana]] and #tag",
		"a half [link](x":                "a half [link](x",
	} {
		if got := CutInlineMarks(src); got != want {
			t.Errorf("CutInlineMarks(%q) = %q, want %q", src, got, want)
		}
	}
}
