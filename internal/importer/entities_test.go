package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// the guide's "entities.md": the owner decides each new name before a row exists (RFC 0008, issue 0052)

const cafe = "Journal/2031-04-12.md"

func (f *fixture) countPeople(t *testing.T) int {
	t.Helper()
	c, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c.ByType["person"]
}

func TestANewNameHoldsTheFileUntilTheOwnerDecides(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	people := f.countPeople(t)

	// the apply writes nothing, refuses with class held, and notes the line, which stays to do
	_, err := f.w.Apply(ctx, f.s, cafe)
	if code(err) != 422 || codeOf(err) != "held" {
		t.Fatalf("a new person: %v (code %q)", err, codeOf(err))
	}
	lines, _, _ := f.w.Ledger()
	if ledgerState(lines, cafe) != " " || ledgerNote(lines, cafe) != `held: person "Cara"` {
		t.Errorf("the held line: [%s] %s", ledgerState(lines, cafe), ledgerNote(lines, cafe))
	}
	if f.countPeople(t) != people {
		t.Error("a held file wrote a person")
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next == cafe || st.Next == "" || st.Held["files"] != 1 || st.Held["undecided_names"] != 1 {
		t.Errorf("status: the other files come first: next %q, held %v", st.Next, st.Held)
	}
	// with nothing else to do, status asks for the proposal
	for _, l := range lines {
		if l.File != cafe {
			if err := f.w.Skip(l.File, "not in this test"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if st, _ = f.w.Status(ctx, f.s, f.trial); !strings.Contains(st.DoNow, "propose them (propose-entities)") {
		t.Errorf("do now with only a held file: %s", st.DoNow)
	}

	// propose: one row per name, with where it is from; a second propose adds nothing
	added, err := f.w.ProposeEntities(ctx, f.s)
	if err != nil || len(added) != 1 || added[0].Kind != "person" || added[0].Name != "Cara" || added[0].From != cafe || added[0].Status != "proposed" {
		t.Fatalf("propose: %+v, %v", added, err)
	}
	if again, err := f.w.ProposeEntities(ctx, f.s); err != nil || len(again) != 0 {
		t.Errorf("a second propose: %+v, %v", again, err)
	}
	if g, _ := f.w.Gate(entitiesFile); g != "draft" {
		t.Errorf("entities.md after propose: %s", g)
	}
	if g, _ := f.w.Gate("rules.md"); g != "approved" {
		t.Errorf("a name decision closed the rules gate: %s", g)
	}
	st, _ = f.w.Status(ctx, f.s, f.trial)
	if !strings.HasPrefix(st.DoNow, "Stop: entities.md waits for the owner") {
		t.Errorf("do now: %s", st.DoNow)
	}

	// the owner stamps: the held file is the next file again, and applies whole
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	st, _ = f.w.Status(ctx, f.s, f.trial)
	if st.Next != cafe {
		t.Errorf("after the stamp, next: %q (%s)", st.Next, st.DoNow)
	}
	r, err := f.w.Apply(ctx, f.s, cafe)
	if err != nil || r.Summary != "1 page, 1 person, 1 link (3 new)" {
		t.Fatalf("apply after the decision: %v, %v", r, err)
	}
	if f.countPeople(t) != people+1 {
		t.Error("the approved person was not written")
	}
}

func TestARejectedNameIsSkippedAndAnAliasIsWrittenUnderItsTitle(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, err := tx.CreatePerson("Cara Example", "", "", "", ""); return err }); err != nil {
		t.Fatal(err)
	}
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	// a look-alike is a held name, with what it looks like
	r, err := f.w.Check(ctx, f.s, cafe)
	if err != nil || len(r.Refused) != 2 || r.Refused[0].Class != "held" || len(r.Refused[0].Candidates) != 1 || r.Refused[0].Candidates[0] != `the person "Cara Example" (more words)` || r.Refused[1].Class != "not_yet" || r.Refused[1].Waits != "Cara" {
		t.Fatalf("a look-alike: %+v, %v", r, err)
	}
	// rejected: the name's writes are skipped (the person and the link to her), nothing is refused or held
	f.decideNames(t, Entity{Kind: "person", Name: "Cara", Status: "rejected"})
	if r, err = f.w.Check(ctx, f.s, cafe); err != nil || len(r.Refused) != 0 || r.Summary != "1 page (1 new); 2 rejected" {
		t.Errorf("a rejected name: %+v, %v", r, err)
	}
	// the owner makes "Cara" an alias of Cara Example instead: the facts are written under the title
	editLine(t, f.w, entitiesFile, "| rejected | person | Cara | |", "| proposed | person | Cara | Cara Example |")
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	people := f.countPeople(t)
	r, err = f.w.Apply(ctx, f.s, cafe)
	if err != nil || !strings.Contains(r.Summary, "1 person") || !strings.Contains(r.Summary, "1 existing") {
		t.Fatalf("an alias: %v, %v", r, err)
	}
	if f.countPeople(t) != people {
		t.Error("an alias made a new person")
	}
	var linked bool
	f.s.DryRun(ctx, "cli", func(tx *core.Tx) error {
		res, err := tx.Lookup("Cara Example")
		if err == nil && res != nil {
			for _, o := range r.Outcomes {
				linked = linked || o.What == "2031-04-12 about Cara Example"
			}
		}
		return nil
	})
	if !linked {
		t.Errorf("the link names the alias's title: %+v", r.Outcomes)
	}
}

func TestATombstonedNameIsHeldNotRevived(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	var id int64
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) (err error) { id, _, err = tx.CreatePerson("Cara", "", "", "", ""); return err }); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Check(ctx, f.s, cafe)
	if err != nil || len(r.Refused) == 0 || r.Refused[0].Class != "held" || r.Refused[0].Candidates[0] != `revives the tombstoned person "Cara"` {
		t.Fatalf("a tombstoned name: %+v, %v", r, err)
	}
	if _, err := f.w.Apply(ctx, f.s, cafe); codeOf(err) != "held" {
		t.Fatalf("apply: %v", err)
	}
	if p := person(t, f.s, "Cara"); p.DeletedAt == "" {
		t.Error("a write revived a tombstoned person without the owner")
	}
	f.decideNames(t, Entity{Kind: "person", Name: "Cara"})
	if _, err := f.w.Apply(ctx, f.s, cafe); err != nil {
		t.Fatal(err)
	}
	if p := person(t, f.s, "Cara"); p.DeletedAt != "" {
		t.Error("the owner's approval did not revive the person")
	}
}

