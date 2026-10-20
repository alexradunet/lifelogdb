package view

import (
	"slices"
	"strings"
	"testing"
)

// TestAResultIsSaidInSentences: each result shape of API.md in sentences; a key no sentence knows as a fact; a title
// with markup escaped.
func TestAResultIsSaidInSentences(t *testing.T) {
	c := clock{today: "2026-10-12"}
	say := func(m map[string]any) string {
		var out []string
		for _, l := range resultSentences(m, c, "") {
			out = append(out, string(l.HTML))
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
				`Its own day is <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>. | Shown on its day page.`},
		{"add-file of a photo with no day, kept before", map[string]any{"id": 7.0, "existing": true, "stored_day": "2026-10-11"},
			`Kept the file: <a href="/pages/7">its page</a>. | The same original was kept before. | ` +
				`Its page keeps the day <time datetime="2026-10-11">yesterday · Sun 11 Oct</time>.`},
		{"a capture sent again", map[string]any{"already": true}, "Already added. Nothing changed."},
		{"snapshot", map[string]any{"snapshot": "snaps/life-2026-10-12.db", "restore_check": "ok"},
			`Took a snapshot: <code>snaps/life-2026-10-12.db</code>. | <dl class="facts"><dt>restore check</dt><dd>ok</dd></dl>`},
		{"plan", map[string]any{"page_id": 4.0, "line": 3.0, "written": "- [ ] Call"}, `Planned on line 3 of <a href="/pages/4">its page</a>.`},
		// A line nested two levels deep is an indented code block when it is read alone: the message reads the dates of
		// the result, not the written line.
		{"schedule of a nested line", map[string]any{"page_id": 4.0, "line": 3.0, "written": "    - [ ] deep on:2026-10-13", "on": "2026-10-13", "due": ""},
			`Scheduled line 3 of <a href="/pages/4">its page</a>: on Tue 13 Oct.`},
		{"schedule with no date left", map[string]any{"page_id": 4.0, "line": 3.0, "written": "- [ ] Call", "on": "", "due": ""},
			`Scheduled line 3 of <a href="/pages/4">its page</a>: no dates.`},
		{"tick done", map[string]any{"line": 2.0, "outcome": "done"}, "Done."},
		{"tick dropped", map[string]any{"line": 2.0, "outcome": "dropped"}, "Dropped."},
		{"tick with the next line", map[string]any{"line": 2.0, "outcome": "done", "appended": "- [ ] Pay due:2026-12-01", "appended_line": 5.0}, "Done. | The next one is line 5."},
		{"tick reopen", map[string]any{"line": 2.0, "outcome": "reopen", "text": "Pay", "page_id": 4.0}, "Reopened."},
		{"tick reopen removed the next", map[string]any{"line": 2.0, "outcome": "reopen", "text": "Pay", "page_id": 4.0, "removed": "- [ ] Pay", "removed_line": 5.0}, "Reopened. | The next one, line 5, is removed."},
		{"tick reopen kept the next", map[string]any{"line": 2.0, "outcome": "reopen", "text": "Pay", "page_id": 4.0, "next_kept": true}, "Reopened. | The next one was changed since, so it stays."},
		{"carry", map[string]any{"line": 2.0, "to_id": 9.0, "to_line": 1.0}, `Carried to <a href="/pages/9">its page</a>, line 1.`},
		{"unknown", map[string]any{"foo_bar": 1.0}, `<dl class="facts"><dt>foo bar</dt><dd>1</dd></dl>`},
	} {
		if got := say(tc.in); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// TestATickOffersItsUndo: the line of a done or dropped tick carries one form that posts a reopen of the same line to
// its page and returns to the page the toast is on. Without the page or the words there is no form.
func TestATickOffersItsUndo(t *testing.T) {
	m := map[string]any{"line": 2.0, "outcome": "dropped", "text": "Pay <rent>", "page_id": 4.0}
	lines := resultSentences(m, clock{today: "2026-10-12"}, "/days/2026-10-12")
	if len(lines) != 1 || lines[0].Form == nil {
		t.Fatalf("lines %+v", lines)
	}
	want := resultForm{Href: "/pages/4/tick", Label: "Undo",
		Hidden: [][2]string{{"back", "/days/2026-10-12"}, {"line", "2"}, {"text", "Pay <rent>"}, {"outcome", "reopen"}}}
	if got := *lines[0].Form; got.Href != want.Href || got.Label != want.Label || !slices.Equal(got.Hidden, want.Hidden) {
		t.Errorf("form %+v, want %+v", got, want)
	}
	for _, l := range resultSentences(map[string]any{"line": 2.0, "outcome": "done"}, clock{}, "/") {
		if l.Form != nil {
			t.Errorf("a form without a page: %+v", l)
		}
	}
	if l := resultSentences(map[string]any{"line": 2.0, "outcome": "reopen", "text": "x", "page_id": 4.0}, clock{}, "/"); l[0].Form != nil {
		t.Errorf("a reopen offers no undo: %+v", l)
	}
}
