package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// Facts is one facts file (the guide's "The facts file"). An unknown field is an error.
type Facts struct {
	File       string   `json:"file"`
	Writes     []Write  `json:"writes"`
	Waiting    []string `json:"waiting,omitempty"`
	KeptAsText []Kept   `json:"kept_as_text,omitempty"`
}

type Write struct {
	Person  *Person   `json:"person,omitempty"`
	Place   *Named    `json:"place,omitempty"`
	Page    *Named    `json:"page,omitempty"`
	Link    *LinkW    `json:"link,omitempty"`
	Reading *ReadingW `json:"reading,omitempty"`
	// there is no event and no task (D22, D23): these exist only to refuse them with the reason
	Event json.RawMessage `json:"event,omitempty"`
	Task  json.RawMessage `json:"task,omitempty"`
	Quote string          `json:"quote"`
}

type Person struct {
	Title    string `json:"title"`
	Name     string `json:"name,omitempty"`
	BirthDay string `json:"birth_day,omitempty"`
	DeathDay string `json:"death_day,omitempty"`
}

type Named struct {
	Title string `json:"title"`
}

type LinkW struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Note string `json:"note,omitempty"`
}

type ReadingW struct {
	Metric  string `json:"metric"`
	Day     string `json:"day"`
	Value   string `json:"value"`
	Unit    string `json:"unit,omitempty"`
	TakenAt string `json:"taken_at,omitempty"`
	TZ      string `json:"tz,omitempty"`
	With    string `json:"with,omitempty"`
}

type Kept struct {
	Quote string `json:"quote"`
	Why   string `json:"why"`
}

// kind names a write's one kind; "" when it has none or more than one.
func (wr Write) kind() string {
	var ks []string
	if wr.Person != nil {
		ks = append(ks, "person")
	}
	if wr.Place != nil {
		ks = append(ks, "place")
	}
	if wr.Page != nil {
		ks = append(ks, "page")
	}
	if wr.Link != nil {
		ks = append(ks, "link")
	}
	if wr.Reading != nil {
		ks = append(ks, "reading")
	}
	if len(wr.Event) > 0 {
		ks = append(ks, "event")
	}
	if len(wr.Task) > 0 {
		ks = append(ks, "task")
	}
	if len(ks) != 1 {
		return ""
	}
	return ks[0]
}

func parseFacts(data []byte) (*Facts, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f Facts
	if err := dec.Decode(&f); err != nil {
		return nil, refuse("facts file: %v", err)
	}
	if dec.More() {
		return nil, refuse("facts file: one JSON object only")
	}
	if f.Writes == nil {
		return nil, refuse("facts file: writes is required (it may be [])")
	}
	return &f, nil
}

func (w *Workspace) factsPath(file string) (string, error) {
	if _, err := w.SourcePath(file); err != nil {
		return "", err
	}
	return filepath.Join(w.Dir, "facts", filepath.FromSlash(file)+".json"), nil
}

// WriteFacts stores the model's facts file for a source file, after checking its shape (the content is
// checked by CheckFacts). The source file must be in the ledger and not skipped.
func (w *Workspace) WriteFacts(file string, data []byte) error {
	f, err := parseFacts(data)
	if err != nil {
		return err
	}
	if f.File != file {
		return refuse("the facts say file %q, written for %q", f.File, file)
	}
	if state, err := w.ledgerState(file); err != nil {
		return err
	} else if state == "" || state == "-" {
		return refuse("%s is not a ledger file to import (unknown or skipped)", file)
	}
	p, err := w.factsPath(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, data, "", "  ")
	return writeAtomic(p, pretty.Bytes())
}

// LoadFacts reads a stored facts file.
func (w *Workspace) LoadFacts(file string) (*Facts, error) {
	p, err := w.factsPath(file)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &core.Error{Status: 404, Msg: "no facts file for " + file + " yet"}
	}
	if err != nil {
		return nil, err
	}
	f, err := parseFacts(data)
	if err != nil {
		return nil, err
	}
	if f.File != file {
		return nil, refuse("the facts file for %s says file %q", file, f.File)
	}
	return f, nil
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(p, append(b, '\n'))
}

// ---- the checks against the source file and the workspace (no database)

// collapse is how quote and file are compared: NFC, runs of whitespace as one space.
func collapse(s string) string { return strings.Join(strings.Fields(norm.NFC.String(s)), " ") }

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_'
}

// wholeIndex finds needle in hay as whole words or tokens: the match neither starts nor ends inside a word.
// It returns the byte index of the first such match, or -1.
func wholeIndex(hay, needle string) int {
	if needle == "" {
		return -1
	}
	for from := 0; from <= len(hay)-len(needle); {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return -1
		}
		i += from
		before, after := rune(0), rune(0)
		if i > 0 {
			before = lastRune(hay[:i])
		}
		if j := i + len(needle); j < len(hay) {
			after = firstRune(hay[j:])
		}
		first, last := firstRune(needle), lastRune(needle)
		if !(isWordChar(first) && isWordChar(before)) && !(isWordChar(last) && isWordChar(after)) {
			return i
		}
		from = i + 1
	}
	return -1
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	rs := []rune(s)
	if len(rs) == 0 {
		return 0
	}
	return rs[len(rs)-1]
}

