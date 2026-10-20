package text

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
)

// Plan lines: docs/contract/plan-lines.md is the one home of the grammar. This file reads the lines of a body and
// edits one line at a time. The file life.db does not know a plan: the body is the truth.

var (
	// ErrLineChanged means that the line is not an open plan line, or that its text differs from the text the client sent.
	ErrLineChanged = errors.New("the line changed")
	// ErrCarried means that a reopen names a carried line. Its copy is on another page, so the copy is the line to drop.
	ErrCarried = errors.New("the line was carried: its copy is on another page, so drop that copy instead")
	// ErrMalformed means that the line breaks the grammar. The message names the offending token.
	ErrMalformed = errors.New("the line is malformed")
)

// Plan is one plan line of a body.
type Plan struct {
	Line     int    // 1-based line of the body
	Box      byte   // ' ' open, 'x' done, '-' dropped, '>' carried
	Text     string // the words without box and tokens, spaces collapsed, trimmed
	Raw      string // the source line as written, without its line break
	On       string
	Due      string
	Done     string
	Every    string
	FromDone bool
	Bad      string // the first offending token of a malformed line, else ""

	// BoxAt is the offset in the body of the "[" of the box, and Tokens the offsets (start, end) of the tokens of a
	// line that is not malformed. A reader that draws the body uses them to find the line in its own parse.
	BoxAt  int
	Tokens [][2]int

	editAt int // offset in the body where a "done:" token goes: after the last token, else after the last word
}

// Plans lists the plan lines of a body in order.
func Plans(body string) []Plan {
	starts := lineStarts([]byte(body))
	// goldmark does not end a line at a lone CR. The copy has the same length, so every offset stays valid.
	var out []Plan
	_ = ast.Walk(md.Parser().Parse(gtext.NewReader(loneCRToLF(body))), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		item, ok := n.(*ast.ListItem)
		if !entering || !ok || inQuote(item) {
			return ast.WalkContinue, nil
		}
		// Only a text block or a paragraph holds a box. A code block in the item (indented code) does not.
		switch item.FirstChild().(type) {
		case *ast.TextBlock, *ast.Paragraph:
		default:
			return ast.WalkContinue, nil
		}
		lines := item.FirstChild().Lines()
		if lines.Len() == 0 {
			return ast.WalkContinue, nil
		}
		if p, ok := readPlan(body, starts, lines.At(0).Start); ok {
			out = append(out, p)
		}
		return ast.WalkContinue, nil
	})
	return out
}

// loneCRToLF replaces each CR that no LF follows by an LF. The result has the length of the body.
func loneCRToLF(body string) []byte {
	b := []byte(body)
	for i, c := range b {
		if c == '\r' && (i+1 == len(b) || b[i+1] != '\n') {
			b[i] = '\n'
		}
	}
	return b
}

func inQuote(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.Blockquote); ok {
			return true
		}
	}
	return false
}

// lineStarts holds the offset of each line. LF, CRLF and CR each end a line once.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		case '\n':
			starts = append(starts, i+1)
		}
	}
	return starts
}

// token is a key:value word of a plan line. at and end are offsets in the body.
type token struct {
	key, val, word string
	at, end        int
}

var tokenKeys = map[string]bool{"on": true, "due": true, "done": true, "every": true, "from": true}

