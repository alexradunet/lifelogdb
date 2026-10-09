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
	Workspace string            `json:"workspace"`
	Database  string            `json:"database"`
	Source    string            `json:"source,omitempty"`
	Gates     map[string]string `json:"gates"`
	Vault     string            `json:"vault,omitempty"`
	Notes     *NotesCount       `json:"notes,omitempty"`
	Ledger    map[string]int    `json:"ledger"`
	Questions map[string]int    `json:"questions"`
	ToApply   []string          `json:"answered_to_apply,omitempty"`
	Next      string            `json:"next_file,omitempty"`
	Later     string            `json:"later_file,omitempty"`
	Held      map[string]int    `json:"held,omitempty"` // files that wait for name decisions, names without one
	// RejectedLive counts the live rows this import wrote under a name the owner rejects (tombstone-rejected
	// tombstones them); Uncarried the rows this import wrote that are tombstoned while their name is not rejected,
	// which a replay writes again.
	RejectedLive int    `json:"rejected_live,omitempty"`
	Uncarried    int    `json:"tombstoned_not_rejected,omitempty"`
	DoNow        string `json:"do_now"`
	// Step names the case of DoNow in one word, for a program that follows status (lifelog import run): the
	// sentence is for a person or a model, the word for code.
	Step        string       `json:"step"`
	Mismatches  []string     `json:"mismatches"`
	Outside     []string     `json:"written_outside_the_facts,omitempty"`
	Corrections []string     `json:"correction_intents,omitempty"`
	Counts      *core.Counts `json:"counts,omitempty"`

	// Changed is, for each gate that is not approved, the lines changed since the owner's last approval.
	Changed map[string][]string `json:"changed_since_approval,omitempty"`
}