// containsFold is a whole-word match ignoring case.
func containsFold(hay, needle string) bool {
	return wholeIndex(strings.ToLower(collapse(hay)), strings.ToLower(collapse(needle))) >= 0
}

var (
	numberRE    = regexp.MustCompile(`^([+-]?\d+(?:\.\d+)?)\s*(.*)$`)
	questionNum = regexp.MustCompile(`\bQ\d+\b|#\d+\b`)
	fileDayRE   = regexp.MustCompile(`(?:^|/)(\d{4}-\d{2}-\d{2})\.md$`)
	frontDayRE  = regexp.MustCompile(`(?m)^(?:date|day|created):\s*"?(\d{4}-\d{2}-\d{2})`)
)

// parseValue reads a reading's value as written: a number and an optional unit ("48 ng/mL", "71.4").
// A comma decimal, a censored, approximate or qualitative value is refused.
func parseValue(v, unit string) (num float64, text, u string, err error) {
	v = strings.TrimSpace(v)
	m := numberRE.FindStringSubmatch(v)
	if m == nil || strings.ContainsAny(m[2], ",") && regexp.MustCompile(`^,\d`).MatchString(m[2]) {
		return 0, "", "", refuse("value %q is not a plain number (a censored, approximate, qualitative or comma-decimal value goes in kept_as_text)", v)
	}
	num, perr := strconv.ParseFloat(m[1], 64)
	if perr != nil {
		return 0, "", "", refuse("value %q is not a number", v)
	}
	u = strings.TrimSpace(m[2])
	if u != "" && unit != "" {
		return 0, "", "", refuse("value %q carries a unit and unit is %q too: write one", v, unit)
	}
	if u == "" {
		u = strings.TrimSpace(unit)
	}
	return num, m[1], u, nil
}

// fileDay is the day a file is of: its YYYY-MM-DD file name, else a date in its frontmatter.
func fileDay(file, text string) string {
	if m := fileDayRE.FindStringSubmatch(file); m != nil && core.IsDay(m[1]) {
		return m[1]
	}
	if strings.HasPrefix(text, "---") {
		if end := strings.Index(text[3:], "\n---"); end > 0 {
			if m := frontDayRE.FindStringSubmatch(text[3 : 3+end]); m != nil && core.IsDay(m[1]) {
				return m[1]
			}
		}
	}
	return ""
}

// ownTitle is the title a file is the note of: its title in the vault plan, else its file name without its
// extension. Like a daily note's day, it is evidence from the file itself: a note names its own title.
func (w *Workspace) ownTitle(file string) string {
	if p, ok, err := w.LoadPlan(); err == nil && ok {
		for _, n := range p.Notes {
			if n.Path == file {
				return n.Title
			}
		}
	}
	return norm.NFC.String(strings.TrimSuffix(path.Base(file), path.Ext(file)))
}

