package importer

import (
	"context"
	"fmt"
	"strings"

	"lifelog/internal/core"
)

// Status is the guide's status: gates, ledger counts, questions, the next file, one "do now" sentence and the
// mismatches between the workspace and the database.
type Status struct {
	Workspace  string            `json:"workspace"`
	Database   string            `json:"database"`
	Source     string            `json:"source,omitempty"`
	Gates      map[string]string `json:"gates"`
	Vault      string            `json:"vault,omitempty"`
	Ledger     map[string]int    `json:"ledger"`
	Questions  map[string]int    `json:"questions"`
	ToApply    []string          `json:"answered_to_apply,omitempty"`
	Next       string            `json:"next_file,omitempty"`
	DoNow      string            `json:"do_now"`
	Mismatches []string          `json:"mismatches"`
	Outside    []string          `json:"written_outside_the_facts,omitempty"`
	Counts     *core.Counts      `json:"counts,omitempty"`

	// Changed is, for each gate that is not approved, the lines changed since the owner's last approval.
	Changed map[string][]string `json:"changed_since_approval,omitempty"`
}

// Status reads the workspace and checks it against the database: every done file is dry-run and must write
// nothing; rows of this import that no facts file or note explains, and rows agents wrote directly, are listed.
func (w *Workspace) Status(ctx context.Context, s *core.Store, dbPath string) (*Status, error) {
	st := &Status{Workspace: w.Dir, Database: dbPath, Gates: map[string]string{}, Ledger: map[string]int{},
		Questions: map[string]int{}, Mismatches: []string{}}
	for _, f := range []string{"rules.md", "metrics.md"} {
		g, err := w.Gate(f)
		if err != nil {
			return nil, err
		}
		st.Gates[f] = g
		// a closed gate names what changed since the owner's last approval (the guide's "Gates")
		if g == "stale" || g == "draft" {
			if d := w.Changed(f, 20); d != nil {
				if st.Changed == nil {
					st.Changed = map[string][]string{}
				}
				st.Changed[f] = d
			}
		}
	}
	if r, err := w.Rules(); err == nil {
		st.Source = r.Source
	}
	lines, hasLedger, err := w.Ledger()
	if err != nil {
		return nil, err
	}
	names := map[string]string{" ": "to_do", "x": "done", "?": "waiting", "-": "skipped"}
	for _, l := range lines {
		st.Ledger[names[l.State]]++
	}
	qs, err := w.Questions()
	if err != nil {
		return nil, err
	}
	open := map[string]bool{}
	for _, q := range qs {
		st.Questions[q.Status]++
		if q.Status == "answered" {
			st.ToApply = append(st.ToApply, q.ID)
		}
		if q.Status == "open" || q.Status == "parked" {
			open[q.ID] = true
		}
	}
	plan, hasPlan, err := w.LoadPlan()
	if err != nil {
		return nil, err
	}
	if s != nil {
		if st.Counts, err = s.Counts(ctx); err != nil {
			return nil, err
		}
		if st.Source != "" && st.Gates["rules.md"] == "approved" {
			w.mismatches(ctx, s, st, lines, plan)
		}
	} else {
		st.Mismatches = append(st.Mismatches, "no database was checked")
	}
	// the next file: the first to do, else the first waiting whose questions are all answered
	for _, l := range lines {
		if l.State == " " {
			st.Next = l.File
			break
		}
	}
	if st.Next == "" {
		for _, l := range lines {
			if l.State == "?" && !waitsOn(l.Note, open) {
				st.Next = l.File
				break
			}
		}
	}
	vaultApplied := false
	if hasPlan {
		problems, pending := 0, 0
		for _, n := range plan.Notes {
			problems += len(n.Problems)
		}
		if s != nil && st.Source != "" {
			pending = w.pendingNotes(ctx, s, plan, st.Source)
		}
		vaultApplied = pending == 0 && problems == 0
		st.Vault = fmt.Sprintf("%d notes, %d problems, %d not applied", len(plan.Notes), problems, pending)
	}
	switch {
	case st.Gates["rules.md"] == "missing":
		st.DoNow = "Survey the source and draft rules.md (step 2), then stop for the owner."
	case st.Gates["rules.md"] != "approved":
		st.DoNow = "Stop: rules.md is " + st.Gates["rules.md"] + changedLines(st.Changed["rules.md"]) + "; the owner approves it (lifelog import approve rules)."
	case w.IsVault() && !hasPlan:
		st.DoNow = "Plan the vault (plan-vault), then fix every problem it lists (step 3)."
	case hasPlan && !vaultApplied:
		st.DoNow = "Fix the plan's problems (fix-plan) and apply the vault plan (apply-vault) (step 3)."
	case st.Gates["metrics.md"] == "draft" || st.Gates["metrics.md"] == "stale":
		st.DoNow = "Stop: metrics.md waits for the owner's approval" + changedLines(st.Changed["metrics.md"]) + " (lifelog import approve metrics)."
	case !hasLedger:
		st.DoNow = "Make the ledger (make-ledger), then skip what the rules skip (step 5)."
	case len(st.Mismatches) > 0:
		st.DoNow = "Mismatches: apply their files again, or ask. Never explain one away."
	case len(st.ToApply) > 0:
		st.DoNow = "Use the answer of " + st.ToApply[0] + ": rules, facts, check, apply, then close it (step 8)."
	case st.Next != "":
		st.DoNow = "Do " + st.Next + " (step 6): inspect it, find, write its facts, check, apply."
	case st.Ledger["waiting"] > 0:
		st.DoNow = fmt.Sprintf("Stop: %d files wait for questions only the owner can answer.", st.Ledger["waiting"])
	default:
		st.DoNow = "Every file is done: run the integrity check and report the counts (step 9)."
	}
	return st, nil
}

