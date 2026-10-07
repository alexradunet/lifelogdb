package importer

import (
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/db"
)

func TestSessionOnlyActualReplayUnappliedAndBindingChanges(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if e := os.RemoveAll(f.w.Source); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(f.w.Source, 0700); e != nil {
		t.Fatal(e)
	}
	writeSource(t, f, "Workout.md", "")
	writeSource(t, f, "fit.json", `{"fitnessActivity":"running","startTime":"2020-01-02T12:00:00Z","aggregate":[]}`)
	mustLedger(t, f)
	if _, e := f.w.PlanVault(ctx, f.s); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.ApplyVault(ctx, f.s); e != nil {
		t.Fatal(e)
	}
	binding := &FitSessionBinding{Key: "reviewed-1", Day: "2020-01-03", Activity: "running"}
	if _, e := f.w.DraftPrepared(ctx, "fit.json", "fit-session-object-v1", "Workout", binding, map[string]string{}); e != nil {
		t.Fatal(e)
	}
	if e := ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(t.TempDir(), "target.db")
	if _, e := f.w.Replay(ctx, f.s, target); e == nil {
		t.Fatal("approved but unapplied session claimed trial root")
	}
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("unapplied target effect")
	}
	if _, e := f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Replay(ctx, f.s, target); e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(target)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var day, at, key string
	if e = d.R.QueryRow(`SELECT day,start_at,import_key FROM sessions`).Scan(&day, &at, &key); e != nil || day != "2020-01-03" || at != "2020-01-02T12:00:00.000Z" || key != preparedKey("fit-session-object-v1", "fit-bound:reviewed-1", "session") {
		t.Fatalf("session-only semantics %s %s %s %v", day, at, key, e)
	}
	changed := *binding
	changed.Key = "reviewed-2"
	if _, e = f.w.DraftPrepared(ctx, "fit.json", "fit-session-object-v1", "Workout", &changed, map[string]string{}); e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Prepared(ctx, f.s, false); e == nil {
		t.Fatal("bound identity reassigned")
	}
	var count int
	if e = f.s.DB.R.QueryRow(`SELECT count(*) FROM sessions`).Scan(&count); e != nil || count != 1 {
		t.Fatal("reassigned key wrote SQL")
	}
	if _, e = f.w.DraftPrepared(ctx, "fit.json", "fit-session-object-v1", "Workout", binding, map[string]string{}); e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	// Deliberate ledger damage: ordinary Skip correctly refuses a done source.
	if e = f.w.mark("fit.json", func(line *Line) error { line.State = "-"; return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Replay(ctx, f.s, target); e == nil {
		t.Fatal("skipped session selection replayed")
	}
}
