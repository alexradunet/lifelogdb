package core

import (
	"slices"
	"testing"
)

// partOf is the titles of the pages id is part-of, by title.
func partOf(t *testing.T, s *Store, id int64) []string {
	t.Helper()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT n.title FROM links l JOIN entities p ON p.id = l.to_id
		JOIN entity_names n ON n.entity_id = p.id AND n.name_key = p.preferred_name_key
		 WHERE l.from_id = ? AND l.kind = 'part-of' ORDER BY n.title`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func metricID(t *testing.T, s *Store, name, unit string) int64 {
	t.Helper()
	if _, err := s.RegisterMetric(ctx, "cli", name, unit, ""); err != nil {
		t.Fatal(err)
	}
	return pageByTitle(t, s, name).ID
}

func TestFilePages(t *testing.T) {
	s := fresh(t)
	ferritin, ldl, weight := metricID(t, s, "Ferritin", "ug/L"), metricID(t, s, "LDL", "mg/dL"), metricID(t, s, "Weight", "kg")

	f, err := s.FilePages(ctx, "cli", []int64{ferritin, ldl}, "Bloodwork", false)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Created || f.Added != 2 || f.Moved != 0 || f.Category.Title != "Bloodwork" {
		t.Fatalf("first filing = %+v, want Bloodwork made, 2 added", f)
	}
	cat, err := s.PageByID(ctx, f.Category.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Type != "page" || cat.Day != "" || cat.Body != "" {
		t.Errorf("the made category = type %q day %q body %q, want a plain empty page with no day", cat.Type, cat.Day, cat.Body)
	}

	again, err := s.FilePages(ctx, "cli", []int64{ferritin, ldl}, "bloodwork", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Added != 0 || again.Category.ID != f.Category.ID {
		t.Errorf("second filing = %+v, want nothing new in the same page", again)
	}

	if _, err := s.FilePages(ctx, "cli", []int64{ferritin}, "Iron", false); err != nil {
		t.Fatal(err)
	}
	if got := partOf(t, s, ferritin); !slices.Equal(got, []string{"Bloodwork", "Iron"}) {
		t.Fatalf("after add, Ferritin is part-of %v", got)
	}

	if _, err := s.FilePages(ctx, "cli", []int64{weight}, "Sport", false); err != nil {
		t.Fatal(err)
	}
	moved, err := s.FilePages(ctx, "cli", []int64{ferritin, weight}, "Iron", true)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 2 || moved.Added != 1 {
		t.Errorf("move = %+v, want 2 links removed (Bloodwork, Sport) and 1 added", moved)
	}
	if got := partOf(t, s, ferritin); !slices.Equal(got, []string{"Iron"}) {
		t.Errorf("after move, Ferritin is part-of %v, want [Iron]", got)
	}
	if got := partOf(t, s, weight); !slices.Equal(got, []string{"Iron"}) {
		t.Errorf("after move, Weight is part-of %v, want [Iron]", got)
	}
	if got := partOf(t, s, ldl); !slices.Equal(got, []string{"Bloodwork"}) {
		t.Errorf("a move of other pages changed LDL: part-of %v", got)
	}
}

func TestFilePagesRefusals(t *testing.T) {
	s := fresh(t)
	ferritin := metricID(t, s, "Ferritin", "ug/L")
	_, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		ids      []int64
		category string
		want     int
	}{
		{"a missing page", []int64{ferritin, 9999}, "Bloodwork", 404},
		{"a deleted category", []int64{ferritin}, "Old Group", 409},
		{"a deleted page", []int64{gone}, "Bloodwork", 409},
		{"a person as the category", []int64{ferritin}, "Bob Sample", 422},
		{"a day as the category", []int64{ferritin}, "2026-10-01", 422},
		{"a title the contract refuses", []int64{ferritin}, "Health/Diet", 422},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.FilePages(ctx, "cli", c.ids, c.category, false)
			if status(err) != c.want {
				t.Fatalf("status %d (%v), want %d", status(err), err, c.want)
			}
			if got := partOf(t, s, ferritin); len(got) != 0 {
				t.Errorf("a refused filing wrote part-of %v", got)
			}
			if id, err := s.PageID(ctx, "Bloodwork"); err != nil || id != 0 {
				t.Errorf("a refused filing made the category, page %d (%v)", id, err)
			}
		})
	}
}

func TestRecentReadings(t *testing.T) {
	s := fresh(t)
	weight := metricID(t, s, "Weight", "kg")
	gone := metricID(t, s, "Gone", "kg")
	record := func(metric, day string, v float64) int64 {
		t.Helper()
		id, err := s.Record(ctx, "cli", Reading{Metric: metric, Day: day, Value: v})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	record("Weight", "2026-10-01", 80)
	wrong := record("Weight", "2026-10-02", 90)
	mistake := record("Weight", "2026-10-03", 70)
	record("Weight", "2026-10-04", 79)
	last := record("Weight", "2026-10-04", 78.5) // a later reading of the same day comes last
	if _, err := s.Correct(ctx, "cli", wrong, ptr(81.0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", mistake, nil); err != nil {
		t.Fatal(err)
	}
	record("Gone", "2026-10-05", 1)
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}

	got, err := s.RecentReadings(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day   string
		value float64
	}{{"2026-10-01", 80}, {"2026-10-02", 81}, {"2026-10-04", 79}, {"2026-10-04", 78.5}}
	w := got[weight]
	if len(w) != len(want) {
		t.Fatalf("Weight recent = %+v, want %v", w, want)
	}
	for i, r := range w {
		if r.Day != want[i].day || r.Value != want[i].value {
			t.Errorf("Weight recent[%d] = %s %v, want %s %v", i, r.Day, r.Value, want[i].day, want[i].value)
		}
	}
	if w[len(w)-1].ID != last {
		t.Errorf("the latest is measurement %d, want %d", w[len(w)-1].ID, last)
	}
	if _, ok := got[gone]; ok {
		t.Errorf("a deleted metric has recent readings: %+v", got[gone])
	}

	two, err := s.RecentReadings(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if w := two[weight]; len(w) != 2 || w[0].Value != 79 || w[1].Value != 78.5 {
		t.Errorf("the newest 2 = %+v, want 79 then 78.5", w)
	}
}

// TestCreateMetric: the metric and its category are one write (docs/plans/113-new-metric-on-metrics.md).
func TestCreateMetric(t *testing.T) {
	s := fresh(t)
	added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Walk", Category: "Sport"})
	if err != nil || !added {
		t.Fatalf("CreateMetric = %v, %v; want a new metric", added, err)
	}
	walk := pageByTitle(t, s, "Walk")
	if walk.Type != "metric" {
		t.Errorf("Walk is a %s, want a metric", walk.Type)
	}
	if got := partOf(t, s, walk.ID); !slices.Equal(got, []string{"Sport"}) {
		t.Errorf("Walk is part-of %v, want [Sport]", got)
	}

	// A metric that exists is filed in one more category, and nothing else changes.
	added, err = s.CreateMetric(ctx, "cli", NewMetric{Name: "walk", Category: "Daily"})
	if err != nil || added {
		t.Fatalf("CreateMetric of an existing metric = %v, %v; want no new metric", added, err)
	}
	if got := partOf(t, s, walk.ID); !slices.Equal(got, []string{"Daily", "Sport"}) {
		t.Errorf("Walk is part-of %v, want [Daily Sport]", got)
	}

	// A plain metric with no category: no link.
	weight := func() int64 {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
			t.Fatal(err)
		}
		return pageByTitle(t, s, "Weight").ID
	}()
	if got := partOf(t, s, weight); len(got) != 0 {
		t.Errorf("Weight is part-of %v, want none", got)
	}
}

func TestCreateMetricRefusalsWriteNothing(t *testing.T) {
	s := fresh(t)
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		m    NewMetric
		want int
	}{
		{"a deleted category", NewMetric{Name: "Run", Unit: "km", Category: "Old Group"}, 409},
		{"a day as the category", NewMetric{Name: "Run", Unit: "km", Category: "2031-05-01"}, 422},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.CreateMetric(ctx, "cli", c.m)
			if status(err) != c.want {
				t.Fatalf("status %d (%v), want %d", status(err), err, c.want)
			}
			for _, title := range []string{"Run", "Sport"} {
				if id, err := s.PageID(ctx, title); err != nil || id != 0 {
					t.Errorf("a refused CreateMetric made %s, page %d (%v)", title, id, err)
				}
			}
		})
	}
}
