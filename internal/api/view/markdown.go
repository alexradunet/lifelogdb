package view

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strconv"
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

func markdown(src string) template.HTML { return markdownCover(src, "", "", "") }

// markdownCover is markdown with the page's cover drawn apart, above the body (docs/plans/127-a-cover-for-every-page.md):
// the first embed that names the cover (embed, its title as the body writes it) is left out, and so is its paragraph
// when nothing else is in it. An empty embed leaves out nothing.
//
// tickHref is the address of the tick action of the page the body belongs to, and back the page the browser returns to
// after it: they turn the open plan lines of the body into boxes that tick. An empty tickHref draws every box without
// a form (a deleted page, the preview, a search snippet, a card of the stream).
func markdownCover(src, embed, tickHref, back string) template.HTML {
	var b bytes.Buffer
	source := []byte(src)
	doc := bodyMD.Parser().Parse(gtext.NewReader(source))
	forms := enhancePlans(doc, source, tickHref, back)
	enhanceText(doc, source)
	if embed != "" {
		leaveOutEmbed(doc, source, text.TitleKey(embed))
	}
	if err := bodyMD.Renderer().Render(&b, source, doc); err != nil {
		return template.HTML("<pre>" + template.HTMLEscapeString(src) + "</pre>")
	}
	if forms != "" {
		b.WriteString("<div hidden>" + forms + "</div>")
	}
	return template.HTML(b.String())
}

// inline draws the words of a plan line in a list as one inline run: CommonMark emphasis, [[Title]], ![[embed]] and
// #tag as the body draws them. A line has no block structure, so the one paragraph around the run is cut.
func inline(words string) template.HTML {
	html := string(markdownCover(words, "", "", ""))
	html = strings.TrimSpace(html)
	if strings.HasPrefix(html, "<p>") && strings.HasSuffix(html, "</p>") && strings.Count(html, "<p>") == 1 {
		html = html[3 : len(html)-4]
	}
	return template.HTML(html)
}

// A plan line in a body (docs/contract/plan-lines.md, which is the one home of the grammar; text.Plans reads it). Its
// box becomes a glyph, and the tokens (on:, due:, done:, every:, from:) become small words, so the words read first.
// The box of an open line is a button of a tick form, and so is the box of a done or dropped line (it reopens). The form cannot stand inside the item: in a loose list the item
// holds a paragraph, and a form would end it. So the forms stand together after the body, and each button names its
// form (form="tick-N", N the line).
var boxGlyph = map[byte][2]string{' ': {"open", "\u2610"}, 'x': {"done", "\u2611"}, '-': {"dropped", "\u2612"}, '>': {"carried", "\u25b7"}}

// span is a range of the source that is drawn as html instead.
type span struct {
	start, end int
	html       string
}

// enhancePlans replaces the box and the tokens of each plan line in doc, and returns the tick forms that the open,
// done and dropped lines need ("" when tickHref is empty or no line ticks).
func enhancePlans(doc ast.Node, source []byte, tickHref, back string) string {
	plans := text.Plans(string(source))
	if len(plans) == 0 {
		return ""
	}
	byBox := make(map[int]text.Plan, len(plans))
	for _, p := range plans {
		byBox[p.BoxAt] = p
	}
	esc := template.HTMLEscapeString
	var forms strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		item, ok := n.(*ast.ListItem)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		switch item.FirstChild().(type) {
		case *ast.TextBlock, *ast.Paragraph:
		default:
			return ast.WalkContinue, nil
		}
		block := item.FirstChild()
		if block.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		p, ok := byBox[block.Lines().At(0).Start]
		if !ok {
			return ast.WalkContinue, nil
		}
		word, glyph := boxGlyph[p.Box][0], boxGlyph[p.Box][1]
		box := `<span class="box ` + word + `" role="img" aria-label="` + word + `">` + glyph + `</span>`
		// An open box ticks done. A done or dropped box reopens its line. A carried box stays a glyph.
		outcome, verb := "done", "Done"
		if p.Box == 'x' || p.Box == '-' {
			outcome, verb = "reopen", "Reopen"
		}
		if p.Box != '>' && p.Bad == "" && p.Text != "" && tickHref != "" {
			id := "tick-" + strconv.Itoa(p.Line)
			box = `<button class="box ` + word + `" form="` + id + `" name="outcome" value="` + outcome + `" aria-label="` + verb + `: ` + esc(p.Text) + `">` + glyph + `</button>`
			forms.WriteString(`<form id="` + id + `" method="post" action="` + esc(tickHref) + `">` +
				`<input type="hidden" name="back" value="` + esc(back) + `">` +
				`<input type="hidden" name="line" value="` + strconv.Itoa(p.Line) + `">` +
				`<input type="hidden" name="text" value="` + esc(p.Text) + `"></form>`)
		}
		spans := []span{{p.BoxAt, p.BoxAt + 3, box}}
		for _, t := range p.Tokens {
			spans = append(spans, span{t[0], t[1], tokenChip(string(source[t[0]:t[1]]))})
		}
		replaceSpans(block, spans)
		return ast.WalkSkipChildren, nil
	})
	return forms.String()
}

// tokenChip draws a token of a well-formed plan line (on:, due:, done:, every:, from:done) as the badge that the plan
// rows use (planrow in plan.html): "on Tue 20 Oct", "every week". The days are in the words of shortDay, which needs no
// clock. The grammar has been checked already, so the key and the value are known.
func tokenChip(token string) string {
	key, value, _ := strings.Cut(token, ":")
	words := key + " " + value
	switch key {
	case "on", "due", "done":
		words = key + " " + shortDay(value)
	case "from":
		words = "from " + value
	}
	return `<span class="badge">` + template.HTMLEscapeString(words) + `</span>`
}

