package api

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/text/unicode/norm"

	"lifelog/internal/text"
)

// CommonMark owns links and images before eligible plain text is enhanced. The links panel,
// not this rendering, is what the save contract wrote.
var bodyMD = goldmark.New()

func markdown(src string) template.HTML {
	var b bytes.Buffer
	source := []byte(src)
	doc := bodyMD.Parser().Parse(gtext.NewReader(source))
	enhanceText(doc, source)
	if err := bodyMD.Renderer().Render(&b, source, doc); err != nil {
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

// enhanceText joins adjacent text segments before decoding, but never crosses an inline
// container or line break. Existing anchors, images, code and HTML are left to CommonMark.
func enhanceText(parent ast.Node, source []byte) {
	for c := parent.FirstChild(); c != nil; {
		switch c.(type) {
		case *ast.Link, *ast.Image, *ast.AutoLink, *ast.CodeSpan, *ast.RawHTML,
			*ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			c = c.NextSibling()
		case *ast.Text:
			var raw bytes.Buffer
			first := c
			var last *ast.Text
			for c != nil {
				t, ok := c.(*ast.Text)
				if !ok {
					break
				}
				raw.Write(t.Segment.Value(source))
				last = t
				c = c.NextSibling()
				if t.SoftLineBreak() || t.HardLineBreak() {
					break
				}
			}
			decoded := util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(raw.Bytes())))
			for _, node := range enhancedRun(norm.NFC.String(string(decoded))) {
				parent.InsertBefore(parent, first, node)
			}
			if last.SoftLineBreak() || last.HardLineBreak() {
				br := ast.NewTextSegment(gtext.NewSegment(last.Segment.Stop, last.Segment.Stop))
				br.SetSoftLineBreak(last.SoftLineBreak())
				br.SetHardLineBreak(last.HardLineBreak())
				parent.InsertBefore(parent, first, br)
			}
			for first != c {
				next := first.NextSibling()
				parent.RemoveChild(parent, first)
				first = next
			}
		default:
			enhanceText(c, source)
			c = c.NextSibling()
		}
	}
}

func enhancedRun(run string) []ast.Node {
	var nodes []ast.Node
	start := 0
	plain := func(end int) {
		if end > start {
			s := ast.NewString([]byte(run[start:end]))
			s.SetRaw(true)
			nodes = append(nodes, s)
		}
	}
	for i := 0; i < len(run); {
		var node ast.Node
		n := 0
		if run[i] == '[' || run[i] == '!' {
			if m := wikiAtStart.FindStringSubmatch(run[i:]); m != nil {
				title, label, piped := strings.Cut(m[2], "|")
				title = strings.Trim(title, " ")
				if text.ValidTitle(title) {
					if !piped || strings.TrimSpace(label) == "" {
						label = title
					}
					if m[1] != "" {
						node = embedLink(title, label)
					} else {
						node = pageLink(title, label)
					}
					n = len(m[0])
				}
			}
		} else if run[i] == '#' {
			prev := rune(10)
			if i > 0 {
				prev, _ = utf8.DecodeLastRuneInString(run[:i])
			}
			tag, size := text.TagAt(prev, run[i:])
			if size > 0 && text.ValidTitle(tag) {
				node = pageLink(tag, "#"+tag)
				n = size
			}
		}
		if node != nil {
			plain(i)
			nodes = append(nodes, node)
			i += n
			start = i
		} else {
			_, size := utf8.DecodeRuneInString(run[i:])
			i += size
		}
	}
	plain(len(run))
	return nodes
}
