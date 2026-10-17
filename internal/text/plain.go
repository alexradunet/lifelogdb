package text

import (
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
)

// The plain forms that the writer and its view both read: a day and an instant (lifelog_meta.days and .instants), the
// words of a body without its CommonMark (Plain), the markers that a plain-text line cuts, and the marks of a match in a snippet of the search.

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

// embedMark is an ![[embed]] in plain text, its label with it.
var embedMark = regexp.MustCompile(`!\[\[[^\[\]\n\r]*\]\]`)

// Plain is the words of a CommonMark body as a reader sees them, on one line. It keeps the text of emphasis, strong,
// code spans and link labels. It drops images, embeds, raw HTML, autolinks, code blocks and link destinations. It
// decodes escapes and character references. A [[Title]] becomes Title and a [[Title|alias]] becomes the alias, as
// the contract reads the alias as display text. Lines and blocks are joined with one space, and the block marks of
// a heading, a quote or a list are gone with the block. It does not cut the text to a length and does not change its
// Unicode form. A body with no words gives "".
func Plain(body string) string {
	src := []byte(body)
	doc := md.Parser().Parse(gtext.NewReader(src))
	var b strings.Builder
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				b.Write(decode(c, src))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			case *ast.Image, *ast.RawHTML, *ast.AutoLink, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			default: // emphasis, a code span, a link label and the blocks: the words are in the children
				walk(c)
				if c.Type() == ast.TypeBlock {
					b.WriteByte(' ')
				}
			}
		}
	}
	walk(doc)
	words := embedMark.ReplaceAllString(b.String(), "")
	words = wiki.ReplaceAllStringFunc(words, func(link string) string {
		title, alias, found := strings.Cut(link[2:len(link)-2], "|")
		if found && strings.TrimSpace(alias) != "" {
			title = alias
		}
		return strings.TrimSpace(title)
	})
	return strings.Join(strings.Fields(words), " ")
}

// The inline marks of a fragment of CommonMark. A link or an image is a label in brackets and a destination in
// parentheses. Stars and backticks are cut wherever they are. An underscore is cut only at the edge of a word, so the
// "_" in file_name stays.
var (
	linkMark       = regexp.MustCompile(`!?\[([^\[\]]*)\]\([^()]*\)`)
	starMark       = regexp.MustCompile("[*`]+")
	underscoreOpen = regexp.MustCompile(`(^|[^\pL\pN_])_+`)
	underscoreEnd  = regexp.MustCompile(`_+($|[^\pL\pN_])`)
)

// CutInlineMarks cuts the inline CommonMark marks of a fragment of a body: the stars and underscores of emphasis, the
// backticks of code, and the destination of a link (the label stays). It is for a snippet of the search, which is a
// piece of a body. Goldmark would read a piece wrongly, so the cut is by regular expression and is not exact: use
// Plain on a whole line or body.
func CutInlineMarks(s string) string {
	s = linkMark.ReplaceAllString(s, "$1")
	s = starMark.ReplaceAllString(s, "")
	s = underscoreOpen.ReplaceAllString(s, "$1")
	return underscoreEnd.ReplaceAllString(s, "$1")
}
