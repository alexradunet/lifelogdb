package importer

import (
	"strings"
	"testing"
)

// A scale's range (metrics.unit "1-5", "0-10") describes the metric, not a quantity the source writes next to
// the number: a line "mood: 4" is the whole evidence of a Mood reading, while a physical unit still has to be
// written beside its number so that nothing is relabeled or converted (the guide's "Numbers and units").

// readingRow is the stored reading of a metric on a day: its value and key, read straight from the file.
func readingRow(t *testing.T, f *fixture, metric, day string) (value float64, key, source string) {
	t.Helper()
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT me.value, me.import_key, me.source FROM measurements me
	    WHERE me.day = ? AND me.metric_id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, day, strings.ToLower(metric)).
		Scan(&value, &key, &source); err != nil {
		t.Fatalf("the %s reading of %s: %v", metric, day, err)
	}
	return
}

func metricUnit(t *testing.T, f *fixture, name string) string {
	t.Helper()
	var unit string
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT unit FROM metrics WHERE id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, strings.ToLower(name)).Scan(&unit); err != nil {
		t.Fatalf("the unit of %s: %v", name, err)
	}
	return unit
}

func TestMoodLineImportsAsAMoodReading(t *testing.T) {
	const file = "Journal/2031-08-01.md"
	// the model proposes Mood the way the source writes it: with no unit
	f := setupEvidenceMetric(t, file, "Walked to the lake.\nmood: 4\n", Metric{Name: "mood", Note: "1-5"})
	if got := metricUnit(t, f, "mood"); got != "1-5" {
		t.Fatalf("registering the proposed Mood changed its unit to %q", got)
	}
	if err := f.facts(t, file, readingFacts(file, "mood", "2031-08-01", "4", "", "mood: 4")); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Apply(ctx, f.s, file)
	if err != nil {
		t.Fatalf("mood: 4 refused: %v", err)
	}
	if len(r.Outcomes) != 1 || r.Outcomes[0].Kind != "reading" || r.Outcomes[0].Status != "new" {
		t.Errorf("outcomes %+v, want one new reading", r.Outcomes)
	}
	value, key, source := readingRow(t, f, "mood", "2031-08-01")
	if value != 4 || key != file+"|reading|mood|2031-08-01|1" || source != "import:notebook" {
		t.Errorf("stored reading: value %g, key %q, source %q", value, key, source)
	}
	// a second apply writes nothing
	if r, err = f.w.Apply(ctx, f.s, file); err != nil || r.Outcomes[0].Status != "existing" || totalMeasurements(t, f) != 1 {
		t.Errorf("second apply: %+v, %v, %d measurements", r, err, totalMeasurements(t, f))
	}
	// registering again is a no-op for the adopted metric
	if done, err := f.w.RegisterMetrics(ctx, f.s); err != nil || len(done) != 1 || done[0] != "mood: existing" {
		t.Errorf("second register-metrics: %v, %v", done, err)
	}
}

func TestMoodOutsideItsRangeIsRefusedAndNothingIsStored(t *testing.T) {
	for _, value := range []string{"7", "0", "2.5"} {
		t.Run(value, func(t *testing.T) {
			const file = "Journal/2031-08-02.md"
			f := setupEvidenceMetric(t, file, "mood: "+value+"\n", Metric{Name: "mood", Note: "1-5"})
			facts := map[string]any{"file": file, "writes": []any{
				map[string]any{"page": map[string]any{"title": "2031-08-02"}, "quote": "mood: " + value},
				map[string]any{"reading": map[string]any{"metric": "mood", "day": "2031-08-02", "value": value}, "quote": "mood: " + value},
			}}
			if err := f.facts(t, file, facts); err != nil {
				t.Fatal(err)
			}
			before, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for name, run := range map[string]func() error{
				"check": func() error { return checkErr(f.w, f.s, file) },
				"apply": func() error { _, err := f.w.Apply(ctx, f.s, file); return err },
			} {
				err := run()
				if err == nil || !strings.Contains(err.Error(), "1-5") || !strings.Contains(err.Error(), "whole number") {
					t.Errorf("%s of mood: %s: %v, want a refusal naming the 1-5 scale", name, value, err)
				}
				if code(err) != 422 {
					t.Errorf("%s of mood: %s: status %d, want 422", name, value, code(err))
				}
			}
			after, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if after.Pages != before.Pages || after.Readings != 0 || totalMeasurements(t, f) != 0 {
				t.Errorf("a refused file left rows behind: pages %d -> %d, %d measurements", before.Pages, after.Pages, totalMeasurements(t, f))
			}
			if lines, _, _ := f.w.Ledger(); ledgerState(lines, file) == "x" {
				t.Error("the refused file was marked done")
			}
		})
	}
}

func ledgerState(lines []Line, file string) string {
	for _, l := range lines {
		if l.File == file {
			return l.State
		}
	}
	return ""
}

// A word does not give a scale's number: the result-word exception is for a 0/1 habit with no unit, and a scale is
// not one even when the model proposed it without a unit.
func TestAScaleNeedsItsNumberInTheQuote(t *testing.T) {
	const file = "Journal/2031-08-03.md"
	f := setupEvidenceMetric(t, file, "mood: low\n", Metric{Name: "mood", Note: "1-5"})
	if err := f.facts(t, file, readingFacts(file, "mood", "2031-08-03", "1", "", "mood: low")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "number") {
		t.Errorf("a word given as the value 1 of a scale: %v", err)
	}
	if n := totalMeasurements(t, f); n != 0 {
		t.Errorf("%d measurements stored", n)
	}
}

