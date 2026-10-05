package importer

import (
	"encoding/csv"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	gtext "github.com/yuin/goldmark/text"
	"golang.org/x/text/unicode/norm"
)

type numberToken struct {
	text        string
	start, end  int
	approximate bool
}

// readingEvidence is invocation-local and never retained across source snapshots.
type readingEvidence struct {
	collapsed string
	tokens    []numberToken
	tables    []evidenceTable
}

func newReadingEvidence(file, source string) *readingEvidence {
	collapsed := collapse(source)
	return &readingEvidence{collapsed: collapsed, tokens: numberTokens(collapsed), tables: sourceTables(file, source)}
}

func (e *readingEvidence) check(quote string, quotePos int, numText, unit string, marker bool, approved map[string]Metric) error {
	matches, approximate := matchingNumberTokensWithSource(quote, numText, e.tokens, quotePos)
	tableValue := tableValueEvidence(e.tables, quote, quotePos, numText)
	if marker {
		if len(matches) == 0 && !tableValue {
			if quoteHasMeasurementNumber(quote) {
				return refuse("the result word marker %s is not in the quote; a numeric quantity in the quote is incompatible with the result-word exception", numText)
			}
			return nil
		}
	} else if len(matches) == 0 && !tableValue {
		if approximate {
			return refuse("the value %s is not in the quote exactly as written; censored, approximate, qualitative or converted values go in kept_as_text", numText)
		}
		return refuse("the value %s is not in the quote exactly as written; partial, converted or reformatted values go in kept_as_text", numText)
	}
	if unit == "" {
		if got := inlineUnitCandidate(quote, 0, matches); got != "" {
			return refuse("the source evidence has unit %q at value %s, but the facts omit the unit", got, numText)
		}
		if got := omittedInlineUnit(e.collapsed, quotePos, matches, approved); got != "" {
			return refuse("the source evidence has unit %q at value %s, but the facts omit the unit", got, numText)
		}
		if _, conflict := tableUnitEvidence(e.tables, quote, quotePos, numText, unit, matches); conflict != "" {
			return refuse("the source evidence has unit %q at value %s, but the facts omit the unit", conflict, numText)
		}
		return nil
	}
	inlineOK := inlineUnitEvidence(e.collapsed, quotePos, matches, unit)
	if ok, conflict := tableUnitEvidence(e.tables, quote, quotePos, numText, unit, matches); conflict != "" {
		return refuse("the source evidence has unit %q at value %s, not %q; nothing is converted or relabeled", conflict, numText, unit)
	} else if ok || inlineOK {
		return nil
	}
	if got := inlineUnitCandidate(e.collapsed, quotePos, matches); got != "" && got != unit {
		return refuse("the source evidence has unit %q at value %s, not %q; nothing is converted or relabeled", got, numText, unit)
	}
	return refuse("the unit %q has no unambiguous source evidence at value %s; ask the owner or keep the source text instead", unit, numText)
}

func matchingNumberTokens(quote, numText, context string, base int) ([]numberToken, bool) {
	return matchingNumberTokensWithSource(quote, numText, numberTokens(context), base)
}

func matchingNumberTokensWithSource(quote, numText string, sourceTokens []numberToken, base int) ([]numberToken, bool) {
	var matches []numberToken
	approximate := false
	for _, tok := range numberTokens(quote) {
		if tok.text != numText {
			continue
		}
		if base >= 0 {
			sourceTok, ok := sourceNumberTokenAt(sourceTokens, base+tok.start)
			if !ok || sourceTok.start != base+tok.start || sourceTok.text != numText {
				if ok && sourceTok.approximate && sourceTok.text == numText {
					approximate = true
				}
				continue
			}
			if sourceTok.approximate {
				approximate = true
				continue
			}
		} else if tok.approximate {
			approximate = true
			continue
		}
		matches = append(matches, tok)
	}
	return matches, approximate
}

func sourceNumberTokenAt(tokens []numberToken, pos int) (numberToken, bool) {
	i := sort.Search(len(tokens), func(i int) bool { return tokens[i].end > pos })
	if i < len(tokens) && tokens[i].start <= pos {
		return tokens[i], true
	}
	return numberToken{}, false
}

func quoteHasMeasurementNumber(quote string) bool {
	for _, tok := range numberTokens(quote) {
		if !isDateYearToken(quote, tok) {
			return true
		}
	}
	return false
}

