package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addSourceFile(t *testing.T, f *fixture, file, body string) {
	t.Helper()
	p := filepath.Join(f.w.Source, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupEvidence(t *testing.T, file, body string) *fixture {
	t.Helper()
	return setupEvidenceMetric(t, file, body, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin (blood)"}, Metric{Name: "mood", Note: "1-5"})
}

func setupEvidenceMetric(t *testing.T, file, body string, metrics ...Metric) *fixture {
	t.Helper()
	f := setup(t)
	addSourceFile(t, f, file, body)
	f.approveRules(t, rulesBody)
	if _, err := f.w.MakeLedger(); err != nil {
		t.Fatal(err)
	}
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
	return f
}

func readingFacts(file, metric, day, value, unit, quote string) map[string]any {
	reading := map[string]any{"metric": metric, "day": day, "value": value}
	if unit != "" {
		reading["unit"] = unit
	}
	return map[string]any{"file": file, "writes": []any{map[string]any{"reading": reading, "quote": quote}}}
}

func ferritinFacts(file, day, value, unit, quote string) map[string]any {
	return readingFacts(file, "ferritin", day, value, unit, quote)
}

func TestNumericSourceEvidence(t *testing.T) {
	rejects := []struct {
		name string
		file string
		body string
		day  string
		val  string
		unit string
		q    string
		want string
	}{
		{
			name: "decimal fragment",
			file: "Medical/DecimalFragment.md",
			body: "2031-06-01 ferritin 48.5 ng/mL\n",
			day:  "2031-06-01",
			val:  "48 ng/mL",
			q:    "2031-06-01 ferritin 48.5 ng/mL",
			want: "exactly as written",
		},
		{
			name: "omitted sign",
			file: "Medical/OmittedSign.md",
			body: "2031-06-02 ferritin -48 ng/mL\n",
			day:  "2031-06-02",
			val:  "48 ng/mL",
			q:    "2031-06-02 ferritin -48 ng/mL",
			want: "exactly as written",
		},
		{
			name: "exponent fragment",
			file: "Medical/ExponentFragment.md",
			body: "2031-06-03 ferritin 1e-2 ng/mL\n",
			day:  "2031-06-03",
			val:  "-2 ng/mL",
			q:    "2031-06-03 ferritin 1e-2 ng/mL",
			want: "exactly as written",
		},
		{
			name: "comma decimal fragment",
			file: "Medical/CommaDecimalFragment.md",
			body: "2031-06-04 ferritin 48,5 ng/mL\n",
			day:  "2031-06-04",
			val:  "48 ng/mL",
			q:    "2031-06-04 ferritin 48,5 ng/mL",
			want: "exactly as written",
		},
		{
			name: "censored exact",
			file: "Medical/CensoredExact.md",
			body: "2031-06-05 ferritin <5 ng/mL\n",
			day:  "2031-06-05",
			val:  "5 ng/mL",
			q:    "2031-06-05 ferritin <5 ng/mL",
			want: "kept_as_text",
		},
		{
			name: "source unit relabeled",
			file: "Medical/RelabeledUnit.md",
			body: "2031-06-06 ferritin 48 mg/L\n",
			day:  "2031-06-06",
			val:  "48",
			unit: "ng/mL",
			q:    "2031-06-06 ferritin 48 mg/L",
			want: "source evidence",
		},
		{
			name: "clipped censored context",
			file: "Medical/ClippedCensored.md",
			body: "2031-06-07 ferritin <48 ng/mL\n",
			day:  "2031-06-07",
			val:  "48 ng/mL",
			q:    "48 ng/mL",
			want: "kept_as_text",
		},
		{
			name: "clipped sign context",
			file: "Medical/ClippedSign.md",
			body: "2031-06-08 ferritin -48 ng/mL\n",
			day:  "2031-06-08",
			val:  "48 ng/mL",
			q:    "48 ng/mL",
			want: "exactly as written",
		},
		{
			name: "clipped unit context",
			file: "Medical/ClippedUnit.md",
			body: "2031-06-09 ferritin 48 ng/mL\n",
			day:  "2031-06-09",
			val:  "48",
			unit: "ng",
			q:    "48 ng",
			want: "source evidence",
		},
		{
			name: "word approximate",
			file: "Medical/WordApproximate.md",
			body: "2031-06-10 ferritin approximately 48 ng/mL\n",
			day:  "2031-06-10",
			val:  "48 ng/mL",
			q:    "2031-06-10 ferritin approximately 48 ng/mL",
			want: "kept_as_text",
		},
		{
			name: "abbreviated approximate",
			file: "Medical/AbbrevApproximate.md",
			body: "2031-06-11 ferritin approx. 48 ng/mL\n",
			day:  "2031-06-11",
			val:  "48 ng/mL",
			q:    "2031-06-11 ferritin approx. 48 ng/mL",
			want: "kept_as_text",
		},
	}
	assertReject := func(t *testing.T, f *fixture, file string, facts map[string]any, want string) {
		t.Helper()
		if err := f.facts(t, file, facts); err != nil {
			t.Fatal(err)
		}
		before, err := f.s.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.w.Apply(ctx, f.s, file)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Apply error = %v, want containing %q", err, want)
		}
		after, err := f.s.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if after.Readings != before.Readings {
			t.Fatalf("rejected apply wrote readings: before %d after %d", before.Readings, after.Readings)
		}
		if st, err := f.w.ledgerState(file); err != nil || st != " " {
			t.Fatalf("rejected apply changed ledger state to %q: %v", st, err)
		}
	}
	for _, tc := range rejects {
		t.Run("reject "+tc.name, func(t *testing.T) {
			f := setupEvidence(t, tc.file, tc.body)
			assertReject(t, f, tc.file, ferritinFacts(tc.file, tc.day, tc.val, tc.unit, tc.q), tc.want)
		})
	}

	t.Run("reject header unit prefix", func(t *testing.T) {
		file := "Medical/HeaderPrefix.md"
		body := "| day | dose (mg/L) |\n|---|---:|\n| 2031-06-12 | 48 |\n"
		f := setupEvidenceMetric(t, file, body, Metric{Name: "dose", Unit: "mg"})
		assertReject(t, f, file, readingFacts(file, "dose", "2031-06-12", "48", "mg", "| 2031-06-12 | 48 |"), "source evidence")
	})

	t.Run("reject conflicting inline over agreeing header", func(t *testing.T) {
		file := "Medical/ConflictingInline.md"
		body := "| day | dose (mg) |\n|---|---:|\n| 2031-06-13 | 48 mg/L |\n"
		f := setupEvidenceMetric(t, file, body, Metric{Name: "dose", Unit: "mg"})
		assertReject(t, f, file, readingFacts(file, "dose", "2031-06-13", "48", "mg", "| 2031-06-13 | 48 mg/L |"), "source evidence")
	})

	t.Run("reject unrelated same-row unit cell", func(t *testing.T) {
		file := "Medical/UnrelatedUnitCell.md"
		body := "| day | A (mg/L) | B | B unit |\n|---|---:|---:|---|\n| 2031-06-14 | 48 | 12 | ng/mL |\n"
		f := setupEvidence(t, file, body)
		assertReject(t, f, file, ferritinFacts(file, "2031-06-14", "48", "ng/mL", "| 2031-06-14 | 48 | 12 | ng/mL |"), "source evidence")
	})

	t.Run("reject same row text under different header", func(t *testing.T) {
		file := "Medical/SameRowDifferentHeader.md"
		body := "| day | ferritin (mg/L) |\n|---|---:|\n| 2031-06-15 | 48 |\n\n| day | ferritin (ng/mL) |\n|---|---:|\n| 2031-06-15 | 48 |\n"
		f := setupEvidence(t, file, body)
		assertReject(t, f, file, ferritinFacts(file, "2031-06-15", "48", "ng/mL", "| 2031-06-15 | 48 |"), "source evidence")
	})

	accepts := []struct {
		name string
		file string
		body string
		day  string
		val  string
		unit string
		q    string
	}{
		{
			name: "exact signed decimal inline unit",
			file: "Medical/SignedDecimal.md",
			body: "2031-07-01 ferritin -48.5 ng/mL\n",
			day:  "2031-07-01",
			val:  "-48.5 ng/mL",
			q:    "2031-07-01 ferritin -48.5 ng/mL",
		},
		{
			name: "markdown separate unit cell",
			file: "Medical/SeparateUnit.md",
			body: "| day | value | unit |\n|---|---:|---|\n| 2031-07-02 | 48 | ng/mL |\n",
			day:  "2031-07-02",
			val:  "48",
			unit: "ng/mL",
			q:    "| 2031-07-02 | 48 | ng/mL |",
		},
		{
			name: "markdown header unit",
			file: "Medical/HeaderUnit.md",
			body: "| day | ferritin (ng/mL) |\n|---|---:|\n| 2031-07-03 | 52 |\n",
			day:  "2031-07-03",
			val:  "52",
			unit: "ng/mL",
			q:    "| 2031-07-03 | 52 |",
		},
		{
			name: "csv separate unit cell",
			file: "Medical/SeparateUnit.csv",
			body: "day,value,unit\n2031-07-04,48,ng/mL\n",
			day:  "2031-07-04",
			val:  "48",
			unit: "ng/mL",
			q:    "2031-07-04,48,ng/mL",
		},
		{
			name: "csv numeric adjacent column",
			file: "Medical/NumericAdjacent.csv",
			body: "day,sample,value,unit\n2031-07-08,1,48,ng/mL\n",
			day:  "2031-07-08",
			val:  "48",
			unit: "ng/mL",
			q:    "2031-07-08,1,48,ng/mL",
		},
		{
			name: "csv header unit",
			file: "Medical/HeaderUnit.csv",
			body: "day,ferritin (ng/mL)\n2031-07-05,52\n",
			day:  "2031-07-05",
			val:  "52",
			unit: "ng/mL",
			q:    "2031-07-05,52",
		},
	}
	for _, tc := range accepts {
		t.Run("accept "+tc.name, func(t *testing.T) {
			f := setupEvidence(t, tc.file, tc.body)
			if err := f.facts(t, tc.file, ferritinFacts(tc.file, tc.day, tc.val, tc.unit, tc.q)); err != nil {
				t.Fatal(err)
			}
			r, err := f.w.Apply(ctx, f.s, tc.file)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if r.Summary != "1 reading (1 new)" {
				t.Fatalf("summary = %q", r.Summary)
			}
		})
	}

	t.Run("unitless result word accepts without numeric token", func(t *testing.T) {
		file := "Journal/2031-07-06.md"
		f := setupEvidence(t, file, "evening walk done\n")
		reading := map[string]any{"metric": "mood", "day": "2031-07-06", "value": "1"}
		facts := map[string]any{"file": file, "writes": []any{map[string]any{"reading": reading, "quote": "evening walk done"}}}
		if err := f.facts(t, file, facts); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatalf("result word marker: %v", err)
		}
	})

	t.Run("unitless result word rejects incompatible numeric token", func(t *testing.T) {
		file := "Journal/2031-07-07.md"
		f := setupEvidence(t, file, "evening walk 30 minutes\n")
		reading := map[string]any{"metric": "mood", "day": "2031-07-07", "value": "1"}
		facts := map[string]any{"file": file, "writes": []any{map[string]any{"reading": reading, "quote": "evening walk 30 minutes"}}}
		if err := f.facts(t, file, facts); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err == nil || !strings.Contains(err.Error(), "result word") {
			t.Fatalf("Apply error = %v, want result word refusal", err)
		}
	})
}
