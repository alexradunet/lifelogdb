package importer

import (
	"testing"

	"lifelog/internal/core"
)

func TestCorrectionProofBindsCheckedFacts(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Proof.md"
	writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-02 second: 6 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "synthetic"})
	oldKey := file + "|reading|Ferritin|2031-10-01|1"
	first := recordReading(t, f, "ferritin", "2031-10-01", "", 5, oldKey)
	recordReading(t, f, "ferritin", "2031-10-02", "", 6, file+"|reading|Ferritin|2031-10-02|1")
	facts := []any{readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil), readingFact("ferritin", "2031-10-02", "6 ng/mL", "2031-10-02 second: 6 ng/mL", nil)}
	if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
		t.Fatal(err)
	}
	corrected := 4.0
	if _, key, err := f.s.Correct(ctx, "cli", first, &corrected); err != nil {
		t.Fatal(err)
	} else if err := f.w.RecordCorrection(key); err != nil {
		t.Fatal(err)
	}
	target := setupReadingKeyFixture(t)
	registerMetrics(t, target, Metric{Name: "ferritin", Unit: "ng/mL", Note: "synthetic"})
	recordReading(t, target, "ferritin", "2031-10-01", "", 5, file+"|reading|ferritin|2031-10-01|1")
	recordReading(t, target, "ferritin", "2031-10-02", "", 6, file+"|reading|ferritin|2031-10-02|1")
	proof, err := f.w.buildCorrectionProof(ctx, f.s, target.s, nil)
	if err != nil {
		t.Fatal(err)
	}
	facts[0], facts[1] = facts[1], facts[0]
	if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
		t.Fatal(err)
	}
	var mapped string
	if err := target.s.Do(ctx, "cli", func(tx *core.Tx) error {
		var err error
		mapped, err = f.w.resolveCorrectionKeyFromProof(proof, tx, "import:notebook", "ferritin", oldKey)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := file + "|reading|ferritin|2031-10-01|1"; mapped != want {
		t.Fatalf("reordered facts rebound proven root to %q, want %q", mapped, want)
	}
}