// readPlan reads the line that holds the offset at (the start of the content of a list item).
func readPlan(body string, starts []int, at int) (Plan, bool) {
	ln := sort.SearchInts(starts, at+1) - 1
	// The box starts the item on the line of the bullet. Only blanks before it means the bullet is on an earlier line.
	if strings.Trim(body[starts[ln]:at], " \t") == "" {
		return Plan{}, false
	}
	eol := strings.IndexAny(body[at:], "\r\n")
	if eol < 0 {
		eol = len(body)
	} else {
		eol += at
	}
	s := body[at:eol]
	if len(s) < 3 || s[0] != '[' || s[2] != ']' || !strings.ContainsRune(" xX->", rune(s[1])) || (len(s) > 3 && s[3] != ' ') {
		return Plan{}, false
	}
	p := Plan{Line: ln + 1, Box: s[1], Raw: body[starts[ln]:eol], BoxAt: at}
	if p.Box == 'X' {
		p.Box = 'x'
	}

	// Split the rest on U+0020 only: a tab or a no-break space belongs to its word.
	var words []string
	var toks []token
	p.editAt = at + 3
	for i := at + 3; i < eol; {
		if body[i] == ' ' {
			i++
			continue
		}
		j := i
		for j < eol && body[j] != ' ' {
			j++
		}
		w := body[i:j]
		if k, v, ok := strings.Cut(w, ":"); ok && v != "" && tokenKeys[k] {
			toks = append(toks, token{key: k, val: v, word: w, at: i, end: j})
		} else {
			words = append(words, w)
		}
		p.editAt = j
		i = j
	}
	if len(toks) > 0 {
		p.editAt = toks[len(toks)-1].end
	}
	p.Text = strings.Join(words, " ")
	p.Bad = firstBad(p.Box, toks)
	if p.Bad != "" {
		return p, true
	}
	for _, t := range toks {
		p.Tokens = append(p.Tokens, [2]int{t.at, t.end})
		switch t.key {
		case "on":
			p.On = t.val
		case "due":
			p.Due = t.val
		case "done":
			p.Done = t.val
		case "every":
			p.Every = t.val
		case "from":
			p.FromDone = true
		}
	}
	return p, true
}

// firstBad returns the first token, in the order of the line, that breaks a rule of the grammar. The rules that
// look at other tokens (from:done needs a valid every:, every: needs a valid day) look at the whole line, so the
// offender is the token that carries the rule, even if the other token is later in the line.
func firstBad(box byte, toks []token) string {
	seen := map[string]string{}
	for _, t := range toks {
		if _, dup := seen[t.key]; !dup {
			seen[t.key] = t.val
		}
	}
	_, everyOK := parseRepeat(seen["every"])
	dayOK := IsDay(seen["on"]) || IsDay(seen["due"])
	first := map[string]bool{}
	for _, t := range toks {
		bad := first[t.key]
		first[t.key] = true
		switch t.key {
		case "on", "due":
			bad = bad || !IsDay(t.val)
		case "done":
			bad = bad || !IsDay(t.val) || box != 'x'
		case "every":
			bad = bad || !everyOK || !dayOK
		case "from":
			bad = bad || t.val != "done" || !everyOK
		}
		if bad {
			return t.word
		}
	}
	return ""
}

// repeat is a parsed value of every:.
type repeat struct {
	kind string // days, weeks, months, years, weekday, dow, month-end, month-day
	n    int    // the count, the day of the week (time.Weekday) or the day of the month
}

var dayNames = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// parseRepeat reads a value of every:. A count has no sign and no leading zero. A count too big for an int is
// kept as a huge number, so that Next refuses it by range and not by shape.
func parseRepeat(s string) (repeat, bool) {
	switch s {
	case "day":
		return repeat{"days", 1}, true
	case "week":
		return repeat{"weeks", 1}, true
	case "month":
		return repeat{"months", 1}, true
	case "year":
		return repeat{"years", 1}, true
	case "weekday", "month-end":
		return repeat{kind: s}, true
	}
	if d, ok := dayNames[s]; ok {
		return repeat{"dow", int(d)}, true
	}
	if rest, ok := strings.CutPrefix(s, "month-"); ok {
		n, ok := count(rest)
		return repeat{"month-day", n}, ok && n >= 1 && n <= 31
	}
	if num, unit, ok := strings.Cut(s, "-"); ok {
		n, nok := count(num)
		if nok && n >= 2 && (unit == "days" || unit == "weeks" || unit == "months" || unit == "years") {
			return repeat{unit, n}, true
		}
	}
	return repeat{}, false
}

func count(s string) (int, bool) {
	if s == "" || s[0] == '0' && len(s) > 1 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	if len(s) > 9 {
		return 1 << 30, true
	}
	n, _ := strconv.Atoi(s)
	return n, true
}