// checkStatic is the guide's first list of checks: against the source file and the workspace only.
// It also resolves aliases and returns, per write, the position of its quote in the source (for reading keys).
func (w *Workspace) checkStatic(f *Facts, source string, rules *Rules, approved map[string]Metric) (pos []int, errs []string) {
	src := collapse(source)
	srcLower := strings.ToLower(src)
	day := fileDay(f.File, source)
	own := w.ownTitle(f.File)
	pos = make([]int, len(f.Writes))
	bad := func(i int, format string, a ...any) {
		errs = append(errs, fmt.Sprintf("write %d: ", i+1)+fmt.Sprintf(format, a...))
	}
	for i, wr := range f.Writes {
		q := collapse(wr.Quote)
		pos[i] = wholeIndex(src, q)
		k := wr.kind()
		switch {
		case len(wr.Event) > 0 && k == "event":
			bad(i, "there is no event (D22): a daily note is its day's page and its text says what happened")
			continue
		case len(wr.Task) > 0 && k == "task":
			bad(i, "there is no task (D23): the page keeps the words of a plan")
			continue
		case k == "":
			bad(i, "a write has exactly one kind: person, place, page, link or reading")
			continue
		case q == "":
			bad(i, "the quote is empty")
			continue
		case pos[i] < 0:
			bad(i, "the quote %q is not in %s (as whole words)", wr.Quote, f.File)
			continue
		case len([]rune(q)) < 3:
			bad(i, "the quote %q is too short to state anything", wr.Quote)
			continue
		}
		hasNum := func(s string) bool {
			return questionNum.MatchString(s) && !questionNum.MatchString(q)
		}
		states := func(title string) bool { // the quote names the title, or a name an alias maps to it; a note names its own title, a daily note its own day
			if containsFold(q, title) || (day != "" && title == day) || (own != "" && text.TitleKey(title) == text.TitleKey(own)) {
				return true
			}
			for name, t := range rules.Aliases {
				if nameKey(t) == nameKey(title) && containsFold(q, name) {
					return true
				}
			}
			return false
		}
		checkTitle := func(title string) {
			if t, ok := rules.Aliases[nameKey(title)]; ok && nameKey(t) != nameKey(title) {
				bad(i, "%q is an alias of %q in rules.md: write %q", title, t, t)
			}
			if hasNum(title) {
				bad(i, "%q holds a question or row number its quote does not", title)
			}
		}
		switch k {
		case "person":
			checkTitle(wr.Person.Title)
			if hasNum(wr.Person.Name) {
				bad(i, "the name %q holds a question or row number", wr.Person.Name)
			}
			if !states(wr.Person.Title) && (wr.Person.Name == "" || !containsFold(q, wr.Person.Name)) {
				bad(i, "the quote does not name %q", wr.Person.Title)
			}
			for _, d := range [2][2]string{{"birth_day", wr.Person.BirthDay}, {"death_day", wr.Person.DeathDay}} {
				switch {
				case d[1] == "":
				case !core.IsDay(d[1]):
					bad(i, "%s %q is not YYYY-MM-DD", d[0], d[1])
				case !containsFold(q, d[1]):
					bad(i, "the %s %s is not in the quote", d[0], d[1])
				}
			}
		case "place":
			checkTitle(wr.Place.Title)
			if !states(wr.Place.Title) {
				bad(i, "the quote does not name %q", wr.Place.Title)
			}
		case "page":
			checkTitle(wr.Page.Title)
			if !states(wr.Page.Title) {
				bad(i, "the quote does not name %q", wr.Page.Title)
			}
		case "link":
			l := wr.Link
			switch {
			case l.Kind == "":
				bad(i, "a link needs its kind")
			case l.Kind == "wikilink" || l.Kind == "redirect":
				bad(i, "a %s link is never written by a facts file: the body's text makes wikilinks", l.Kind)
			case strings.TrimSpace(l.From) == "" || strings.TrimSpace(l.To) == "":
				bad(i, "a link's ends are titles")
			}
			checkTitle(l.From)
			checkTitle(l.To)
			if hasNum(l.Note) {
				bad(i, "the note %q holds a question or row number", l.Note)
			}
			if !states(l.To) && !states(l.From) {
				bad(i, "the quote names neither end of the link")
			}
		case "reading":
			r := wr.Reading
			m, ok := approved[r.Metric]
			if !ok {
				bad(i, "metric %q is not approved in a stamped metrics.md", r.Metric)
				continue
			}
			if !core.IsDay(r.Day) {
				bad(i, "day %q is not YYYY-MM-DD", r.Day)
			} else if !containsFold(q, r.Day) && r.Day != day {
				bad(i, "the day %s is neither in the quote nor the file's own day", r.Day)
			}
			if r.TakenAt != "" && !core.IsInstant(r.TakenAt) {
				bad(i, "taken_at %q is not a UTC instant", r.TakenAt)
			}
			num, numText, _, err := parseValue(r.Value, r.Unit)
			if err != nil {
				bad(i, "%v", err)
				continue
			}
			marker := m.Unit == "" && (num == 0 || num == 1)
			if !marker && wholeIndex(q, numText) < 0 {
				bad(i, "the value %s is not in the quote as a whole number", numText)
			}
			if r.With != "" {
				checkTitle(r.With)
			}
		}
	}
	for i, k := range f.KeptAsText {
		if strings.TrimSpace(k.Quote) == "" || strings.TrimSpace(k.Why) == "" {
			errs = append(errs, fmt.Sprintf("kept_as_text %d: needs a quote and a why", i+1))
		} else if wholeIndex(src, collapse(k.Quote)) < 0 && !strings.Contains(srcLower, strings.ToLower(collapse(k.Quote))) {
			errs = append(errs, fmt.Sprintf("kept_as_text %d: the quote is not in %s", i+1, f.File))
		}
	}
	for _, q := range f.Waiting {
		if !regexp.MustCompile(`^Q\d+$`).MatchString(q) {
			errs = append(errs, fmt.Sprintf("waiting: %q is not a question id (Q<n>)", q))
		}
	}
	return pos, errs
}

// readingKeys derives each reading's key: the path, the metric, the day, and its taken_at or its place among
// that metric's readings of that day in the source file, by where its quote first appears (so reordering the
// facts file never changes a key).
func readingKeys(f *Facts, pos []int) map[int]string {
	type rk struct {
		i, pos int
	}
	groups := map[string][]rk{}
	keys := map[int]string{}
	for i, wr := range f.Writes {
		if wr.Reading == nil {
			continue
		}
		r := wr.Reading
		if r.TakenAt != "" {
			keys[i] = strings.Join([]string{f.File, "reading", r.Metric, r.Day, r.TakenAt}, "|")
			continue
		}
		g := r.Metric + "|" + r.Day
		groups[g] = append(groups[g], rk{i, pos[i]})
	}
	for g, rs := range groups {
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].pos < rs[b].pos })
		for n, x := range rs {
			keys[x.i] = strings.Join([]string{f.File, "reading", g, strconv.Itoa(n + 1)}, "|")
		}
	}
	return keys
}

func entityKey(file, kind, title string) string {
	return file + "|" + kind + "|" + text.TitleKey(title)
}
