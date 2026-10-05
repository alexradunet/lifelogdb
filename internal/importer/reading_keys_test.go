package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestReadingKeyIdentity(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Canonical.md"
	writeSource(t, f, file, "2031-07-01 timed: 48 ng/mL\n2031-07-02 first: 49 ng/mL\n2031-07-02 second: 50 ng/mL\n2031-07-03 unicode: 7\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"}, Metric{Name: "Café", Note: "Unicode metric"})

	first := []any{
		readingFact("ferritin", "2031-07-01", "48 ng/mL", "2031-07-01 timed: 48 ng/mL", map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"}),
		readingFact("Ferritin", "2031-07-02", "49 ng/mL", "2031-07-02 first: 49 ng/mL", nil),
		readingFact("FERRITIN", "2031-07-02", "50 ng/mL", "2031-07-02 second: 50 ng/mL", nil),
		readingFact("Cafe\u0301", "2031-07-03", "7", "2031-07-03 unicode: 7", nil),
	}
	applyFacts(t, f, file, first)
	before := importerMeasurementRows(t, f)

	second := []any{
		readingFact("CAFÉ", "2031-07-03", "7", "2031-07-03 unicode: 7", nil),
		readingFact("FERRITIN", "2031-07-02", "50 ng/mL", "2031-07-02 second: 50 ng/mL", nil),
		readingFact("ferritin", "2031-07-02", "49 ng/mL", "2031-07-02 first: 49 ng/mL", nil),
		readingFact("Ferritin", "2031-07-01", "48 ng/mL", "2031-07-01 timed: 48 ng/mL", map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"}),
	}
	if err := f.facts(t, file, map[string]any{"file": file, "writes": second}); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Apply(ctx, f.s, file)
	if err != nil {
		t.Fatalf("case/NFC spelling changed only: %v", err)
	}
	if got := importerMeasurementRows(t, f); got != before {
		t.Fatalf("case/NFC spelling changed only wrote %d measurement rows, want %d", got, before)
	}
	if !strings.Contains(r.Summary, "4 existing") || strings.Contains(r.Summary, "new") {
		t.Fatalf("case/NFC spelling changed only summary = %q, want all existing", r.Summary)
	}
}

func TestLegacyReadingKeyCompatibility(t *testing.T) {
	t.Run("tied source positions fail closed", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/TiedPosition.md"
		writeSource(t, f, file, "2031-08-01 same quote: 5 ng/mL and 6 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		writes := []any{
			readingFact("Ferritin", "2031-08-01", "5 ng/mL", "2031-08-01 same quote: 5 ng/mL and 6 ng/mL", nil),
			readingFact("ferritin", "2031-08-01", "6 ng/mL", "2031-08-01 same quote: 5 ng/mL and 6 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		before := importerMeasurementRows(t, f)
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "source position") {
			t.Fatalf("tied source-position error = %v, want fail closed", err)
		}
		if got := importerMeasurementRows(t, f); got != before {
			t.Fatalf("tied source-position apply mutated measurements to %d, want %d", got, before)
		}
	})

	t.Run("apply reuses unambiguous legacy keys", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/Legacy.md"
		writeSource(t, f, file, "2031-08-01 timed: 48 ng/mL\n2031-08-02 one: 49 ng/mL\n2031-08-02 two: 50 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})

		recordReading(t, f, "ferritin", "2031-08-01", "2031-08-01T08:00:00.000Z", 48, file+"|reading|Ferritin|2031-08-01|2031-08-01T08:00:00.000Z")
		recordReading(t, f, "ferritin", "2031-08-02", "", 49, file+"|reading|Ferritin|2031-08-02|1")
		recordReading(t, f, "ferritin", "2031-08-02", "", 50, file+"|reading|Ferritin|2031-08-02|2")
		before := importerMeasurementRows(t, f)

		writes := []any{
			readingFact("ferritin", "2031-08-01", "48 ng/mL", "2031-08-01 timed: 48 ng/mL", map[string]string{"taken_at": "2031-08-01T08:00:00.000Z"}),
			readingFact("ferritin", "2031-08-02", "49 ng/mL", "2031-08-02 one: 49 ng/mL", nil),
			readingFact("ferritin", "2031-08-02", "50 ng/mL", "2031-08-02 two: 50 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		r, err := f.w.Apply(ctx, f.s, file)
		if err != nil {
			t.Fatalf("unambiguous legacy keys: %v", err)
		}
		if got := importerMeasurementRows(t, f); got != before {
			t.Fatalf("legacy key apply wrote %d rows, want %d", got, before)
		}
		if !strings.Contains(r.Summary, "3 existing") || strings.Contains(r.Summary, "new") {
			t.Fatalf("legacy key apply summary = %q, want all existing", r.Summary)
		}
	})

	t.Run("exact canonical key still refuses ambiguous mixed group", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/AmbiguousExact.md"
		writeSource(t, f, file, "2031-08-03 first quote: 5 ng/mL\n2031-08-03 second quote: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		recordReading(t, f, "ferritin", "2031-08-03", "", 5, file+"|reading|ferritin|2031-08-03|1")
		recordReading(t, f, "ferritin", "2031-08-03", "", 5, file+"|reading|Ferritin|2031-08-03|1")
		before := importerMeasurementRows(t, f)
		writes := []any{
			readingFact("Ferritin", "2031-08-03", "5 ng/mL", "2031-08-03 first quote: 5 ng/mL", nil),
			readingFact("ferritin", "2031-08-03", "5 ng/mL", "2031-08-03 second quote: 5 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("ambiguous exact key error = %v, want fail closed", err)
		}
		if got := importerMeasurementRows(t, f); got != before {
			t.Fatalf("ambiguous exact key mutated measurements to %d, want %d", got, before)
		}
	})

	t.Run("duplicate canonical and legacy roots sharing identity refuse", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/DuplicateIdentity.md"
		writeSource(t, f, file, "2031-08-04 timed: 48 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		canonicalKey := file + "|reading|ferritin|2031-08-04|2031-08-04T08:00:00.000Z"
		legacyKey := file + "|reading|Ferritin|2031-08-04|2031-08-04T08:00:00.000Z"
		recordReading(t, f, "ferritin", "2031-08-04", "2031-08-04T08:00:00.000Z", 48, canonicalKey)
		recordReading(t, f, "ferritin", "2031-08-04", "2031-08-04T08:00:00.000Z", 48, legacyKey)
		before := importerMeasurementRows(t, f)
		writes := []any{readingFact("ferritin", "2031-08-04", "48 ng/mL", "2031-08-04 timed: 48 ng/mL", map[string]string{"taken_at": "2031-08-04T08:00:00.000Z"})}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("duplicate identity error = %v, want fail closed", err)
		}
		if got := importerMeasurementRows(t, f); got != before {
			t.Fatalf("duplicate identity mutated measurements to %d, want %d", got, before)
		}
	})

	t.Run("legacy corrections retractions and event roots replay through canonical keys", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/ReplayLegacy.md"
		writeSource(t, f, file, "2031-09-01 correction: 48 ng/mL\n2031-09-02 retraction: 52 ng/mL\n2031-09-03 event: 60 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})

		correctionKey := file + "|reading|Ferritin|2031-09-01|1"
		retractionKey := file + "|reading|Ferritin|2031-09-02|1"
		eventKey := file + "|reading|Ferritin|2031-09-03|1"
		correctionID := recordReading(t, f, "ferritin", "2031-09-01", "", 48, correctionKey)
		retractionID := recordReading(t, f, "ferritin", "2031-09-02", "", 52, retractionKey)
		eventID := recordReading(t, f, "ferritin", "2031-09-03", "", 60, eventKey)

		v47 := 47.0
		if _, key, err := f.s.Correct(ctx, "cli", correctionID, &v47); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		if _, key, err := f.s.Correct(ctx, "cli", retractionID, nil); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		v59 := 59.0
		if _, handled, err := f.w.CorrectImported(ctx, f.s, "agent:owner", eventID, &v59); !handled || err != nil {
			t.Fatalf("event correction on legacy root: handled %v err %v", handled, err)
		}

		writes := []any{
			readingFact("ferritin", "2031-09-01", "48 ng/mL", "2031-09-01 correction: 48 ng/mL", nil),
			readingFact("ferritin", "2031-09-02", "52 ng/mL", "2031-09-02 retraction: 52 ng/mL", nil),
			readingFact("ferritin", "2031-09-03", "60 ng/mL", "2031-09-03 event: 60 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		if r, err := f.w.Apply(ctx, f.s, file); err != nil || !strings.Contains(r.Summary, "3 existing") {
			t.Fatalf("apply legacy roots before correction replay: %v, %v", r, err)
		}

		target := filepath.Join(t.TempDir(), "life.db")
		res, err := f.w.Replay(ctx, f.s, target)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Initialised || len(res.Differences) != 0 || !res.Integrity.OK || res.Corrections != 3 {
			t.Fatalf("legacy replay result: %+v", res)
		}
		firstRows := targetMeasurementRows(t, target)
		res, err = f.w.Replay(ctx, f.s, target)
		if err != nil {
			t.Fatal(err)
		}
		if got := targetMeasurementRows(t, target); got != firstRows {
			t.Fatalf("second legacy replay changed measurement rows to %d, want %d", got, firstRows)
		}
		if res.Corrections != 0 || len(res.Differences) != 0 {
			t.Fatalf("second legacy replay result: %+v", res)
		}
		copied := filepath.Join(t.TempDir(), "copied.db")
		if err := db.Copy(target, copied); err != nil {
			t.Fatal(err)
		}
		copiedRows := targetMeasurementRows(t, copied)
		res, err = f.w.Replay(ctx, f.s, copied)
		if err != nil {
			t.Fatal(err)
		}
		if got := targetMeasurementRows(t, copied); got != copiedRows || res.Corrections != 0 || len(res.Differences) != 0 {
			t.Fatalf("copied target replay rows %d->%d result %+v", copiedRows, got, res)
		}
	})

	t.Run("ambiguous replay leaves target and ledger unchanged", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/ReplayAmbiguous.md"
		writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		legacyKey := file + "|reading|Ferritin|2031-10-01|1"
		canonicalKey := file + "|reading|ferritin|2031-10-01|1"
		legacyID := recordReading(t, f, "ferritin", "2031-10-01", "", 5, legacyKey)
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, canonicalKey)
		v4 := 4.0
		if _, key, err := f.s.Correct(ctx, "cli", legacyID, &v4); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		writes := []any{
			readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
			readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		markLedgerDone(t, f, file)
		ledgerBefore := ledgerSnapshot(t, f)
		target := filepath.Join(t.TempDir(), "life.db")
		if err := db.Init(target); err != nil {
			t.Fatal(err)
		}
		rowsBefore := targetMeasurementRows(t, target)
		if _, err := f.w.Replay(ctx, f.s, target); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("ambiguous replay error = %v, want fail closed before target write", err)
		}
		if got := targetMeasurementRows(t, target); got != rowsBefore {
			t.Fatalf("ambiguous replay mutated target measurements to %d, want %d", got, rowsBefore)
		}
		if got := ledgerSnapshot(t, f); got != ledgerBefore {
			t.Fatalf("ambiguous replay mutated ledger\n got: %s\nwant: %s", got, ledgerBefore)
		}
	})
}

func setupReadingKeyFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	f.approveRules(t, rulesBody)
	return f
}

func writeSource(t *testing.T, f *fixture, file, body string) {
	t.Helper()
	path := filepath.Join(f.w.Source, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustLedger(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
}

func registerMetrics(t *testing.T, f *fixture, metrics ...Metric) {
	t.Helper()
	for _, m := range metrics {
		if err := f.w.ProposeMetric(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := ownerApproves(f.w, "metrics.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.RegisterMetrics(ctx, f.s); err != nil {
		t.Fatal(err)
	}
}

func readingFact(metric, day, value, quote string, extra map[string]string) map[string]any {
	r := map[string]any{"metric": metric, "day": day, "value": value}
	for k, v := range extra {
		r[k] = v
	}
	return map[string]any{"reading": r, "quote": quote}
}

func applyFacts(t *testing.T, f *fixture, file string, writes []any) {
	t.Helper()
	if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, file); err != nil {
		t.Fatal(err)
	}
}

func recordReading(t *testing.T, f *fixture, metric, day, takenAt string, value float64, key string) int64 {
	t.Helper()
	id, err := f.s.Record(ctx, "import:notebook", core.Reading{Metric: metric, Day: day, TakenAt: takenAt, Value: value, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatalf("legacy reading %s already existed", key)
	}
	return id
}

func importerMeasurementRows(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM measurements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func targetMeasurementRows(t *testing.T, dbPath string) int {
	t.Helper()
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.R.QueryRowContext(ctx, `SELECT count(*) FROM measurements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func markLedgerDone(t *testing.T, f *fixture, file string) {
	t.Helper()
	if err := f.w.mark(file, func(l *Line) error {
		l.State = "x"
		l.Note = "synthetic gate fixture"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func ledgerSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.w.Dir, "ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
