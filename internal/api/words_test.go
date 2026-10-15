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
		"task_version": 1000000.0, "members": []any{map[string]any{"page_id": 3.0, "kind_deleted_at": "2026-10-11T21:14:03.000Z"}},
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
		`<dt>tags</dt><dd><ul><li>a</li><li>b</li></ul></dd>`, `<dt>task version</dt><dd>1000000</dd>`,
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
		{"add-file", map[string]any{"id": 7.0, "existing": true, "day": "2026-10-11", "place": "Lakeside", "linked": true, "point_set": true,
			"embedded": true, "sync": map[string]any{"linked": []any{"A"}, "added": []any{"A"}}, "day_sync": map[string]any{"revived": []any{"B"}}},
			`In its page: Linked <a href="/pages?title=A">A</a>. | In its day page: Revived <a href="/pages?title=B">B</a>. | ` +
				`Kept the file: <a href="/pages/7">its page</a>. | The same original was kept before. | ` +
				`Filed under <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>. | At <a href="/pages?title=Lakeside">Lakeside</a>. | ` +
				`Its day is linked to the place. | The place took this position as its point. | Shown on its day page.`},
		{"unmatched", map[string]any{"unmatched": map[string]any{"lat": 41.1496, "lon": -8.611, "map": "https://example.org/m?a=1&b=2"}},
			`Near no known place: 41.1496, -8.611 (<a href="https://example.org/m?a=1&amp;b=2" rel="noreferrer">map</a>).`},
		{"snapshot", map[string]any{"snapshot": "snaps/life-2026-10-12.db", "restore_check": "ok"},
			`Took a snapshot: <code>snaps/life-2026-10-12.db</code>. | <dl class="facts"><dt>restore check</dt><dd>ok</dd></dl>`},
		{"unknown", map[string]any{"foo_bar": 1.0}, `<dl class="facts"><dt>foo bar</dt><dd>1</dd></dl>`},
	} {
		if got := say(tc.in); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
