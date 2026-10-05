package importer

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func importedMoodCorrectionFixture(t *testing.T) (*fixture, int64) {
	t.Helper()
	f := setup(t)
	f.approveRules(t, rulesBody)
	id, err := f.s.Record(ctx, "import:notebook", core.Reading{Metric: "Mood", Day: "2031-06-01", Value: 3, Key: "Journal/Mood.md|reading|Mood|2031-06-01|1"})
	if err != nil {
		t.Fatal(err)
	}
	return f, id
}

func resetCorrectionHooks(t *testing.T) {
	t.Helper()
	oldReady, oldCommit := correctionAfterReadyHook, correctionAfterCommitHook
	oldWrite, oldSync, oldRename, oldPostRename, oldResync := correctionTempWriteHook, correctionFileSyncHook, correctionPreRenameHook, correctionPostRenameSyncHook, correctionResyncHook
	correctionAfterReadyHook, correctionAfterCommitHook = nil, nil
	correctionTempWriteHook, correctionFileSyncHook, correctionPreRenameHook, correctionPostRenameSyncHook, correctionResyncHook = nil, nil, nil, nil, nil
	t.Cleanup(func() {
		correctionAfterReadyHook, correctionAfterCommitHook = oldReady, oldCommit
		correctionTempWriteHook, correctionFileSyncHook, correctionPreRenameHook, correctionResyncHook = oldWrite, oldSync, oldRename, oldResync
		correctionPostRenameSyncHook = oldPostRename
	})
}

func readyIntentCount(t *testing.T, f *fixture) int {
	t.Helper()
	intents, err := f.w.correctionIntents()
	if err != nil {
		t.Fatal(err)
	}
	return len(intents)
}

