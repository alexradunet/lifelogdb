package importer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
)

func TestCorrectionIntentInvariants(t *testing.T) {
	t.Run("actual commit failure is pending", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		c, cancel := context.WithCancel(ctx)
		defer cancel()
		correctionAfterReadyHook = func(correctionIntent) error { cancel(); return nil }
		v := 4.0
		_, _, err := f.w.CorrectImported(c, f.s, "cli", id, &v)
		if err == nil || !strings.Contains(err.Error(), "pending recovery") {
			t.Fatalf("published intent commit failure: %v", err)
		}
	})
	t.Run("committed event is not pending", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err != nil {
			t.Fatal(err)
		}
		st, err := f.w.Status(ctx, f.s, f.trial)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Corrections) != 0 {
			t.Fatalf("committed event status: %v", st.Corrections)
		}
	})
	t.Run("unrelated later owner correction conflicts", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v := 4.0
		event, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Correct(ctx, "cli", event, &v); err != nil {
			t.Fatal(err)
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err == nil {
			t.Fatal("accepted unrelated later leaf with same numeric value")
		}
	})
	t.Run("replay does not overwrite unrelated target leaf", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err != nil {
			t.Fatal(err)
		}
		target, targetRoot := importedMoodCorrectionFixture(t)
		other := 5.0
		if _, _, err := target.s.Correct(ctx, "cli", targetRoot, &other); err != nil {
			t.Fatal(err)
		}
		before, value, _ := measurementRowsAndValue(t, target)
		if _, err := f.w.replayCorrections(ctx, target.s); err == nil {
			t.Fatal("rewound unrelated target correction instead of refusing")
		}
		if rows, got, _ := measurementRowsAndValue(t, target); rows != before || got != value {
			t.Fatalf("refusal changed target: %d/%g -> %d/%g", before, value, rows, got)
		}
	})
	t.Run("event references order replay despite clock rollback", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v4, v5 := 4.0, 5.0
		first, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v4)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", first, &v5); err != nil {
			t.Fatal(err)
		}
		intents, err := f.w.correctionIntents()
		if err != nil {
			t.Fatal(err)
		}
		for _, intent := range intents {
			if intent.PredecessorKind == "legacy" {
				intent.CreatedAt = "2031-01-02T00:00:00.000Z"
			} else {
				intent.CreatedAt = "2031-01-01T00:00:00.000Z"
			}
			rewriteParentIntent(t, f, intent)
		}
		target, _ := importedMoodCorrectionFixture(t)
		if _, err := f.w.replayCorrections(ctx, target.s); err != nil {
			t.Fatal(err)
		}
		if rows, got, _ := measurementRowsAndValue(t, target); rows != 3 || got != 5 {
			t.Fatalf("rows/value %d/%g", rows, got)
		}
	})
	t.Run("historical event value is verified", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v4, v5 := 4.0, 5.0
		first, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v4)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", first, &v5); err != nil {
			t.Fatal(err)
		}
		intents, err := f.w.correctionIntents()
		if err != nil {
			t.Fatal(err)
		}
		for _, intent := range intents {
			if intent.PredecessorKind == "legacy" {
				intent.Value = &v5
				rewriteParentIntent(t, f, intent)
			}
		}
		if err := f.w.RecoverCorrections(ctx, f.s); err == nil {
			t.Fatal("accepted historical event whose stored row has different value")
		}
	})
	t.Run("status sees edited legacy prefix", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, id := importedMoodCorrectionFixture(t)
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", id, &v); err != nil {
			t.Fatal(err)
		}
		if err := f.w.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Metric: "Mood", Key: "Journal/Mood.md|reading|Mood|2031-06-01|1", Value: &v}); err != nil {
			t.Fatal(err)
		}
		st, err := f.w.Status(ctx, f.s, f.trial)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Corrections) == 0 || !strings.Contains(st.Corrections[0], "conflict") {
			t.Fatalf("legacy conflict status: %v", st.Corrections)
		}
	})
}

func rewriteParentIntent(t *testing.T, f *fixture, intent correctionIntent) {
	t.Helper()
	b, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.w.Dir, correctionIntentDir, intent.EventKey+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
