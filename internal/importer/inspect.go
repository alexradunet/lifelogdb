package importer

import (
	"bytes"
	"context"
	"encoding/csv"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gtext "github.com/yuin/goldmark/text"

	"lifelog/internal/core"
)

// RegisterMetrics registers the approved metrics of a stamped metrics.md, and each habit's period re-sent with
// its end_day (cookbook/habits.md). It returns what it did, one line per metric.
func (w *Workspace) RegisterMetrics(ctx context.Context, s *core.Store) ([]string, error) {
	approved, err := w.ApprovedMetrics()
	if err != nil {
		return nil, err
	}
	if approved == nil {
		g, _ := w.Gate("metrics.md")
		return nil, refuse("metrics.md is %s: the owner approves it first (lifelog import approve metrics)", g)
	}
	src, err := w.Name()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(approved))
	for n := range approved {
		names = append(names, n)
	}
	sort.Strings(names)
	var done []string
	err = s.Do(ctx, src, func(t *core.Tx) error {
		done = nil
		for _, n := range names {
			m := approved[n]
			added, err := t.RegisterMetric(m.Name, m.Unit, m.Note)
			if err != nil {
				return refuse("%s: %v", m.Name, err)
			}
			line := m.Name + ": " + map[bool]string{true: "registered", false: "existing"}[added]
			if m.Category != "" { // the pages of its path, top first, then the metric filed in the last (D26)
				id, err := t.MetricID(m.Name)
				if err != nil {
					return refuse("%s: %v", m.Name, err)
				}
				filed, err := t.File(id, m.Category)
				if err != nil {
					return refuse("%s: %v", m.Name, err)
				}
				if filed {
					line += ", filed in " + strings.Trim(strings.TrimSpace(m.Category), "/")
				}
			}
			if m.Since != "" {
				if err := t.StartHabit(m.Name, m.Since, m.Until); err != nil {
					return refuse("%s: %v", m.Name, err)
				}
				line += ", a habit from " + m.Since
				if m.Until != "" {
					line += " to " + m.Until
				}
			}
			done = append(done, line)
		}
		return nil
	})
	return done, err
}

// Inspection is what inspect returns for one source file: its structure, never a judgement.
type Inspection struct {
	File        string       `json:"file"`
	Type        string       `json:"type"`
	Size        int          `json:"bytes"`
	Day         string       `json:"day,omitempty"`
	Frontmatter string       `json:"frontmatter,omitempty"`
	Headings    []string     `json:"headings,omitempty"`
	Tables      [][][]string `json:"tables,omitempty"`
	Checkboxes  []string     `json:"checkboxes,omitempty"`
	Links       []string     `json:"links,omitempty"`
	Rows        [][]string   `json:"rows,omitempty"`
	Text        string       `json:"text,omitempty"`
}

var (
	gfm        = goldmark.New(goldmark.WithExtensions(extension.Table, extension.TaskList))
	obsidianRE = regexp.MustCompile(`!?\[\[[^\[\]\n]*\]\]`)
)

// Inspect reads a source file's structure: frontmatter, headings, tables as rows, checkboxes and links for a
// note; rows for a CSV; the text for anything else that is text.
func (w *Workspace) Inspect(file string) (*Inspection, error) {
	src, err := w.ReadSource(file)
	if err != nil {
		return nil, err
	}
	in := &Inspection{File: file, Size: len(src), Day: fileDay(file, src)}
	lower := strings.ToLower(file)
	switch {
	case !utf8.ValidString(src) || strings.ContainsRune(src, 0):
		in.Type = "binary"
	case strings.HasSuffix(lower, ".csv"):
		in.Type = "csv"
		rows, err := csvRows(src)
		if err != nil {
			return nil, refuse("%s: %v", file, err)
		}
		in.Rows = rows
	case strings.HasSuffix(lower, ".md"):
		in.Type = "markdown"
		body := src
		if fm, rest, ok := frontmatter(src); ok {
			in.Frontmatter, body = fm, rest
		}
		b := []byte(body)
		doc := gfm.Parser().Parse(gtext.NewReader(b))
		ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n := n.(type) {
			case *ast.Heading:
				in.Headings = append(in.Headings, strings.Repeat("#", n.Level)+" "+plain(n, b))
			case *east.Table:
				in.Tables = append(in.Tables, tableRows(n, b))
				return ast.WalkSkipChildren, nil
			case *east.TaskCheckBox:
				mark := "[ ] "
				if n.IsChecked {
					mark = "[x] "
				}
				in.Checkboxes = append(in.Checkboxes, mark+strings.TrimSpace(plain(n.Parent(), b)))
			case *ast.Link:
				in.Links = append(in.Links, string(n.Destination))
			}
			return ast.WalkContinue, nil
		})
		in.Links = append(in.Links, obsidianRE.FindAllString(body, -1)...)
	default:
		in.Type = "text"
		in.Text = src
	}
	return in, nil
}

func csvRows(src string) ([][]string, error) {
	return csv.NewReader(strings.NewReader(src)).ReadAll()
}

func markdownTables(src string) [][][]string {
	b := []byte(src)
	doc := gfm.Parser().Parse(gtext.NewReader(b))
	var tables [][][]string
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n, ok := n.(*east.Table); ok {
			tables = append(tables, tableRows(n, b))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return tables
}

func tableRows(n *east.Table, src []byte) [][]string {
	var rows [][]string
	for r := n.FirstChild(); r != nil; r = r.NextSibling() {
		var cells []string
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, plain(c, src))
		}
		rows = append(rows, cells)
	}
	return rows
}

// frontmatter splits a leading --- block from a note.
func frontmatter(src string) (fm, rest string, ok bool) {
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return "", src, false
	}
	i := strings.Index(src[3:], "\n---")
	if i < 0 {
		return "", src, false
	}
	end := 3 + i + len("\n---")
	if nl := strings.IndexByte(src[end:], '\n'); nl >= 0 {
		return strings.TrimSpace(src[4 : 3+i]), src[end+nl+1:], true
	}
	return strings.TrimSpace(src[4 : 3+i]), "", true
}

// plain is a node's text, raw as written in the source (inline markup kept).
func plain(n ast.Node, src []byte) string {
	var buf bytes.Buffer
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				buf.Write(c.Segment.Value(src))
				if c.SoftLineBreak() {
					buf.WriteByte(' ')
				}
			case *ast.String:
				buf.Write(c.Value)
			case *east.TaskCheckBox:
			default:
				if c.Kind() == ast.KindCodeSpan {
					buf.WriteByte('`')
					walk(c)
					buf.WriteByte('`')
					continue
				}
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(buf.String())
}
