package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func assertEvidenceRefused(t *testing.T, f *fixture, file string, facts map[string]any) {
	t.Helper()
	if err := f.facts(t, file, facts); err != nil {
		t.Fatal(err)
	}
	ledger, err := os.ReadFile(f.w.file("ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	before := totalMeasurements(t, f)
	for _, apply := range []bool{false, true} {
		var err error
		if apply {
			_, err = f.w.Apply(ctx, f.s, file)
		} else {
			_, err = f.w.Check(ctx, f.s, file)
		}
		if err == nil {
			t.Errorf("apply=%v accepted unsafe evidence", apply)
		}
	}
	if totalMeasurements(t, f) != before {
		t.Error("refusal wrote measurements")
	}
	after, err := os.ReadFile(f.w.file("ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(ledger) {
		t.Error("refusal changed ledger")
	}
}

func TestReadingApprovedUnitBeforeMarker(t *testing.T) {
	file := "Medical/2031-06-12.md"
	f := setupEvidenceMetric(t, file, "walk done\n", Metric{Name: "dose", Unit: "mg"})
	body, err := os.ReadFile(f.w.file("metrics.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.w.file("metrics.md"), []byte(strings.ReplaceAll(string(body), "| mg |", "|  |")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, "metrics.md"); err != nil {
		t.Fatal(err)
	}
	// Do not register the changed approval: the existing metric still has mg.
	assertEvidenceRefused(t, f, file, readingFacts(file, "dose", "2031-06-12", "1", "mg", "walk done"))
}

func TestCroppedTableUnitEvidence(t *testing.T) {
	for _, tc := range []struct{ name, body, quote string }{
		{"separate", "| day | value | unit |\n|---|---|---|\n| 2031-06-12 | 48 | mg |\n", "2031-06-12 | 48"},
		{"header", "| day | value (mg) | comment |\n|---|---|---|\n| 2031-06-12 | 48 | sample |\n", "2031-06-12 | 48"},
		{"repeated", "| day | A | B (mg) |\n|---|---|---|\n| 2031-06-12 | 48 | 48 |\n", "2031-06-12 | 48"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := "Medical/Cropped.md"
			f := setupEvidenceMetric(t, file, tc.body, Metric{Name: "dose"})
			assertEvidenceRefused(t, f, file, readingFacts(file, "dose", "2031-06-12", "48", "", tc.quote))
		})
	}
}

// Repeated checks share the already-built immutable token/table slices, not a global cache.
func TestReadingEvidenceContextSnapshot(t *testing.T) {
	source := "Cafe\u0301\u2003測定\n| day | value (mg) | note |\n|---|---|---|\n| 2031-06-12 | 48 | échantillon |\n"
	quote := "2031-06-12 | 48"
	e := newReadingEvidence("Synthetic.md", source)
	pos := wholeIndex(e.collapsed, quote)
	if pos < 0 {
		t.Fatal("missing normalized quote")
	}
	tokens, tables := &e.tokens[0], &e.tables[0]
	approved := map[string]Metric{"dose": {Name: "dose", Unit: "mg"}}
	for i := 0; i < 20; i++ {
		if err := e.check(quote, pos, "48", "mg", false, approved); err != nil {
			t.Fatal(err)
		}
		if tokens != &e.tokens[0] || tables != &e.tables[0] {
			t.Fatal("context rebuilt during reading check")
		}
	}
	w, f, _, metrics := syntheticEvidenceCheck(2)
	if _, errs := w.checkStatic(f, source, &Rules{}, metrics); len(errs) == 0 {
		t.Fatal("unrelated source accepted")
	}
	facts := &Facts{File: "Synthetic.md", Writes: []Write{{Quote: quote, Reading: &ReadingW{Metric: "dose", Day: "2031-06-12", Value: "48", Unit: "mg"}}}}
	if _, errs := w.checkStatic(facts, source, &Rules{}, approved); len(errs) != 0 {
		t.Fatal(errs)
	}
	changed := strings.ReplaceAll(source, "(mg)", "(kg)")
	if _, errs := w.checkStatic(facts, changed, &Rules{}, approved); len(errs) == 0 {
		t.Fatal("changed source reused old evidence")
	}
}

func TestEvidenceBackwardRuneAndOffsets(t *testing.T) {
	for _, s := range []string{"", "a", "é", "測定", "a\xff", "\xffé"} {
		var want rune
		size := 0
		for i, r := range s {
			want = r
			size = len(s) - i
		}
		got, n := lastRuneSize(s)
		if got != want || n != size || lastRune(s) != want {
			t.Fatalf("%q: got %U/%d want %U/%d", s, got, n, want, size)
		}
	}
	for _, source := range []string{"", "  é\u2003測定\n48  ", "a\xff  b"} {
		offsets := collapsedOffsets(source)
		for i := range source {
			// A boundary at a non-space rune includes the pending separator.
			prefix := collapse(source[:i])
			want := len(prefix)
			r := firstRune(source[i:])
			if i > 0 && want > 0 && unicode.IsSpace(lastRune(source[:i])) && !unicode.IsSpace(r) {
				want++
			}
			if offsets[i] != want {
				t.Fatalf("%q offset %d=%d want %d", source, i, offsets[i], want)
			}
		}
	}
}

func TestCroppedTableQuantityControls(t *testing.T) {
	for _, tc := range []struct{ file, body, quote string }{
		{"Medical/Control.md", "| day | value | unit |\n|---|---|---|\n| 2031-06-12 | 48 | mg |\n", "2031-06-12 | 48"},
		{"Medical/Control.csv", "day,value,unit,note\n2031-06-12,\"48\",mg,\"synthetic, sample\"\n", "2031-06-12,\"48\""},
		{"Medical/Control.md", "| day | value (mg) | comment |\n|---|---|---|\n| 2031-06-12 | 48 | sample |\n", "2031-06-12 | 48"},
	} {
		t.Run(tc.file+tc.quote, func(t *testing.T) {
			f := setupEvidenceMetric(t, tc.file, tc.body, Metric{Name: "dose", Unit: "mg"})
			if err := f.facts(t, tc.file, readingFacts(tc.file, "dose", "2031-06-12", "48", "mg", tc.quote)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.Check(ctx, f.s, tc.file); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.Apply(ctx, f.s, tc.file); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEmptyAndMalformedEvidenceContext(t *testing.T) {
	for _, tc := range []struct{ file, source string }{
		{"Empty.csv", ""}, {"Invalid.csv", "\"unterminated"}, {"Empty.md", ""}, {"Invalid.md", "\xff"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			e := newReadingEvidence(tc.file, tc.source)
			if err := e.check("48", 0, "48", "mg", false, nil); err == nil {
				t.Fatal("accepted missing quantity")
			}
		})
	}
}

func TestReadingEvidenceOriginalMarkdownSyntax(t *testing.T) {
	file := "Medical/Syntax.md"
	body := "\u1fef``\n\n| day | value (mg) |\n|---|---|\n| 2031-06-12 | 48 |\n\n```\n"
	f := setupEvidenceMetric(t, file, body, Metric{Name: "dose"})
	assertEvidenceRefused(t, f, file, readingFacts(file, "dose", "2031-06-12", "48", "", "2031-06-12 | 48 |"))
	if err := f.w.mark(file, func(line *Line) error { line.State = "x"; return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.w.file("ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	failures, err := f.w.collectTrialReadingIdentityFailures(ctx, f.s)
	if err != nil || len(failures) != 1 || failures[0].File != file || !strings.Contains(failures[0].Error, "unit") {
		t.Fatalf("preflight lost source-unit refusal: %v %v", failures, err)
	}
	after, err := os.ReadFile(f.w.file("ledger.md"))
	if err != nil || string(after) != string(before) || totalMeasurements(t, f) != 0 {
		t.Fatalf("preflight changed ledger or measurements: %v", err)
	}
}

func TestReadingEvidenceOriginalSyntaxControls(t *testing.T) {
	row := "| 2031-06-12 | 48 | Cafe\u0301 |"
	table := "| day | value (mg) | note |\n|---|---|---|\n" + row + "\n"
	for _, prefix := range []string{"", "Cafe\u0301\u2003測定\n\n", "\xff\n\n", "\u1fef``\n\n"} {
		source := prefix + table
		e := newReadingEvidence("Control.md", source)
		quote := collapse("2031-06-12 | 48 | Cafe\u0301")
		pos := wholeIndex(e.collapsed, quote)
		if len(e.tables) != 1 || pos < 0 {
			t.Fatalf("prefix %q: tables=%d quote=%d", prefix, len(e.tables), pos)
		}
		if err := e.check(quote, pos, "48", "mg", false, nil); err != nil {
			t.Fatalf("legitimate header quantity: %v", err)
		}
		if err := e.check(quote, pos, "48", "", false, nil); err == nil {
			t.Fatal("omitted header unit accepted")
		}
		start := len(prefix) + strings.Index(table, row)
		want := len(collapse(source[:start])) + 1
		if got := sourceCollapsedOffsets(source)[start]; got != want {
			t.Fatalf("row offset=%d want=%d", got, want)
		}
	}
	for _, file := range []string{"Control.md", "Control.csv"} {
		source := "note,value (mg)\nCafe\u0301,48\n"
		quote := "Café,48"
		if file == "Control.md" {
			source = "```\n" + table + "```\n"
			quote = "2031-06-12 | 48 | Café"
		}
		e := newReadingEvidence(file, source)
		pos := wholeIndex(e.collapsed, quote)
		if pos < 0 {
			t.Fatal("missing normalized quote")
		}
		if file == "Control.md" {
			if len(e.tables) != 0 || e.check(quote, pos, "48", "", false, nil) != nil {
				t.Fatal("actual code fence became unit table")
			}
		} else if len(e.tables) != 1 || e.check(quote, pos, "48", "mg", false, nil) != nil || e.check(quote, pos, "48", "", false, nil) == nil {
			t.Fatal("NFD CSV header/offset mismatch")
		}
	}
}

func TestRawEvidenceSourceIdentityAndConfinement(t *testing.T) {
	f := setup(t)
	body := "\u1fef``\nCafe\u0301\xff\n"
	physical := "Journe\u0301e/Cafe\u0301.md"
	logical := "Journée/Café.md"
	addSourceFile(t, f, physical, body)
	if raw, err := f.w.readRawSource(logical); err != nil || raw != body {
		t.Fatalf("raw logical read=%q %v", raw, err)
	}
	if normalized, err := f.w.ReadSource(logical); err != nil || normalized != norm.NFC.String(body) {
		t.Fatalf("public read=%q %v", normalized, err)
	}
	for _, rel := range []string{"missing.md", "../outside.md"} {
		if raw, err := f.w.readRawSource(rel); err == nil || raw != "" {
			t.Fatalf("invalid raw read %q=%q %v", rel, raw, err)
		}
	}
	t.Run("escaping-symlink", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "outside.md")
		if err := os.WriteFile(outside, []byte("OUTSIDE SYNTHETIC"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(f.w.Source, "Link.md")); err != nil {
			t.Skipf("symlink privilege/platform unavailable: %v", err)
		}
		if raw, err := f.w.readRawSource("Link.md"); err == nil || raw != "" {
			t.Fatalf("raw escape=%q %v", raw, err)
		}
	})
}
