package importer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// Outcome is what one write did, or would do: new, existing, promoted or updated (the guide's statuses).
type Outcome struct {
	Write  int    `json:"write"`
	Kind   string `json:"kind"`
	What   string `json:"what"`
	Status string `json:"status"`
}

// Report is what check-facts would do or apply-facts did with one file.
type Report struct {
	File     string    `json:"file"`
	Applied  bool      `json:"applied"`
	Outcomes []Outcome `json:"writes"`
	Kept     int       `json:"kept_as_text"`
	Waiting  []string  `json:"waiting,omitempty"`
	Summary  string    `json:"summary"`
}

// Check runs every check of a facts file and what apply would write, then rolls the transaction back.
func (w *Workspace) Check(ctx context.Context, s *core.Store, file string) (*Report, error) {
	return w.run(ctx, s, file, true)
}

// Apply runs the same checks and writes the file's facts in one transaction, then its ledger line.
func (w *Workspace) Apply(ctx context.Context, s *core.Store, file string) (*Report, error) {
	return w.run(ctx, s, file, false)
}

func (w *Workspace) run(ctx context.Context, s *core.Store, file string, dry bool) (*Report, error) {
	f, pos, rules, err := w.prepare(file)
	if err != nil {
		return nil, err
	}
	r, err := w.write(ctx, s, f, pos, rules, dry)
	if err != nil || dry {
		return r, err
	}
	newState := "x"
	if len(f.Waiting) > 0 {
		newState = "?"
	}
	r.Applied = true
	return r, w.mark(file, func(l *Line) error {
		l.State, l.Note = newState, r.Summary
		return nil
	})
}

// prepare runs everything that needs no database: the ledger (before any database work), the rules gate, the
// facts file and the checks against its source file.
func (w *Workspace) prepare(file string) (*Facts, []int, *Rules, error) {
	state, err := w.ledgerState(file)
	if err != nil {
		return nil, nil, nil, err
	}
	if state == "" || state == "-" {
		return nil, nil, nil, refuse("%s is not a ledger file to import (unknown or skipped)", file)
	}
	if g, err := w.Gate("rules.md"); err != nil {
		return nil, nil, nil, err
	} else if g != "approved" {
		return nil, nil, nil, refuse("rules.md is %s: the owner approves it first (lifelog import approve rules)", g)
	}
	rules, err := w.Rules()
	if err != nil {
		return nil, nil, nil, err
	}
	approved, err := w.ApprovedMetrics()
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := w.LoadFacts(file)
	if err != nil {
		return nil, nil, nil, err
	}
	src, err := w.readRawSource(file)
	if err != nil {
		return nil, nil, nil, err
	}
	pos, errs := w.checkStatic(f, src, rules, approved)
	if len(errs) > 0 {
		return nil, nil, nil, refuse("%s: %s", file, strings.Join(errs, "; "))
	}
	return f, pos, rules, nil
}

// write applies a checked facts file in one transaction (rolled back when dry): the guide's checks against
// life.db, then each write through the same core operations the API runs.
func (w *Workspace) write(ctx context.Context, s *core.Store, f *Facts, pos []int, rules *Rules, dry bool) (*Report, error) {
	r := &Report{File: f.File, Kept: len(f.KeptAsText), Waiting: f.Waiting, Outcomes: []Outcome{}}
	fn := func(t *core.Tx) error {
		keys, err := resolveReadingKeys(t, rules.Source, f, pos)
		if err != nil {
			return err
		}
		for i, wr := range f.Writes {
			o, err := applyWrite(t, f.File, wr, keys.byWrite[i], rules)
			if err != nil {
				e := refuse("%s: write %d (%s): %v", f.File, i+1, wr.kind(), err)
				if errors.Is(err, errNotYet) {
					return &notYet{e.(*core.Error)}
				}
				return e
			}
			o.Write = i + 1
			r.Outcomes = append(r.Outcomes, o)
		}
		return nil
	}
	var err error
	if dry {
		err = s.DryRun(ctx, rules.Source, fn)
	} else {
		err = s.Do(ctx, rules.Source, fn)
	}
	r.Summary = summary(r)
	return r, err
}

