package importer

import (
	"errors"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// checkErr is what an apply would refuse, as a check sees it: the file-level error, else the file refused for its
// refused writes (the tests written when a check stopped at the first refusal keep their meaning through it).
func checkErr(w *Workspace, s *core.Store, file string) error {
	r, err := w.Check(ctx, s, file)
	if err != nil {
		return err
	}
	if len(r.Refused) > 0 {
		return fileRefusal(file, r.Refused)
	}
	return nil
}

// the guide's "The checks": a check reports every refused write with its class; an apply refuses the file whole,
// with the first class (issue 0050)

func TestCheckReportsEveryRefusedWrite(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, err := tx.CreatePerson("Cara Example", "", "", "", ""); return err })
	file := "Journal/2031-04-12.md"
	facts := map[string]any{"file": file, "writes": []any{
		map[string]any{"page": map[string]any{"title": "2031-04-12"}, "quote": "Coffee with Cara"},                                            // 1 good
		map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"},                                                // 2 look-alike (tx stage)
		map[string]any{"link": map[string]any{"from": "2031-04-12", "to": "Cara", "kind": "about"}, "quote": "Coffee with Cara"},              // 3 waits for 2
		map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-04-12", "value": "48 mg/L"}, "quote": "Coffee with Cara"}, // 4 unit (static)
		map[string]any{"place": map[string]any{"title": "the lake"}, "quote": "we try the lake"},                                              // 5 good
		map[string]any{"person": map[string]any{"title": "Nobody"}, "quote": "Coffee with Cara"},                                              // 6 not named (static)
	}}
	if err := f.facts(t, file, facts); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Check(ctx, f.s, file)
	if err != nil {
		t.Fatalf("a check reports, it does not fail: %v", err)
	}
	got := map[int]Refusal{}
	for _, x := range r.Refused {
		got[x.Write] = x
	}
	want := map[int]string{2: "held", 3: "not_yet", 4: "unit", 5: "held", 6: "not_named"}
	if len(got) != len(want) {
		t.Fatalf("refused %d writes, want %d: %+v", len(got), len(want), r.Refused)
	}
	for n, class := range want {
		if got[n].Class != class {
			t.Errorf("write %d: class %q, want %q (%s)", n, got[n].Class, class, got[n].Reason)
		}
	}
	if got[2].Kind != "person" || got[2].What != "Cara" || got[2].Quote != "Coffee with Cara" || len(got[2].Candidates) != 1 || !strings.Contains(got[2].Candidates[0], `"Cara Example" (more words)`) {
		t.Errorf("the look-alike refusal names the write and its candidates: %+v", got[2])
	}
	if got[3].Waits != "Cara" {
		t.Errorf("the link waits for the person: %+v", got[3])
	}
	// the writes that pass are in the report, with their numbers, each checked in its own savepoint
	var ok []int
	for _, o := range r.Outcomes {
		ok = append(ok, o.Write)
	}
	if len(ok) != 1 || ok[0] != 1 {
		t.Errorf("the good writes: %v", ok)
	}
	if !strings.Contains(r.Summary, "5 refused") {
		t.Errorf("summary: %s", r.Summary)
	}
	// an apply refuses the file whole, with the first class, and writes nothing
	before, _ := f.s.Counts(ctx)
	_, err = f.w.Apply(ctx, f.s, file)
	var ce *core.Error
	if !errors.As(err, &ce) || ce.Status != 422 || ce.Code != "held" || !strings.Contains(err.Error(), "write 4") || !strings.Contains(err.Error(), "write 6") {
		t.Errorf("apply: %v (code %q)", err, codeOf(err))
	}
	after, _ := f.s.Counts(ctx)
	if after.Pages != before.Pages {
		t.Errorf("a refused apply wrote rows: %d -> %d pages", before.Pages, after.Pages)
	}
	if lines, _, _ := f.w.Ledger(); ledgerState(lines, file) != " " || ledgerNote(lines, file) != "" {
		t.Error("a refused file was marked, or noted as held while it has other refusals")
	}
	// once the model drops the refused writes and the owner approves the place, the rest applies
	f.decideNames(t, Entity{Kind: "place", Name: "the lake"})
	facts["writes"] = []any{facts["writes"].([]any)[0], facts["writes"].([]any)[4]}
	if err := f.facts(t, file, facts); err != nil {
		t.Fatal(err)
	}
	if r, err := f.w.Apply(ctx, f.s, file); err != nil || len(r.Refused) != 0 || !strings.Contains(r.Summary, "1 page, 1 place") {
		t.Errorf("the good writes alone: %v, %v", r, err)
	}
}

