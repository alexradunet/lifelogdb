package core

import (
	"fmt"
	"strings"
)

type sqlToken struct {
	lit    string
	punct  byte
	quoted bool
}

func validateAdHocSQL(q string) error {
	tokens, err := adHocTokens(q)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("query must contain one read-only statement")
	}
	first := keyword(tokens[0])
	switch first {
	case "SELECT", "WITH", "VALUES":
		return nil
	case "EXPLAIN":
		return validateExplain(tokens[1:])
	case "PRAGMA":
		return validatePragma(tokens[1:])
	default:
		return fmt.Errorf("query must be one read-only SELECT, WITH, VALUES, EXPLAIN or allowed PRAGMA statement")
	}
}

func validateExplain(tokens []sqlToken) error {
	if len(tokens) >= 2 && keyword(tokens[0]) == "QUERY" && keyword(tokens[1]) == "PLAN" {
		tokens = tokens[2:]
	}
	if len(tokens) == 0 {
		return fmt.Errorf("EXPLAIN must describe a read-only statement")
	}
	switch keyword(tokens[0]) {
	case "SELECT", "WITH", "VALUES":
		return nil
	default:
		return fmt.Errorf("EXPLAIN is allowed only for SELECT, WITH or VALUES")
	}
}

func validatePragma(tokens []sqlToken) error {
	if len(tokens) == 0 {
		return fmt.Errorf("PRAGMA must name an allowed read-only introspection pragma")
	}
	for _, tok := range tokens {
		if tok.punct == '=' {
			return fmt.Errorf("PRAGMA setters are not allowed")
		}
	}
	nameAt := 0
	if len(tokens) >= 3 && ident(tokens[0]) && tokens[1].punct == '.' && ident(tokens[2]) {
		nameAt = 2
	}
	if !ident(tokens[nameAt]) {
		return fmt.Errorf("PRAGMA must name an allowed read-only introspection pragma")
	}
	name := strings.ToLower(tokens[nameAt].lit)
	if pragmaWithArgument[name] {
		return nil
	}
	if pragmaWithoutArgument[name] {
		for _, tok := range tokens[nameAt+1:] {
			if tok.punct == '(' {
				return fmt.Errorf("PRAGMA setters are not allowed")
			}
			if tok.punct == 0 {
				return fmt.Errorf("PRAGMA %s takes no query argument here", name)
			}
		}
		return nil
	}
	return fmt.Errorf("PRAGMA %s is not in the read-only introspection allowlist", name)
}

var pragmaWithoutArgument = map[string]bool{
	"application_id":     true,
	"busy_timeout":       true,
	"collation_list":     true,
	"compile_options":    true,
	"database_list":      true,
	"foreign_keys":       true,
	"function_list":      true,
	"module_list":        true,
	"pragma_list":        true,
	"query_only":         true,
	"recursive_triggers": true,
	"synchronous":        true,
	"table_list":         true,
	"trusted_schema":     true,
	"user_version":       true,
}

var pragmaWithArgument = map[string]bool{
	"foreign_key_list": true,
	"index_info":       true,
	"index_list":       true,
	"index_xinfo":      true,
	"integrity_check":  true,
	"quick_check":      true,
	"table_info":       true,
	"table_xinfo":      true,
}

func keyword(tok sqlToken) string {
	if !ident(tok) {
		return ""
	}
	return strings.ToUpper(tok.lit)
}

func ident(tok sqlToken) bool { return tok.punct == 0 && !tok.quoted && tok.lit != "" }

func adHocTokens(q string) ([]sqlToken, error) {
	var tokens []sqlToken
	for i := 0; i < len(q); {
		switch c := q[i]; {
		case isSpace(c):
			i++
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			i = skipLineComment(q, i+2)
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			i = skipBlockComment(q, i+2)
		case c == ';':
			if !onlySpaceAndComments(q[i+1:]) {
				return nil, fmt.Errorf("query must contain exactly one statement")
			}
			return tokens, nil
		case c == '\'':
			i = skipQuoted(q, i+1, '\'')
		case c == '"':
			start := i + 1
			i = skipQuoted(q, start, '"')
			tokens = append(tokens, sqlToken{lit: unescapeDoubled(quotedContent(q, start, i), '"'), quoted: true})
		case c == '`':
			start := i + 1
			i = skipQuoted(q, start, '`')
			tokens = append(tokens, sqlToken{lit: unescapeDoubled(quotedContent(q, start, i), '`'), quoted: true})
		case c == '[':
			start := i + 1
			i = skipBracketed(q, start)
			tokens = append(tokens, sqlToken{lit: quotedContent(q, start, i), quoted: true})
		case isPunct(c):
			tokens = append(tokens, sqlToken{punct: c})
			i++
		default:
			start := i
			for i < len(q) && !isSpace(q[i]) && !isPunct(q[i]) && q[i] != ';' && q[i] != '\'' && q[i] != '"' && q[i] != '`' && q[i] != '[' && !(q[i] == '-' && i+1 < len(q) && q[i+1] == '-') && !(q[i] == '/' && i+1 < len(q) && q[i+1] == '*') {
				i++
			}
			tokens = append(tokens, sqlToken{lit: q[start:i]})
		}
	}
	return tokens, nil
}

func onlySpaceAndComments(s string) bool {
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case isSpace(c):
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			i = skipLineComment(s, i+2)
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipBlockComment(s, i+2)
		default:
			return false
		}
	}
	return true
}

func skipLineComment(s string, i int) int {
	for i < len(s) && s[i] != '\n' && s[i] != '\r' {
		i++
	}
	return i
}

func skipBlockComment(s string, i int) int {
	for i+1 < len(s) {
		if s[i] == '*' && s[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(s)
}

func skipQuoted(s string, i int, quote byte) int {
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(s)
}

func skipBracketed(s string, i int) int {
	for i < len(s) {
		if s[i] == ']' {
			return i + 1
		}
		i++
	}
	return len(s)
}

func quotedContent(s string, start, after int) string {
	end := after - 1
	if end < start {
		end = start
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

func unescapeDoubled(s string, quote byte) string {
	return strings.ReplaceAll(s, string([]byte{quote, quote}), string(quote))
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isPunct(c byte) bool {
	switch c {
	case '(', ')', ',', '.', '=':
		return true
	default:
		return false
	}
}