func TestAPhysicalUnitStillNeedsItsEvidence(t *testing.T) {
	weight := Metric{Name: "weight", Unit: "kg", Note: "body weight"}
	t.Run("no unit beside the number", func(t *testing.T) {
		const file = "Journal/2031-08-04.md"
		f := setupEvidenceMetric(t, file, "weight: 70\n", weight)
		if err := f.facts(t, file, readingFacts(file, "weight", "2031-08-04", "70", "kg", "weight: 70")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "no unambiguous source evidence") {
			t.Errorf("weight: 70 as kg: %v", err)
		}
		if n := totalMeasurements(t, f); n != 0 {
			t.Errorf("%d measurements stored", n)
		}
	})
	t.Run("the facts omit the unit", func(t *testing.T) {
		const file = "Journal/2031-08-05.md"
		f := setupEvidenceMetric(t, file, "weight: 70 kg\n", weight)
		if err := f.facts(t, file, readingFacts(file, "weight", "2031-08-05", "70", "", "weight: 70 kg")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "not the approved metric unit") {
			t.Errorf("weight: 70 kg with no unit in the facts: %v", err)
		}
	})
	t.Run("the unit is written", func(t *testing.T) {
		const file = "Journal/2031-08-06.md"
		f := setupEvidenceMetric(t, file, "weight: 70 kg\n", weight)
		if err := f.facts(t, file, readingFacts(file, "weight", "2031-08-06", "70", "kg", "weight: 70 kg")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatal(err)
		}
		if value, _, _ := readingRow(t, f, "weight", "2031-08-06"); value != 70 {
			t.Errorf("weight %g", value)
		}
	})
	t.Run("a scale takes no physical unit", func(t *testing.T) {
		const file = "Journal/2031-08-07.md"
		f := setupEvidenceMetric(t, file, "mood: 4 kg\n", Metric{Name: "mood", Note: "1-5"})
		if err := f.facts(t, file, readingFacts(file, "mood", "2031-08-07", "4", "kg", "mood: 4 kg")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "unit") {
			t.Errorf("Mood relabeled as kg: %v", err)
		}
		if n := totalMeasurements(t, f); n != 0 {
			t.Errorf("%d measurements stored", n)
		}
	})
}

// A new scale is proposed with its range as its unit, which no source line writes; it is registered with that
// unit, and its readings are imported on the number alone and held to the range.
func TestAProposedScaleIsRegisteredWithItsRange(t *testing.T) {
	const file = "Journal/2031-08-08.md"
	f := setupEvidenceMetric(t, file, "2031-08-08 energy: 3\n2031-08-09 energy: 9\n2031-08-10 energy: 3/5 in the afternoon\n",
		Metric{Name: "energy", Unit: "1-5", Note: "how much energy"})
	if got := metricUnit(t, f, "energy"); got != "1-5" {
		t.Fatalf("energy registered with unit %q, want 1-5", got)
	}
	apply := func(day, value, quote string) error {
		t.Helper()
		if err := f.facts(t, file, readingFacts(file, "energy", day, value, "", quote)); err != nil {
			t.Fatal(err)
		}
		_, err := f.w.Apply(ctx, f.s, file)
		return err
	}
	if err := apply("2031-08-08", "3", "2031-08-08 energy: 3"); err != nil {
		t.Fatalf("energy: 3 refused: %v", err)
	}
	if value, _, _ := readingRow(t, f, "energy", "2031-08-08"); value != 3 {
		t.Errorf("energy %g, want 3", value)
	}
	if err := apply("2031-08-09", "9", "2031-08-09 energy: 9"); err == nil || !strings.Contains(err.Error(), "1-5") {
		t.Errorf("energy: 9 on a 1-5 scale: %v", err)
	}
	// the number alone is the evidence, whatever the source adds after it
	if err := apply("2031-08-10", "3", "2031-08-10 energy: 3/5 in the afternoon"); err != nil {
		t.Errorf("energy: 3/5: %v", err)
	}
	if n := totalMeasurements(t, f); n != 2 {
		t.Errorf("%d measurements, want the 2 valid readings", n)
	}
}

// The approved unit of Mood may repeat its range, but never another unit: a unit never changes.
func TestAMetricProposedOverAScaleAdoptsItOrConflicts(t *testing.T) {
	for _, c := range []struct {
		unit    string
		adopted bool
	}{{"", true}, {"1-5", true}, {"kg", false}, {"1-10", false}} {
		t.Run("unit "+c.unit, func(t *testing.T) {
			f := setup(t)
			f.approveRules(t, rulesBody)
			if err := f.w.ProposeMetric(Metric{Name: "Mood", Unit: c.unit, Note: "1-5"}); err != nil {
				t.Fatal(err)
			}
			if err := ownerApproves(f.w, "metrics.md"); err != nil {
				t.Fatal(err)
			}
			done, err := f.w.RegisterMetrics(ctx, f.s)
			switch {
			case c.adopted && (err != nil || len(done) != 1 || done[0] != "Mood: existing"):
				t.Errorf("Mood with unit %q: %v, %v; want it adopted", c.unit, done, err)
			case !c.adopted && (err == nil || code(err) != 422 && code(err) != 409 || !strings.Contains(err.Error(), "never changes")):
				t.Errorf("Mood with unit %q: %v, %v; want a conflict", c.unit, done, err)
			}
			if got := metricUnit(t, f, "mood"); got != "1-5" {
				t.Errorf("Mood's unit is now %q", got)
			}
		})
	}
}
