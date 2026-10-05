package importer

import (
	"path/filepath"
	"strings"
	"unicode"
)

type numberToken struct {
	text        string
	start, end  int
	approximate bool
}

func checkReadingSourceEvidence(file, source, sourceCollapsed, quote string, quotePos int, numText, unit string, marker bool) error {
	matches, approximate := matchingNumberTokens(quote, numText, sourceCollapsed, quotePos)
	tableValue := tableValueEvidence(file, source, sourceCollapsed, quote, quotePos, numText)
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
		return nil
	}
	if inlineUnitEvidence(sourceCollapsed, quotePos, matches, unit) {
		return nil
	}
	if ok, conflict := tableUnitEvidence(file, source, sourceCollapsed, quote, quotePos, numText, unit); ok {
		return nil
	} else if conflict != "" {
		return refuse("the source evidence has unit %q at value %s, not %q; nothing is converted or relabeled", conflict, numText, unit)
	}
	if got := inlineUnitCandidate(sourceCollapsed, quotePos, matches); got != "" && got != unit {
		return refuse("the source evidence has unit %q at value %s, not %q; nothing is converted or relabeled", got, numText, unit)
	}
	return refuse("the unit %q has no unambiguous source evidence at value %s; ask the owner or keep the source text instead", unit, numText)
}

func matchingNumberTokens(quote, numText, context string, base int) ([]numberToken, bool) {
	var matches []numberToken
	approximate := false
	for _, tok := range numberTokens(quote) {
		if tok.text != numText {
			continue
		}
		if base >= 0 {
			sourceTok, ok := sourceNumberTokenAt(context, base+tok.start)
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

func sourceNumberTokenAt(s string, pos int) (numberToken, bool) {
	for _, tok := range numberTokens(s) {
		if pos >= tok.start && pos < tok.end {
			return tok, true
		}
		if tok.start > pos {
			break
		}
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
		return i+1 < len(s) && isASCIIDigit(s[i+1])
	}
	return isASCIIDigit(s[i])
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
	var last rune
	var size int
	for i, r := range s {
		last = r
		size = len(s) - i
	}
	return last, size
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

type evidenceTable struct {
	rows         [][]string
	starts, ends []int
}

func tableValueEvidence(file, source, sourceCollapsed, quote string, quotePos int, numText string) bool {
	for _, table := range sourceTables(file, source, sourceCollapsed) {
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

func tableUnitEvidence(file, source, sourceCollapsed, quote string, quotePos int, numText, unit string) (bool, string) {
	for _, table := range sourceTables(file, source, sourceCollapsed) {
		if len(table.rows) < 2 {
			continue
		}
		head := table.rows[0]
		for rowIndex, row := range table.rows[1:] {
			absoluteRow := rowIndex + 1
			if !rowAtQuote(table, absoluteRow, quotePos) || !quoteCoversRow(quote, row) {
				continue
			}
			cols := numericColumns(row, numText)
			if len(cols) != 1 {
				continue
			}
			col := cols[0]
			if col < len(row) {
				cell := collapse(row[col])
				matches, _ := matchingNumberTokens(cell, numText, cell, 0)
				if inlineUnitEvidence(cell, 0, matches, unit) {
					return true, ""
				}
				if got := inlineUnitCandidate(cell, 0, matches); got != "" && got != unit {
					return false, got
				}
			}
			if col < len(head) {
				if headerHasExactUnit(head[col], unit) {
					return true, ""
				}
				if got := headerUnitConflict(head[col], unit); got != "" {
					return false, got
				}
			}
			if ok, conflict := rowHasSeparateUnit(head, row, col, unit); ok || conflict != "" {
				return ok, conflict
			}
		}
	}
	return false, ""
}

func rowAtQuote(table evidenceTable, row int, quotePos int) bool {
	return row < len(table.starts) && quotePos >= table.starts[row] && quotePos < table.ends[row]
}

func sourceTables(file, source, sourceCollapsed string) []evidenceTable {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".csv":
		rows, err := csvRows(source)
		if err != nil {
			return nil
		}
		return []evidenceTable{withRowPositions(rows, source, sourceCollapsed)}
	case ".md":
		body := source
		if _, rest, ok := frontmatter(source); ok {
			body = rest
		}
		return markdownEvidenceTables(markdownTables(body), source, sourceCollapsed)
	}
	return nil
}

func withRowPositions(rows [][]string, source, sourceCollapsed string) evidenceTable {
	t := evidenceTable{rows: rows, starts: make([]int, len(rows)), ends: make([]int, len(rows))}
	cursor := 0
	for i, line := range strings.Split(source, "\n") {
		if i >= len(rows) {
			break
		}
		line = collapse(line)
		start := strings.Index(sourceCollapsed[cursor:], line)
		if start < 0 {
			continue
		}
		start += cursor
		t.starts[i], t.ends[i] = start, start+len(line)
		cursor = t.ends[i]
	}
	return t
}

func markdownEvidenceTables(tables [][][]string, source, sourceCollapsed string) []evidenceTable {
	lines := strings.Split(source, "\n")
	cursor := 0
	lineIndex := 0
	var out []evidenceTable
	for _, rows := range tables {
		t := evidenceTable{rows: rows, starts: make([]int, len(rows)), ends: make([]int, len(rows))}
		for i, row := range rows {
			for lineIndex < len(lines) {
				line := strings.TrimSpace(lines[lineIndex])
				lineIndex++
				if !strings.Contains(line, "|") || markdownSeparatorLine(line) || !lineCoversCells(line, row) {
					continue
				}
				collapsed := collapse(line)
				start := strings.Index(sourceCollapsed[cursor:], collapsed)
				if start >= 0 {
					start += cursor
					t.starts[i], t.ends[i] = start, start+len(collapsed)
					cursor = t.ends[i]
				}
				break
			}
		}
		out = append(out, t)
	}
	return out
}

func markdownSeparatorLine(line string) bool {
	line = strings.Trim(line, " |")
	if line == "" {
		return false
	}
	for _, r := range line {
		if r != '-' && r != ':' && r != '|' && !unicode.IsSpace(r) {
			return false
		}
	}
	return strings.Contains(line, "-")
}

func lineCoversCells(line string, row []string) bool {
	line = collapse(line)
	for _, cell := range row {
		cell = collapse(cell)
		if cell != "" && !strings.Contains(line, cell) {
			return false
		}
	}
	return true
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
	for _, field := range strings.Fields(s) {
		field = strings.Trim(field, "()[]{}:;,")
		if strings.ContainsAny(field, "/%µμ") {
			out = append(out, field)
		}
	}
	return out
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
