package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// the guide's "entities.md": a rejected name is skipped, and the row the import wrote under it is tombstoned
// (RFC 0009, issue 0053)

// rejectLater applies the cafe note with Cara approved, then the owner rejects her and stamps again.
func rejectLater(t *testing.T) *fixture {
	t.Helper()
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
	editLine(t, f.w, entitiesFile, "| approved | person | Cara |", "| rejected | person | Cara |")
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestARejectedNameIsSkippedInADoneFile(t *testing.T) {
	f := rejectLater(t)
	r, err := f.w.Check(ctx, f.s, cafe)
	if err != nil || len(r.Refused) != 0 {
		t.Fatalf("a done file that writes a rejected name: %+v, %v", r, err)
	}
	got := map[int]string{}
	for _, o := range r.Outcomes {
		got[o.Write] = o.Status
	}
	// write 1 the person, write 2 the day page, write 3 the link to her
	if got[1] != "rejected" || got[2] != "existing" || got[3] != "rejected" {
		t.Errorf("outcomes: %+v", r.Outcomes)
	}
	if r.Summary != "1 page (1 existing); 2 rejected" {
		t.Errorf("summary: %q", r.Summary)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Mismatches {
		if strings.Contains(m, cafe) {
			t.Errorf("a skipped write is a mismatch: %s", m)
		}
	}
}

func TestTombstoneRejectedTouchesOnlyThisImportsRows(t *testing.T) {
	f := rejectLater(t)
	// a person the owner made, with a name the owner also rejects: not a row of this import
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, err := tx.CreatePerson("Dana", "", "", "", ""); return err }); err != nil {
		t.Fatal(err)
	}
	f.decideNames(t, Entity{Kind: "person", Name: "Dana", Status: "rejected"})
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || st.RejectedLive != 1 || !strings.Contains(st.DoNow, "tombstone them (tombstone-rejected)") {
		t.Fatalf("status before: %d rejected live, do now %q, %v", st.RejectedLive, st.DoNow, err)
	}
	done, err := f.w.TombstoneRejected(ctx, f.s)
	if err != nil || len(done) != 1 || done[0] != `person "Cara"` {
		t.Fatalf("tombstoned: %v, %v", done, err)
	}
	if person(t, f.s, "Cara").DeletedAt == "" {
		t.Error("the rejected person of this import is live")
	}
	if person(t, f.s, "Dana").DeletedAt != "" {
		t.Error("a person of another source was tombstoned")
	}
	if again, err := f.w.TombstoneRejected(ctx, f.s); err != nil || len(again) != 0 {
		t.Errorf("a second run: %v, %v", again, err)
	}
	st, err = f.w.Status(ctx, f.s, f.trial)
	if err != nil || st.RejectedLive != 0 || st.Uncarried != 0 || len(st.Mismatches) != 0 {
		t.Errorf("status after: %d rejected live, %d uncarried, mismatches %v, %v", st.RejectedLive, st.Uncarried, st.Mismatches, err)
	}
}

func TestAHeldFileWhoseNameIsRejectedApplies(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if err := f.facts(t, cafe, cafeFacts); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, cafe); codeOf(err) != "held" {
		t.Fatalf("a new name: %v", err)
	}
	f.decideNames(t, Entity{Kind: "person", Name: "Cara", Status: "rejected"})
	r, err := f.w.Apply(ctx, f.s, cafe)
	if err != nil || r.Summary != "1 page (1 new); 2 rejected" {
		t.Fatalf("apply: %+v, %v", r, err)
	}
	if lines, _, _ := f.w.Ledger(); ledgerState(lines, cafe) != "x" || ledgerNote(lines, cafe) != r.Summary {
		t.Errorf("the ledger line: [%s] %s", ledgerState(lines, cafe), ledgerNote(lines, cafe))
	}
	if id, err := f.s.PageID(ctx, "Cara"); err != nil || id != 0 {
		t.Errorf("a rejected name was written: %d, %v", id, err)
	}
}