func daysIn(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

var errRange = errors.New("the next date is outside 0000-01-01 to 9999-12-31")

// inRange keeps a date inside the days that lifelog_meta.days can hold.
func inRange(t time.Time) (time.Time, error) {
	if t.Year() < 0 || t.Year() > 9999 {
		return t, errRange
	}
	return t, nil
}

// addMonths keeps the day of the month and moves it to the last day of the target month if that month is shorter.
func addMonths(t time.Time, n int) (time.Time, error) {
	if n > 120000 {
		return t, errRange
	}
	total := t.Year()*12 + int(t.Month()) - 1 + n
	y, m := total/12, total%12+1
	if y > 9999 {
		return t, errRange
	}
	return inRange(time.Date(y, time.Month(m), min(t.Day(), daysIn(y, m)), 0, 0, 0, 0, time.UTC))
}

// advance is the rule of one repeat applied to a base day.
func advance(r repeat, base time.Time) (time.Time, error) {
	switch r.kind {
	case "days", "weeks":
		n := r.n
		if r.kind == "weeks" {
			n *= 7
		}
		if n > 4_000_000 {
			return base, errRange
		}
		return inRange(base.AddDate(0, 0, n))
	case "months":
		return addMonths(base, r.n)
	case "years":
		if r.n > 10000 {
			return base, errRange
		}
		y := base.Year() + r.n
		if y > 9999 {
			return base, errRange
		}
		return time.Date(y, base.Month(), min(base.Day(), daysIn(y, int(base.Month()))), 0, 0, 0, 0, time.UTC), nil
	case "weekday":
		t := base.AddDate(0, 0, 1)
		for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			t = t.AddDate(0, 0, 1)
		}
		return inRange(t)
	case "dow":
		delta := (r.n - int(base.Weekday()) + 7) % 7
		if delta == 0 {
			delta = 7
		}
		return inRange(base.AddDate(0, 0, delta))
	case "month-end", "month-day":
		first, err := addMonths(time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, time.UTC), 1)
		if err != nil {
			return base, err
		}
		d := daysIn(first.Year(), int(first.Month()))
		if r.kind == "month-day" {
			d = min(r.n, d)
		}
		return first.AddDate(0, 0, d-1), nil
	}
	return base, fmt.Errorf("unknown repeat %q", r.kind)
}

// Next gives the tokens of the line that a tick adds, by the rules of the contract. done is the day of the tick.
func Next(p Plan, done string) (on, due, every string, err error) {
	if p.Bad != "" {
		return "", "", "", fmt.Errorf("%w: %s", ErrMalformed, p.Bad)
	}
	r, ok := parseRepeat(p.Every)
	if !ok {
		return "", "", "", errors.New("the line has no every token")
	}
	doneDay, ok := ParseDay(done)
	if !ok {
		return "", "", "", fmt.Errorf("%q is not a day", done)
	}
	primary, other := p.Due, p.On
	if primary == "" {
		primary, other = p.On, ""
	}
	pt, ok := ParseDay(primary)
	if !ok {
		return "", "", "", errors.New("the line has no day")
	}
	base := pt
	if p.FromDone {
		base = doneDay
	}
	next, err := advance(r, base)
	if err != nil {
		return "", "", "", err
	}
	every = p.Every
	// A month that clamped keeps the day it meant, so that the next tick can go back to it.
	if p.Every == "month" && !p.FromDone && next.Day() < base.Day() {
		every = "month-" + strconv.Itoa(base.Day())
	}
	nextDay := next.Format(time.DateOnly)
	if p.Due == "" {
		return nextDay, "", every, nil
	}
	if other == "" {
		return "", nextDay, every, nil
	}
	// The other date keeps its distance in days from the primary date. Both are UTC midnights; Duration would saturate.
	ot, _ := ParseDay(other)
	shifted, err := inRange(next.AddDate(0, 0, int((ot.Unix()-pt.Unix())/86400)))
	if err != nil {
		return "", "", "", err
	}
	return shifted.Format(time.DateOnly), nextDay, every, nil
}