func numberTokens(s string) []numberToken {
	var out []numberToken
	for i := 0; i < len(s); {
		if !canStartNumber(s, i) {
			i++
			continue
		}
		start := i
		if s[i] == '+' || s[i] == '-' {
			i++
		}
		for i < len(s) && isASCIIDigit(s[i]) {
			i++
		}
		if i < len(s) && (s[i] == '.' || s[i] == ',') && i+1 < len(s) && isASCIIDigit(s[i+1]) {
			i++
			for i < len(s) && isASCIIDigit(s[i]) {
				i++
			}
		}
		if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
			j := i + 1
			if j < len(s) && (s[j] == '+' || s[j] == '-') {
				j++
			}
			k := j
			for k < len(s) && isASCIIDigit(s[k]) {
				k++
			}
			if k > j {
				i = k
			}
		}
		out = append(out, numberToken{text: s[start:i], start: start, end: i, approximate: approximateNumberPrefix(s, start)})
	}
	return out
}

func canStartNumber(s string, i int) bool {
	if i > 0 {
		before := lastRune(s[:i])
		if isWordChar(before) {
			return false
		}
		if s[i-1] == '.' && i >= 2 && isASCIIDigit(s[i-2]) {
			return false
		}
		if s[i-1] == ',' && i >= 2 && isASCIIDigit(s[i-2]) && commaContinuesNumber(s, i-1) {
			return false
		}
		if s[i-1] == '-' && i >= 2 && isASCIIDigit(s[i-2]) {
			return false
		}
		if (s[i-1] == '+' || s[i-1] == '-') && i >= 2 && (s[i-2] == 'e' || s[i-2] == 'E') {
			return false
		}
	}
	if s[i] == '+' || s[i] == '-' {
		return i+1 < len(s) && (isASCIIDigit(s[i+1]) || ((s[i+1] == '.' || s[i+1] == ',') && i+2 < len(s) && isASCIIDigit(s[i+2])))
	}
	return isASCIIDigit(s[i]) || ((s[i] == '.' || s[i] == ',') && i+1 < len(s) && isASCIIDigit(s[i+1]))
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func commaContinuesNumber(s string, comma int) bool {
	j := comma - 1
	for j >= 0 && isASCIIDigit(s[j]) {
		j--
	}
	return !(j >= 1 && s[j] == '-' && isASCIIDigit(s[j-1]))
}

func approximateNumberPrefix(s string, start int) bool {
	j := start
	for j > 0 {
		r, size := lastRuneSize(s[:j])
		if !unicode.IsSpace(r) {
			break
		}
		j -= size
	}
	if j == 0 {
		return false
	}
	r, _ := lastRuneSize(s[:j])
	if strings.ContainsRune("<>≤≥~≈≃≲≳±", r) {
		return true
	}
	prefix := strings.ToLower(strings.TrimSpace(s[max(0, j-24):j]))
	prefix = strings.TrimRight(prefix, ".:")
	return strings.HasSuffix(prefix, "about") || strings.HasSuffix(prefix, "approx") || strings.HasSuffix(prefix, "approximately")
}

func lastRuneSize(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	return utf8.DecodeLastRuneInString(s)
}

func isDateYearToken(s string, tok numberToken) bool {
	return len(tok.text) == 4 && tok.end+6 <= len(s) && s[tok.end] == '-' && isASCIIDigit(s[tok.end+1]) && isASCIIDigit(s[tok.end+2]) && s[tok.end+3] == '-' && isASCIIDigit(s[tok.end+4]) && isASCIIDigit(s[tok.end+5])
}

func inlineUnitEvidence(context string, base int, matches []numberToken, unit string) bool {
	for _, tok := range matches {
		end := base + tok.end
		if end >= 0 && end <= len(context) && hasUnitAt(context[end:], unit) {
			return true
		}
	}
	return false
}

func hasUnitAt(after, unit string) bool {
	after = strings.TrimLeftFunc(after, unicode.IsSpace)
	if !strings.HasPrefix(after, unit) {
		return false
	}
	if len(after) == len(unit) {
		return true
	}
	next := firstRune(after[len(unit):])
	return next == 0 || unicode.IsSpace(next) || strings.ContainsRune("|,;.)]}", next)
}

func inlineUnitCandidate(context string, base int, matches []numberToken) string {
	for _, tok := range matches {
		end := base + tok.end
		if end < 0 || end > len(context) {
			continue
		}
		after := strings.TrimLeftFunc(context[end:], unicode.IsSpace)
		if after == "" {
			continue
		}
		var b strings.Builder
		for _, r := range after {
			if unicode.IsSpace(r) || strings.ContainsRune("|,;.)]}", r) {
				break
			}
			b.WriteRune(r)
		}
		got := b.String()
		if strings.ContainsFunc(got, unicode.IsLetter) || strings.ContainsAny(got, "/%") {
			return got
		}
	}
	return ""
}

// Outside quote bounds, only lexical unit evidence is used, never a prose guess.
func omittedInlineUnit(context string, base int, matches []numberToken, approved map[string]Metric) string {
	for _, tok := range matches {
		got := inlineUnitCandidate(context, base, []numberToken{tok})
		if strings.ContainsAny(got, "/%µμ") {
			return got
		}
		for _, m := range approved {
			if m.Unit != "" && got == m.Unit {
				return got
			}
		}
		switch got {
		case "kg", "g", "mg", "ng", "L", "mL", "dL", "cm", "mm", "km", "mmHg", "bpm", "kcal":
			return got
		}
	}
	return ""
}

type evidenceTable struct {
	rows         [][]string
	starts, ends []int
}

func tableValueEvidence(tables []evidenceTable, quote string, quotePos int, numText string) bool {
	for _, table := range tables {
		if len(table.rows) < 2 {
			continue
		}
		for rowIndex, row := range table.rows[1:] {
			absoluteRow := rowIndex + 1
			if !rowAtQuote(table, absoluteRow, quotePos) || !quoteCoversRow(quote, row) {
				continue
			}
			if len(numericColumns(row, numText)) == 1 {
				return true
			}
		}
	}
	return false
}

func tableUnitEvidence(tables []evidenceTable, quote string, quotePos int, numText, unit string, matches []numberToken) (bool, string) {
	for _, table := range tables {
		if len(table.rows) < 2 {
			continue
		}
		head := table.rows[0]
		for rowIndex, row := range table.rows[1:] {
			absoluteRow := rowIndex + 1
			associated := false
			for _, tok := range matches {
				associated = associated || rowAtQuote(table, absoluteRow, quotePos+tok.start)
			}
			// CSV quoting can prevent lexical matching; the complete row remains evidence.
			associated = associated || (rowAtQuote(table, absoluteRow, quotePos) && quoteCoversRow(quote, row))
			if !associated {
				continue
			}
			cols := numericColumns(row, numText)
			if len(cols) > 1 {
				return false, "ambiguous repeated numeric cells"
			}
			if len(cols) == 0 {
				continue
			}
			col := cols[0]
			ok := false
			var conflict string
			if col < len(row) {
				cell := collapse(row[col])
				matches, _ := matchingNumberTokens(cell, numText, cell, 0)
				if inlineUnitEvidence(cell, 0, matches, unit) {
					ok = true
				}
				if got := inlineUnitCandidate(cell, 0, matches); got != "" && got != unit {
					conflict = got
				}
			}
			if col < len(head) {
				if headerHasExactUnit(head[col], unit) {
					ok = true
				}
				if got := headerUnitConflict(head[col], unit); got != "" {
					conflict = got
				}
			}
			if separateOK, got := rowHasSeparateUnit(head, row, col, unit); separateOK || got != "" {
				ok = ok || separateOK
				if got != "" {
					conflict = got
				}
			}
			if conflict != "" {
				return false, conflict
			}
			if ok {
				return true, ""
			}
		}
	}
	return false, ""
}

func rowAtQuote(table evidenceTable, row int, quotePos int) bool {
	return row < len(table.starts) && table.starts[row] >= 0 && quotePos >= table.starts[row] && quotePos < table.ends[row]
}

func sourceTables(file, source string) []evidenceTable {
	offsets := sourceCollapsedOffsets(source)
	switch strings.ToLower(filepath.Ext(file)) {
	case ".csv":
		table, err := csvEvidenceTable(source, offsets)
		if err != nil {
			return nil
		}
		return []evidenceTable{table}
	case ".md":
		return markdownEvidenceTables(source, offsets)
	}
	return nil
}

func csvEvidenceTable(source string, offsets []int) (evidenceTable, error) {
	r := csv.NewReader(strings.NewReader(source))
	var rows [][]string
	var starts, ends []int
	recordStart := int64(0)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return evidenceTable{}, err
		}
		recordEnd := r.InputOffset()
		for i := range rec {
			rec[i] = norm.NFC.String(rec[i])
		}
		rows = append(rows, rec)
		start := offsets[int(recordStart)]
		end := offsets[int(recordEnd)]
		recordStart = recordEnd
		starts = append(starts, start)
		ends = append(ends, end)
	}
	return evidenceTable{rows: rows, starts: starts, ends: ends}, nil
}