// a reading keeps its value when the page it was captured with is rejected: it loses its with
func TestAReadingLosesARejectedWith(t *testing.T) {
	day := "Journal/2031-04-11.md"
	facts := map[string]any{"file": day, "writes": []any{
		map[string]any{"place": map[string]any{"title": "Riverside Pool"}, "quote": "Swam at Riverside Pool"},
		map[string]any{"reading": map[string]any{"metric": "mood", "day": "2031-04-11", "value": "4", "with": "Riverside Pool"}, "quote": "mood: 4"},
	}}
	start := func(decision string) *fixture {
		f := setup(t)
		f.approveRules(t, rulesBody)
		f.w.MakeLedger()
		f.metrics(t)
		if err := f.facts(t, day, facts); err != nil {
			t.Fatal(err)
		}
		f.decideNames(t, Entity{Kind: "place", Name: "Riverside Pool", Status: decision})
		return f
	}
	f := start("proposed")
	if _, err := f.w.Apply(ctx, f.s, day); err != nil {
		t.Fatal(err)
	}
	// rejected after the write: the done file still checks clean, its reading existing
	editLine(t, f.w, entitiesFile, "| approved | place | Riverside Pool |", "| rejected | place | Riverside Pool |")
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"after the rejection", "after the tombstone"} {
		r, err := f.w.Check(ctx, f.s, day)
		if err != nil || len(r.Refused) != 0 || r.Summary != "1 reading (1 existing); 1 rejected" {
			t.Fatalf("a done file %s: %+v, %v", step, r, err)
		}
		if _, err := f.w.TombstoneRejected(ctx, f.s); err != nil {
			t.Fatal(err)
		}
	}
	// into a fresh trial, the reading is written without its with (a with that is not written refuses the file),
	// and a second check finds it
	g := start("rejected")
	if r, err := g.w.Apply(ctx, g.s, day); err != nil || r.Summary != "1 reading (1 new); 1 rejected" {
		t.Fatalf("a fresh apply: %+v, %v", r, err)
	}
	if r, err := g.w.Check(ctx, g.s, day); err != nil || r.Summary != "1 reading (1 existing); 1 rejected" {
		t.Fatalf("a check after the fresh apply: %+v, %v", r, err)
	}
}

func TestReplayTombstonesARejectedRowAndNeverWritesIt(t *testing.T) {
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
	if _, err := f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	if person(t, openStore(t, target), "Cara").DeletedAt != "" {
		t.Fatal("the first replay did not write the person")
	}
	// the owner rejects her and the trial carries it; the dry run lists the row, the replay tombstones it
	editLine(t, f.w, entitiesFile, "| approved | person | Cara |", "| rejected | person | Cara |")
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.TombstoneRejected(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	dry, err := f.w.Rehearse(ctx, f.s, target)
	if err != nil || len(dry.Failures) != 0 || len(dry.Tombstoned) != 1 || dry.Tombstoned[0] != `person "Cara"` {
		t.Fatalf("rehearsal: %+v, %v", dry, err)
	}
	if person(t, openStore(t, target), "Cara").DeletedAt != "" {
		t.Error("the rehearsal changed the target")
	}
	res, err := f.w.Replay(ctx, f.s, target)
	if err != nil || len(res.Tombstoned) != 1 || len(res.Differences) != 0 {
		t.Fatalf("replay: %+v, %v", res, err)
	}
	if person(t, openStore(t, target), "Cara").DeletedAt == "" {
		t.Error("the replay did not tombstone the rejected person")
	}
	// a fresh target never gets her
	fresh := filepath.Join(t.TempDir(), "life.db")
	res, err = f.w.Replay(ctx, f.s, fresh)
	if err != nil || len(res.Failures) != 0 || len(res.Tombstoned) != 0 {
		t.Fatalf("a fresh replay: %+v, %v", res, err)
	}
	if id, err := openStore(t, fresh).PageID(ctx, "Cara"); err != nil || id != 0 {
		t.Errorf("a fresh replay wrote a rejected name: %d, %v", id, err)
	}
}

func TestStatusCountsATombstoneThatReplayDoesNotCarry(t *testing.T) {
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
	// the owner tombstones her by hand: the approved name writes her again in a replay
	if err := f.s.Tombstone(ctx, "cli", person(t, f.s, "Cara").ID); err != nil {
		t.Fatal(err)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || st.Uncarried != 1 || !strings.HasPrefix(st.DoNow, "Stop: 1 rows this import wrote are tombstoned here") {
		t.Fatalf("status: %d uncarried, do now %q, %v", st.Uncarried, st.DoNow, err)
	}
	editLine(t, f.w, entitiesFile, "| approved | person | Cara |", "| rejected | person | Cara |")
	if err := ownerApproves(f.w, entitiesFile); err != nil {
		t.Fatal(err)
	}
	if st, err = f.w.Status(ctx, f.s, f.trial); err != nil || st.Uncarried != 0 || st.RejectedLive != 0 || len(st.Mismatches) != 0 {
		t.Errorf("after the rejection: %d uncarried, %d rejected live, %v, %v", st.Uncarried, st.RejectedLive, st.Mismatches, err)
	}
}

func TestNamesListsDecisionStateAndEvidence(t *testing.T) {
	f := rejectLater(t)
	f.decideNames(t, Entity{Kind: "place", Name: "The Lake"})
	rows, err := f.w.Names(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	want := []NameRow{
		{Kind: "person", Name: "Cara", Decision: "rejected", Row: "written", Files: 1, From: cafe, Quote: "Coffee with Cara"},
		{Kind: "place", Name: "The Lake", Decision: "approved", Row: "not written"},
	}
	if len(rows) != len(want) {
		t.Fatalf("names: %+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, rows[i], want[i])
		}
	}
	if _, err := f.w.TombstoneRejected(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if rows, _ = f.w.Names(ctx, f.s); rows[0].Row != "tombstoned" {
		t.Errorf("after the tombstone: %+v", rows[0])
	}
}
