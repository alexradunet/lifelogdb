package importer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
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

// Refusal is one write the writer would not make, with the class a client branches on (the guide's "The
// checks"): a look-alike lists its candidates, a reference not written yet names the title it waits for.
type Refusal struct {
	Write      int      `json:"write"`
	Kind       string   `json:"kind,omitempty"`
	What       string   `json:"what,omitempty"`
	Quote      string   `json:"quote"`
	Class      string   `json:"class"`
	Reason     string   `json:"reason"`
	Candidates []string `json:"candidates,omitempty"`
	Waits      string   `json:"waits,omitempty"`
}

// Report is what check-facts would do or apply-facts did with one file. Refused is every write check-facts
// would refuse, with its class; apply-facts refuses the whole file at the first of them instead.
type Report struct {
	File     string    `json:"file"`
	Applied  bool      `json:"applied"`
	Outcomes []Outcome `json:"writes"`
	Refused  []Refusal `json:"refused,omitempty"`
	Kept     int       `json:"kept_as_text"`
	Waiting  []string  `json:"waiting,omitempty"`
	Summary  string    `json:"summary"`
}

// Check runs every check of a facts file and what apply would write, then rolls the transaction back. Every
// write the writer would refuse is in the report, each run in its own savepoint so the ones after it are checked
// too; a file-level refusal (a kept_as_text or waiting entry, a reading identity) is still an error.
func (w *Workspace) Check(ctx context.Context, s *core.Store, file string) (*Report, error) {
	return w.run(ctx, s, file, true, nil)
}

// check is Check for an operation that checks many files: it reads the file's state from the ledger it read once.
func (w *Workspace) check(ctx context.Context, s *core.Store, file string, idx ledgerIndex) (*Report, error) {
	return w.run(ctx, s, file, true, idx)
}

// Apply runs the same checks and writes the file's facts in one transaction, then its ledger line. A file is
// written whole or not at all: the first refused write refuses the file, with its class on the error.
func (w *Workspace) Apply(ctx context.Context, s *core.Store, file string) (*Report, error) {
	return w.run(ctx, s, file, false, nil)
}