func ledgerNote(lines []Line, file string) string {
	for _, l := range lines {
		if l.File == file {
			return l.Note
		}
	}
	return ""
}

func codeOf(err error) string {
	var ce *core.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestRefusalClassesOfTheTransactionStage(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	day := "Journal/2031-04-12.md"
	one := func(w map[string]any) map[string]any { return map[string]any{"file": day, "writes": []any{w}} }
	first := func(facts map[string]any) Refusal {
		t.Helper()
		if err := f.facts(t, day, facts); err != nil {
			t.Fatal(err)
		}
		r, err := f.w.Check(ctx, f.s, day)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if len(r.Refused) == 0 {
			t.Fatalf("nothing refused: %+v", r)
		}
		return r.Refused[0]
	}
	// a day page is never a person: taken
	if x := first(one(map[string]any{"person": map[string]any{"title": "2031-04-12"}, "quote": "Coffee with Cara"})); x.Class != "taken" {
		t.Errorf("a day page as a person: %+v", x)
	}
	// a link the registry refuses between these ends: link_endpoint, never not_yet
	if x := first(map[string]any{"file": day, "writes": []any{
		map[string]any{"page": map[string]any{"title": "2031-04-12"}, "quote": "Coffee with Cara"},
		map[string]any{"link": map[string]any{"from": "2031-04-12", "to": "2031-04-12", "kind": "about"}, "quote": "Coffee with Cara"},
	}}); x.Write != 2 || x.Class != "link_endpoint" || x.Waits != "" {
		t.Errorf("a link to a day page: %+v", x)
	}
	// a reading whose key already holds another value: value
	if err := f.facts(t, day, one(map[string]any{"reading": map[string]any{"metric": "mood", "day": "2031-04-12", "value": "4"}, "quote": "Coffee with Cara. Next month we try the lake."})); err != nil {
		t.Fatal(err)
	}
	// the day's mood is not in this note: the static check refuses it as evidence, and the class says so
	if r, err := f.w.Check(ctx, f.s, day); err != nil || len(r.Refused) != 1 || r.Refused[0].Class != "evidence" {
		t.Errorf("a scale's number not in the quote: %v, %v", r, err)
	}
	// an invalid title is refused by the static checks, before any transaction
	if x := first(one(map[string]any{"page": map[string]any{"title": "Coffee/with"}, "quote": "Coffee with Cara"})); x.Class != "invalid_title" {
		t.Errorf("an invalid title: %+v", x)
	}
}

// the guide's look-alike rule, scoped (issue 0051): persons, places and plain pages are candidates for each other;
// a day page, a metric, a file or a period never is, and every candidate is reported
func TestLookAlikesByKindAllCandidates(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error {
		if _, _, err := tx.CreatePerson("Cara Example", "", "", "", ""); err != nil {
			return err
		}
		if _, _, err := tx.CreateImported("Cara Example Notes", nil, "", "k1"); err != nil {
			return err
		}
		_, err := tx.RegisterMetric("Cara Index", "", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	day := "Journal/2031-04-12.md"
	if err := f.facts(t, day, map[string]any{"file": day, "writes": []any{map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"}}}); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Check(ctx, f.s, day)
	if err != nil || len(r.Refused) != 1 {
		t.Fatalf("check: %v %v", r, err)
	}
	c := r.Refused[0].Candidates
	if len(c) != 2 || !strings.Contains(c[0], `the person "Cara Example" (more words)`) || !strings.Contains(c[1], `the page "Cara Example Notes" (more words)`) {
		t.Errorf("candidates: person and page, best first; never the metric: %v", c)
	}
	// a name that shares words only with a day page or a metric looks like nothing
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, err := tx.Capture("2031-04-12", "Coffee.", nil); return err }); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"2031 04", "Index"} {
		var like []string
		if err := f.s.DryRun(ctx, "import:notebook", func(tx *core.Tx) (err error) { like, err = lookAlikes(tx, title); return err }); err != nil {
			t.Fatal(err)
		}
		if len(like) != 0 {
			t.Errorf("%q: a look-alike of a day page or a metric: %v", title, like)
		}
	}
}