// Status reads the workspace and checks it against the database: every done file is dry-run and must write
// nothing; rows of this import that no facts file or note explains, and rows agents wrote directly, are listed.
func (w *Workspace) Status(ctx context.Context, s *core.Store, dbPath string) (*Status, error) {
	st := &Status{Workspace: w.Dir, Database: dbPath, Gates: map[string]string{}, Ledger: map[string]int{},
		Questions: map[string]int{}, Mismatches: []string{}}
	for _, f := range []string{"rules.md", "metrics.md", entitiesFile, preparedFile, selectedPhotoFile} {
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
	legacy := 0
	if r, err := w.Rules(); err == nil {
		st.Source = r.Source
		legacy = len(r.Legacy.Aliases) + len(r.Legacy.Distinct)
	}
	decisions, err := w.nameDecisions()
	if err != nil {
		return nil, err
	}
	lines, hasLedger, err := w.Ledger()
	if err != nil {
		return nil, err
	}
	names := map[string]string{" ": "to_do", "x": "done", "?": "waiting", ">": "later", "-": "skipped"}
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
		verified := map[int64]bool{}
		if st.Corrections, err = w.correctionIntentStates(ctx, s, verified); err != nil {
			return nil, err
		}
		if st.Source != "" && st.Gates["rules.md"] == "approved" {
			w.mismatches(ctx, s, st, lines, plan)
			rejected, uncarried, err := w.rejectedRows(ctx, s, true)
			if err != nil {
				return nil, err
			}
			st.RejectedLive, st.Uncarried = len(rejected), uncarried
		}
		n, err := s.DirectAgentRows(ctx, verified)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			st.Outside = append(st.Outside, fmt.Sprintf("%d rows were written by agents directly, outside the facts: a replay does not carry them", n))
		}
	} else {
		st.Mismatches = append(st.Mismatches, "no database was checked")
	}
	// the next file: the first to do, else the first that waited for names the owner has now decided, else the first
	// waiting whose questions are all answered; a file held for a later pass is never the next file, only the first
	// of its pass
	for _, l := range lines {
		if l.State == " " && len(heldNames(l.Note)) == 0 {
			st.Next = l.File
			break
		}
	}
	if st.Next == "" {
		for _, l := range lines {
			if l.State == " " && len(heldNames(l.Note)) > 0 && decided(l, decisions) {
				st.Next = l.File
				break
			}
		}
	}
	heldFiles, missing := undecided(lines, decisions)
	if heldFiles > 0 {
		st.Held = map[string]int{"files": heldFiles, "undecided_names": missing}
	}
	if st.Next == "" {
		for _, l := range lines {
			if l.State == "?" && !waitsOn(l.Note, open) {
				st.Next = l.File
				break
			}
		}
	}
	for _, l := range lines {
		if l.State == ">" {
			st.Later = l.File
			break
		}
	}
	if st.Gates["rules.md"] == "approved" && !hasPlan && !hasLedger {
		if st.Notes, err = w.notesCount(); err != nil {
			return nil, err
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
		st.Step = "draft-rules"
		st.DoNow = "Survey the source and draft rules.md (step 2), then stop for the owner."
	case st.Gates["rules.md"] != "approved":
		st.Step = "approve-rules"
		st.DoNow = "Stop: rules.md is " + st.Gates["rules.md"] + changedLines(st.Changed["rules.md"]) + "; the owner approves it (lifelog import approve rules)."
	case legacy > 0:
		st.Step = "move-name-lines"
		st.DoNow = fmt.Sprintf("rules.md holds %d alias or distinct lines: name decisions live in entities.md. Move them (propose-entities), then stop for the owner.", legacy)
	case st.Notes != nil && st.Notes.Markdown > 0:
		st.Step = "plan-or-ledger"
		st.DoNow = fmt.Sprintf("The source holds %d Markdown files among %d: if the rules say they are notes to keep as pages, plan them (plan-vault), then fix every problem the plan lists (step 3); else make the ledger (make-ledger).", st.Notes.Markdown, st.Notes.Files)
	case hasPlan && !vaultApplied:
		st.Step = "apply-vault"
		st.DoNow = "Fix the plan's problems (fix-plan) and apply the vault plan (apply-vault) (step 3)."
	case st.Gates["metrics.md"] == "draft" || st.Gates["metrics.md"] == "stale":
		st.Step = "approve-metrics"
		st.DoNow = "Stop: metrics.md waits for the owner's approval" + changedLines(st.Changed["metrics.md"]) + " (lifelog import approve metrics)."
	case !hasLedger:
		st.Step = "make-ledger"
		st.DoNow = "Make the ledger (make-ledger), then skip what the rules skip (step 5)."
	case st.RejectedLive > 0:
		st.Step = "tombstone-rejected"
		st.DoNow = fmt.Sprintf("%d rows this import wrote are rejected in entities.md: tombstone them (tombstone-rejected).", st.RejectedLive)
	case st.Uncarried > 0:
		st.Step = "reject-or-revive"
		st.DoNow = fmt.Sprintf("Stop: %d rows this import wrote are tombstoned here, but entities.md does not reject their names, so a replay writes them again. The owner rejects them in entities.md (lifelog import approve entities) or revives them.", st.Uncarried)
	case len(st.Mismatches) > 0:
		st.Step = "mismatches"
		st.DoNow = "Mismatches: apply their files again, or ask. Never explain one away."
	case len(st.ToApply) > 0:
		st.Step = "use-answer"
		st.DoNow = "Use the answer of " + st.ToApply[0] + ": rules, facts, check, apply, then close it (step 8)."
	case st.Next != "":
		st.Step = "facts"
		st.DoNow = "Do " + st.Next + " (step 6): inspect it, find, write its facts, check, apply."
	case missing > 0 && st.Gates[entitiesFile] != "draft" && st.Gates[entitiesFile] != "stale":
		st.Step = "propose-names"
		st.DoNow = fmt.Sprintf("%d files wait for %d names the owner has not decided: propose them (propose-entities), then stop for the owner.", heldFiles, missing)
	case st.Gates[entitiesFile] == "draft" || st.Gates[entitiesFile] == "stale":
		st.Step = "approve-entities"
		st.DoNow = "Stop: entities.md waits for the owner's decisions" + changedLines(st.Changed[entitiesFile]) + " (lifelog import approve entities)."
	case st.Later != "":
		st.Step = "later-pass"
		st.DoNow = fmt.Sprintf("The later pass: %d files are held for it. Do %s as its rule says (keep it as a file with its text, or a selected photo), or skip it.", st.Ledger["later"], st.Later)
	case st.Ledger["waiting"] > 0:
		st.Step = "answer-questions"
		st.DoNow = fmt.Sprintf("Stop: %d files wait for questions only the owner can answer.", st.Ledger["waiting"])
	case w.doneNamesUndecided(lines) > 0:
		st.Step = "propose-names"
		st.DoNow = "The done files write names entities.md does not decide, and a replay writes them only with the owner's decision: propose them (propose-entities), then stop for the owner."
	default:
		st.Step = "done"
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
	batches, e := w.appliedPrepared(ctx)
	preparedFiles := map[string]bool{}
	if e != nil {
		st.Mismatches = append(st.Mismatches, "immutable prepared source verification failed")
	} else {
		for _, b := range batches {
			preparedFiles[b.File] = true
			if e := w.admitSource(b.File); e != nil {
				st.Mismatches = append(st.Mismatches, "completed prepared source admission failed")
			}
			if e := verifyPrepared(ctx, s, b); e != nil {
				st.Mismatches = append(st.Mismatches, "prepared source database verification failed")
			}
			for _, r := range b.Records {
				if b.Profile != "fit-date-csv-v1" {
					expected[preparedKey(b.Profile, r.Key, "session")] = true
				}
				for _, q := range r.Quantities {
					expected[preparedKey(b.Profile, r.Key, q.Code)] = true
				}
			}
		}
	}
	photos, e := w.appliedSelectedPhotos(ctx)
	if e != nil {
		st.Mismatches = append(st.Mismatches, "completed selected history verification failed")
	} else {
		for _, p := range photos {
			preparedFiles[p.File] = true
			if p.Sidecar != "" {
				preparedFiles[p.Sidecar] = true
			}
			if _, e = w.verifySelectedPhoto(ctx, s, p, true); e != nil {
				st.Mismatches = append(st.Mismatches, "completed selected original database verification failed")
			}
		}
	}
	// A current draft/reservation remains gate evidence, never evidence of completed history.
	if _, ok, e := w.read(selectedPhotoFile); e != nil {
		st.Mismatches = append(st.Mismatches, "selected artifact cannot be read")
	} else if ok {
		p, _, e := w.readSelectedPhoto(ctx, true)
		if e != nil {
			st.Mismatches = append(st.Mismatches, "selected pair verification failed")
		} else if e = w.bindSelectedPhoto(p, false); e != nil {
			st.Mismatches = append(st.Mismatches, "selected pair binding verification failed")
		}
	}
	idx := indexLedger(lines)
	for _, l := range lines {
		if l.State != "x" && l.State != "?" {
			continue
		}
		if preparedFiles[l.File] {
			continue
		}
		r, err := w.check(ctx, s, l.File, idx)
		if err != nil {
			st.Mismatches = append(st.Mismatches, l.File+": "+err.Error())
			continue
		}
		for _, o := range r.Outcomes {
			if o.Status != "existing" && o.Status != "rejected" {
				st.Mismatches = append(st.Mismatches, fmt.Sprintf("%s: write %d (%s) is not in the database", l.File, o.Write, o.What))
			}
		}
		for _, x := range r.Refused {
			st.Mismatches = append(st.Mismatches, fmt.Sprintf("%s: write %d (%s) is refused now: %s", l.File, x.Write, x.What, x.Class))
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
			if err := s.DryRun(ctx, st.Source, func(t *core.Tx) error {
				keys, err := resolveReadingKeys(t, st.Source, f, pos)
				if err != nil {
					return err
				}
				for _, k := range keys.byWrite {
					expected[k] = true
				}
				return nil
			}); err != nil {
				st.Mismatches = append(st.Mismatches, l.File+": "+err.Error())
			}
		}
	}
	if plan != nil {
		for _, n := range plan.Notes {
			expected[n.key()] = true
		}
	}
	if plan != nil {
		w.editedNotes(ctx, s, st, plan)
	}
	src := strings.ReplaceAll(st.Source, "'", "")
	res, err := s.Query(ctx, `SELECT import_key FROM entities WHERE source = '`+src+`' AND import_key IS NOT NULL
	                          UNION ALL SELECT import_key FROM measurements WHERE source = '`+src+`' AND import_key IS NOT NULL
 UNION ALL SELECT import_key FROM sessions WHERE source = '`+src+`' AND import_key IS NOT NULL`, 100000)
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

}

// pendingNotes counts the notes of a plan not in the database yet.
func (w *Workspace) pendingNotes(ctx context.Context, s *core.Store, p *Plan, source string) int {
	n := 0
	s.DryRun(ctx, source, func(t *core.Tx) error {
		for _, note := range p.Notes {
			switch note.Action {
			case "create":
				if id, _ := t.ByImportKey(note.key()); id == 0 {
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
	own := make([]string, len(plan.Notes))
	readable := make([]bool, len(plan.Notes))
	for i := range plan.Notes {
		if raw, err := w.ReadSource(plan.Notes[i].Path); err == nil {
			own[i], readable[i] = rewriteLinks(raw, &plan.Notes[i], links), true
		}
	}
	texts := pageTexts(plan, own)
	s.DryRun(ctx, st.Source, func(t *core.Tx) error {
		for i := range plan.Notes {
			n := &plan.Notes[i]
			if !readable[i] {
				continue
			}
			want := texts[i]
			switch {
			case n.Action == "create":
				id, _ := t.ByImportKey(n.key())
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
