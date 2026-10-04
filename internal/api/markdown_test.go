package api

import (
	"strings"
	"testing"
)

func TestMarkdownLinksWhatNamesAPage(t *testing.T) {
	for _, c := range []struct{ in, want, not string }{
		{"with [[Ana]]", `<a href="/pages?title=Ana" class="wikilink">Ana</a>`, ""},
		{"[[Ana Popescu|Ana]]", `<a href="/pages?title=Ana+Popescu" class="wikilink">Ana</a>`, ""},
		{"[[Café Lume]]", `href="/pages?title=Caf%C3%A9+Lume"`, ""},
		{"`[[Ana]]`", "<code>[[Ana]]</code>", "wikilink"},
		{"[[a/b]]", "[[a/b]]", "wikilink"},
		{"ran #run-club", `<a href="/pages?title=run-club" class="wikilink">#run-club</a>`, ""},
		{"a#b #123 #redirect", "a#b #123 #redirect", "wikilink"},
		{"[[<b>x]]", "", "<b>"},
		{"<script>alert(1)</script>", "raw HTML omitted", "<script>"},
		{"[x](javascript:alert(1))", `<a href="">x</a>`, "javascript"},
		{"# Heading\n\n*em* **strong**", "<h1>Heading</h1>", ""},
		{"see ![[Lake.jpg|the lake]] here", `<a href="/pages?title=Lake.jpg" class="wikilink embed"><img src="/previews?title=Lake.jpg" alt="the lake" loading="lazy"></a>`, "!"},
		{"![[Lake.jpg]]", `alt="Lake.jpg"`, ""},
		{"![[a/b]] ![x](u.png)", `![[a/b]] <img src="u.png" alt="x">`, "embed"},
	} {
		got := string(markdown(c.in))
		if !strings.Contains(got, c.want) || (c.not != "" && strings.Contains(got, c.not)) {
			t.Errorf("%q renders %q", c.in, got)
		}
	}
}