// run checks a file, and applies it when not dry. idx is the ledger an operation read once; nil reads it now.
func (w *Workspace) run(ctx context.Context, s *core.Store, file string, dry bool, idx ledgerIndex) (*Report, error) {
	f, pos, rules, refused, err := w.prepare(file, true, idx)
	if err != nil {
		return nil, err
	}
	// an apply checks first: a file with a refused write writes nothing; a file that waits only for the owner's
	// name decisions says so on its ledger line, which stays to do
	r, err := w.write(ctx, s, f, pos, rules, true, refused)
	if err != nil || dry {
		return r, err
	}
	if len(r.Refused) > 0 {
		if note := heldNote(r.Refused); note != "" {
			if err := w.mark(file, func(l *Line) error {
				if l.toDo() {
					l.Note = note
				}
				return nil
			}); err != nil {
				return r, err
			}
		}
		return r, fileRefusal(file, r.Refused)
	}
	if r, err = w.write(ctx, s, f, pos, rules, false, nil); err != nil {
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
// facts file and the checks against its source file. Lenient, it returns the writes those checks refuse (a check
// reports them all); strict, any of them refuses the file (an apply writes whole or not at all). idx is the ledger an
// operation read once; nil reads it now.
func (w *Workspace) prepare(file string, lenient bool, idx ledgerIndex) (*Facts, []int, *Rules, []Refusal, error) {
	state, err := w.stateOf(file, idx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if state == "" || state == "-" {
		return nil, nil, nil, nil, refuse("%s is not a ledger file to import (unknown or skipped)", file)
	}
	if g, err := w.Gate("rules.md"); err != nil {
		return nil, nil, nil, nil, err
	} else if g != "approved" {
		return nil, nil, nil, nil, refuse("rules.md is %s: the owner approves it first (lifelog import approve rules)", g)
	}
	rules, err := w.factsRules()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	approved, err := w.ApprovedMetrics()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	f, err := w.LoadFacts(file)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	f = withDecisions(f, rules.Names)
	src, err := w.readRawSource(file)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	pos, refused, errs := w.checkStatic(f, src, rules, approved)
	if len(errs) > 0 {
		return nil, nil, nil, nil, refuse("%s: %s", file, strings.Join(errs, "; "))
	}
	if len(refused) > 0 && !lenient {
		return nil, nil, nil, nil, fileRefusal(file, refused)
	}
	return f, pos, rules, refused, nil
}

// factsRules are the rules a facts file is checked and written under: rules.md's source, with the owner's name
// decisions of a stamped entities.md and the aliases among them.
func (w *Workspace) factsRules() (*Rules, error) {
	rules, err := w.Rules()
	if err != nil {
		return nil, err
	}
	if rules.Names, err = w.nameDecisions(); err != nil {
		return nil, err
	}
	rules.Aliases = rules.Names.aliases()
	return rules, nil
}

// withDecisions is the facts with the owner's name decisions of entities.md applied. Every name the owner made an
// alias (an approved row with an as) is written as its title: a person, place or page under the alias of its kind, a
// link end or a reading's with under any alias. A reading's with that names a rejected name is taken out: the
// reading stays. The writes of a rejected name are skipped later (names.rejects), so that they keep their numbers.
// The facts file is not changed; its quotes still name the alias, which the checks accept.
func withDecisions(f *Facts, n names) *Facts {
	if len(n) == 0 {
		return f
	}
	all := n.aliases()
	of := func(kind, title string) string {
		if e, ok := n.of(kind, title); ok && e.Status == "approved" && e.As != "" {
			return e.As
		}
		return title
	}
	any := func(title string) string {
		if t, ok := all[nameKey(title)]; ok {
			return t
		}
		return title
	}
	out := *f
	out.Writes = make([]Write, len(f.Writes))
	for i, wr := range f.Writes {
		switch {
		case wr.Person != nil:
			p := *wr.Person
			p.Title = of("person", p.Title)
			wr.Person = &p
		case wr.Place != nil:
			p := *wr.Place
			p.Title = of("place", p.Title)
			wr.Place = &p
		case wr.Page != nil:
			p := *wr.Page
			p.Title = of("page", p.Title)
			wr.Page = &p
		case wr.Link != nil:
			l := *wr.Link
			l.From, l.To = any(l.From), any(l.To)
			wr.Link = &l
		case wr.Reading != nil && wr.Reading.With != "":
			r := *wr.Reading
			r.With = any(r.With)
			if n.rejected("", r.With) {
				r.rejectedWith, r.With = r.With, ""
			}
			wr.Reading = &r
		}
		out.Writes[i] = wr
	}
	return &out
}

// fileRefusal is a file refused for its writes: every refusal in the message, the first one's class on the error.
func fileRefusal(file string, refused []Refusal) error {
	parts := make([]string, len(refused))
	for i, x := range refused {
		parts[i] = fmt.Sprintf("write %d: %s", x.Write, x.Reason)
	}
	e := refuse("%s: %s", file, strings.Join(parts, "; ")).(*core.Error)
	e.Code = refused[0].Class
	if refused[0].Class == "not_yet" {
		return &notYet{e}
	}
	return e
}

// write applies a checked facts file in one transaction (rolled back when dry): the guide's checks against
// life.db, then each write through the same core operations the API runs. Dry, every write runs in its own
// savepoint and a refused one is reported, not fatal; the writes refused by the static checks are left out, so a
// reading's place among its day's readings is counted among the writes that could be written.
func (w *Workspace) write(ctx context.Context, s *core.Store, f *Facts, pos []int, rules *Rules, dry bool, refused []Refusal) (*Report, error) {
	r := &Report{File: f.File, Kept: len(f.KeptAsText), Waiting: f.Waiting, Outcomes: []Outcome{}, Refused: refused}
	skip := map[int]bool{}
	for _, x := range refused {
		skip[x.Write-1] = true
	}
	sub := &Facts{File: f.File}
	var subPos, index []int
	for i, wr := range f.Writes {
		if skip[i] {
			continue
		}
		if rules.Names.rejects(wr) {
			r.Outcomes = append(r.Outcomes, Outcome{Write: i + 1, Kind: wr.kind(), What: what(wr), Status: "rejected"})
			continue
		}
		sub.Writes = append(sub.Writes, wr)
		subPos = append(subPos, pos[i])
		index = append(index, i)
	}
	fn := func(t *core.Tx) error {
		keys, err := resolveReadingKeys(t, rules.Source, sub, subPos)
		if err != nil {
			return err
		}
		for j, wr := range sub.Writes {
			i := index[j]
			one := func() error {
				o, err := applyWrite(t, f.File, wr, keys.byWrite[j], rules)
				if err != nil {
					return err
				}
				o.Write = i + 1
				r.Outcomes = append(r.Outcomes, o)
				return nil
			}
			if !dry {
				if err := one(); err != nil {
					return fileRefusal(f.File, []Refusal{refusalOf(i, wr, err)})
				}
				continue
			}
			if err := t.Savepoint(one); err != nil {
				if !isRefusal(err) {
					return err // a failure of the database, not a refusal of the write: the check fails
				}
				r.Refused = append(r.Refused, refusalOf(i, wr, err))
			}
		}
		return nil
	}
	var err error
	if dry {
		err = s.DryRun(ctx, rules.Source, fn)
	} else {
		err = s.Do(ctx, rules.Source, fn)
	}
	sort.SliceStable(r.Refused, func(a, b int) bool { return r.Refused[a].Write < r.Refused[b].Write })
	sort.SliceStable(r.Outcomes, func(a, b int) bool { return r.Outcomes[a].Write < r.Outcomes[b].Write })
	r.Summary = summary(r)
	return r, err
}

// classed is a refusal with its class: what a client branches on, never the words.
type classed struct {
	class      string
	err        error
	candidates []string
	waits      string
}

func (e *classed) Error() string { return e.err.Error() }
func (e *classed) Unwrap() error { return e.err }

func classify(class string, err error) error {
	if err == nil {
		return nil
	}
	return &classed{class: class, err: err}
}

// isRefusal says whether an error refuses a write (a classed refusal, or a 4xx of core: a check or a constraint of
// the DDL), as opposed to a failure of the database or the context.
func isRefusal(err error) bool {
	var c *classed
	if errors.As(err, &c) {
		return true
	}
	var ce *core.Error
	return errors.As(err, &ce) && ce.Status >= 400 && ce.Status < 500
}

// refusalOf is a write's refusal from the error its apply returned: the class of a classed error, else "refused".
func refusalOf(i int, wr Write, err error) Refusal {
	x := Refusal{Write: i + 1, Kind: wr.kind(), What: what(wr), Quote: wr.Quote, Class: "refused", Reason: err.Error()}
	var c *classed
	if errors.As(err, &c) {
		x.Class, x.Candidates, x.Waits = c.class, c.candidates, c.waits
	}
	return x
}

// what names a write the way the report does: the title, the link, the reading.
func what(wr Write) string {
	switch wr.kind() {
	case "person":
		return wr.Person.Title
	case "place":
		return wr.Place.Title
	case "page":
		return wr.Page.Title
	case "link":
		return fmt.Sprintf("%s %s %s", wr.Link.From, wr.Link.Kind, wr.Link.To)
	case "reading":
		return fmt.Sprintf("%s %s on %s", wr.Reading.Metric, wr.Reading.Value, wr.Reading.Day)
	}
	return ""
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
				return Outcome{}, classify("taken", fmt.Errorf("%q is held by a %s, not a page", title, p.Type))
			}
			return Outcome{Kind: "page", What: title, Status: "existing"}, nil
		}
		// a new page needs the owner's decision only when it looks like a person, a place or another page (a page the
		// owner rejected never comes here: names.rejects skips its write)
		like, err := lookAlikes(t, title)
		if err != nil {
			return Outcome{}, err
		}
		if len(like) > 0 {
			if err := decide(rules, "page", title, "", like); err != nil {
				return Outcome{}, err
			}
		}
		_, existing, err := t.CreateImported(title, nil, "", entityKey(file, "page", title))
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
		if isRefusal(err) {
			// a kind the registry does not accept between these ends, or a kind that is not a facts link
			return Outcome{}, classify("link_endpoint", err)
		}
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Kind: "link", What: fmt.Sprintf("%s %s %s", l.From, l.Kind, l.To), Status: status(!added)}, nil
	case "reading":
		rd := wr.Reading
		unit, found, err := t.MetricUnit(rd.Metric)
		if err != nil {
			return Outcome{}, err
		}
		if !found {
			return Outcome{}, classify("metric", fmt.Errorf("metric %s is approved but not registered: run register-metrics", rd.Metric))
		}
		num, numText, u, err := parseValue(rd.Value, rd.Unit)
		if err != nil {
			return Outcome{}, classify("value", err)
		}
		// a scale's range is the metric's: a reading that writes none takes it (core.RangeUnit), and its value, held
		// to the range by the writer, must still be written in the quote: a word gives no scale's number, even when
		// metrics.md approved the metric without a unit
		if _, _, scale := core.RangeUnit(unit); scale {
			if u == "" {
				u = unit
			}
			if tokens, _ := matchingNumberTokens(collapse(wr.Quote), numText, "", -1); len(tokens) == 0 {
				return Outcome{}, classify("value", fmt.Errorf("%s is a %s scale: the quote has to write its number %s", rd.Metric, unit, numText))
			}
		}
		if u != unit {
			return Outcome{}, classify("unit", fmt.Errorf("the unit %q is not %s's unit %q: nothing is converted", u, rd.Metric, unit))
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
				return Outcome{}, classify("value", fmt.Errorf("its key already holds %g, not %g: a correction is the owner's (correct a measurement)", v, num))
			}
			return Outcome{Kind: "reading", What: what, Status: "existing"}, nil
		}
		_, err = t.Record(core.Reading{Metric: rd.Metric, Day: rd.Day, TakenAt: rd.TakenAt, TZ: rd.TZ, Value: num, Key: key, CapturedWith: with})
		return Outcome{Kind: "reading", What: what, Status: "new"}, err
	}
	return Outcome{}, classify("kind", fmt.Errorf("unknown write"))
}

