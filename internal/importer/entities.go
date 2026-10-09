package importer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"lifelog/internal/core"
)

// ProposeEntities adds a proposed row to entities.md for every name the owner has not decided yet (the guide's
// "entities.md"): each name a held file waits for, with what it looks like; each person and place a done facts file
// writes, which a replay into a fresh database writes again; and each alias and distinct line of rules.md, which it
// moves out of rules.md. The model may run it: the writer chooses the names, and only the owner's stamp decides
// them. It returns the rows it added; with none, it writes nothing.
func (w *Workspace) ProposeEntities(ctx context.Context, s *core.Store) ([]Entity, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	rows, err := w.Entities()
	if err != nil {
		return nil, err
	}
	have := map[[2]string]bool{}
	for _, e := range rows {
		have[[2]string{e.Kind, nameKey(e.Name)}] = true
	}
	var added []Entity
	add := func(e Entity) {
		k := [2]string{e.Kind, nameKey(e.Name)}
		if e.Name == "" || have[k] {
			return
		}
		have[k] = true
		e.Status = "proposed"
		added = append(added, e)
	}

	lines, _, err := w.Ledger()
	if err != nil {
		return nil, err
	}
	// the names held files wait for, as a check sees them now: kind, name and look-alikes
	idx := indexLedger(lines)
	for _, l := range lines {
		if !l.toDo() || len(heldNames(l.Note)) == 0 {
			continue
		}
		r, err := w.check(ctx, s, l.File, idx)
		if err != nil {
			continue // a file the checks refuse as a whole waits for its facts, not for a name
		}
		for _, x := range r.Refused {
			if x.Class == "held" {
				add(Entity{Kind: x.Kind, Name: x.What, Like: strings.Join(x.Candidates, "; "), From: l.File})
			}
		}
	}
	// the persons and places done files write: a replay into a fresh database needs their decisions
	for _, l := range lines {
		if l.State != "x" && l.State != "?" {
			continue
		}
		f, err := w.LoadFacts(l.File)
		if err != nil {
			continue // a prepared source or a vault note has no facts file
		}
		for _, wr := range f.Writes {
			switch {
			case wr.Person != nil:
				add(Entity{Kind: "person", Name: wr.Person.Title, From: l.File})
			case wr.Place != nil:
				add(Entity{Kind: "place", Name: wr.Place.Title, From: l.File})
			}
		}
	}
	// the name decisions rules.md still holds move to entities.md
	var rules *Rules
	if g, err := w.Gate("rules.md"); err != nil {
		return nil, err
	} else if g != "missing" {
		if rules, err = w.Rules(); err != nil {
			return nil, err
		}
	}
	moved := rules != nil && len(rules.Legacy.Aliases)+len(rules.Legacy.Distinct) > 0
	if moved {
		kind := func(title string) string {
			k := "person"
			s.DryRun(ctx, rules.Source, func(t *core.Tx) error {
				if p, err := t.Lookup(title); err == nil && p != nil && (p.Type == "place" || p.Type == "page") {
					k = p.Type
				}
				return nil
			})
			return k
		}
		live := func(title string) bool {
			found := false
			s.DryRun(ctx, rules.Source, func(t *core.Tx) error {
				p, err := t.Lookup(title)
				found = err == nil && p != nil && !p.Deleted
				return nil
			})
			return found
		}
		for _, a := range rules.Legacy.Aliases {
			add(Entity{Kind: kind(a[1]), Name: a[0], As: a[1], From: "rules.md"})
		}
		for _, d := range rules.Legacy.Distinct { // a distinct name that is not a live row is a new one, decided
			for _, side := range [][2]string{{d[0], d[1]}, {d[1], d[0]}} {
				if !live(side[0]) {
					add(Entity{Kind: kind(side[1]), Name: side[0], Like: "distinct from " + side[1], From: "rules.md"})
				}
			}
		}
	}
	if len(added) == 0 && !moved {
		return nil, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(added) > 0 {
		text, ok, err := w.read(entitiesFile)
		if err != nil {
			return nil, err
		}
		if !ok {
			text = "status: draft\n\n" + entitiesHeader
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		for _, e := range added {
			text += entityRow(e)
		}
		if err := writeAtomic(w.file(entitiesFile), []byte(text)); err != nil {
			return nil, err
		}
	}
	if moved {
		text, _, err := w.read("rules.md")
		if err != nil {
			return added, err
		}
		_, rest := splitStatus(text)
		if err := writeAtomic(w.file("rules.md"), []byte("status: draft\n"+strings.TrimLeft(withoutNameSections(rest), "\n"))); err != nil {
			return added, err
		}
	}
	return added, nil
}

// undecided counts the names of held ledger lines that a stamped entities.md does not decide, and the names
// proposed rows hold: what the owner still has to decide.
func undecided(lines []Line, n names) (held, missing int) {
	for _, l := range lines {
		hs := heldNames(l.Note)
		if !l.toDo() || len(hs) == 0 {
			continue
		}
		held++
		for _, h := range hs {
			if _, ok := n.of(h[0], h[1]); !ok {
				missing++
			}
		}
	}
	return held, missing
}

// decided says whether every name a held ledger line waits for has a decision under the stamp.
func decided(l Line, n names) bool {
	for _, h := range heldNames(l.Note) {
		if _, ok := n.of(h[0], h[1]); !ok {
			return false
		}
	}
	return true
}

// doneNamesUndecided counts the persons and places that done facts files write and that no row of entities.md
// names: a replay into a fresh database would hold them.
func (w *Workspace) doneNamesUndecided(lines []Line) int {
	rows, err := w.Entities()
	if err != nil {
		return 0
	}
	have := map[[2]string]bool{}
	for _, e := range rows {
		have[[2]string{e.Kind, nameKey(e.Name)}] = true
	}
	count := 0
	for _, l := range lines {
		if l.State != "x" && l.State != "?" {
			continue
		}
		f, err := w.LoadFacts(l.File)
		if err != nil {
			continue
		}
		for _, wr := range f.Writes {
			k := [2]string{}
			switch {
			case wr.Person != nil:
				k = [2]string{"person", nameKey(wr.Person.Title)}
			case wr.Place != nil:
				k = [2]string{"place", nameKey(wr.Place.Title)}
			default:
				continue
			}
			if !have[k] {
				have[k] = true
				count++
			}
		}
	}
	return count
}

// namesStamped refuses a replay while entities.md waits for the owner: a replay writes the names the owner decided,
// never the ones a draft proposes.
func (w *Workspace) namesStamped() error {
	g, err := w.Gate(entitiesFile)
	if err != nil {
		return err
	}
	if g == "draft" || g == "stale" {
		return refuse("entities.md is %s: the owner approves it first (lifelog import approve entities)", g)
	}
	return nil
}

// entityRow is one row of entities.md, its columns in the header's order.
func entityRow(e Entity) string {
	vals := map[string]string{"status": e.Status, "kind": e.Kind, "name": e.Name, "as": e.As, "like": e.Like, "from": e.From, "doubts": e.Doubts}
	row := "|"
	for _, c := range entityCols {
		if v := vals[c]; v != "" {
			row += " " + cell(v)
		}
		row += " |"
	}
	return row + "\n"
}

// written is one person, place or page that a done facts file writes: its kind and title as the owner's decisions
// make it, the file, its quote, whether the owner rejects the name, and the import keys its row can hold (the key of
// the title as written and as the decisions make it, since an alias can change after the write).
type written struct {
	kind, title, file, quote string
	rejected                 bool
	keys                     []string
}

// writtenNames is every person, place and page (not a day page) that the done facts files write, in ledger order.
func (w *Workspace) writtenNames(lines []Line, n names) []written {
	var out []written
	for _, l := range lines {
		if l.State != "x" && l.State != "?" {
			continue
		}
		raw, err := w.LoadFacts(l.File)
		if err != nil {
			continue // a prepared source or a vault note has no facts file
		}
		f := withDecisions(raw, n)
		for i, wr := range f.Writes {
			kind := wr.kind()
			if kind != "person" && kind != "place" && kind != "page" {
				continue
			}
			title, was := what(wr), what(raw.Writes[i])
			if core.IsDay(title) {
				continue
			}
			keys := []string{entityKey(l.File, kind, title)}
			if was != title {
				keys = append(keys, entityKey(l.File, kind, was))
			}
			out = append(out, written{kind: kind, title: title, file: l.File, quote: wr.Quote, rejected: n.rejected(kind, title), keys: keys})
		}
	}
	return out
}

// TombstoneRejected tombstones, in one transaction, each live person, place or page that this import wrote under a
// name the owner rejects in a stamped entities.md (the guide's "entities.md"). A row is this import's when its
// source is the rules' and its import key is one that a done facts file derives for the name: a row of another
// source, a row the owner made, or a note page that the import promoted is never touched. The model may run it: it
// carries only the owner's stamped decisions. It returns what it tombstoned; a second run tombstones nothing.
func (w *Workspace) TombstoneRejected(ctx context.Context, s *core.Store) ([]string, error) {
	done, _, err := w.rejectedRows(ctx, s, false)
	return done, err
}

// rejectedRows tombstones (or, dry, only counts) the live rows of this import under rejected names, and counts the
// rows of this import that are tombstoned while their name is not rejected: a replay writes those again.
func (w *Workspace) rejectedRows(ctx context.Context, s *core.Store, dry bool) (rejected []string, uncarried int, err error) {
	rules, err := w.factsRules()
	if err != nil {
		return nil, 0, err
	}
	lines, _, err := w.Ledger()
	if err != nil {
		return nil, 0, err
	}
	all := w.writtenNames(lines, rules.Names)
	if len(all) == 0 {
		return nil, 0, nil
	}
	fn := func(t *core.Tx) error {
		rejected, uncarried = nil, 0
		seen := map[int64]bool{}
		for _, x := range all {
			for _, k := range x.keys {
				id, typ, deleted, err := t.ImportedEntity(k)
				if err != nil {
					return err
				}
				if id == 0 || typ != x.kind || seen[id] {
					continue
				}
				seen[id] = true
				switch {
				case x.rejected && !deleted:
					if !dry {
						if err := t.Tombstone(id); err != nil {
							return err
						}
					}
					rejected = append(rejected, fmt.Sprintf("%s %q", x.kind, x.title))
				case !x.rejected && deleted:
					uncarried++
				}
			}
		}
		return nil
	}
	if dry {
		err = s.DryRun(ctx, rules.Source, fn)
	} else {
		err = s.Do(ctx, rules.Source, fn)
	}
	return rejected, uncarried, err
}

// NameRow is one name of an import (the guide's "entities.md"): the owner's decision in entities.md, the state of
// the row that holds its title in the database, and the done facts files that write it.
type NameRow struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Decision string `json:"decision"` // the row's status in entities.md; "" when it has none
	As       string `json:"as,omitempty"`
	Row      string `json:"row"` // written, tombstoned, not written, or the type of the row that holds the title
	Files    int    `json:"files"`
	From     string `json:"from,omitempty"`
	Quote    string `json:"quote,omitempty"`
}

// Names lists every name of this import: each row of entities.md and each person, place and page that a done facts
// file writes, by kind and name, with the decision, the state of its row and the first file and quote that write it.
// It writes nothing.
func (w *Workspace) Names(ctx context.Context, s *core.Store) ([]NameRow, error) {
	rows, err := w.Entities()
	if err != nil {
		return nil, err
	}
	n, err := w.nameDecisions()
	if err != nil {
		return nil, err
	}
	lines, _, err := w.Ledger()
	if err != nil {
		return nil, err
	}
	byKey := map[[2]string]*NameRow{}
	var out []*NameRow
	row := func(kind, name string) *NameRow {
		k := [2]string{kind, nameKey(name)}
		if r := byKey[k]; r != nil {
			return r
		}
		r := &NameRow{Kind: kind, Name: name}
		byKey[k] = r
		out = append(out, r)
		return r
	}
	for _, e := range rows {
		r := row(e.Kind, e.Name)
		r.Decision, r.As = e.Status, e.As
	}
	for _, x := range w.writtenNames(lines, n) {
		r := row(x.kind, x.title)
		r.Files++
		if r.From == "" {
			r.From, r.Quote = x.file, x.quote
		}
	}
	source := "import"
	if rules, err := w.Rules(); err == nil && rules.Source != "" {
		source = rules.Source
	}
	err = s.DryRun(ctx, source, func(t *core.Tx) error {
		for _, r := range out {
			title := r.Name
			if r.As != "" {
				title = r.As
			}
			p, err := t.Lookup(title)
			switch {
			case err != nil:
				return err
			case p == nil:
				r.Row = "not written"
			case p.Type != r.Kind:
				r.Row = "a " + p.Type
			case p.Deleted:
				r.Row = "tombstoned"
			default:
				r.Row = "written"
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Kind != out[b].Kind {
			return out[a].Kind < out[b].Kind
		}
		return nameKey(out[a].Name) < nameKey(out[b].Name)
	})
	list := make([]NameRow, len(out))
	for i, r := range out {
		list[i] = *r
	}
	return list, nil
}
