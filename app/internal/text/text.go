// Package text computes what every writer must compute identically: the title predicate, title_key, and the
// wikilinks and #tags a body names. The rules and their vectors live in docs/contract/titles-and-wikilinks.md;
// this package implements them and states none of its own.
package text

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/unicode/rangetable"
)

// AssignedVersion is the Unicode version whose unassigned code points (Cn) a title may not contain. It is the
// reference implementation's version (Python 3.12), not the newer tables Go ships, so this writer never accepts
// a title whose fold the reference cannot compute.
const AssignedVersion = "15.0.0"

var (
	assigned = rangetable.Assigned(AssignedVersion)
	fold     = cases.Fold()
	md       = goldmark.New() // CommonMark, no extensions
	wiki     = regexp.MustCompile(`\[\[([^\[\]\n\r]*)\]\]`)
	devices  = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true}
	invisible = map[rune]bool{0xAD: true, 0x61C: true, 0x200B: true, 0x200E: true, 0x200F: true, 0xFEFF: true}
)

func init() {
	for i := '1'; i <= '9'; i++ {
		devices["COM"+string(i)] = true
		devices["LPT"+string(i)] = true
	}
	for r := rune(0x202A); r <= 0x202E; r++ {
		invisible[r] = true
	}
	for r := rune(0x2060); r <= 0x2069; r++ {
		invisible[r] = true
	}
}

// TitleKey is title_key = NFC(casefold(NFC(title))).
func TitleKey(t string) string {
	return norm.NFC.String(fold.String(norm.NFC.String(t)))
}

// ValidTitle is the DDL's title CHECKs (pages_title_len, pages_title_safe) plus the writer's own Cn rule.
func ValidTitle(t string) bool {
	if t == "" || t != strings.Trim(t, " ") || len(t) > 240 {
		return false
	}
	if strings.ContainsAny(t, `/\:*?"<>|`) {
		return false
	}
	for _, r := range t {
		if r <= 31 || (r >= 127 && r <= 159) || invisible[r] || !unicode.Is(assigned, r) {
			return false
		}
	}
	if t[0] == '.' || t[len(t)-1] == '.' {
		return false
	}
	base, _, _ := strings.Cut(t, ".")
	return !devices[asciiUpper(base)]
}

func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return r
	}, s)
}

// IsStub reports a #REDIRECT rename stub: #REDIRECT (any case) after optional whitespace, then whitespace, then [[.
func IsStub(body string) bool {
	s := strings.TrimLeftFunc(body, unicode.IsSpace)
	const word = "#redirect"
	if len(s) < len(word) || !strings.EqualFold(s[:len(word)], word) {
		return false
	}
	rest := s[len(word):]
	after := strings.TrimLeftFunc(rest, unicode.IsSpace)
	return len(after) < len(rest) && strings.HasPrefix(after, "[[")
}

// Candidates returns the titles a body names, in order of appearance, valid or not and not de-duplicated.
func Candidates(body string) []string {
	if IsStub(body) {
		return nil
	}
	var out []string
	for _, run := range textRuns(body) {
		type hit struct {
			pos   int
			title string
		}
		var found []hit
		blanked := []rune(run)
		for _, m := range wiki.FindAllStringSubmatchIndex(run, -1) {
			title, _, _ := strings.Cut(run[m[2]:m[3]], "|")
			found = append(found, hit{m[0], strings.Trim(title, " ")})
		}
		// blank the wikilinks (by rune position) so their text is not re-read for tags
		runeStarts := runeIndex(run)
		for _, m := range wiki.FindAllStringIndex(run, -1) {
			for i := runeStarts[m[0]]; i < runeStarts[m[1]]; i++ {
				blanked[i] = ' '
			}
		}
		for _, t := range scanTags(blanked) {
			if allDigits(t.tag) || strings.ToLower(t.tag) == "redirect" {
				continue
			}
			found = append(found, hit{runeToByte(run, t.pos), t.tag})
		}
		// stable sort by position (insertion sort; runs are short)
		for i := 1; i < len(found); i++ {
			for j := i; j > 0 && found[j].pos < found[j-1].pos; j-- {
				found[j], found[j-1] = found[j-1], found[j]
			}
		}
		for _, h := range found {
			out = append(out, h.title)
		}
	}
	return out
}

// Targets returns the distinct valid targets as (key, first valid spelling) pairs in order, skipping ownKey,
// and the rejected titles.
func Targets(body, ownKey string) (keys, titles, rejected []string) {
	seen := map[string]bool{}
	for _, t := range Candidates(body) {
		if !ValidTitle(t) {
			rejected = append(rejected, t)
			continue
		}
		k := TitleKey(t)
		if k == ownKey || seen[k] {
			continue
		}
		seen[k] = true
		keys, titles = append(keys, k), append(titles, t)
	}
	return
}

func tagChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || r == '_'
}

type tag struct {
	pos int
	tag string
}

func scanTags(run []rune) []tag {
	var out []tag
	n := len(run)
	for i := 0; i < n; {
		if run[i] == '#' && (i == 0 || !(tagChar(run[i-1]) || run[i-1] == '/' || run[i-1] == '#')) {
			j := i + 1
			for {
				k := j
				for k < n && tagChar(run[k]) {
					k++
				}
				j = k
				if j+1 < n && run[j] == '-' && tagChar(run[j+1]) && k > i+1 {
					j++
					continue
				}
				break
			}
			if j > i+1 {
				out = append(out, tag{i, string(run[i+1 : j])})
			}
			i = max(j, i+1)
		} else {
			i++
		}
	}
	return out
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Nd, r) {
			return false
		}
	}
	return true
}

func runeIndex(s string) map[int]int {
	m := map[int]int{}
	i := 0
	for b := range s {
		m[b] = i
		i++
	}
	m[len(s)] = i
	return m
}

func runeToByte(s string, pos int) int {
	i := 0
	for b := range s {
		if i == pos {
			return b
		}
		i++
	}
	return len(s)
}

// textRuns returns the CommonMark text of a body as runs of plain text, each NFC-normalised. A run is a maximal
// sequence of adjacent text nodes inside one inline container, broken at line breaks and at any other inline
// (emphasis, code span, link, raw HTML). Code spans, raw HTML, link destinations and image alt text are not read.
func textRuns(body string) []string {
	src := []byte(body)
	doc := md.Parser().Parse(gtext.NewReader(src))
	var runs []string
	var cur bytes.Buffer
	open := false
	flush := func() {
		if open {
			runs = append(runs, norm.NFC.String(cur.String()))
			cur.Reset()
			open = false
		}
	}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				cur.Write(decode(c, src))
				open = true
				if c.SoftLineBreak() || c.HardLineBreak() {
					flush()
				}
			case *ast.String:
				cur.Write(c.Value)
				open = true
			case *ast.CodeSpan, *ast.Image, *ast.RawHTML, *ast.AutoLink,
				*ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
				flush()
			default:
				flush()
				walk(c)
				flush()
			}
		}
	}
	walk(doc)
	flush()
	return runs
}

// decode is the text a text node stands for: backslash escapes and character references resolved.
func decode(t *ast.Text, src []byte) []byte {
	v := t.Segment.Value(src)
	if t.IsRaw() {
		return v
	}
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}