func status(existing bool) string {
	if existing {
		return "existing"
	}
	return "new"
}

// named writes a person or a place. A live row of that kind holding the title is that row. Every other case needs
// the owner's decision in entities.md (decide): a new title, a tombstoned row of that kind (revived), a plain page
// holding the title (promoted; never a day page or a stub). fill, when given, writes what the row lacks (a person's
// days) and says whether it wrote: an existing row it fills is updated.
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
		var ce *core.Error
		if errors.As(err, &ce) && ce.Status == 422 {
			return o, classify("person_day", err) // the person holds another day: a correction is the owner's
		}
		return o, err
	}
	needs := func(why string) error {
		like, err := lookAlikes(t, title)
		if err != nil {
			return err
		}
		return decide(rules, typ, title, why, like)
	}
	switch {
	case p != nil && p.Type == typ && !p.Deleted:
		o.Status = "existing"
		return filled(p.ID)
	case p == nil:
		if err := needs(""); err != nil {
			return o, err
		}
		id, existing, err := create(key)
		o.Status = status(existing)
		if err != nil || !existing {
			return o, err
		}
		return filled(id)
	case p.Type == typ:
		if err := needs(fmt.Sprintf("revives the tombstoned %s %q", typ, title)); err != nil {
			return o, err
		}
		if err := t.Revive(p.ID); err != nil {
			return o, err
		}
		o.Status = "existing"
		return filled(p.ID)
	case p.Type == "page" && p.DayPage:
		return o, classify("taken", fmt.Errorf("%q is a day page or a redirect stub, never a %s", title, typ))
	case p.Type == "page":
		if err := needs(fmt.Sprintf("promotes the page %q", title)); err != nil {
			return o, err
		}
		o.Status = "promoted"
		if err := t.Promote(p.ID, typ, name); err != nil {
			return o, err
		}
		return filled(p.ID)
	}
	return o, classify("taken", fmt.Errorf("%q is held by a %s, not a %s", title, p.Type, typ))
}