// changedLines names the lines of a short diff, from its hunk headers: " (changed since the approval: line 12,
// lines 20-22)". Nothing when there is no diff.
func changedLines(diff []string) string {
	var spans []string
	for _, l := range diff {
		var a, b string
		if _, err := fmt.Sscanf(l, "@@ %s %s @@", &a, &b); err != nil || !strings.HasPrefix(b, "+") {
			continue
		}
		start, n := b[1:], 1
		if s, c, ok := strings.Cut(b[1:], ","); ok {
			start = s
			fmt.Sscan(c, &n)
		}
		var first int
		fmt.Sscan(start, &first)
		switch {
		case n == 0:
			spans = append(spans, fmt.Sprintf("removed after line %d", first))
		case n == 1:
			spans = append(spans, "line "+start)
		default:
			spans = append(spans, fmt.Sprintf("lines %d-%d", first, first+n-1))
		}
	}
	if len(spans) == 0 {
		return ""
	}
	return " (changed since the approval: " + strings.Join(spans, ", ") + ")"
}

func waitsOn(note string, open map[string]bool) bool {
	_, after, ok := strings.Cut(note, "waiting: ")
	if !ok {
		return false
	}
	for _, q := range strings.Split(after, ",") {
		if open[strings.TrimSpace(q)] {
			return true
		}
	}
	return false
}

