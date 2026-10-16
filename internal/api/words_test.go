package api

import (
	"html/template"
	"strings"
	"testing"
	"time"
)

// TestADayIsADate: a day in words against today, with the year only when it is not this year.
func TestADayIsADate(t *testing.T) {
	c := clock{today: "2026-10-12"}
	for in, want := range map[string]string{
		"2026-10-05": "Mon 5 Oct",
		"2025-12-31": "Wed 31 Dec 2025",
		"2027-01-01": "Fri 1 Jan 2027",
		"not a day":  "not a day",
		"2026-02-30": "2026-02-30",
	} {
		if got := c.day(in); got != want {
			t.Errorf("day(%q) = %q, want %q", in, got, want)
		}
	}
	if got := (clock{}).day("2026-10-05"); got != "Mon 5 Oct 2026" {
		t.Errorf("with no today: %q", got)
	}
}

// TestAnInstantIsATimeInTheServersZone: an instant in the clock of the server's day; with no zone, in UTC.
func TestAnInstantIsATimeInTheServersZone(t *testing.T) {
	utc := clock{today: "2026-10-12"}
	bucharest := clock{today: "2026-10-12", zone: time.FixedZone("EEST", 3*3600)}
	for _, c := range []struct {
		c        clock
		in, want string
	}{
		{utc, "2026-10-12T06:48:45.484Z", "today, 06:48 UTC"},
		{utc, "2026-10-11T21:14:03.000Z", "yesterday, 21:14 UTC"},
		{utc, "2026-10-01T06:00:00.000Z", "Thu 1 Oct, 06:00 UTC"},
		{utc, "2025-06-09T21:14:03.482Z", "Mon 9 Jun 2025, 21:14 UTC"},
		{bucharest, "2026-10-12T06:48:45.484Z", "today, 09:48"},
		{bucharest, "2026-10-11T21:14:03.000Z", "today, 00:14"}, // the server's day, not UTC's
		{bucharest, "2026-10-10T22:00:00.000Z", "yesterday, 01:00"},
		{utc, "2020-01-01T23:00:00.000", "2020-01-01T23:00:00.000"}, // a local time with no zone is not an instant
		{utc, "soon", "soon"},
	} {
		if got := c.c.instant(c.in); got != c.want {
			t.Errorf("instant(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestATimeKeepsItsExactValue: the words are inside a <time> element that carries the value as stored.
func TestATimeKeepsItsExactValue(t *testing.T) {
	c := clock{today: "2026-10-12"}
	for in, want := range map[string]template.HTML{
		"2026-10-12T06:48:45.484Z": `<time datetime="2026-10-12T06:48:45.484Z" title="2026-10-12T06:48:45.484Z">today, 06:48 UTC</time>`,
		"2026-10-11":               `<time datetime="2026-10-11">yesterday · Sun 11 Oct</time>`,
		"<b>bold</b>":              `&lt;b&gt;bold&lt;/b&gt;`,
	} {
		if got := c.timeHTML(in); got != want {
			t.Errorf("timeHTML(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestAValueIsDrawnByItsType: the generic view's object as facts with its names in words, a list of objects as a
// table, and every value by its type, never by its name.
func TestAValueIsDrawnByItsType(t *testing.T) {
	c := clock{today: "2026-10-12"}
	var sb strings.Builder
	c.writeValue(&sb, map[string]any{
		"start_utc": "2026-10-12T06:00:00.000Z", "day": "2026-10-05", "open": true, "closed": false, "end_utc": nil,
		"edit_version": 1000000.0, "members": []any{map[string]any{"page_id": 3.0, "kind_deleted_at": "2026-10-11T21:14:03.000Z"}},
		"tags": []any{"a", "b"}, "note": "<script>",
	})
	got := sb.String()
	for _, want := range []string{
		`<dl class="facts"><dt>closed</dt><dd>no</dd><dt>day</dt><dd><time datetime="2026-10-05">7 days ago · Mon 5 Oct</time></dd>`,
		`<dt>end utc</dt><dd><small>—</small></dd>`,
		`<th scope="col">kind deleted at</th><th scope="col">page id</th>`,
		`<td><time datetime="2026-10-11T21:14:03.000Z" title="2026-10-11T21:14:03.000Z">yesterday, 21:14 UTC</time></td><td>3</td>`,
		`<dt>note</dt><dd>&lt;script&gt;</dd>`, `<dt>open</dt><dd>yes</dd>`,
		`<dt>start utc</dt><dd><time datetime="2026-10-12T06:00:00.000Z" title="2026-10-12T06:00:00.000Z">today, 06:00 UTC</time></dd>`,
		`<dt>tags</dt><dd><ul><li>a</li><li>b</li></ul></dd>`, `<dt>edit version</dt><dd>1000000</dd>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the value lacks %s:\n%s", want, got)
		}
	}
}

// TestAResultIsSaidInSentences: each result shape of API.md in sentences; a key no sentence knows as a fact; a title
// with markup escaped.
func TestAResultIsSaidInSentences(t *testing.T) {
	c := clock{today: "2026-10-12"}
	say := func(m map[string]any) string {
		var out []string
		for _, l := range resultSentences(m, c) {
			out = append(out, string(l))
		}
		return strings.Join(out, " | ")
	}
	for _, tc := range []struct {
		name string
		in   map[string]any
		want string
	}{
		{"sync", map[string]any{"linked": []any{"B", "C", "A"}, "added": []any{"B", "A"}, "created": []any{"<b>"}, "revived": []any{"D"}, "skipped": []any{"x/y"}},
			`Linked <a href="/pages?title=A">A</a> and <a href="/pages?title=B">B</a>. | Created <a href="/pages?title=%3Cb%3E">&lt;b&gt;</a>. | ` +
				`Revived <a href="/pages?title=D">D</a>. | Not linked, not a valid title: <code>[[x/y]]</code>.`},
		{"nothing to say", map[string]any{"linked": []any{}}, "Done."},
		{"no link added", map[string]any{"linked": []any{"A", "B"}}, "Done."},
		{"add-file", map[string]any{"id": 7.0, "existing": true, "day": "2026-10-11",
			"embedded": true, "sync": map[string]any{"linked": []any{"A"}, "added": []any{"A"}}, "day_sync": map[string]any{"revived": []any{"B"}}},
			`In its page: Linked <a href="/pages?title=A">A</a>. | In its day page: Revived <a href="/pages?title=B">B</a>. | ` +
				`Kept the file: <a href="/pages/7">its page</a>. | The same original was kept before. | ` +
				`Filed under <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>. | Shown on its day page.`},
		{"add-file filed in a new category", map[string]any{"id": 7.0, "filed": map[string]any{"category": map[string]any{"id": 9.0, "title": "Papers", "path": ""}, "created": true, "added": 1.0, "moved": 0.0}},
			`Kept the file: <a href="/pages/7">its page</a>. | Put in the new category <a href="/pages?title=Papers">Papers</a>.`},
		{"add-file filed", map[string]any{"filed": map[string]any{"category": map[string]any{"title": "Papers"}, "created": false, "added": 1.0, "moved": 0.0}},
			`Put in the category <a href="/pages?title=Papers">Papers</a>.`},
		{"add-file filed before", map[string]any{"filed": map[string]any{"category": map[string]any{"title": "Papers"}, "created": false, "added": 0.0, "moved": 0.0}},
			`It was in the category <a href="/pages?title=Papers">Papers</a> already.`},
		{"snapshot", map[string]any{"snapshot": "snaps/life-2026-10-12.db", "restore_check": "ok"},
			`Took a snapshot: <code>snaps/life-2026-10-12.db</code>. | <dl class="facts"><dt>restore check</dt><dd>ok</dd></dl>`},
		{"unknown", map[string]any{"foo_bar": 1.0}, `<dl class="facts"><dt>foo bar</dt><dd>1</dd></dl>`},
	} {
		if got := say(tc.in); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// TestCountSaysTheNumberAndTheFormOfItsNoun: count writes the number in full and the noun in the form for that
// number, for a JSON number and for a length, with the words of each site that calls it.
func TestCountSaysTheNumberAndTheFormOfItsNoun(t *testing.T) {
	for _, c := range []struct {
		n         any
		one, many string
		want      string
	}{
		{0.0, "reading", "readings", "0 readings"},
		{1.0, "reading", "readings", "1 reading"},
		{2.0, "reading", "readings", "2 readings"},
		{1234567.0, "reading", "readings", "1234567 readings"},
		{1e6, "day", "days", "1000000 days"},
		{0.5, "day", "days", "0.5 days"},
		{0, "more field", "more fields", "0 more fields"},
		{1, "more field", "more fields", "1 more field"},
		{2, "more field", "more fields", "2 more fields"},
		{1.0, "more page", "more pages", "1 more page"},
		{2.0, "more page", "more pages", "2 more pages"},
		{1.0, "day", "days", "1 day"},
		{1.0, "year", "years", "1 year"},
		{36.0, "year", "years", "36 years"},
		{1.0, "page", "pages", "1 page"},
		{1.0, "picture", "pictures", "1 picture"},
		{nil, "reading", "readings", "0 readings"},
		{"1", "reading", "readings", "0 readings"},
	} {
		if got := count(c.n, c.one, c.many); got != c.want {
			t.Errorf("count(%#v, %q, %q) = %q, want %q", c.n, c.one, c.many, got, c.want)
		}
	}
}

// TestTheSitesOfCountSayZeroTheirOwnWay: a site that calls count says 0 in its own words, and every other number
// with count.
func TestTheSitesOfCountSayZeroTheirOwnWay(t *testing.T) {
	v := &view{Shell: &shellView{Today: "2026-10-16", Zone: time.UTC}}
	cell := func(kind string, years float64) string { // a calendar item as its cell and its note say it
		w := calItem(map[string]any{"kind": kind, "years": years})
		return strings.TrimSpace(w.Word + " " + w.Note)
	}
	for _, c := range []struct {
		n                                      float64
		media, mentions, birthday, death, year string
	}{
		{0, "no pictures", "not mentioned yet", "born", "died", "0 readings"},
		{1, "1 picture", "mentioned on 1 page, last Thu 1 Oct", "birthday 1 year", "died 1 year ago", "1 reading · 70 kg"},
		{2, "2 pictures", "mentioned on 2 pages, last Thu 1 Oct", "birthday 2 years", "died 2 years ago", "2 readings · 70 to 71 kg"},
		{1234567, "1234567 pictures", "mentioned on 1234567 pages, last Thu 1 Oct", "birthday 1234567 years", "died 1234567 years ago", ""},
	} {
		if got := v.MediaCount(c.n, "picture", "pictures"); got != c.media {
			t.Errorf("MediaCount(%g) = %q, want %q", c.n, got, c.media)
		}
		if got := v.Mentions(c.n, "2026-10-01"); got != c.mentions {
			t.Errorf("Mentions(%g) = %q, want %q", c.n, got, c.mentions)
		}
		if got := cell("birthday", c.n); got != c.birthday {
			t.Errorf("a birthday of %g years: %q, want %q", c.n, got, c.birthday)
		}
		if got := cell("death", c.n); got != c.death {
			t.Errorf("a death %g years ago: %q, want %q", c.n, got, c.death)
		}
		if c.year == "" {
			continue
		}
		rows := make([]map[string]any, int(c.n))
		for i := range rows {
			rows[i] = map[string]any{"value": float64(70 + i)}
		}
		if got := yearSummary(rows, "kg"); got != c.year {
			t.Errorf("the summary of a year of %g readings = %q, want %q", c.n, got, c.year)
		}
	}
}
