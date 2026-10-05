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

func checkReadingSourceEvidence(file, source, quote, numText, unit string, marker bool) error {
	matches, approximate := matchingNumberTokens(quote, numText)
	if marker {
		if len(matches) == 0 {
			if quoteHasMeasurementNumber(quote) {
				return refuse("the result word marker %s is not in the quote; a numeric quantity in the quote is incompatible with the result-word exception", numText)
			}
			return nil
		}
	} else if len(matches) == 0 {
		if approximate {
			return refuse("the value %s is not in the quote exactly as written; censored, approximate, qualitative or converted values go in kept_as_text", numText)
		}
		return refuse("the value %s is not in the quote exactly as written; partial, converted or reformatted values go in kept_as_text", numText)
	}
	if unit == "" {
		return nil
	}
	if inlineUnitEvidence(quote, matches, unit) {
		return nil
	}
	if ok := tableUnitEvidence(file, source, quote, numText, unit); ok {
		return nil
	}
	if got := inlineUnitCandidate(quote, matches); got != "" && got != unit {
		return refuse("the source evidence has unit %q at value %s, not %q; nothing is converted or relabeled", got, numText, unit)
	}
	return refuse("the unit %q has no unambiguous source evidence at value %s; ask the owner or keep the source text instead", unit, numText)
}

func matchingNumberTokens(quote, numText string) ([]numberToken, bool) {
	var matches []numberToken
	approximate := false
	for _, tok := range numberTokens(quote) {
		if tok.text != numText {
			continue
		}
		if tok.approximate {
			approximate = true
			continue
		}
		matches = append(matches, tok)
	}
	return matches, approximate
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
	prefix := strings.ToLower(strings.TrimSpace(s[max(0, j-12):j]))
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

func inlineUnitEvidence(quote string, matches []numberToken, unit string) bool {
	for _, tok := range matches {
		if hasUnitAt(quote[tok.end:], unit) {
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

func inlineUnitCandidate(quote string, matches []numberToken) string {
	for _, tok := range matches {
		after := strings.TrimLeftFunc(quote[tok.end:], unicode.IsSpace)
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

func tableUnitEvidence(file, source, quote, numText, unit string) bool {
	for _, table := range sourceTables(file, source) {
		if len(table) < 2 {
			continue
		}
		head := table[0]
		for _, row := range table[1:] {
			if !quoteCoversRow(quote, row) {
				continue
			}
			cols := numericColumns(row, numText)
			if len(cols) != 1 {
				continue
			}
			col := cols[0]
			if col < len(row) {
				cell := collapse(row[col])
				matches, _ := matchingNumberTokens(cell, numText)
				if inlineUnitEvidence(cell, matches, unit) {
					return true
				}
			}
			if col < len(head) && headerHasUnit(head[col], unit) {
				return true
			}
			if rowHasSeparateUnit(row, col, unit) {
				return true
			}
		}
	}
	return false
}

func sourceTables(file, source string) [][][]string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".csv":
		rows, err := csvRows(source)
		if err != nil {
			return nil
		}
		return [][][]string{rows}
	case ".md":
		body := source
		if _, rest, ok := frontmatter(source); ok {
			body = rest
		}
		return markdownTables(body)
	}
	return nil
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
		matches, _ := matchingNumberTokens(collapse(cell), numText)
		if len(matches) > 0 {
			cols = append(cols, i)
		}
	}
	return cols
}

func headerHasUnit(header, unit string) bool {
	return wholeIndex(collapse(header), unit) >= 0
}

func rowHasSeparateUnit(row []string, valueCol int, unit string) bool {
	count := 0
	for i, cell := range row {
		if i == valueCol {
			continue
		}
		if collapse(cell) == unit {
			count++
		}
	}
	return count == 1
}