func (w *Workspace) mismatches(ctx context.Context, s *core.Store, st *Status, lines []Line, plan *Plan) {
	expected := map[string]bool{}
	for _, l := range lines {
		if l.State != "x" && l.State != "?" {
			continue
		}
		r, err := w.Check(ctx, s, l.File)
		if err != nil {
			st.Mismatches = append(st.Mismatches, l.File+": "+err.Error())
			continue
		}
		for _, o := range r.Outcomes {
			if o.Status != "existing" {
				st.Mismatches = append(st.Mismatches, fmt.Sprintf("%s: write %d (%s) is not in the database", l.File, o.Write, o.What))
			}
		}
		if f, err := w.LoadFacts(l.File); err == nil {
			for _, wr := range f.Writes {
				switch {
				case wr.Person != nil:
					expected[entityKey(f.File, "person", wr.Person.Title)] = true
				case wr.Place != nil:
					expected[entityKey(f.File, "place", wr.Place.Title)] = true
				case wr.Page != nil:
					expected[entityKey(f.File, "page", wr.Page.Title)] = true
				}
			}
			src, _ := w.ReadSource(f.File)
			pos := make([]int, len(f.Writes))
			for i, wr := range f.Writes {
				pos[i] = wholeIndex(collapse(src), collapse(wr.Quote))
			}
			for _, k := range readingKeys(f, pos) {
				expected[k] = true
			}
		}
	}
	if plan != nil {
		for _, n := range plan.Notes {
			expected[n.Path] = true
		}
	}
	if plan != nil {
		w.editedNotes(ctx, s, st, plan)
	}
	src := strings.ReplaceAll(st.Source, "'", "")
	res, err := s.Query(ctx, `SELECT import_key FROM entities WHERE source = '`+src+`' AND import_key IS NOT NULL
	                          UNION ALL SELECT import_key FROM measurements WHERE source = '`+src+`' AND import_key IS NOT NULL`, 100000)
	if err == nil {
		outside := 0
		for _, row := range res.Rows {
			if k, _ := row[0].(string); !expected[k] {
				outside++
			}
		}
		if outside > 0 {
			st.Mismatches = append(st.Mismatches, fmt.Sprintf("%d rows of %s are in no done facts file or vault note", outside, st.Source))
		}
	}
	if res, err := s.Query(ctx, `SELECT (SELECT count(*) FROM entities WHERE source LIKE 'agent:%')
	                                  + (SELECT count(*) FROM links WHERE source LIKE 'agent:%')
	                                  + (SELECT count(*) FROM measurements WHERE source LIKE 'agent:%')`, 1); err == nil {
		if n, _ := res.Rows[0][0].(int64); n > 0 {
			st.Outside = append(st.Outside, fmt.Sprintf("%d rows were written by agents directly, outside the facts: a replay does not carry them", n))
		}
	}
}

// pendingNotes counts the notes of a plan not in the database yet.
func (w *Workspace) pendingNotes(ctx context.Context, s *core.Store, p *Plan, source string) int {
	n := 0
	s.DryRun(ctx, source, func(t *core.Tx) error {
		for _, note := range p.Notes {
			switch note.Action {
			case "create":
				if id, _ := t.ByImportKey(note.Path); id == 0 {
					n++
				}
			case "append":
				if !note.Appended {
					n++
				}
			}
		}
		return nil
	})
	return n
}

// editedNotes lists imported note pages whose text is no longer the note's: an edit made outside the import,
// which a replay does not carry (it writes the note's text).
func (w *Workspace) editedNotes(ctx context.Context, s *core.Store, st *Status, plan *Plan) {
	links := linkIndex(plan)
	s.DryRun(ctx, st.Source, func(t *core.Tx) error {
		for i := range plan.Notes {
			n := &plan.Notes[i]
			raw, err := w.ReadSource(n.Path)
			if err != nil {
				continue
			}
			want := rewriteLinks(raw, n, links)
			switch {
			case n.Action == "create":
				id, _ := t.ByImportKey(n.Path)
				if id == 0 {
					continue
				}
				if body, _ := t.Body(id); body != want {
					st.Outside = append(st.Outside, fmt.Sprintf("the page of %s was edited outside the import: a replay writes the note's text", n.Path))
				}
			case n.Action == "append" && n.Appended:
				if p, _ := t.Lookup(n.Title); p == nil || !strings.Contains(p.Body, want) {
					st.Outside = append(st.Outside, fmt.Sprintf("the day page %s no longer holds the text of %s", n.Title, n.Path))
				}
			}
		}
		return nil
	})
}