// Line writes a plan line: "- [ ] text on:… due:… every:… from:done", in this key order. It refuses what Plans
// would read in another way: an empty text, a line break, a word that is a token, a day or a repeat that is not
// valid, from:done without every:, and every: without a day.
func Line(text, on, due, every string, fromDone bool) (string, error) {
	if strings.ContainsAny(text, "\r\n") {
		return "", errors.New("the text holds a line break")
	}
	words := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' })
	if len(words) == 0 {
		return "", errors.New("the text is empty")
	}
	for _, w := range words {
		if k, v, ok := strings.Cut(w, ":"); ok && v != "" && tokenKeys[k] {
			return "", fmt.Errorf("the text holds the token %q", w)
		}
	}
	for _, d := range []string{on, due} {
		if d != "" && !IsDay(d) {
			return "", fmt.Errorf("%q is not a day", d)
		}
	}
	if every != "" {
		if _, ok := parseRepeat(every); !ok {
			return "", fmt.Errorf("%q is not a repeat", every)
		}
		if on == "" && due == "" {
			//lint:ignore ST1005 the owner reads this sentence in the form; it names the labels of the form
			return "", errors.New("A repeat needs a Planned for or a Must be done by day.")
		}
	}
	if fromDone && every == "" {
		return "", errors.New("a from:done token needs an every token")
	}
	var b strings.Builder
	b.WriteString("- [ ] " + strings.Join(words, " "))
	for _, t := range [][2]string{{"on", on}, {"due", due}, {"every", every}} {
		if t[1] != "" {
			b.WriteString(" " + t[0] + ":" + t[1])
		}
	}
	if fromDone {
		b.WriteString(" from:done")
	}
	return b.String(), nil
}

// findLine finds the plan line that a tick or a carry names, whatever its box. The line has no id: the number and
// the text name it, and a body edited since the client read it gives ErrLineChanged.
func findLine(body string, line int, text string) (Plan, error) {
	for _, p := range Plans(body) {
		if p.Line == line {
			if p.Text != text {
				return p, ErrLineChanged
			}
			return p, nil
		}
	}
	return Plan{}, ErrLineChanged
}

// checkLine refuses a line that the grammar does not let a tick, a carry or a reopen move.
func checkLine(p Plan) error {
	switch {
	case p.Bad != "":
		return fmt.Errorf("%w: %s", ErrMalformed, p.Bad)
	case p.Text == "":
		return fmt.Errorf("%w: the text is empty", ErrMalformed)
	}
	return nil
}

// openLine finds the open plan line that a tick or a carry names.
func openLine(body string, line int, text string) (Plan, error) {
	p, err := findLine(body, line, text)
	if err == nil && p.Box != ' ' {
		err = ErrLineChanged
	}
	if err == nil {
		err = checkLine(p)
	}
	return p, err
}

// Tick ends an open line: outcome 'x' (done) or '-' (dropped). A done line gets done:<done>. If it has every:,
// appended is the next line, for the caller to add to the end of the body as its own paragraph. Only one line of
// the body changes, and its line break stays.
func Tick(body string, line int, text string, outcome byte, done string) (newBody, appended string, err error) {
	if outcome != 'x' && outcome != '-' {
		return "", "", fmt.Errorf("outcome %q is not x or -", outcome)
	}
	p, err := openLine(body, line, text)
	if err != nil {
		return "", "", err
	}
	if outcome == '-' {
		return body[:p.BoxAt+1] + "-" + body[p.BoxAt+2:], "", nil
	}
	if !IsDay(done) {
		return "", "", fmt.Errorf("%q is not a day", done)
	}
	if p.Every != "" {
		on, due, every, err := Next(p, done)
		if err != nil {
			return "", "", err
		}
		if appended, err = Line(p.Text, on, due, every, p.FromDone); err != nil {
			return "", "", err
		}
	}
	return body[:p.BoxAt+1] + "x" + body[p.BoxAt+2:p.editAt] + " done:" + done + body[p.editAt:], appended, nil
}

// Carry moves an open line to another page. The box becomes '>', and copy is the open line for the end of the
// target page. If the target is a day page, on is that day and replaces on:. Else on is "" and on: stays.
func Carry(body string, line int, text, on string) (newBody, copy string, err error) {
	if on != "" && !IsDay(on) {
		return "", "", fmt.Errorf("%q is not a day", on)
	}
	p, err := openLine(body, line, text)
	if err != nil {
		return "", "", err
	}
	if on == "" {
		on = p.On
	}
	if copy, err = Line(p.Text, on, p.Due, p.Every, p.FromDone); err != nil {
		return "", "", err
	}
	return body[:p.BoxAt+1] + ">" + body[p.BoxAt+2:], copy, nil
}

