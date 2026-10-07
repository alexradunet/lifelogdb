package importer

import (
	"errors"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspaceScopedValueCorrectionRecoveryAndRelocationRefusal(t *testing.T) {
	resetCorrectionHooks(t)
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, _, err := f.s.CreatePage(ctx, "cli", "Workout", ""); err != nil {
		t.Fatal(err)
	}
	captured, _, err := f.s.CreatePage(ctx, "cli", "2031-06-01", "")
	if err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error {
		var err error
		session, _, err = tx.CaptureSession(core.SessionInput{Kind: "Workout", Day: "2031-06-02", StartAt: "2031-06-01T22:00:00.000Z"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	root, err := f.s.Record(ctx, "import:notebook", core.Reading{Metric: "Mood", Day: "2031-06-01", Value: 3, Key: "Journal/Mood.md|reading|Mood|2031-06-01|1", SessionID: session, CapturedWith: captured, TakenAt: "2031-06-01T23:00:00.000Z", TZ: "Claimed/Zone"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		if err := filepath.WalkDir(f.w.Dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".db" || filepath.Ext(path) == ".db-wal" || filepath.Ext(path) == ".db-shm" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err == nil {
				out[path] = string(b)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	assertRefused := func(id int64) {
		t.Helper()
		beforeFiles := snapshot()
		before, err := f.s.Measurement(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		counts, err := f.s.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.RelocateReading(ctx, "cli", id, core.Reading{Metric: "Mood", Day: "2031-06-01", Value: 3}); err == nil {
			t.Fatal("imported/keyed relocation accepted")
		}
		after, err := f.s.Measurement(ctx, id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("refusal changed row %+v %v", after, err)
		}
		afterCounts, err := f.s.Counts(ctx)
		if err != nil || !reflect.DeepEqual(counts, afterCounts) || !reflect.DeepEqual(beforeFiles, snapshot()) {
			t.Fatalf("refusal changed SQL/workspace %v", err)
		}
	}
	assertRefused(root)
	correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic interruption after durable publication") }
	four := 4.0
	if _, handled, err := f.w.CorrectImported(ctx, f.s, "cli", root, &four); !handled || err == nil {
		t.Fatalf("publication interruption %v %v", handled, err)
	}
	original, err := f.s.Measurement(ctx, root)
	if err != nil || !original.Current || original.Value != 3 || readyIntentCount(t, f) != 1 {
		t.Fatalf("interruption %+v %v", original, err)
	}
	have, err := f.s.Session(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	changed := have.SessionInput
	changed.Day = "2031-06-03"
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { return tx.EditSession(session, have.Version, changed) }); err != nil {
		t.Fatal(err)
	}
	correctionAfterReadyHook = nil
	if err := f.s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(f.trial)
	if err != nil {
		t.Fatal(err)
	}
	f.s.DB = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	var leaf int64
	if err := f.s.DB.R.QueryRow("SELECT id FROM measurement_values WHERE supersedes_id=?", root).Scan(&leaf); err != nil {
		t.Fatal(err)
	}
	row, err := f.s.Measurement(ctx, leaf)
	if err != nil || row.Value != 4 || row.SessionID != session || row.Day != "2031-06-01" || row.TakenAt != "2031-06-01T23:00:00.000Z" || row.TZ != "Claimed/Zone" || row.CapturedWith != captured {
		t.Fatalf("recovered attribution %+v %v", row, err)
	}
	files := snapshot()
	if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.s.DB.R.QueryRow("SELECT count(*) FROM measurements").Scan(&n); err != nil || n != 2 || !reflect.DeepEqual(files, snapshot()) {
		t.Fatalf("retry mutation %d %v", n, err)
	}
	assertRefused(leaf)
	// A plain owner correction has no event key, but must not bypass its imported ancestor.
	five := 5.0
	unkeyed, _, err := f.s.Correct(ctx, "cli", leaf, &five)
	if err != nil {
		t.Fatal(err)
	}
	assertRefused(unkeyed)
}