func markdownEvidenceTables(source string, offsets []int) []evidenceTable {
	b := []byte(source)
	doc := gfm.Parser().Parse(gtext.NewReader(b))
	var out []evidenceTable
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		table, ok := n.(*east.Table)
		if !ok {
			return ast.WalkContinue, nil
		}
		rows := tableRows(table, b)
		for _, row := range rows {
			for i := range row {
				row[i] = norm.NFC.String(row[i])
			}
		}
		t := evidenceTable{rows: rows, starts: make([]int, len(rows)), ends: make([]int, len(rows))}
		rowIndex := 0
		for r := table.FirstChild(); r != nil && rowIndex < len(rows); r = r.NextSibling() {
			start, end := lineSpanAtByte(source, r.Pos())
			t.starts[rowIndex] = offsets[start]
			t.ends[rowIndex] = offsets[end]
			rowIndex++
		}
		out = append(out, t)
		return ast.WalkSkipChildren, nil
	})
	return out
}

func lineSpanAtByte(source string, pos int) (int, int) {
	if pos < 0 || pos > len(source) {
		return 0, 0
	}
	start := strings.LastIndexByte(source[:pos], '\n') + 1
	end := len(source)
	if nl := strings.IndexByte(source[pos:], '\n'); nl >= 0 {
		end = pos + nl
	}
	return start, end
}

