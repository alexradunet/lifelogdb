package api

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"lifelog/internal/text"
)

// A body is shown as CommonMark with goldmark's safe defaults (raw HTML omitted, dangerous URLs dropped), and
// every [[wikilink]] and #tag that names a page links to it. Code spans and blocks are parsed first, so they are
// never linked. The links panel, not this rendering, is what the save contract wrote.
var bodyMD = goldmark.New(goldmark.WithParserOptions(parser.WithInlineParsers(
	util.Prioritized(wikilinkParser{}, 199), // before the link parser, which also starts at '['
	util.Prioritized(tagParser{}, 199),
)))

func markdown(src string) template.HTML {
	var b bytes.Buffer
	if err := bodyMD.Convert([]byte(src), &b); err != nil {
		return template.HTML("<pre>" + template.HTMLEscapeString(src) + "</pre>")
	}
	return template.HTML(b.String())
}

// titleHref opens the page a title names, through GET /pages?title= (any case or normalisation).
func titleHref(title string) string { return "/pages?title=" + url.QueryEscape(title) }

func pageLink(title, label string) ast.Node {
	l := ast.NewLink()
	l.Destination = []byte(titleHref(title))
	l.SetAttributeString("class", []byte("wikilink"))
	s := ast.NewString([]byte(label))
	s.SetRaw(true) // escaped when written, never re-read as markdown
	l.AppendChild(l, s)
	return l
}

// embedLink is ![[title]]: the picture of the file page the title names, linking the page; a title with no picture
// shows its alt text, the label.
func embedLink(title, label string) ast.Node {
	img := ast.NewImage(ast.NewLink())
	img.Destination = []byte("/previews?title=" + url.QueryEscape(title))
	img.SetAttributeString("loading", []byte("lazy"))
	alt := ast.NewString([]byte(label))
	alt.SetRaw(true)
	img.AppendChild(img, alt)
	l := ast.NewLink()
	l.Destination = []byte(titleHref(title))
	l.SetAttributeString("class", []byte("wikilink embed"))
	l.AppendChild(l, img)
	return l
}

var wikiAtStart = regexp.MustCompile(`^(!?)\[\[([^\[\]\n\r]*)\]\]`)

type wikilinkParser struct{}

func (wikilinkParser) Trigger() []byte { return []byte{'[', '!'} } // '!' before the image parser, for an embed

func (wikilinkParser) Parse(_ ast.Node, block gtext.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	m := wikiAtStart.FindSubmatch(line)
	if m == nil {
		return nil
	}
	inner := string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(m[2]))))
	title, label, piped := strings.Cut(inner, "|")
	title = strings.Trim(title, " ")
	if !text.ValidTitle(title) {
		return nil // no link is made of it: the text stays as written
	}
	if !piped || strings.TrimSpace(label) == "" {
		label = title
	}
	block.Advance(len(m[0]))
	if len(m[1]) > 0 {
		return embedLink(title, label)
	}
	return pageLink(title, label)
}

type tagParser struct{}

func (tagParser) Trigger() []byte { return []byte{'#'} }

func (tagParser) Parse(_ ast.Node, block gtext.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	tag, n := text.TagAt(block.PrecendingCharacter(), string(line))
	if n == 0 || !text.ValidTitle(tag) {
		return nil
	}
	block.Advance(n)
	return pageLink(tag, "#"+tag)
}