func TestProposeMovesTheNameLinesOfRulesAndTheDoneNames(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody+"\n## Aliases\n- \"Bobby\" → \"Bob Sample\"\n\n## Distinct\n- \"Cara\" ≠ \"Cara Example\"\n")
	f.w.MakeLedger()
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error {
		if _, _, err := tx.CreatePerson("Bob Sample", "", "", "", ""); err != nil {
			return err
		}
		_, _, err := tx.CreatePerson("Cara Example", "", "", "", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || !strings.HasPrefix(st.DoNow, "rules.md holds 2 alias or distinct lines") {
		t.Fatalf("status with old name lines: %v, %v", st, err)
	}
	added, err := f.w.ProposeEntities(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entity{}
	for _, e := range added {
		got[e.Name] = e
	}
	if len(added) != 2 || got["Bobby"].Kind != "person" || got["Bobby"].As != "Bob Sample" || got["Cara"].Kind != "person" || got["Cara"].As != "" {
		t.Errorf("the moved lines: %+v", added)
	}
	rules, _, _ := f.w.read("rules.md")
	if strings.Contains(rules, "## Aliases") || strings.Contains(rules, "## Distinct") || !strings.HasPrefix(rules, "status: draft\n") || !strings.Contains(rules, "## Folders") {
		t.Errorf("rules.md after the move:\n%s", rules)
	}
	// the owner stamps both; a done file's person is proposed too, for a replay into a fresh database
	if err := ownerApproves(f.w, "rules.md"); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, cafe); err != nil {
		t.Fatalf("Cara decided by the moved distinct line: %v", err)
	}
	if more, err := f.w.ProposeEntities(ctx, f.s); err != nil || len(more) != 0 {
		t.Errorf("a done file's names that entities.md decides already: %+v, %v", more, err)
	}
}

func TestReplayWritesTheOwnersDecisionsAndRefusesADraft(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.decideNames(t, Entity{Kind: "person", Name: "Cara"})
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, cafe); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	// a draft entities.md: the owner has not stamped what a replay would write
	f.decideNames(t) // stamps again, unchanged
	editLine(t, f.w, entitiesFile, "| approved | person | Cara |", "| approved | person | Cara | | | | changed |")
	if _, err := f.w.Replay(ctx, f.s, target); code(err) != 422 || !strings.Contains(err.Error(), "entities.md is stale") {
		t.Fatalf("a replay with a stale entities.md: %v", err)
	}
	if exists(target) {
		t.Error("a refused replay created the target")
	}
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	res, err := f.w.Replay(ctx, f.s, target)
	if err != nil || len(res.Failures) != 0 || res.Counts.ByType["person"] != 1 {
		t.Fatalf("replay: %+v, %v", res, err)
	}
}