// replaceSpans draws the spans of the source as html in the text nodes of a block. A span may cover parts of several
// nodes (goldmark splits "[ ]" at the brackets), and html is written where the span starts. The spans are in order.
func replaceSpans(block ast.Node, spans []span) {
	for c := block.FirstChild(); c != nil; {
		next := c.NextSibling()
		if t, ok := c.(*ast.Text); ok {
			if pieces := splitText(t, spans); pieces != nil {
				for _, n := range pieces {
					block.InsertBefore(block, c, n)
				}
				block.RemoveChild(block, c)
			}
		}
		c = next
	}
}

// splitText is the nodes that replace t when a span overlaps it, and nil when none does. The line break of t stays
// on the last node.
func splitText(t *ast.Text, spans []span) []ast.Node {
	a, b := t.Segment.Start, t.Segment.Stop
	out := []ast.Node{} // empty and not nil when a span covers all of t: t goes away
	cursor, hit := a, false
	piece := func(from, to int) {
		if to > from {
			out = append(out, ast.NewTextSegment(gtext.NewSegment(from, to)))
		}
	}
	for _, s := range spans {
		if s.end <= a || s.start >= b {
			continue
		}
		hit = true
		piece(cursor, s.start)
		if s.start >= a {
			h := ast.NewString([]byte(s.html))
			h.SetCode(true) // written as it is: the html was escaped when it was built
			out = append(out, h)
		}
		cursor = min(max(cursor, s.end), b)
	}
	if !hit {
		return nil
	}
	piece(cursor, b)
	if t.SoftLineBreak() || t.HardLineBreak() {
		var last *ast.Text
		if len(out) > 0 {
			last, _ = out[len(out)-1].(*ast.Text)
		}
		if last == nil {
			last = ast.NewTextSegment(gtext.NewSegment(b, b))
			out = append(out, last)
		}
		last.SetSoftLineBreak(t.SoftLineBreak())
		last.SetHardLineBreak(t.HardLineBreak())
	}
	return out
}

// leaveOutEmbed removes the first embed whose title has the key, in the order of the text, and its paragraph when
// nothing but blanks and line breaks is left in it.
func leaveOutEmbed(doc ast.Node, source []byte, key string) {
	var found ast.Node
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found != nil {
			return ast.WalkContinue, nil
		}
		if l, ok := n.(*ast.Link); ok {
			if c, ok := l.Attribute([]byte("class")); ok && string(c.([]byte)) == "wikilink embed" {
				if title, ok := embedTitle(l); ok && text.TitleKey(title) == key {
					found = l
					return ast.WalkStop, nil
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if found == nil {
		return
	}
	para := found.Parent()
	para.RemoveChild(para, found)
	if _, ok := para.(*ast.Paragraph); !ok {
		return
	}
	for c := para.FirstChild(); c != nil; c = c.NextSibling() {
		if s, ok := c.(*ast.String); ok && strings.TrimSpace(string(s.Value)) == "" {
			continue
		}
		if t, ok := c.(*ast.Text); ok && strings.TrimSpace(string(t.Segment.Value(source))) == "" {
			continue
		}
		return
	}
	para.Parent().RemoveChild(para.Parent(), para)
}

// embedTitle is the title of the file an embed link of embedLink draws, read back from its destination.
func embedTitle(l *ast.Link) (string, bool) {
	title, ok := strings.CutPrefix(string(l.Destination), "/pages?title=")
	if !ok {
		return "", false
	}
	title, err := url.QueryUnescape(title)
	return title, err == nil
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
// It decodes the joined run, not each node as internal/text does: after a hard line break, goldmark can put the
// backslash of an escape and the next character in two nodes, and only the joined run decodes that escape.
func enhanceText(parent ast.Node, source []byte) {
	for c := parent.FirstChild(); c != nil; {
		switch node := c.(type) {
		case *ast.Link, *ast.Image, *ast.AutoLink, *ast.CodeSpan, *ast.RawHTML,
			*ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			c = c.NextSibling()
		case *ast.Text:
			var raw bytes.Buffer
			first := c
			var last *ast.Text
			for t := node; t != nil; t, _ = c.(*ast.Text) {
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

// mark is a search snippet as plain text with its matched words marked: the search puts text.SnippetOpen and
// text.SnippetClose around them. The text is escaped first, so a snippet can never carry HTML of its own.
func mark(snippet string) template.HTML {
	return template.HTML(markRE.ReplaceAllString(template.HTMLEscapeString(plainSnippet(snippet)), "<mark>$1</mark>"))
}

// plainSnippet is the words of a snippet without its CommonMark, on one line: the marker of a heading, a quote, a
// list item or a task box at the start of a line is cut, the brackets of a wikilink or an embed, and the inline marks
// (text.CutInlineMarks; a snippet is a fragment, so goldmark does not read it). It is for a list of hits, not a
// rendering; the JSON keeps the text as it is.
func plainSnippet(s string) string {
	var words []string
	for line := range strings.SplitSeq(s, "\n") {
		line = text.BlockMark.ReplaceAllString(strings.TrimSpace(line), "")
		if line != "" {
			words = append(words, line)
		}
	}
	return strings.NewReplacer("![[", "", "[[", "", "]]", "").Replace(text.CutInlineMarks(strings.Join(words, " ")))
}

var markRE = regexp.MustCompile(text.SnippetOpen + `([^` + text.SnippetOpen + text.SnippetClose + `]+)` + text.SnippetClose)
