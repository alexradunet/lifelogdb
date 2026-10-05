package takeout

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	if strings.Contains(strings.ToLower(out), strings.ToLower("PRIVATE_MARKER")) {
		t.Fatalf("inventory leaked a value, data key, header, filename marker, extension, or skipped photo path:\n%s", out)
	}
	if strings.Contains(out, "2020-99") {
		t.Fatalf("inventory treated a non-date value as coverage:\n%s", out)
	}

	families := familiesByName(report)
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
	if got := families["Fit Sessions"].Records; got != 3 {
		t.Fatalf("Fit Sessions records = %d, want CSV row + single object + wrapper session = 3", got)
	}
	if got := families["Fitbit Weight"].Records; got != 1 {
		t.Fatalf("Fitbit Weight records = %d, want only the public body-weight row; private data key is shape only", got)
	}
	if !hasShapePath(families["Fitbit Weight"], "$.<key>.dateTime") {
		t.Fatalf("Fitbit Weight shapes did not fold a data-key object into <key>: %#v", families["Fitbit Weight"].Shapes)
	}
	if !hasCSVColumn(families["Fit Daily Aggregates"], "Date") || hasCSVColumn(families["Fit Daily Aggregates"], "PRIVATE_MARKER_HEADER") {
		t.Fatalf("Fit daily aggregate CSV columns were not sanitized as expected: %#v", families["Fit Daily Aggregates"].CSVColumns)
	}
	if !hasTopExtension(report, "Fitbit", "<other>") || hasTopExtension(report, "Fitbit", ".private_marker") {
		t.Fatalf("top folder extensions were not bucketed safely: %#v", report.TopFolders)
	}
	for _, metric := range []string{"steps", "heart-rate", "weight"} {
		if !hasOverlap(report, metric, "2020-01") {
			t.Fatalf("overlaps = %#v, want %s in 2020-01", report.Overlaps, metric)
		}
	}
}

func TestInventoryDoesNotInventStepsOverlapWhenFitStepsColumnIsAbsent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fit/Daily activity metrics/Daily activity metrics.csv", "Date,Move Minutes count\n2020-01-10,35\n")
	writeFile(t, root, "Fitbit/Physical Activity/steps-2020-01.json", `[{"dateTime":"2020-01-10","value":"9999"}]`)
	report, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasOverlap(report, "steps", "2020-01") {
		t.Fatalf("steps overlap was invented without a Fit steps column: %#v", report.Overlaps)
	}
}

func TestInventoryStreamsLargeRecordsAndIsDeterministic(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString(`{"locations":[`)
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"timestampMs":"%d","latitudeE7":1,"longitudeE7":2}`, int64(1578618000000+i))
	}
	b.WriteString(`]}`)
	writeFile(t, root, "Location History/Records/Records.json", b.String())
	first, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	fb, _ := json.Marshal(first)
	sb, _ := json.Marshal(second)
	if !bytes.Equal(fb, sb) {
		t.Fatalf("inventory output is not deterministic:\n%s\n---\n%s", fb, sb)
	}
	families := familiesByName(first)
	if got := families["Timeline Records"].Records; got != 5000 {
		t.Fatalf("streamed Records count = %d, want 5000", got)
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
	if strings.Contains(strings.ToLower(err.Error()), strings.ToLower("PRIVATE_MARKER")) || strings.Contains(err.Error(), root) {
		t.Fatalf("error leaked a private filename or path: %v", err)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func familiesByName(report *Report) map[string]Family {
	families := map[string]Family{}
	for _, f := range report.Families {
		families[f.Name] = f
	}
	return families
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

func hasTopExtension(report *Report, top, ext string) bool {
	for _, f := range report.TopFolders {
		if f.Name != top {
			continue
		}
		for _, got := range f.Extensions {
			if got.Extension == ext {
				return true
			}
		}
	}
	return false
}

func hasOverlap(report *Report, metric, month string) bool {
	for _, o := range report.Overlaps {
		if o.Metric != metric {
			continue
		}
		for _, m := range o.Months {
			if m == month {
				return true
			}
		}
	}
	return false
}
