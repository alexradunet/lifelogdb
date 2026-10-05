package takeout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventorySummarizesPhaseAWithoutLeakingValuesDataKeysOrFilenames(t *testing.T) {
	report, err := Inventory(filepath.Join("testdata", "phase-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "PRIVATE_MARKER") {
		t.Fatalf("inventory leaked a value, data key, header, or filename marker:\n%s", out)
	}

	families := map[string]Family{}
	for _, f := range report.Families {
		families[f.Name] = f
	}
	for _, name := range []string{
		"Timeline Semantic Visits",
		"Timeline Records",
		"Timeline On-Device",
		"Fit Daily Aggregates",
		"Fit Sessions",
		"Fit Raw Data Sources",
		"Fitbit Sleep",
		"Fitbit Steps",
		"Fitbit Heart Rate",
		"Fitbit Weight",
		"Fitbit Exercise",
	} {
		if families[name].Name == "" {
			t.Fatalf("missing family %q in %#v", name, report.Families)
		}
	}
	if got := families["Timeline Semantic Visits"].Records; got != 2 {
		t.Fatalf("semantic visit records = %d, want 2", got)
	}
	if got, want := families["Timeline Semantic Visits"].FirstMonth, "2020-01"; got != want {
		t.Fatalf("semantic visit first month = %q, want %q", got, want)
	}
	if got, want := families["Timeline Semantic Visits"].LastMonth, "2020-02"; got != want {
		t.Fatalf("semantic visit last month = %q, want %q", got, want)
	}
	if got := families["Timeline Records"].Records; got != 2 {
		t.Fatalf("records count = %d, want 2", got)
	}
	if got := families["Fit Raw Data Sources"].Files; got != 1 {
		t.Fatalf("raw data-source file count = %d, want 1", got)
	}
	if !hasShapePath(families["Fitbit Weight"], "$.<key>.dateTime") {
		t.Fatalf("Fitbit Weight shapes did not fold a data-key object into <key>: %#v", families["Fitbit Weight"].Shapes)
	}
	if !hasCSVColumn(families["Fit Daily Aggregates"], "Date") || hasCSVColumn(families["Fit Daily Aggregates"], "PRIVATE_MARKER_HEADER") {
		t.Fatalf("Fit daily aggregate CSV columns were not sanitized as expected: %#v", families["Fit Daily Aggregates"].CSVColumns)
	}
	if len(report.Overlaps) != 1 || report.Overlaps[0].Metric != "steps" || report.Overlaps[0].Months[0] != "2020-01" {
		t.Fatalf("overlaps = %#v, want steps in 2020-01", report.Overlaps)
	}
}

func TestInventoryErrorDoesNotNamePrivateFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Fitbit", "Sleep")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PRIVATE_MARKER_bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Inventory(root)
	if err == nil {
		t.Fatal("Inventory succeeded on malformed JSON")
	}
	if strings.Contains(err.Error(), "PRIVATE_MARKER") || strings.Contains(err.Error(), root) {
		t.Fatalf("error leaked a private filename or path: %v", err)
	}
}

func hasShapePath(f Family, path string) bool {
	for _, s := range f.Shapes {
		if s.Path == path {
			return true
		}
	}
	return false
}

func hasCSVColumn(f Family, column string) bool {
	for _, c := range f.CSVColumns {
		if c.Name == column {
			return true
		}
	}
	return false
}