// Map original parser boundaries through NFC segments, then whitespace collapse.
// Parser row/record boundaries do not split a normalization segment. Building
// this map once avoids normalizing every source prefix or every reading.
func sourceCollapsedOffsets(source string) []int {
	var iter norm.Iter
	iter.InitString(norm.NFC, source)
	var normalized strings.Builder
	offsets := make([]int, len(source)+1)
	for !iter.Done() {
		start, at := iter.Pos(), normalized.Len()
		segment := iter.Next()
		for i := start; i < iter.Pos(); i++ {
			offsets[i] = at
		}
		normalized.Write(segment)
		offsets[iter.Pos()] = normalized.Len()
	}
	collapsed := collapsedOffsets(normalized.String())
	for i, at := range offsets {
		offsets[i] = collapsed[at]
	}
	return offsets
}

// Offsets use whitespace collapse on already-normalized comparison text.
func collapsedOffsets(source string) []int {
	offsets := make([]int, len(source)+1)
	out := 0
	pending := false
	for i, r := range source {
		space := unicode.IsSpace(r)
		at := out
		if !space && pending && out > 0 {
			at++
		}
		size := utf8.RuneLen(r)
		if r == utf8.RuneError {
			_, size = utf8.DecodeRuneInString(source[i:])
		}
		for j := i; j < i+size; j++ {
			offsets[j] = at
		}
		if space {
			pending = out > 0
		} else {
			out = at + size
			pending = false
		}
	}
	offsets[len(source)] = out
	return offsets
}

func quoteCoversRow(quote string, row []string) bool {
	for _, cell := range row {
		cell = collapse(cell)
		if cell == "" {
			continue
		}
		if !strings.Contains(quote, cell) {
			return false
		}
	}
	return true
}

func numericColumns(row []string, numText string) []int {
	var cols []int
	for i, cell := range row {
		cell = collapse(cell)
		matches, _ := matchingNumberTokens(cell, numText, cell, 0)
		if len(matches) > 0 {
			cols = append(cols, i)
		}
	}
	return cols
}

func headerHasExactUnit(header, unit string) bool {
	return explicitUnitIndex(collapse(header), unit) >= 0
}

func headerUnitConflict(header, unit string) string {
	header = collapse(header)
	if explicitUnitIndex(header, unit) >= 0 {
		return ""
	}
	for _, candidate := range unitLikeTokens(header) {
		if candidate != unit {
			return candidate
		}
	}
	return ""
}

func explicitUnitIndex(s, unit string) int {
	if unit == "" {
		return -1
	}
	for from := 0; from <= len(s)-len(unit); {
		i := strings.Index(s[from:], unit)
		if i < 0 {
			return -1
		}
		i += from
		before, after := rune(0), rune(0)
		if i > 0 {
			before = lastRune(s[:i])
		}
		if j := i + len(unit); j < len(s) {
			after = firstRune(s[j:])
		}
		if !isUnitChar(before) && !isUnitChar(after) {
			return i
		}
		from = i + 1
	}
	return -1
}

func isUnitChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("/%µμ.", r)
}

func unitLikeTokens(s string) []string {
	var out []string
	for _, pair := range [][2]rune{{'(', ')'}, {'[', ']'}} {
		for rest := s; ; {
			start := strings.IndexRune(rest, pair[0])
			if start < 0 {
				break
			}
			rest = rest[start+len(string(pair[0])):]
			end := strings.IndexRune(rest, pair[1])
			if end < 0 {
				break
			}
			candidate := strings.TrimSpace(rest[:end])
			if candidate != "" && explicitUnitToken(candidate) {
				out = append(out, candidate)
			}
			rest = rest[end+len(string(pair[1])):]
		}
	}
	return out
}

func explicitUnitToken(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || !isUnitChar(r) {
			return false
		}
	}
	return true
}

func rowHasSeparateUnit(head, row []string, valueCol int, unit string) (bool, string) {
	for _, unitCol := range []int{valueCol - 1, valueCol + 1} {
		if unitCol < 0 || unitCol >= len(row) || unitCol >= len(head) {
			continue
		}
		h := strings.ToLower(collapse(head[unitCol]))
		if h != "unit" && h != "units" {
			continue
		}
		got := collapse(row[unitCol])
		if got == unit {
			return true, ""
		}
		if got != "" {
			return false, got
		}
	}
	return false, ""
}
