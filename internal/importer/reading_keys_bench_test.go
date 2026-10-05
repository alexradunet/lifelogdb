package importer

import (
	"fmt"
	"path/filepath"
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
						t.Fatalf("matched %d roots, want %d", len(resolved.storedKeyWrite), len(f.Writes))
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
				t.Fatalf("stored roots %d, want %d", len(r.storedKeyWrite), want)
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
