package importer

import (
	"context"
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
	for _, l := range lines {
		if !l.toDo() || len(heldNames(l.Note)) == 0 {
			continue
		}
		r, err := w.Check(ctx, s, l.File)
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