// Schedule changes the dates of an open line where it is. on and due are the new values of on: and due:; an empty
// value removes that token. The bullet, the blanks and the box stay. The line after the box is written again by Line,
// so the tokens go after the words, in its key order. Line refuses what a new line cannot be (every: with no day).
func Schedule(body string, line int, text, on, due string) (newBody string, err error) {
	p, err := openLine(body, line, text)
	if err != nil {
		return "", err
	}
	rest, err := Line(p.Text, on, due, p.Every, p.FromDone)
	if err != nil {
		return "", err
	}
	// Line starts "- [ ] ": its tail is what follows the box of any bullet.
	start := lineStarts([]byte(body))[p.Line-1]
	return body[:p.BoxAt] + "[ ] " + rest[len("- [ ] "):] + body[start+len(p.Raw):], nil
}

// Reopened is the result of Reopen.
type Reopened struct {
	Body        string // the new body
	Removed     string // the next line of a repeat that the reopen removed, as written; "" if none
	RemovedLine int    // the number that the removed line had in the old body
	NextKept    bool   // the line was done with every:, and its next line was not found, so it stays
}

// Reopen sets a done or dropped line back to open. The box becomes ' ' and the done: token goes, with the space
// before it. If the line was done with every:, the next line that the tick added goes too, when it is still there and
// unchanged. The contract says how that line is found and cut.
func Reopen(body string, line int, text string) (Reopened, error) {
	p, err := findLine(body, line, text)
	switch {
	case err != nil:
		return Reopened{}, err
	case p.Box == ' ':
		return Reopened{}, ErrLineChanged
	case p.Box == '>':
		return Reopened{}, ErrCarried
	}
	if err := checkLine(p); err != nil {
		return Reopened{}, err
	}
	r := Reopened{Body: body}
	// The edits go from the end of the body to its start, so that each offset stays valid.
	if p.Box == 'x' && p.Every != "" {
		if next, ok := nextLineOf(p, Plans(body)); ok {
			r.Body, r.Removed, r.RemovedLine = cutLine(body, next), next.Raw, next.Line
		} else {
			r.NextKept = true
		}
	}
	if p.Done != "" {
		for _, t := range p.Tokens {
			if strings.HasPrefix(body[t[0]:t[1]], "done:") {
				r.Body = r.Body[:t[0]-1] + r.Body[t[1]:]
			}
		}
	}
	r.Body = r.Body[:p.BoxAt+1] + " " + r.Body[p.BoxAt+2:]
	return r, nil
}

// nextLineOf finds the line that a tick of p added: the last open line after p that equals what Next gives.
func nextLineOf(p Plan, all []Plan) (Plan, bool) {
	if p.Done == "" {
		return Plan{}, false
	}
	on, due, every, err := Next(p, p.Done)
	if err != nil {
		return Plan{}, false
	}
	for i := len(all) - 1; i >= 0; i-- {
		q := all[i]
		if q.Line > p.Line && q.Box == ' ' && q.Bad == "" && q.Text == p.Text && q.On == on && q.Due == due &&
			q.Every == every && q.FromDone == p.FromDone {
			return q, true
		}
	}
	return Plan{}, false
}

// breakBefore is the length of the line break that ends just before the offset at.
func breakBefore(body string, at int) int {
	switch {
	case at >= 2 && body[at-2:at] == "\r\n":
		return 2
	case at >= 1 && (body[at-1] == '\n' || body[at-1] == '\r'):
		return 1
	}
	return 0
}

// breakAt is the length of the line break that starts at the offset at.
func breakAt(body string, at int) int {
	switch {
	case strings.HasPrefix(body[at:], "\r\n"):
		return 2
	case at < len(body) && (body[at] == '\n' || body[at] == '\r'):
		return 1
	}
	return 0
}

// cutLine removes the plan line q from the body, with its line break. At the end of the body it removes the line
// breaks before it instead: two LF, else up to two line breaks. In the middle, if a blank line is before and after it, one blank
// line goes too, so that two blank lines do not meet.
func cutLine(body string, q Plan) string {
	start := lineStarts([]byte(body))[q.Line-1]
	end := start + len(q.Raw)
	if end == len(body) {
		if strings.HasSuffix(body[:start], "\n\n") {
			return body[:start-2] // what a tick wrote
		}
		for i := 0; i < 2; i++ {
			start -= breakBefore(body, start)
		}
		return body[:start]
	}
	end += breakAt(body, end)
	blankBefore := breakBefore(body, start) > 0 && breakBefore(body, start-breakBefore(body, start)) > 0
	if blankBefore {
		end += breakAt(body, end)
	}
	return body[:start] + body[end:]
}