func measurementRowsAndValue(t *testing.T, f *fixture) (rows int, value float64, current bool) {
	t.Helper()
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM measurements`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	err := f.s.DB.R.QueryRowContext(ctx, `SELECT value FROM measurement_values WHERE day = '2031-06-01'`).Scan(&value)
	if err == nil {
		return rows, value, true
	}
	if strings.Contains(err.Error(), "no rows") {
		return rows, 0, false
	}
	t.Fatal(err)
	return 0, 0, false
}

func TestCorrectionRecovery(t *testing.T) {
	t.Run("publication boundary failures", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			install   func(error)
			pending   bool
			wantReady int
		}{
			{"write", func(err error) { correctionTempWriteHook = func(correctionIntent) error { return err } }, false, 0},
			{"file sync", func(err error) { correctionFileSyncHook = func(correctionIntent) error { return err } }, false, 0},
			{"pre-rename", func(err error) { correctionPreRenameHook = func(correctionIntent) error { return err } }, false, 0},
			{"post-rename sync", func(err error) { correctionPostRenameSyncHook = func(correctionIntent) error { return err } }, true, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				resetCorrectionHooks(t)
				f, id := importedMoodCorrectionFixture(t)
				beforeRows, beforeValue, _ := measurementRowsAndValue(t, f)
				tc.install(errors.New("synthetic " + tc.name + " failure"))
				v := 4.0
				_, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v)
				if err == nil {
					t.Fatal("correction succeeded")
				}
				if tc.pending != strings.Contains(err.Error(), "pending recovery") {
					t.Fatalf("error %v pending=%v, want %v", err, strings.Contains(err.Error(), "pending recovery"), tc.pending)
				}
				if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows || value != beforeValue {
					t.Fatalf("publication failure changed rows/value %d/%g, want %d/%g", rows, value, beforeRows, beforeValue)
				}
				if got := readyIntentCount(t, f); got != tc.wantReady {
					t.Fatalf("ready intents %d, want %d", got, tc.wantReady)
				}
			})
		}
	})

	t.Run("post-rename failure rolls SQL back and recovers once", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic post-rename failure") }
		beforeRows, beforeValue, _ := measurementRowsAndValue(t, f)
		v := 4.0
		if _, handled, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); !handled || err == nil || !strings.Contains(err.Error(), "pending recovery") {
			t.Fatalf("post-rename failure = handled %v err %v, want pending recovery", handled, err)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows || value != beforeValue {
			t.Fatalf("pending correction changed SQL before recovery: rows/value %d/%g, want %d/%g", rows, value, beforeRows, beforeValue)
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows+1 || value != 4 {
			t.Fatalf("recovery rows/value %d/%g, want %d/4", rows, value, beforeRows+1)
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows+1 || value != 4 {
			t.Fatalf("second recovery rows/value %d/%g, want unchanged", rows, value)
		}
	})

	t.Run("resync failure prevents recovery SQL", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic post-rename failure") }
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err == nil {
			t.Fatal("pending correction succeeded")
		}
		beforeRows, beforeValue, _ := measurementRowsAndValue(t, f)
		correctionAfterReadyHook = nil
		correctionResyncHook = func(correctionIntent) error { return errors.New("synthetic resync failure") }
		if err := f.w.RecoverCorrections(ctx, f.s); err == nil || !strings.Contains(err.Error(), "resync") {
			t.Fatalf("resync failure error = %v, want resync refusal", err)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows || value != beforeValue {
			t.Fatalf("resync failure changed rows/value %d/%g, want %d/%g", rows, value, beforeRows, beforeValue)
		}
	})

	t.Run("reopened workspace and database recover once", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic post-rename failure") }
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err == nil {
			t.Fatal("pending correction succeeded")
		}
		if err := f.s.DB.Close(); err != nil {
			t.Fatal(err)
		}
		w, err := Open(f.w.Dir)
		if err != nil {
			t.Fatal(err)
		}
		d, err := db.Open(f.trial)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		reopened := &fixture{w: w, s: &core.Store{DB: d}, trial: f.trial}
		if err := w.RecoverCorrections(ctx, reopened.s); err != nil {
			t.Fatal(err)
		}
		if err := w.RecoverCorrections(ctx, reopened.s); err != nil {
			t.Fatal(err)
		}
		if rows, value, _ := measurementRowsAndValue(t, reopened); rows != 2 || value != 4 {
			t.Fatalf("reopened recovery rows/value %d/%g, want 2/4", rows, value)
		}
	})

	t.Run("ambiguous after-commit error is idempotent", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterCommitHook = func(correctionIntent) error { return errors.New("synthetic commit result lost") }
		v := 4.0
		if _, handled, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); !handled || err == nil || !strings.Contains(err.Error(), "pending recovery") {
			t.Fatalf("after-commit ambiguity = handled %v err %v, want pending recovery", handled, err)
		}
		rows, value, _ := measurementRowsAndValue(t, f)
		if rows != 2 || value != 4 {
			t.Fatalf("ambiguous after commit rows/value %d/%g, want committed correction", rows, value)
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != 2 || value != 4 {
			t.Fatalf("after recovery rows/value %d/%g, want unchanged", rows, value)
		}
	})

	t.Run("nil retraction recovers", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic post-rename failure") }
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, nil); err == nil {
			t.Fatal("nil correction with pending intent succeeded")
		}
		if _, _, current := measurementRowsAndValue(t, f); !current {
			t.Fatal("retraction took effect before recovery")
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if rows, _, current := measurementRowsAndValue(t, f); rows != 2 || current {
			t.Fatalf("retraction recovery rows/current %d/%v, want 2/false", rows, current)
		}
	})

	t.Run("legacy to event replay is repeatable with nil event", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v4 := 4.0
		if fix, key, err := f.s.Correct(ctx, "cli", id, &v4); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		} else if _, handled, err := f.w.CorrectImported(ctx, f.s, "agent:owner", fix, nil); !handled || err != nil {
			t.Fatalf("nil event after legacy: handled %v err %v", handled, err)
		}
		target, _ := importedMoodCorrectionFixture(t)
		if n, err := f.w.replayCorrections(ctx, target.s); err != nil || n != 1 {
			t.Fatalf("first legacy/event replay n=%d err=%v, want 1 nil", n, err)
		}
		if rows, _, current := measurementRowsAndValue(t, target); rows != 3 || current {
			t.Fatalf("first legacy/event replay rows/current %d/%v, want 3/false", rows, current)
		}
		if n, err := f.w.replayCorrections(ctx, target.s); err != nil || n != 0 {
			t.Fatalf("repeat legacy/event replay n=%d err=%v, want 0 nil", n, err)
		}
		if rows, _, current := measurementRowsAndValue(t, target); rows != 3 || current {
			t.Fatalf("repeat legacy/event replay rows/current %d/%v, want unchanged", rows, current)
		}
	})

	t.Run("legacy fingerprint detects later edits", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v4 := 4.0
		if _, key, err := f.s.Correct(ctx, "cli", id, &v4); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		latest := latestMeasurementIDImporter(t, f)
		v5 := 5.0
		if _, handled, err := f.w.CorrectImported(ctx, f.s, "cli", latest, &v5); !handled || err != nil {
			t.Fatalf("event after legacy correction: handled %v err %v", handled, err)
		}
		if err := f.w.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Key: "Journal/Mood.md|reading|Mood|2031-06-01|1", Metric: "Mood", Value: &v4}); err != nil {
			t.Fatal(err)
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err == nil || !strings.Contains(err.Error(), "legacy corrections") {
			t.Fatalf("legacy edit recovery error = %v, want conflict", err)
		}
	})

	t.Run("status reports pending without recovery", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return errors.New("synthetic post-rename failure") }
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err == nil {
			t.Fatal("pending correction succeeded")
		}
		beforeRows, beforeValue, _ := measurementRowsAndValue(t, f)
		st, err := f.w.Status(ctx, f.s, f.trial)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Corrections) != 1 || !strings.Contains(st.Corrections[0], "pending correction intent") {
			t.Fatalf("status corrections = %#v, want pending intent", st.Corrections)
		}
		if rows, value, _ := measurementRowsAndValue(t, f); rows != beforeRows || value != beforeValue {
			t.Fatalf("status recovered pending correction: rows/value %d/%g, want %d/%g", rows, value, beforeRows, beforeValue)
		}
	})

	t.Run("concurrent requests serialize fail-closed", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, value := range []float64{4, 5} {
			wg.Add(1)
			go func(i int, value float64) {
				defer wg.Done()
				_, _, errs[i] = f.w.CorrectImported(ctx, f.s, "cli", id, &value)
			}(i, value)
		}
		wg.Wait()
		succeeded := 0
		for _, err := range errs {
			if err == nil {
				succeeded++
			}
		}
		if succeeded != 1 {
			t.Fatalf("concurrent corrections errors = %v, want exactly one success", errs)
		}
		if rows, _, _ := measurementRowsAndValue(t, f); rows != 2 {
			t.Fatalf("concurrent corrections wrote %d rows, want root+one correction", rows)
		}
	})
}

func latestMeasurementIDImporter(t *testing.T, f *fixture) int64 {
	t.Helper()
	var id int64
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT id FROM measurements ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCorrectionReplayRejectsEditedLegacyPrefix(t *testing.T) {
	resetCorrectionHooks(t)
	f, id := importedMoodCorrectionFixture(t)
	v4 := 4.0
	if _, key, err := f.s.Correct(ctx, "cli", id, &v4); err != nil {
		t.Fatal(err)
	} else if err := f.w.RecordCorrection(key); err != nil {
		t.Fatal(err)
	}
	latest := latestMeasurementIDImporter(t, f)
	v5 := 5.0
	if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", latest, &v5); err != nil {
		t.Fatal(err)
	}
	if err := f.w.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Key: "Journal/Mood.md|reading|Mood|2031-06-01|1", Metric: "Mood", Value: &v4}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	if _, err := f.w.Replay(ctx, f.s, target); err == nil || !strings.Contains(err.Error(), "legacy corrections") {
		t.Fatalf("replay with edited legacy prefix error = %v, want conflict", err)
	}
}
