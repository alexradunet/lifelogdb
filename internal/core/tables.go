package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gtext "github.com/yuin/goldmark/text"
	"golang.org/x/text/unicode/norm"

	"lifelog/internal/text"
)

// TableSource is the source of the readings a page's table gives: one for every surface, so a run from any of them
// finds the readings an earlier run wrote (docs/plans/089-no-import-process.md).
const TableSource = "import:table"

// TableReadings is what readings-from-table did with one page's table.
type TableReadings struct {
	PageID   int64      `json:"page_id"`
	Page     string     `json:"page"`
	Metric   string     `json:"metric"`
	Unit     string     `json:"unit"`
	Column   string     `json:"column"`
	Written  int        `json:"written"`
	Existing int        `json:"existing"`
	Reported []TableRow `json:"reported"`
}

// TableRow is a row of the table that gave no reading, and why.
type TableRow struct {
	Row    int    `json:"row"` // 1 is the first row under the header
	Day    string `json:"day,omitempty"`
	Cell   string `json:"cell"`
	Reason string `json:"reason"`
}

// ReadingsFromTable turns the table of a page into readings of a registered metric: each row with a day and a plain
// number is one reading of that day. The table is the page's first with a column of days; its value column is the
// only other column (a "unit" column aside), or the one column names. The unit comes from the cell, a "unit" column
// or the header, in ( ) or [ ], and must be the metric's: it is checked, never converted. A row with a value that is
// not a plain number (<5, a word, a comma decimal), with another unit, or with a second value for one day is
// reported, not written. A reading's key is the page and the day, so a second run writes nothing.
func (s *Store) ReadingsFromTable(ctx context.Context, page, metric, column string) (*TableReadings, error) {
	res := &TableReadings{Metric: metric, Reported: []TableRow{}}
	err := s.Do(ctx, TableSource, func(t *Tx) error {
		res.Written, res.Existing, res.Reported = 0, 0, []TableRow{}
		p, err := t.Lookup(page)
		if err != nil {
			return err
		}
		if p == nil || p.Deleted {
			return notFound("no page %q", page)
		}
		res.PageID, res.Page = p.ID, p.Title
		unit, found, err := t.MetricUnit(metric)
		if err != nil {
			return err
		}
		if !found {
			return invalid("no metric %q: the owner registers it first (register-metric)", metric)
		}
		res.Unit = unit
		tbl, dayCol, valueCol, unitCol, err := pickTable(markdownTables(p.Body), column)
		if err != nil {
			return err
		}
		res.Column = tbl[0][valueCol]
		headerUnit := ""
		if units := unitTokens(tbl[0][valueCol]); len(units) > 0 {
			headerUnit = units[0]
		}
		seen := map[string]bool{}
		for i, row := range tbl[1:] {
			report := func(day, cell, format string, a ...any) {
				res.Reported = append(res.Reported, TableRow{Row: i + 1, Day: day, Cell: cell, Reason: fmt.Sprintf(format, a...)})
			}
			day, cell := cellAt(row, dayCol), cellAt(row, valueCol)
			if cell == "" {
				continue
			}
			if !IsDay(day) {
				report(day, cell, "no day YYYY-MM-DD in the row")
				continue
			}
			m := plainValueRE.FindStringSubmatch(cell)
			if m == nil || (m[2] != "" && !unitToken(m[2])) {
				report(day, cell, "not a plain number: kept as text")
				continue
			}
			got := m[2]
			if got == "" && unitCol >= 0 {
				got = cellAt(row, unitCol)
			}
			if got == "" {
				got = headerUnit
			}
			if got != unit {
				if got == "" {
					report(day, cell, "no unit: the metric's unit is %q", unit)
				} else {
					report(day, cell, "the unit %q is not the metric's unit %q: nothing is converted", got, unit)
				}
				continue
			}
			if seen[day] {
				report(day, cell, "a second value for this day")
				continue
			}
			seen[day] = true
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				report(day, cell, "not a plain number: kept as text")
				continue
			}
			id, err := t.Record(Reading{Metric: metric, Day: day, Key: tableKey(p.Title, day), Value: v})
			var ce *Error
			switch {
			case errors.As(err, &ce):
				report(day, cell, "%s", ce.Msg)
			case err != nil:
				return err
			case id == 0:
				res.Existing++
			default:
				res.Written++
			}
		}
		return nil
	})
	return res, err
}

// plainValueRE is a plain number, then an optional unit: "48", "4.5 ng/mL", "-2 %". A sign such as < or ~, a word or
// a comma decimal does not match, or leaves a remainder that is no unit.
var plainValueRE = regexp.MustCompile(`^([+-]?[0-9]+(?:\.[0-9]+)?)\s*(.*)$`)