// decide is the owner's decision on a name a write needs (entities.md): nil to write it; else the write is held
// until the owner decides (a rejected name never comes here: its write is skipped before the checks, names.rejects).
// why says what the write would do besides creating a row (revive a tombstoned one, promote a page); like is what
// the name looks like. Both go to the owner as candidates.
func decide(rules *Rules, kind, title, why string, like []string) error {
	if e, ok := rules.Names.of(kind, title); ok && e.Status == "approved" {
		return nil
	}
	reason := fmt.Sprintf("the %s %q waits for the owner's decision in entities.md (propose-entities, then lifelog import approve entities)", kind, title)
	candidates := like
	if why != "" {
		reason += "; it " + why
		candidates = append([]string{why}, like...)
	}
	if len(like) > 0 {
		reason += "; it looks like " + strings.Join(like, ", ")
	}
	return &classed{class: "held", candidates: candidates, err: errors.New(reason)}
}

// heldNote is the ledger note of a file that waits only for the owner's name decisions: `held: person "Cara",
// place "Lake"`, the held names in order; "" when any refusal is of another class (a not_yet reference waits too).
func heldNote(refused []Refusal) string {
	var held []string
	seen := map[string]bool{}
	for _, x := range refused {
		switch x.Class {
		case "held":
			if e := x.Kind + " " + strconv.Quote(x.What); !seen[e] {
				seen[e] = true
				held = append(held, e)
			}
		case "not_yet":
		default:
			return ""
		}
	}
	if len(held) == 0 {
		return ""
	}
	return "held: " + strings.Join(held, ", ")
}