func applyWrite(t *core.Tx, file string, wr Write, key string, rules *Rules) (Outcome, error) {
	switch wr.kind() {
	case "person":
		p := wr.Person
		return named(t, rules, "person", p.Title, entityKey(file, "person", p.Title), func(k string) (int64, bool, error) {
			return t.CreatePerson(p.Title, p.Name, p.BirthDay, p.DeathDay, k)
		}, p.Name, func(id int64) (bool, error) {
			return t.FillPersonDays(id, p.BirthDay, p.DeathDay)
		})
	case "place":
		return named(t, rules, "place", wr.Place.Title, entityKey(file, "place", wr.Place.Title), func(k string) (int64, bool, error) {
			return t.CreatePlace(wr.Place.Title, k)
		}, "", nil)
	case "page":
		title := wr.Page.Title
		p, err := t.Lookup(title)
		if err != nil {
			return Outcome{}, err
		}
		if p != nil {
			if p.Type != "page" {
				return Outcome{}, fmt.Errorf("%q is held by a %s, not a page", title, p.Type)
			}
			return Outcome{Kind: "page", What: title, Status: "existing"}, nil
		}
		if err := lookAlike(t, rules, title); err != nil {
			return Outcome{}, err
		}
		_, existing, err := t.CreateImported(title, nil, entityKey(file, "page", title))
		return Outcome{Kind: "page", What: title, Status: status(existing)}, err
	case "link":
		l := wr.Link
		from, err := resolve(t, l.From)
		if err != nil {
			return Outcome{}, err
		}
		to, err := resolve(t, l.To)
		if err != nil {
			return Outcome{}, err
		}
		if err := notPromotedYet(t, l); err != nil {
			return Outcome{}, err
		}
		added, err := t.Link(from, to, l.Kind, l.Note)
		return Outcome{Kind: "link", What: fmt.Sprintf("%s %s %s", l.From, l.Kind, l.To), Status: status(!added)}, err
	case "reading":
		rd := wr.Reading
		unit, found, err := t.MetricUnit(rd.Metric)
		if err != nil {
			return Outcome{}, err
		}
		if !found {
			return Outcome{}, fmt.Errorf("metric %s is approved but not registered: run register-metrics", rd.Metric)
		}
		num, _, u, err := parseValue(rd.Value, rd.Unit)
		if err != nil {
			return Outcome{}, err
		}
		if u != unit {
			return Outcome{}, fmt.Errorf("the unit %q is not %s's unit %q: nothing is converted", u, rd.Metric, unit)
		}
		var with int64
		if rd.With != "" {
			if with, err = resolve(t, rd.With); err != nil {
				return Outcome{}, err
			}
		}
		what := fmt.Sprintf("%s %s on %s", rd.Metric, rd.Value, rd.Day)
		if v, found, err := t.KeyedValue(rd.Metric, key); err != nil {
			return Outcome{}, err
		} else if found {
			if v != num {
				return Outcome{}, fmt.Errorf("its key already holds %g, not %g: a correction is the owner's (correct a measurement)", v, num)
			}
			return Outcome{Kind: "reading", What: what, Status: "existing"}, nil
		}
		_, err = t.Record(core.Reading{Metric: rd.Metric, Day: rd.Day, TakenAt: rd.TakenAt, TZ: rd.TZ, Value: num, Key: key, CapturedWith: with})
		return Outcome{Kind: "reading", What: what, Status: "new"}, err
	}
	return Outcome{}, fmt.Errorf("unknown write")
}

func status(existing bool) string {
	if existing {
		return "existing"
	}
	return "new"
}

