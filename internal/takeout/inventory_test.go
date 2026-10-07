package takeout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	if got := families["Fitbit Sleep"].Records; got != 2 {
		t.Fatalf("Fitbit Sleep records = %d, want two sleep sessions, not their nested stages or wrapper metadata", got)
	}
	if got := families["Fitbit Weight"].Records; got != 1 {
		t.Fatalf("Fitbit Weight records = %d, want only the public body-weight row; private data key is shape only", got)
	}
	if got, want := families["Fitbit Weight"].LastMonth, "2020-01"; got != want {
		t.Fatalf("Fitbit Weight last month = %q, want %q; lastModified must not extend coverage", got, want)
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

func TestInventoryRecognizedEmptyContainersAreNotUnsupported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fitbit/Sleep/sleep-empty.json", `{"sleep":[]}`)
	writeFile(t, root, "Location History/Records/Records.json", `{"locations":[]}`)
	writeFile(t, root, "Location History/Semantic Location History/2020/2020_JANUARY.json", `{"timelineObjects":[{"activitySegment":{"duration":{"startTimestamp":"2020-01-10T10:00:00Z"}}}]}`)
	report, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	families := familiesByName(report)
	for _, name := range []string{"Fitbit Sleep", "Timeline Records", "Timeline Semantic Visits"} {
		if got := families[name].UnsupportedFiles; got != 0 {
			t.Fatalf("%s unsupported files = %d, want 0 for recognized empty/no-visit container; family=%+v", name, got, families[name])
		}
	}
}

func TestInventoryReportsScalarUnknownShape(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fitbit/Sleep/sleep-scalar.json", `"PRIVATE_MARKER scalar"`)
	report, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(report)
	if strings.Contains(strings.ToLower(string(out)), strings.ToLower("PRIVATE_MARKER")) {
		t.Fatalf("scalar unsupported-shape report leaked private detail: %s", out)
	}
	f := familiesByName(report)["Fitbit Sleep"]
	if f.Records != 0 || f.UnsupportedFiles != 1 {
		t.Fatalf("scalar unsupported shape = %+v, want zero records and one unsupported file", f)
	}
}

func TestInventoryReportsUnsupportedShapeWithoutPrivateDetail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fitbit/Sleep/sleep-unknown.json", `{"unexpected":[{"when":"2020-01-10","PRIVATE_MARKER_DATA_KEY":"PRIVATE_MARKER value"}]}`)
	report, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(report)
	if strings.Contains(strings.ToLower(string(out)), strings.ToLower("PRIVATE_MARKER")) {
		t.Fatalf("unsupported-shape report leaked private detail: %s", out)
	}
	f := familiesByName(report)["Fitbit Sleep"]
	if f.Records != 0 || f.UnsupportedFiles != 1 || len(f.Shapes) == 0 {
		t.Fatalf("unsupported shape = %+v, want zero records, one unsupported file, and shapes", f)
	}
}

func TestTimestampMillisOutsideFourDigitYearsDoesNotBecomeCoverage(t *testing.T) {
	if got := monthFromExplicitField("timestampMs", "9223372036854775807"); got != "" {
		t.Fatalf("extreme timestampMs produced month %q, want none", got)
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

func TestInventoryExtractionRootIncludesSiblingTimeline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Takeout/Fitbit/Sleep/sleep-PRIVATE_MARKER.json", `{"sleep":[{"startTime":"2020-01-01","PRIVATE_MARKER":"PRIVATE_MARKER"}]}`)
	writeFile(t, root, "Timeline.json", `{"semanticSegments":[{"startTime":"2020-01-01","visit":{},"PRIVATE_MARKER":"PRIVATE_MARKER"}]}`)
	writeFile(t, root, "Takeout/Google Photos/PRIVATE_MARKER.json", `{`)
	direct, err := Inventory(filepath.Join(root, "Takeout"))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if familiesByName(wrapped)["Timeline On-Device"].Records != 1 {
		t.Error("sibling Timeline omitted")
	}
	if familiesByName(direct)["Fitbit Sleep"].Records != 1 || familiesByName(wrapped)["Fitbit Sleep"].Records != 1 {
		t.Error("wrapped family omitted or duplicated")
	}
	if strings.Contains(wrapped.String(), "PRIVATE_MARKER") {
		t.Fatal("private marker leaked")
	}
}

func TestInventoryPopulatedContainersAndExerciseOverlap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fitbit/Sleep/sleep-PRIVATE_MARKER.json", `{"sleep":[{"PRIVATE_MARKER":"PRIVATE_MARKER"}]}`)
	writeFile(t, root, "Fitbit/Physical Activity/exercise-PRIVATE_MARKER.json", `{"exercise":[{"startTime":"2020-01-10","PRIVATE_MARKER":"PRIVATE_MARKER"},{"PRIVATE_MARKER":"PRIVATE_MARKER"}]}`)
	writeFile(t, root, "Fit/Sessions/PRIVATE_MARKER.csv", "Start time,PRIVATE_MARKER\n2020-01-10,PRIVATE_MARKER\n")
	writeFile(t, root, "Fit/Sessions/PRIVATE_MARKER.json", `{"sessions":[{"startTime":"2020-02-10","PRIVATE_MARKER":"PRIVATE_MARKER"}]}`)
	writeFile(t, root, "Fitbit/Physical Activity/exercise-more.json", `[{"startTime":"2020-02-10"},{"startTime":"2020-03-10"}]`)
	writeFile(t, root, "Fit/Sessions/missing.csv", "PRIVATE_MARKER\n2020-03-10\n")
	writeFile(t, root, "Fit/Sessions/unknown.json", `{"sessions":[{"PRIVATE_MARKER":"2020-03-10"}]}`)
	r, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	f := familiesByName(r)
	if f["Fitbit Sleep"].UnsupportedFiles != 1 || f["Fitbit Exercise"].UnsupportedFiles != 1 || f["Fit Sessions"].UnsupportedFiles != 1 {
		t.Errorf("unsupported populated/mixed contents: %+v", r.Families)
	}
	for _, month := range []string{"2020-01", "2020-02"} {
		if !hasOverlap(r, "exercise", month) {
			t.Errorf("missing exercise overlap %s", month)
		}
	}
	if hasOverlap(r, "exercise", "2020-03") {
		t.Error("invented overlap")
	}
	if strings.Contains(r.String(), "PRIVATE_MARKER") {
		t.Fatal("private marker leaked")
	}
}

func TestInventorySymlinkRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Fitbit/Sleep/sleep.json", `{"sleep":[]}`)
	link := filepath.Join(t.TempDir(), "PRIVATE_MARKER")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("platform or privilege does not support directory symlinks: %v", err)
	}
	direct, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := Inventory(link)
	if err != nil {
		t.Fatal(err)
	}
	if direct.String() != linked.String() {
		t.Fatal("symlink root differs from direct root")
	}
}

// countedContext cancels at a deterministic cooperative checkpoint, without timers.
type countedContext struct {
	context.Context
	calls, limit int
	cause        error
}

func (c *countedContext) Err() error {
	c.calls++
	if c.calls >= c.limit {
		return c.cause
	}
	return nil
}

func TestInventoryContextCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			for _, kind := range []string{"traversal", "json", "csv"} {
				t.Run(kind, func(t *testing.T) {
					root := t.TempDir()
					switch kind {
					case "traversal":
						for n := 0; n < 100; n++ {
							writeFile(t, root, fmt.Sprintf("Other/%03d.txt", n), "PRIVATE_MARKER")
						}
					case "json":
						writeFile(t, root, "Fitbit/Sleep/sleep.json", `{"sleep":[{"startTime":"2020-01-01"},{"startTime":"2020-01-02"}]}`)
					case "csv":
						writeFile(t, root, "Fit/Daily activity metrics/data.csv", `Date,Step count
2020-01-01,1
2020-01-02,2
`)
					}
					ctx := &countedContext{Context: context.Background(), limit: 10, cause: cause}
					report, err := InventoryContext(ctx, root)
					if report != nil || !errors.Is(err, cause) {
						t.Fatalf("report=%v error=%v checkpoints=%d", report, err, ctx.calls)
					}
					if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "PRIVATE_MARKER") {
						t.Fatal("private error detail")
					}
				})
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := InventoryContext(ctx, "PRIVATE_MARKER_missing")
	if report != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled: %v %v", report, err)
	}
}

func TestInventoryDoesNotFollowDescendantSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "sleep.json", `{"sleep":[]}`)
	if err := os.Symlink(outside, filepath.Join(root, "Fitbit")); err != nil {
		t.Skipf("platform or privilege does not support directory symlinks: %v", err)
	}
	r, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Families) != 0 || len(r.TopFolders) != 0 {
		t.Fatal("followed descendant symlink")
	}
}

func TestGoogleHealthRecognitionAndFitCSVHeaderFamilies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Google Health/Sleep/PRIVATE_MARKER.json", `[{"logId":9007199254740993,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","minutesAsleep":420}]`)
	writeFile(t, root, "Google Health/Weight/PRIVATE_MARKER.csv", "timestamp,weight grams,data source\n2020-01-02T00:00:00Z,70000,PRIVATE_MARKER\n")
	writeFile(t, root, "Fit/Activity metrics/PRIVATE_MARKER.csv", "Start time,End time,Step count\n2020-01-02T00:00:00Z,2020-01-02T01:00:00Z,4000\n")
	writeFile(t, root, "Fit/Sessions/daily.csv", "Date,Step count,Average heart rate (bpm)\n2020-01-02,10000,60\n")
	report, err := Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	families := familiesByName(report)
	for _, name := range []string{"Google Health Legacy Sleep", "Google Health CSV (preparation unsupported)", "Fit Sessions", "Fit Daily Aggregates"} {
		if families[name].Records != 1 {
			t.Fatalf("header/family missing %s %+v", name, report)
		}
	}
	if strings.Contains(report.String(), "PRIVATE_MARKER") || strings.Contains(report.String(), "9007199254740993") || strings.Contains(report.String(), "70000") {
		t.Fatal("private inventory values leaked")
	}
}
