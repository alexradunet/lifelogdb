package importer

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lifelog/internal/core"
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
	correctionAfterReadyHook, correctionAfterCommitHook = nil, nil
	t.Cleanup(func() { correctionAfterReadyHook, correctionAfterCommitHook = oldReady, oldCommit })
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
