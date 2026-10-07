package importer

import "testing"

func TestReadingAliasesDoNotCombineHistoricalRawNamespaces(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Raw namespace gaps.md"
	writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Synthetic ferritin"})
	// Deliberate historical source-key setup through the writer: independent raw
	// spelling namespaces are not evidence of one complete canonical ordinal group.
	recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|ferritin|2031-10-01|1")
	recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|FERRITIN|2031-10-01|2")
	writes := []any{
		readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
		readingFact("FERRITIN", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil),
	}
	if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, file); code(err) != 422 {
		t.Fatalf("incomplete mixed raw namespaces must refuse, got %v", err)
	}
	if importerMeasurementRows(t, f) != 2 {
		t.Fatal("ambiguous reapply changed roots")
	}
}