// named writes a person or a place: an exact title is that row (revived if tombstoned), a plain page holding
// the title is promoted (never a day page or a stub), a new title passes the look-alike check first. fill, when
// given, writes what the row lacks (a person's days) and says whether it wrote: an existing row it fills is updated.
func named(t *core.Tx, rules *Rules, typ, title, key string, create func(string) (int64, bool, error), name string, fill func(int64) (bool, error)) (Outcome, error) {
	o := Outcome{Kind: typ, What: title}
	p, err := t.Lookup(title)
	if err != nil {
		return o, err
	}
	filled := func(id int64) (Outcome, error) {
		if fill == nil {
			return o, nil
		}
		changed, err := fill(id)
		if changed && o.Status == "existing" {
			o.Status = "updated"
		}
		return o, err
	}
	switch {
	case p == nil:
		if err := lookAlike(t, rules, title); err != nil {
			return o, err
		}
		id, existing, err := create(key)
		o.Status = status(existing)
		if err != nil || !existing {
			return o, err
		}
		return filled(id)
	case p.Type == typ:
		if p.Deleted {
			if err := t.Revive(p.ID); err != nil {
				return o, err
			}
		}
		o.Status = "existing"
		return filled(p.ID)
	case p.Type == "page" && p.DayPage:
		return o, fmt.Errorf("%q is a day page or a redirect stub, never a %s", title, typ)
	case p.Type == "page":
		o.Status = "promoted"
		if err := t.Promote(p.ID, typ, name); err != nil {
			return o, err
		}
		return filled(p.ID)
	}
	return o, fmt.Errorf("%q is held by a %s, not a %s", title, p.Type, typ)
}

// errNotYet is a reference to a row another write makes: a title no live row holds, or a plain page that a link
// needs as a person or a place before the file that promotes it is applied. Applied alone, the file is refused;
// a replay applies it again after the rest (the guide's "Trial, then the real run").
var errNotYet = errors.New("not written yet")

// notYet is a file's refusal caused by errNotYet: still a core.Error (a 422), and errors.Is(err, errNotYet).
type notYet struct{ refusal *core.Error }

func (e *notYet) Error() string   { return e.refusal.Msg }
func (e *notYet) Unwrap() []error { return []error{e.refusal, errNotYet} }

// waitsForAnother reports whether a file was refused only for a row another file writes.
func waitsForAnother(err error) bool { return errors.Is(err, errNotYet) }

// resolve is a reference by title: it must name a live row already written.
func resolve(t *core.Tx, title string) (int64, error) {
	p, err := t.Lookup(title)
	if err != nil {
		return 0, err
	}
	if p == nil || p.Deleted {
		return 0, fmt.Errorf("%q is %w: write it earlier in the file, or apply the other file first", title, errNotYet)
	}
	return p.ID, nil
}

// notPromotedYet refuses, as not written yet, a link end that is a plain page where the kind needs a person or a
// place (link_kinds.from_types / to_types): a person or place write promotes it, in this file or another. Any other
// type the kind does not accept (a day page, a stub, a person where a place is needed) is left to the registry's
// own refusal.
func notPromotedYet(t *core.Tx, l *LinkW) error {
	fromTypes, toTypes, found, err := t.LinkEnds(l.Kind)
	if err != nil || !found {
		return err // an unknown kind is refused by the registry
	}
	for _, end := range []struct {
		title string
		types []string
	}{{l.From, fromTypes}, {l.To, toTypes}} {
		if end.types == nil || slices.Contains(end.types, "page") {
			continue
		}
		var wanted []string
		for _, typ := range []string{"person", "place"} {
			if slices.Contains(end.types, typ) {
				wanted = append(wanted, typ)
			}
		}
		p, err := t.Lookup(end.title)
		if err != nil {
			return err
		}
		if len(wanted) == 0 || p.Type != "page" || p.DayPage {
			continue
		}
		need := strings.Join(wanted, " or ")
		return fmt.Errorf("%q is still a plain page, and the %s link needs a %s there: that %s is %w; write it earlier in the file, or apply the file that writes it first",
			end.title, l.Kind, need, need, errNotYet)
	}
	return nil
}

// ---- look-alikes and find

