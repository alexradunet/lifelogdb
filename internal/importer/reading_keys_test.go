package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/text"
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
	t.Run("fresh canonical equal-value mixed spellings rerun", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/Equal.md"
		writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		facts := []any{
			readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
			readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil),
		}
		applyFacts(t, f, file, facts)
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatalf("unchanged canonical rerun: %v", err)
		}
		if rows := importerMeasurementRows(t, f); rows != 2 {
			t.Fatalf("rows %d, want 2", rows)
		}
	})

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

	t.Run("single legacy spelling equal values can use mixed new spellings", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/SingleLegacy.md"
		writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|Ferritin|2031-10-01|1")
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|Ferritin|2031-10-01|2")
		facts := []any{
			readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
			readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatal(err)
		}
		if n := importerMeasurementRows(t, f); n != 2 {
			t.Fatalf("rows %d, want 2", n)
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

	t.Run("ambiguous historical trial without corrections refuses fresh replay", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/OldEqual.md"
		writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|Ferritin|2031-10-01|1")
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|ferritin|2031-10-01|1")
		facts := []any{
			readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
			readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
			t.Fatal(err)
		}
		markLedgerDone(t, f, file)
		target := filepath.Join(t.TempDir(), "life.db")
		if _, err := f.w.Replay(ctx, f.s, target); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("ambiguous historical replay error = %v, want fail closed", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("refused target was created: %v", err)
		}
	})

	t.Run("legacy lowercase shadow cannot correct another reading", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/Shadow.md"
		writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 6 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|Ferritin|2031-10-01|1")
		second := recordReading(t, f, "ferritin", "2031-10-01", "", 6, file+"|reading|ferritin|2031-10-01|1")
		corrected := 9.0
		if _, key, err := f.s.Correct(ctx, "cli", second, &corrected); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		facts := []any{
			readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil),
			readingFact("ferritin", "2031-10-01", "6 ng/mL", "2031-10-01 second: 6 ng/mL", nil),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
			t.Fatal(err)
		}
		markLedgerDone(t, f, file)
		target := filepath.Join(t.TempDir(), "life.db")
		res, err := f.w.Replay(ctx, f.s, target)
		if err != nil {
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("ambiguity refusal created target: %v", statErr)
			}
			return
		}
		if len(res.Differences) != 0 {
			t.Fatalf("shadow replay differences: %+v", res)
		}
		if got := targetReadingValue(t, target, file+"|reading|ferritin|2031-10-01|1"); got != 5 {
			t.Fatalf("first reading became %g; correction belongs to second reading", got)
		}
		if got := targetReadingValue(t, target, file+"|reading|ferritin|2031-10-01|2"); got != 9 {
			t.Fatalf("second reading value = %g, want 9", got)
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
		if _, key, err := f.s.Correct(ctx, "cli", eventID, &v59); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		v58 := 58.0
		if _, handled, err := f.w.CorrectImported(ctx, f.s, "agent:owner", latestMeasurementIDImporter(t, f), &v58); !handled || err != nil {
			t.Fatalf("event correction after legacy correction on legacy root: handled %v err %v", handled, err)
		}
		artifactsBefore := legacyCorrectionArtifacts(t, f)
		keysBefore := rootImportKeys(t, f, file)

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
		copiedLegacy := filepath.Join(t.TempDir(), "copied-legacy.db")
		if err := db.Copy(f.trial, copiedLegacy); err != nil {
			t.Fatal(err)
		}
		copiedRows := targetMeasurementRows(t, copiedLegacy)
		res, err = f.w.Replay(ctx, f.s, copiedLegacy)
		if err != nil {
			t.Fatal(err)
		}
		if got := targetMeasurementRows(t, copiedLegacy); got != copiedRows || res.Corrections != 0 || len(res.Differences) != 0 {
			t.Fatalf("copied legacy target replay rows %d->%d result %+v", copiedRows, got, res)
		}
		res, err = f.w.Replay(ctx, f.s, copiedLegacy)
		if err != nil {
			t.Fatal(err)
		}
		if got := targetMeasurementRows(t, copiedLegacy); got != copiedRows || res.Corrections != 0 || len(res.Differences) != 0 {
			t.Fatalf("second copied legacy replay rows %d->%d result %+v", copiedRows, got, res)
		}
		if got := legacyCorrectionArtifacts(t, f); !equalByteMaps(got, artifactsBefore) {
			t.Fatalf("legacy correction artifacts changed")
		}
		if got := rootImportKeys(t, f, file); strings.Join(got, "\n") != strings.Join(keysBefore, "\n") {
			t.Fatalf("trial root import keys changed\n got: %v\nwant: %v", got, keysBefore)
		}
	})

	t.Run("captured-with aliases use page identity not rebuilt ids", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/With.md"
		writeSource(t, f, file, "Context captured 48 ng/mL on 2031-11-01\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		capturedTrial := createImportedPage(t, f, file, "Context")
		legacyKey := file + "|reading|Ferritin|2031-11-01|1"
		root := recordReadingWith(t, f, "ferritin", "2031-11-01", 48, legacyKey, capturedTrial)
		v47 := 47.0
		if _, key, err := f.s.Correct(ctx, "cli", root, &v47); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		writes := []any{
			map[string]any{"page": map[string]any{"title": "Context"}, "quote": "Context captured"},
			readingFact("ferritin", "2031-11-01", "48 ng/mL", "Context captured 48 ng/mL on 2031-11-01", map[string]string{"with": "Context"}),
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
			t.Fatal(err)
		}
		if r, err := f.w.Apply(ctx, f.s, file); err != nil || !strings.Contains(r.Summary, "2 existing") {
			t.Fatalf("captured-with trial apply: %v, %v", r, err)
		}

		target := filepath.Join(t.TempDir(), "life.db")
		if err := db.Init(target); err != nil {
			t.Fatal(err)
		}
		d, err := db.Open(target)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		ts := &core.Store{DB: d}
		if _, _, err := ts.CreatePage(ctx, "cli", "Offset Page", "id offset"); err != nil {
			t.Fatal(err)
		}
		if _, err := ts.RegisterMetric(ctx, "cli", "mood", "", "id offset"); err != nil {
			t.Fatal(err)
		}
		if _, err := ts.Record(ctx, "cli", core.Reading{Metric: "mood", Day: "2031-01-01", Value: 1}); err != nil {
			t.Fatal(err)
		}
		res := newReplayResult(target)
		if err := f.w.replayInto(ctx, f.s, ts, res, false); err != nil {
			t.Fatal(err)
		}
		capturedTarget, rootTarget, gotValue := importedReadingMetadata(t, ts, "ferritin", "2031-11-01")
		if capturedTarget == capturedTrial || rootTarget == root {
			t.Fatalf("test did not rebuild ids: captured %d->%d root %d->%d", capturedTrial, capturedTarget, root, rootTarget)
		}
		if gotValue != 47 {
			t.Fatalf("captured-with correction value = %g, want 47", gotValue)
		}
	})

	t.Run("correction requires actual original trial root evidence", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/MissingRoot.md"
		writeSource(t, f, file, "2031-10-01 value: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		facts := []any{readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 value: 5 ng/mL", nil)}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": facts}); err != nil {
			t.Fatal(err)
		}
		markLedgerDone(t, f, file)
		corrected := 4.0
		if err := f.w.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Metric: "ferritin", Key: file + "|reading|Ferritin|2031-10-01|1", Value: &corrected}); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "life.db")
		if _, err := f.w.Replay(ctx, f.s, target); err == nil || !strings.Contains(err.Error(), "no validated original trial reading") {
			t.Fatalf("missing-root replay error = %v, want proof refusal", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("refusal created target: %v", err)
		}
	})

	t.Run("same store already-present aliased event does not acquire nested writer", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/EventAlias.md"
		writeSource(t, f, file, "2031-10-01 value: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		root := recordReading(t, f, "ferritin", "2031-10-01", "", 5, file+"|reading|Ferritin|2031-10-01|1")
		v := 4.0
		if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", root, &v); err != nil {
			t.Fatal(err)
		}
		if err := f.facts(t, file, map[string]any{"file": file, "writes": []any{readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 value: 5 ng/mL", nil)}}); err != nil {
			t.Fatal(err)
		}
		markLedgerDone(t, f, file)
		target := filepath.Join(t.TempDir(), "life.db")
		if _, err := f.w.Replay(ctx, f.s, target); err != nil {
			t.Fatal(err)
		}
		d, err := db.Open(target)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if _, err := f.w.replayCorrections(short, &core.Store{DB: d}); err != nil {
			t.Fatalf("already-present event alias lookup: %v", err)
		}
	})

	t.Run("same store alias resolver does not acquire nested writer", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/SameStore.md"
		writeSource(t, f, file, "2031-10-01 value: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		applyFacts(t, f, file, []any{readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 value: 5 ng/mL", nil)})
		corrected := 4.0
		if err := f.w.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Metric: "ferritin", Key: file + "|reading|Ferritin|2031-10-01|1", Value: &corrected}); err != nil {
			t.Fatal(err)
		}
		short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if _, err := f.w.replayCorrections(short, f.s); err != nil {
			t.Fatalf("same-store alias replay blocked/failed: %v", err)
		}
	})

	t.Run("different store handles on same file do not nest writers", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/HandleAlias.md"
		writeSource(t, f, file, "2031-10-01 value: 5 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin"})
		applyFacts(t, f, file, []any{readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 value: 5 ng/mL", nil)})
		_, root, _ := importedReadingMetadata(t, f.s, "ferritin", "2031-10-01")
		v := 4.0
		if _, key, err := f.s.Correct(ctx, "cli", root, &v); err != nil {
			t.Fatal(err)
		} else if err := f.w.RecordCorrection(key); err != nil {
			t.Fatal(err)
		}
		d, err := db.Open(f.trial)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if _, err := f.w.replayCorrections(short, f.s, &core.Store{DB: d}); err != nil {
			t.Fatalf("same-file alias proof tried to acquire another writer: %v", err)
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
	return recordReadingWith(t, f, metric, day, value, key, 0, takenAt)
}

func recordReadingWith(t *testing.T, f *fixture, metric, day string, value float64, key string, capturedWith int64, takenAt ...string) int64 {
	t.Helper()
	var at string
	if len(takenAt) > 0 {
		at = takenAt[0]
	}
	id, err := f.s.Record(ctx, "import:notebook", core.Reading{Metric: metric, Day: day, TakenAt: at, Value: value, Key: key, CapturedWith: capturedWith})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatalf("legacy reading %s already existed", key)
	}
	return id
}

func createImportedPage(t *testing.T, f *fixture, file, title string) int64 {
	t.Helper()
	var id int64
	if err := f.s.Do(ctx, "import:notebook", func(tx *core.Tx) error {
		var err error
		id, _, err = tx.CreateImported(title, nil, entityKey(file, "page", title))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func importedReadingMetadata(t *testing.T, s *core.Store, metric, day string) (capturedWith int64, rootID int64, currentValue float64) {
	t.Helper()
	if err := s.DB.R.QueryRowContext(ctx, `SELECT me.id, coalesce(me.captured_with_id, 0)
		FROM measurements me JOIN pages m ON m.id = me.metric_id
		WHERE me.source = 'import:notebook' AND m.title_key = ? AND me.day = ? AND me.supersedes_id IS NULL`, text.TitleKey(metric), day).
		Scan(&rootID, &capturedWith); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.R.QueryRowContext(ctx, `WITH RECURSIVE chain(id, value, depth) AS (
		SELECT id, value, 0 FROM measurements WHERE id = ?
		UNION ALL SELECT x.id, x.value, depth + 1 FROM measurements x JOIN chain c ON x.supersedes_id = c.id)
		SELECT value FROM chain ORDER BY depth DESC LIMIT 1`, rootID).Scan(&currentValue); err != nil {
		t.Fatal(err)
	}
	return capturedWith, rootID, currentValue
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

func targetReadingValue(t *testing.T, dbPath, key string) float64 {
	t.Helper()
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var value float64
	if err := d.R.QueryRowContext(ctx, `WITH RECURSIVE c(id, value) AS (
		SELECT id, value FROM measurements WHERE import_key = ? AND source = 'import:notebook'
		UNION ALL SELECT m.id, m.value FROM measurements m JOIN c ON m.supersedes_id = c.id)
		SELECT value FROM c ORDER BY id DESC LIMIT 1`, key).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
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

func legacyCorrectionArtifacts(t *testing.T, f *fixture) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, rel := range []string{"corrections.json"} {
		p := filepath.Join(f.w.Dir, rel)
		if b, err := os.ReadFile(p); err == nil {
			out[rel] = b
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(f.w.Dir, correctionIntentDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rel := filepath.Join(correctionIntentDir, entry.Name())
		b, err := os.ReadFile(filepath.Join(f.w.Dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		out[rel] = b
	}
	return out
}

func equalByteMaps(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || string(av) != string(bv) {
			return false
		}
	}
	return true
}

func rootImportKeys(t *testing.T, f *fixture, file string) []string {
	t.Helper()
	rows, err := f.s.DB.R.QueryContext(ctx, `SELECT import_key FROM measurements WHERE source = 'import:notebook' AND import_key LIKE ? AND supersedes_id IS NULL ORDER BY import_key`, file+"|reading|%")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTimedReadingIdentityRefusedBeforeLookup(t *testing.T) {
	for _, variant := range []string{"identical", "case", "NFC", "value", "tz", "with"} {
		t.Run(variant, func(t *testing.T) {
			a := ReadingW{Metric: "Café", Day: "2031-07-01", TakenAt: "2031-07-01T08:00:00.000Z", Value: "5"}
			b := a
			switch variant {
			case "case":
				b.Metric = "CAFÉ"
			case "NFC":
				b.Metric = "Cafe\u0301"
			case "value":
				b.Value = "6"
			case "tz":
				b.TZ = "Europe/Paris"
			case "with":
				b.With = "Synthetic Page"
			}
			f := &Facts{File: "Medical/Timed.md", Writes: []Write{{Reading: &a}, {Reading: &b}}}
			lookups := 0
			_, err := resolveReadingKeysWithLoader("import:synthetic", f, []int{0, 1}, func(_, _, _ string) ([]core.ImportedMeasurementRoot, error) {
				lookups++
				return nil, nil
			})
			if err == nil || !strings.Contains(err.Error(), "repeated timed reading identity") {
				t.Errorf("error = %v, want repeated timed identity refusal", err)
			}
			if lookups != 0 {
				t.Errorf("lookups = %d, want 0", lookups)
			}
		})
	}
}

func TestTimedReadingIdentityCheckApplyAtomic(t *testing.T) {
	for _, variant := range []string{"identical", "case", "NFC", "value", "tz", "with"} {
		t.Run(variant, func(t *testing.T) {
			f := setupReadingKeyFixture(t)
			file := "Medical/Timed.md"
			quote := "2031-07-01 Café timed: 5 ng/mL and 6 ng/mL with Synthetic Page"
			writeSource(t, f, file, quote)
			mustLedger(t, f)
			registerMetrics(t, f, Metric{Name: "Café", Unit: "ng/mL", Note: "synthetic"})
			metric, value := "Café", "5 ng/mL"
			extra := map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"}
			switch variant {
			case "case":
				metric = "CAFÉ"
			case "NFC":
				metric = "Cafe\u0301"
			case "value":
				value = "6 ng/mL"
			case "tz":
				extra["tz"] = "Europe/Paris"
			case "with":
				extra["with"] = "Synthetic Page"
			}
			writes := []any{
				map[string]any{"page": map[string]string{"title": "Synthetic Page"}, "quote": quote},
				readingFact("Café", "2031-07-01", "5 ng/mL", quote, map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"}),
				readingFact(metric, "2031-07-01", value, quote, extra),
			}
			if err := f.facts(t, file, map[string]any{"file": file, "writes": writes}); err != nil {
				t.Fatal(err)
			}
			ledgerPath := filepath.Join(f.w.Dir, "ledger.md")
			before, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			var entities int
			if err := f.s.DB.R.QueryRow("SELECT count(*) FROM entities").Scan(&entities); err != nil {
				t.Fatal(err)
			}
			for run := 0; run < 2; run++ {
				for _, op := range []struct {
					name string
					fn   func(context.Context, *core.Store, string) (*Report, error)
				}{{"Check", f.w.Check}, {"Apply", f.w.Apply}} {
					if _, err := op.fn(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "repeated timed reading identity") {
						t.Errorf("%s: %v", op.name, err)
					}
				}
			}
			after, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Error("refusal changed ledger")
			}
			var got int
			if err := f.s.DB.R.QueryRow("SELECT count(*) FROM entities").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != entities || importerMeasurementRows(t, f) != 0 {
				t.Error("refusal persisted writes")
			}
		})
	}
}

func TestTimedReadingDistinctInstants(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/DistinctTimed.md"
	quote := "2031-07-01 Café timed: 5 ng/mL"
	writeSource(t, f, file, quote)
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Café", Unit: "ng/mL", Note: "synthetic"})
	writes := []any{
		readingFact("Café", "2031-07-01", "5 ng/mL", quote, map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"}),
		readingFact("CAFE\u0301", "2031-07-01", "5 ng/mL", quote, map[string]string{"taken_at": "2031-07-01T09:00:00.000Z"}),
	}
	applyFacts(t, f, file, writes)
	if _, err := f.w.Check(ctx, f.s, file); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, file); err != nil {
		t.Fatal(err)
	}
	if got := importerMeasurementRows(t, f); got != 2 {
		t.Errorf("readings %d, want 2", got)
	}
}
