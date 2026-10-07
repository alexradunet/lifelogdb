package importer

import (
	"reflect"
	"testing"

	"lifelog/internal/core"
)

func TestReadingAliasesRetainRootsAndCorrections(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Alias roots.md"
	writeSource(t, f, file, "2031-07-01 first: 5 ng/mL\n2031-07-01 second: 5 ng/mL\n2031-07-02 timed: 8 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Synthetic ferritin"})
	capture, _, err := f.s.CreatePage(ctx, "cli", "Café report", "synthetic capture")
	if err != nil {
		t.Fatal(err)
	}
	writes := []any{
		readingFact("ferritin", "2031-07-01", "5 ng/mL", "2031-07-01 first: 5 ng/mL", map[string]string{"with": "Cafe\u0301 report"}),
		readingFact("ferritin", "2031-07-01", "5 ng/mL", "2031-07-01 second: 5 ng/mL", map[string]string{"with": "Café report"}),
		readingFact("ferritin", "2031-07-02", "8 ng/mL", "2031-07-02 timed: 8 ng/mL", map[string]string{"taken_at": "2031-07-02T08:00:00.000Z", "tz": "UTC", "with": "Café report"}),
	}
	applyFacts(t, f, file, writes)
	metric, err := f.s.PageID(ctx, "ferritin")
	if err != nil {
		t.Fatal(err)
	}
	type root struct {
		ID, Metric, Capture int64
		Source, Key         string
		Value               float64
	}
	load := func() []root {
		t.Helper()
		rows, err := f.s.DB.R.QueryContext(ctx, "SELECT id,metric_id,captured_with_id,source,import_key,value FROM measurements WHERE supersedes_id IS NULL ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []root
		for rows.Next() {
			var r root
			if err := rows.Scan(&r.ID, &r.Metric, &r.Capture, &r.Source, &r.Key, &r.Value); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := load()
	if len(before) != 3 {
		t.Fatalf("roots=%+v", before)
	}
	for _, r := range before {
		if r.Metric != metric || r.Capture != capture || r.Source != "import:notebook" {
			t.Fatalf("bad root fields: %+v", r)
		}
	}
	corrected := 7.0
	correction, _, err := f.s.Correct(ctx, "cli", before[1].ID, &corrected)
	if err != nil {
		t.Fatal(err)
	}
	for _, titles := range [][2]string{{"Iron stores", "Capture note"}, {"IRON STORES", "CAPTURE NOTE"}, {"Ferritin", "Café report"}, {"Iron panel", "Capture retained"}} {
		if _, err := f.s.Rename(ctx, "cli", metric, titles[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Rename(ctx, "cli", capture, titles[1]); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatal(err)
		}
		if got := load(); !reflect.DeepEqual(got, before) {
			t.Fatalf("rename/reapply changed roots: %+v", got)
		}
	}
	// Owner-approved source spellings may mix old/new names without allocating new roots.
	registerMetrics(t, f, Metric{Name: "Iron panel", Unit: "ng/mL", Note: "Synthetic ferritin"})
	writes[1] = readingFact("Iron panel", "2031-07-01", "5 ng/mL", "2031-07-01 second: 5 ng/mL", map[string]string{"with": "Capture retained"})
	writes[2] = readingFact("IRON PANEL", "2031-07-02", "8 ng/mL", "2031-07-02 timed: 8 ng/mL", map[string]string{"taken_at": "2031-07-02T08:00:00.000Z", "tz": "UTC", "with": "CAPTURE RETAINED"})
	applyFacts(t, f, file, writes)
	if got := load(); !reflect.DeepEqual(got, before) {
		t.Fatalf("mixed aliases changed roots: %+v", got)
	}
	var parent int64
	var value float64
	if err := f.s.DB.R.QueryRowContext(ctx, "SELECT supersedes_id,value FROM measurements WHERE id=?", correction).Scan(&parent, &value); err != nil {
		t.Fatal(err)
	}
	if parent != before[1].ID || value != 7 || importerMeasurementRows(t, f) != 4 {
		t.Fatal("reapply changed correction chain")
	}
}

func TestReadingTimedAliasCollisionRollsBack(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Alias collision.md"
	writeSource(t, f, file, "2031-07-02 first: 8 ng/mL\n2031-07-02 second: 8 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Synthetic ferritin"})
	metric, err := f.s.PageID(ctx, "ferritin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Rename(ctx, "cli", metric, "Iron panel"); err != nil {
		t.Fatal(err)
	}
	registerMetrics(t, f, Metric{Name: "Iron panel", Unit: "ng/mL", Note: "Synthetic ferritin"})
	// Exercise the production transactional resolver independently of quote validation.
	facts := &Facts{File: file, Writes: []Write{
		{Reading: &ReadingW{Metric: "ferritin", Day: "2031-07-02", Value: "8 ng/mL", TakenAt: "2031-07-02T08:00:00.000Z"}},
		{Reading: &ReadingW{Metric: "Iron panel", Day: "2031-07-02", Value: "8 ng/mL", TakenAt: "2031-07-02T08:00:00.000Z"}},
	}}
	err = f.s.Do(ctx, "import:notebook", func(tx *core.Tx) error {
		if _, _, err := tx.CreateImported("Must roll back", nil, "rollback proof"); err != nil {
			return err
		}
		_, err := resolveReadingKeys(tx, "import:notebook", facts, []int{0, 1})
		return err
	})
	if code(err) != 422 {
		t.Fatalf("alias timed collision: %v", err)
	}
	if id, err := f.s.PageID(ctx, "Must roll back"); err != nil || id != 0 {
		t.Fatalf("collision left partial page: %d, %v", id, err)
	}
	if importerMeasurementRows(t, f) != 0 {
		t.Fatal("collision wrote readings")
	}
}