// words are a name's words, case-folded: letters and digits, everything else a separator.
func words(s string) []string {
	f := strings.FieldsFunc(text.TitleKey(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) })
	sort.Strings(f)
	return f
}

// relation is how a candidate's words relate to a name's: exact, same words, more words, fewer words, or "".
func relation(name, candidate string) string {
	if text.TitleKey(name) == text.TitleKey(candidate) {
		return "exact"
	}
	a, b := set(words(name)), set(words(candidate))
	if len(a) == 0 || len(b) == 0 {
		return ""
	}
	inA, inB := 0, 0
	for x := range a {
		if b[x] {
			inA++
		}
	}
	for x := range b {
		if a[x] {
			inB++
		}
	}
	switch {
	case inA == len(a) && inB == len(b):
		return "same words"
	case inA == len(a):
		return "more words"
	case inB == len(b):
		return "fewer words"
	}
	return ""
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// lookAlike refuses a new name that shares its words with an existing person, place or page, unless rules.md's
// Distinct says they differ: the model never merges or duplicates without the owner.
func lookAlike(t *core.Tx, rules *Rules, title string) error {
	names, err := t.Names()
	if err != nil {
		return err
	}
	for _, n := range names {
		for _, cand := range append([]string{n.Title, n.Name}, n.Aliases...) {
			if cand == "" {
				continue
			}
			rel := relation(title, cand)
			if rel == "" || rel == "exact" || rules.Distinct[[2]string{nameKey(title), nameKey(cand)}] {
				continue
			}
			return fmt.Errorf("%q looks like the %s %q (%s): ask the owner, then add an alias or a Distinct line to rules.md", title, n.Type, n.Title, rel)
		}
	}
	return nil
}

// Match is one thing find found.
type Match struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Href     string `json:"href,omitempty"`
	Relation string `json:"match"`
}

// Find is the guide's find: pages (people and places included) and metrics matching a text.
func Find(ctx context.Context, s *core.Store, q string) ([]Match, error) {
	names, err := s.Names(ctx)
	if err != nil {
		return nil, err
	}
	out := []Match{}
	for _, n := range names {
		best := ""
		for _, cand := range append([]string{n.Title, n.Name}, n.Aliases...) {
			if cand == "" {
				continue
			}
			rel := relation(q, cand)
			if n.Type == "metric" {
				normalized := relation(strings.ReplaceAll(q, "_", " "), strings.ReplaceAll(cand, "_", " "))
				if normalized != "" && (rel == "" || rank(normalized) < rank(rel)) {
					rel = normalized
				}
			}
			if rel != "" && (best == "" || rank(rel) < rank(best)) {
				best = rel
			}
		}
		if best != "" {
			out = append(out, Match{n.Type, n.Title, fmt.Sprintf("/pages/%d", n.ID), best})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return rank(out[a].Relation) < rank(out[b].Relation) })
	return out, nil
}

func rank(rel string) int {
	return map[string]int{"exact": 0, "same words": 1, "more words": 2, "fewer words": 3}[rel]
}

// summary is the ledger's note for a file: counts by kind, then by status.
func summary(r *Report) string {
	kinds := map[string]int{}
	stat := map[string]int{}
	for _, o := range r.Outcomes {
		kinds[o.Kind]++
		stat[o.Status]++
	}
	var parts []string
	for _, k := range []string{"page", "place", "person", "link", "reading"} {
		if n := kinds[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	s := strings.Join(parts, ", ")
	if s == "" {
		s = "nothing written"
	}
	var st []string
	for _, k := range []string{"new", "existing", "promoted", "updated"} {
		if n := stat[k]; n > 0 {
			st = append(st, fmt.Sprintf("%d %s", n, k))
		}
	}
	if len(st) > 0 {
		s += " (" + strings.Join(st, ", ") + ")"
	}
	if r.Kept > 0 {
		s += fmt.Sprintf("; %d kept as text", r.Kept)
	}
	if len(r.Waiting) > 0 {
		s += "; waiting: " + strings.Join(r.Waiting, ", ")
	}
	return s
}