var heldEntry = regexp.MustCompile(`(person|place|page) ("(?:[^"\\]|\\.)*")`)

// heldNames are the kinds and names of a held file's ledger note.
func heldNames(note string) [][2]string {
	_, after, ok := strings.Cut(note, "held: ")
	if !ok {
		return nil
	}
	var out [][2]string
	for _, m := range heldEntry.FindAllStringSubmatch(after, -1) {
		if name, err := strconv.Unquote(m[2]); err == nil {
			out = append(out, [2]string{m[1], name})
		}
	}
	return out
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

// notYetFor is a write refused for a title not written yet: class not_yet, waiting for that title.
func notYetFor(title string, err error) error {
	return &classed{class: "not_yet", err: err, waits: title}
}

// resolve is a reference by title: it must name a live row already written.
func resolve(t *core.Tx, title string) (int64, error) {
	p, err := t.Lookup(title)
	if err != nil {
		return 0, err
	}
	if p == nil || p.Deleted {
		return 0, notYetFor(title, fmt.Errorf("%q is %w: write it earlier in the file, or apply the other file first", title, errNotYet))
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
		return notYetFor(end.title, fmt.Errorf("%q is still a plain page, and the %s link needs a %s there: that %s is %w; write it earlier in the file, or apply the file that writes it first",
			end.title, l.Kind, need, need, errNotYet))
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

// lookAlikes lists every live person, place or plain page a new name shares its words with — the kinds that can be
// the same identity, never a day page, a metric, a file or a period — as "the <kind> "<title>" (<relation>)", the
// best relation first. The owner reads them in entities.md; the model never merges or duplicates a name.
func lookAlikes(t *core.Tx, title string) ([]string, error) {
	names, err := t.Names()
	if err != nil {
		return nil, err
	}
	type hit struct {
		text string
		rel  string
	}
	var hits []hit
	for _, n := range names {
		if (n.Type != "person" && n.Type != "place" && n.Type != "page") || core.IsDay(n.Title) {
			continue
		}
		best := ""
		for _, cand := range append([]string{n.Title, n.Name}, n.Aliases...) {
			if cand == "" {
				continue
			}
			rel := relation(title, cand)
			if rel == "" || rel == "exact" {
				continue
			}
			if best == "" || rank(rel) < rank(best) {
				best = rel
			}
		}
		if best != "" {
			hits = append(hits, hit{fmt.Sprintf("the %s %q (%s)", n.Type, n.Title, best), best})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return rank(hits[a].rel) < rank(hits[b].rel) })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.text
	}
	return out, nil
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

// summary is the ledger's note for a file: counts by kind, then by status, of what is written; then the writes the
// owner's rejections skip; a check adds what it refused.
func summary(r *Report) string {
	kinds := map[string]int{}
	stat := map[string]int{}
	for _, o := range r.Outcomes {
		if o.Status == "rejected" {
			stat[o.Status]++
			continue
		}
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
	if n := stat["rejected"]; n > 0 {
		s += fmt.Sprintf("; %d rejected", n)
	}
	if len(r.Refused) > 0 {
		s += fmt.Sprintf("; %d refused", len(r.Refused))
	}
	if r.Kept > 0 {
		s += fmt.Sprintf("; %d kept as text", r.Kept)
	}
	if len(r.Waiting) > 0 {
		s += "; waiting: " + strings.Join(r.Waiting, ", ")
	}
	return s
}
