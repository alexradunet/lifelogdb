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

func TestCorrectionProofPrepareResolveCount(t *testing.T) {
	needed := map[correctionRoot]bool{}
	for _, source := range []string{"import:one", "import:two"} {
		for _, file := range []string{"Medical/One.md", "Medical/Two.md"} {
			for _, metric := range []string{"alpha", "beta", "gamma"} {
				needed[correctionRoot{source, metric, file + "|reading|" + metric + "|2031-01-01|1"}] = true
			}
		}
	}
	for invocation := 0; invocation < 2; invocation++ {
		prepares, resolves := 0, 0
		proof, err := buildCorrectionProofEntries(needed, func(file string) (*Facts, []int, error) {
			prepares++
			return &Facts{File: file}, []int{invocation}, nil
		}, func(source string, f *Facts, pos []int) (resolvedReadingKeys, error) {
			resolves++
			keys := map[string]int{}
			for i, metric := range []string{"alpha", "beta", "gamma"} {
				keys[f.File+"|reading|"+metric+"|2031-01-01|1"] = i
			}
			return resolvedReadingKeys{storedKeyWrite: keys}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepares != 4 || resolves != 4 {
			t.Errorf("invocation %d: prepares=%d resolves=%d, want 4 each", invocation, prepares, resolves)
		}
		if len(proof.entries) != 12 {
			t.Fatalf("entries %d, want 12", len(proof.entries))
		}
		type group struct{ source, file string }
		snapshots := map[group]*Facts{}
		for root, entry := range proof.entries {
			g := group{root.source, entry.facts.File}
			if old := snapshots[g]; old != nil && old != entry.facts {
				t.Error("roots in one group retained different facts snapshots")
			}
			snapshots[g] = entry.facts
			if entry.positions[0] != invocation {
				t.Error("snapshot reused across invocations")
			}
		}
	}
}

func TestCorrectionProofSharesCheckedFacts(t *testing.T) {
	w, trial, intents := syntheticCorrectionProof(t, 10, 10)
	proof, err := w.buildCorrectionProof(ctx, trial, &core.Store{}, intents)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot *Facts
	var positions []int
	for _, entry := range proof.entries {
		if snapshot == nil {
			snapshot, positions = entry.facts, entry.positions
		}
		if entry.facts != snapshot || &entry.positions[0] != &positions[0] {
			t.Error("same-file roots did not share checked snapshot")
		}
		if entry.facts.Writes[entry.writeIndex].Reading.Day == "" {
			t.Error("root index lost its checked reading")
		}
	}
	if len(proof.entries) != 10 {
		t.Errorf("entries %d, want 10", len(proof.entries))
	}
}