func TestARejectedPageIsSkippedWithNoLookAlike(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.decideNames(t, Entity{Kind: "page", Name: "the lake", Status: "rejected"})
	lake := map[string]any{"file": cafe, "writes": []any{
		map[string]any{"page": map[string]any{"title": "the lake"}, "quote": "we try the lake"},
	}}
	if err := f.facts(t, cafe, lake); err != nil {
		t.Fatal(err)
	}
	// nothing looks like "the lake": the decision alone skips it
	r, err := f.w.Check(ctx, f.s, cafe)
	if err != nil || len(r.Refused) != 0 || len(r.Outcomes) != 1 || r.Outcomes[0].Status != "rejected" {
		t.Fatalf("a rejected page: %+v, %v", r, err)
	}
}

// The owner's editor may leave a blank line, or a note, after the table of entities.md. A proposed row goes after the
// table's last row, so the writer reads it, and a second propose finds it there.
func TestProposeAddsItsRowsInsideTheTable(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody+"\n## Aliases\n- \"Bobby\" → \"Bob Sample\"\n")
	f.w.MakeLedger()
	owner := "status: draft\n\n" + entitiesHeader + entityRow(Entity{Status: "rejected", Kind: "person", Name: "Dana"}) + "\nA note of the owner.\n\n"
	if err := writeAtomic(f.w.file(entitiesFile), []byte(owner)); err != nil {
		t.Fatal(err)
	}
	added, err := f.w.ProposeEntities(ctx, f.s)
	if err != nil || len(added) != 1 {
		t.Fatalf("propose: %+v, %v", added, err)
	}
	rows, err := f.w.Entities()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range rows {
		got[e.Name] = e.Status
	}
	if len(rows) != 2 || got["Dana"] != "rejected" || got["Bobby"] != "proposed" {
		t.Fatalf("the rows the writer reads: %+v", rows)
	}
	text, _, _ := f.w.read(entitiesFile)
	if !strings.HasSuffix(text, "\nA note of the owner.\n\n") {
		t.Errorf("the owner's note after the table moved:\n%s", text)
	}
	if more, err := f.w.ProposeEntities(ctx, f.s); err != nil || len(more) != 0 {
		t.Errorf("a second propose: %+v, %v", more, err)
	}
}

func TestInsertEntityRowsAfterTheLastRow(t *testing.T) {
	row := "| proposed | person | Bobby | | | | |\n"
	for _, c := range []struct{ name, text, want string }{
		{"the last row ends the file without a newline", "status: draft\n\n" + entitiesHeader + "| rejected | person | Dana | | | | |",
			"status: draft\n\n" + entitiesHeader + "| rejected | person | Dana | | | | |\n" + row},
		{"words after the table stay after it", "status: draft\n\n" + entitiesHeader + "\nwords\n",
			"status: draft\n\n" + entitiesHeader + row + "\nwords\n"},
		{"no table: the header comes first", "status: draft\n", "status: draft\n\n" + entitiesHeader + row},
	} {
		if got := insertEntityRows(c.text, row); got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}
