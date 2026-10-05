package api

import (
	"lifelog/internal/text"
	"strings"
	"testing"
)

func TestMarkdownPostParseNFC(t *testing.T) {
	for _, n := range []int{100, 118, 119} {
		title := strings.Repeat("é", n) + ".jpg"
		for _, spelling := range []string{title, strings.Repeat("e\u0301", n) + ".jpg"} {
			body := "![[" + spelling + "|&lt;label&gt;&amp;]]"
			got := string(markdown(body))
			keys, _, _ := text.Targets(body, "")
			want := len(title) <= 240
			if strings.Contains(got, `class="wikilink embed"`) != want || text.HasEmbed(body, title) != want || (len(keys) == 1) != want {
				t.Errorf("n=%d: %s", n, got)
			}
			if want && (!strings.Contains(got, `/previews?title=`+strings.TrimPrefix(titleHref(title), "/pages?title=")) || !strings.Contains(got, `alt="&lt;label&gt;&amp;"`)) {
				t.Errorf("target/escaping: %s", got)
			}
		}
	}
	tag := strings.Repeat("e\u0301", 100)
	if got := string(markdown("#" + tag)); !strings.Contains(got, `class="wikilink"`) {
		t.Errorf("tag: %s", got)
	}
	if got := string(markdown("\u1fef![[Cafe\u0301.jpg]]\u1fef")); !strings.Contains(got, `class="wikilink embed"`) || strings.Contains(got, "<code>") {
		t.Errorf("post-parse: %s", got)
	}
}