// tableKey is a reading's key: the page and the day, hashed when it would be longer than a key may be
// (docs/contract/imports.md).
func tableKey(title, day string) string {
	k := "table|" + text.TitleKey(title) + "|" + day
	if len(k) > 512 {
		sum := sha256.Sum256([]byte(k))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return k
}

// pickTable chooses the first table with a column of days, and in it the value column and a "unit" column, if any.
func pickTable(tables [][][]string, column string) (tbl [][]string, dayCol, valueCol, unitCol int, err error) {
	for _, t := range tables {
		if len(t) < 2 {
			continue
		}
		dayCol = -1
		for c := range t[0] {
			for _, row := range t[1:] {
				if IsDay(cellAt(row, c)) {
					dayCol = c
					break
				}
			}
			if dayCol >= 0 {
				break
			}
		}
		if dayCol < 0 {
			continue
		}
		unitCol = -1
		var values []int
		for c, h := range t[0] {
			switch name := strings.ToLower(strings.TrimSpace(h)); {
			case c == dayCol:
			case name == "unit" || name == "units":
				unitCol = c
			default:
				values = append(values, c)
			}
		}
		if column != "" {
			for _, c := range values {
				if headerName(t[0][c]) == headerName(column) {
					return t, dayCol, c, unitCol, nil
				}
			}
			return nil, 0, 0, 0, invalid("the table has no column %q", column)
		}
		if len(values) != 1 {
			names := make([]string, len(values))
			for i, c := range values {
				names[i] = t[0][c]
			}
			return nil, 0, 0, 0, invalid("the table has %d value columns (%s): name one with column", len(values), strings.Join(names, ", "))
		}
		return t, dayCol, values[0], unitCol, nil
	}
	return nil, 0, 0, 0, invalid("the page has no table with a column of days (YYYY-MM-DD)")
}

// headerName is a header without its unit in ( ) or [ ], case-folded: "Ferritin (ng/mL)" is "ferritin".
func headerName(h string) string {
	for _, pair := range []string{"()", "[]"} {
		if i := strings.IndexByte(h, pair[0]); i >= 0 {
			if j := strings.IndexByte(h[i:], pair[1]); j >= 0 {
				h = h[:i] + h[i+j+1:]
			}
		}
	}
	return strings.ToLower(strings.Join(strings.Fields(h), " "))
}

// unitTokens are the units a header writes in ( ) or [ ].
func unitTokens(h string) []string {
	var out []string
	for _, pair := range []string{"()", "[]"} {
		rest := h
		for {
			i := strings.IndexByte(rest, pair[0])
			if i < 0 {
				break
			}
			j := strings.IndexByte(rest[i:], pair[1])
			if j < 0 {
				break
			}
			if u := strings.TrimSpace(rest[i+1 : i+j]); unitToken(u) {
				out = append(out, u)
			}
			rest = rest[i+j+1:]
		}
	}
	return out
}

// unitToken is a unit as a table writes it: letters, digits and / % µ μ . with no space, not only digits.
func unitToken(s string) bool {
	letter := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || strings.ContainsRune("%µμ", r):
			letter = true
		case unicode.IsNumber(r) || strings.ContainsRune("/.^", r):
		default:
			return false
		}
	}
	return letter
}

func cellAt(row []string, c int) string {
	if c < 0 || c >= len(row) {
		return ""
	}
	return row[c]
}

var tableParser = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

// markdownTables are the GFM tables of a body, each a list of rows of trimmed cells in NFC; the first row is the
// header.
func markdownTables(body string) [][][]string {
	src := []byte(body)
	var out [][][]string
	_ = ast.Walk(tableParser.Parse(gtext.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		t, ok := n.(*east.Table)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		var rows [][]string
		for r := t.FirstChild(); r != nil; r = r.NextSibling() {
			var cells []string
			for c := r.FirstChild(); c != nil; c = c.NextSibling() {
				cells = append(cells, norm.NFC.String(strings.TrimSpace(cellText(c, src))))
			}
			rows = append(rows, cells)
		}
		out = append(out, rows)
		return ast.WalkSkipChildren, nil
	})
	return out
}

// cellText is the plain text of a table cell: its text and code, without emphasis marks.
func cellText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
		case *ast.String:
			b.Write(c.Value)
		default:
			b.WriteString(cellText(c, src))
		}
	}
	return b.String()
}

// HasTable says whether a body holds a GFM table: a page view offers readings-from-table only then.
func HasTable(body string) bool {
	return strings.Contains(body, "|") && len(markdownTables(body)) > 0
}
