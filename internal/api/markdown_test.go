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

// Whole-document expectations catch stolen destinations, nested anchors and leftover delimiters.
func TestMarkdownLinkPrecedence(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[[Ana]](https://example.test)", `<p><a href="https://example.test">[Ana]</a></p>`},
		{"[label [[Ana]] #run](https://example.test)", `<p><a href="https://example.test">label [[Ana]] #run</a></p>`},
		{"[[Ana]][ref]\n\n[ref]: https://example.test", `<p><a href="https://example.test">[Ana]</a></p>`},
		{"![[Ana]](photo.png)", `<p><img src="photo.png" alt="[Ana]"></p>`},
		{"![[Ana]][pic]\n\n[pic]: photo.png", `<p><img src="photo.png" alt="[Ana]"></p>`},
		{`[\[Ana\] &amp; Café](u)`, `<p><a href="u">[Ana] &amp; Café</a></p>`},
		{"[[Ana *x*]]", `<p>[[Ana <em>x</em>]]</p>`},
		{"[[Ana\nPopescu]]", "<p>[[Ana\nPopescu]]</p>"},
		{"&#35;run ![[Café|A &amp; B]]", `<p><a href="/pages?title=run" class="wikilink">#run</a> <a href="/pages?title=Caf%C3%A9" class="wikilink embed"><img src="/previews?title=Caf%C3%A9" alt="A &amp; B" loading="lazy"></a></p>`},
		{"![label [[Ana]] #run](photo.png)", `<p><img src="photo.png" alt="label [[Ana]] #run"></p>`},
		{"[x](https://example.test/[[Ana]])", `<p><a href="https://example.test/%5B%5BAna%5D%5D">x</a></p>`},
		{"[[Café|A &amp; B]]", `<p><a href="/pages?title=Caf%C3%A9" class="wikilink">A &amp; B</a></p>`},
		{`\[\[Ana\]\]`, `<p><a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"&#91;&#91;Ana&#93;&#93;", `<p><a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"[[Ana|&lt;script&gt;]]", `<p><a href="/pages?title=Ana" class="wikilink">&lt;script&gt;</a></p>`},
		{"[[Café]](u) [[Ana]]", `<p><a href="u">[Café]</a> <a href="/pages?title=Ana" class="wikilink">Ana</a></p>`},
		{"[[Ana]](broken", `<p><a href="/pages?title=Ana" class="wikilink">Ana</a>(broken</p>`},
		{"[[Ana] text](u)", `<p><a href="u">[Ana] text</a></p>`},
		{"`[[Ana]]` [#run](javascript:bad)", `<p><code>[[Ana]]</code> <a href="">#run</a></p>`},
		{"[[Ana]]  \n#run", "<p><a href=\"/pages?title=Ana\" class=\"wikilink\">Ana</a><br>\n<a href=\"/pages?title=run\" class=\"wikilink\">#run</a></p>"},
	} {
		if got := strings.TrimSpace(string(markdown(c.in))); got != c.want {
			t.Errorf("%q: got %s; want %s", c.in, got, c.want)
		}
	}
}
