package importer

import (
	"testing"
)

func TestCorrectionIntentAdditionalInvariants(t *testing.T) {
	t.Run("same value target owner leaf is not a baseline identity", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, root := importedMoodCorrectionFixture(t)
		v4 := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", root, &v4); err != nil {
			t.Fatal(err)
		}
		target, targetRoot := importedMoodCorrectionFixture(t)
		v3 := 3.0
		if _, _, err := target.s.Correct(ctx, "cli", targetRoot, &v3); err != nil {
			t.Fatal(err)
		}
		before, _, _ := measurementRowsAndValue(t, target)
		if _, err := f.w.replayCorrections(ctx, target.s); err == nil {
			t.Fatal("accepted unrelated target leaf only because its value matched baseline")
		}
		if rows, _, _ := measurementRowsAndValue(t, target); rows != before {
			t.Fatal("conflict changed target rows")
		}
	})
	t.Run("unapproved workspace cannot recover pending intents", func(t *testing.T) {
		resetCorrectionHooks(t)
		f, root := importedMoodCorrectionFixture(t)
		correctionAfterReadyHook = func(correctionIntent) error { return testPendingFailure{} }
		v4 := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", root, &v4); err == nil {
			t.Fatal("expected pending intent")
		}
		if err := f.w.DraftRules("source: import:other\n"); err != nil {
			t.Fatal(err)
		}
		before, _, _ := measurementRowsAndValue(t, f)
		if err := f.w.RecoverCorrections(ctx, f.s); err == nil {
			t.Fatal("recovered despite changed unapproved workspace source")
		}
		if rows, _, _ := measurementRowsAndValue(t, f); rows != before {
			t.Fatal("source refusal changed rows")
		}
	})
}

type testPendingFailure struct{}

func (testPendingFailure) Error() string { return "synthetic pending publication" }
