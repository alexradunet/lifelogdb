package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func syntheticReadingScan(tb testing.TB, days int) (*core.Store, *Facts, []int) {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		tb.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	f := &Facts{File: "Medical/Scan.md"}
	var pos []int
	err = s.Do(ctx, "import:scan", func(tx *core.Tx) error {
		for _, metric := range []string{"alpha", "beta", "gamma"} {
			if _, err := tx.RegisterMetric(metric, "", "synthetic"); err != nil {
				return err
			}
			for day := 0; day < days; day++ {
				date := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day).Format("2006-01-02")
				f.Writes = append(f.Writes, Write{Reading: &ReadingW{Metric: metric, Day: date, Value: "5"}})
				pos = append(pos, len(pos))
				for _, file := range []string{f.File, "Unrelated.md"} {
					if _, err := tx.Record(core.Reading{Metric: metric, Day: date, Value: 5, Key: file + "|reading|" + metric + "|" + date + "|1"}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return s, f, pos
}

func BenchmarkResolveReadingKeys(b *testing.B) {
	for _, days := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("days_%d", days), func(b *testing.B) {
			s, f, pos := syntheticReadingScan(b, days)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.DryRun(ctx, "import:scan", func(tx *core.Tx) error { _, err := resolveReadingKeys(tx, "import:scan", f, pos); return err }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestReadingRootLookupCount(t *testing.T) {
	for _, days := range []int{10, 100} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			s, f, pos := syntheticReadingScan(t, days)
			for call := 0; call < 2; call++ {
				counts := map[string]int{}
				err := s.DryRun(ctx, "import:scan", func(tx *core.Tx) error {
					resolved, err := resolveReadingKeysWithLoader("import:scan", f, pos, func(source, file, metric string) ([]core.ImportedMeasurementRoot, error) {
						counts[metric]++
						return tx.ImportedMeasurementRootsByFile(source, file, metric)
					})
					if err == nil && len(resolved.storedKeyWrite) != len(f.Writes) {
						return fmt.Errorf("matched %d roots, want %d", len(resolved.storedKeyWrite), len(f.Writes))
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				for metric, n := range counts {
					if n != 1 {
						t.Errorf("%s: %d lookups, want 1", metric, n)
					}
				}
				if len(counts) != 3 {
					t.Fatalf("loaded %d metrics, want 3", len(counts))
				}
			}
		})
	}
}

func TestReadingRootLookupSeesLaterWrite(t *testing.T) {
	s, f, pos := syntheticReadingScan(t, 10)
	day := "2040-01-01"
	f.Writes = append(f.Writes, Write{Reading: &ReadingW{Metric: "alpha", Day: day, Value: "5"}})
	pos = append(pos, len(pos))
	key := f.File + "|reading|alpha|" + day + "|1"
	check := func(want int) {
		t.Helper()
		if err := s.DryRun(ctx, "import:scan", func(tx *core.Tx) error {
			r, err := resolveReadingKeys(tx, "import:scan", f, pos)
			if err == nil && len(r.storedKeyWrite) != want {
				return fmt.Errorf("stored roots %d, want %d", len(r.storedKeyWrite), want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	check(30)
	if _, err := s.Record(ctx, "import:scan", core.Reading{Metric: "alpha", Day: day, Value: 5, Key: key}); err != nil {
		t.Fatal(err)
	}
	check(31)
	var before, after int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM measurements`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if id, err := s.Record(ctx, "import:scan", core.Reading{Metric: "alpha", Day: day, Value: 5, Key: key}); err != nil || id != 0 {
		t.Fatalf("repeat record: id=%d err=%v", id, err)
	}
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM measurements`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("repeat grew rows: %d -> %d", before, after)
	}
}

// Exercise the actual workspace checks and trial transactions, not just key matching.
func syntheticCorrectionProof(tb testing.TB, days, roots int) (*Workspace, *core.Store, []correctionIntent) {
	tb.Helper()
	s, facts, _ := syntheticReadingScan(tb, days)
	root := tb.TempDir()
	src := filepath.Join(root, "Synthetic")
	if err := os.MkdirAll(filepath.Join(src, "Medical"), 0o755); err != nil {
		tb.Fatal(err)
	}
	w, err := Open(src + ".lifelog")
	if err != nil {
		tb.Fatal(err)
	}
	var source strings.Builder
	var intents []correctionIntent
	for i := range facts.Writes {
		r := facts.Writes[i].Reading
		facts.Writes[i].Quote = fmt.Sprintf("%s %s: 5", r.Day, r.Metric)
		source.WriteString(facts.Writes[i].Quote + "\n")
		if i < roots {
			intents = append(intents, correctionIntent{RootSource: "import:scan", Metric: r.Metric, RootImportKey: facts.File + "|reading|" + r.Metric + "|" + r.Day + "|1"})
		}
	}
	if err := os.WriteFile(filepath.Join(src, filepath.FromSlash(facts.File)), []byte(source.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	if err := w.DraftRules("source: import:scan\n\n## Aliases\n\n## Distinct\n"); err != nil {
		tb.Fatal(err)
	}
	if err := ownerApproves(w, "rules.md"); err != nil {
		tb.Fatal(err)
	}
	for _, metric := range []string{"alpha", "beta", "gamma"} {
		if err := w.ProposeMetric(Metric{Name: metric, Note: "synthetic"}); err != nil {
			tb.Fatal(err)
		}
	}
	if err := ownerApproves(w, "metrics.md"); err != nil {
		tb.Fatal(err)
	}
	if _, err := w.MakeLedger(); err != nil {
		tb.Fatal(err)
	}
	p := filepath.Join(w.Dir, "facts", filepath.FromSlash(facts.File)+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		tb.Fatal(err)
	}
	data, err := json.Marshal(facts)
	if err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		tb.Fatal(err)
	}
	return w, s, intents
}

func BenchmarkBuildCorrectionProof(b *testing.B) {
	for _, roots := range []int{1, 3, 10} {
		b.Run(fmt.Sprintf("facts_30_roots_%d", roots), func(b *testing.B) {
			w, trial, intents := syntheticCorrectionProof(b, 10, roots)
			target := &core.Store{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				proof, err := w.buildCorrectionProof(ctx, trial, target, intents)
				if err != nil {
					b.Fatal(err)
				}
				if len(proof.entries) != roots {
					b.Fatalf("entries %d, want %d", len(proof.entries), roots)
				}
			}
		})
	}
}
