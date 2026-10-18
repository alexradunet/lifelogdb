package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The rules that only a trigger holds are checked by queries too (docs/contract/integrity-checks.md). Each test drops
// the trigger of a disposable file, writes the row that the trigger refuses, and reads the finding.

func TestIntegrityDetectsALinkThatBreaksADayPageRule(t *testing.T) {
	for _, rule := range []struct{ name, kind, fromDay, toDay string }{
		{"an at link from a plain page", "at", "", ""},
		{"a part-of link to a day page", "part-of", "", "2026-10-02"},
		{"a part-of link from a day page", "part-of", "2026-10-03", ""},
	} {
		t.Run(rule.name, func(t *testing.T) {
			s := fresh(t)
			page := func(title string) int64 {
				t.Helper()
				id, _, err := s.CreatePage(ctx, "cli", title, "", "")
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			from, to := page("Start"), page("Finish")
			if rule.fromDay != "" {
				from = page(rule.fromDay)
			}
			if rule.toDay != "" {
				to = page(rule.toDay)
			}
			if rule.kind == "at" {
				var err error
				if to, err = s.CreatePlace(ctx, "cli", "Harbour"); err != nil {
					t.Fatal(err)
				}
			}
			clean, err := s.Integrity(ctx)
			if err != nil || !clean.OK {
				t.Fatalf("clean setup: %+v %v", clean, err)
			}
			if _, err := s.DB.W.ExecContext(ctx, `DROP TRIGGER links_endpoint_types`); err != nil {
				t.Fatal(err)
			}
			var edge int64
			if err := s.DB.W.QueryRowContext(ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, ?, `+nowSQL+`, 'cli') RETURNING id`,
				from, to, rule.kind).Scan(&edge); err != nil {
				t.Fatal(err)
			}
			got, err := s.Integrity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got.OK || !reflect.DeepEqual(got.InvalidLinkDayRules, []int64{edge}) || len(got.InvalidTypedLinks) != 0 || got.ForeignKeys != 0 {
				t.Fatalf("want only InvalidLinkDayRules=[%d]: %+v", edge, got)
			}
		})
	}
}

func TestIntegrityDetectsAScaleValueOutsideItsRange(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Mood", Unit: "1-5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-01", Value: 3}); err != nil {
		t.Fatal(err)
	}
	clean, err := s.Integrity(ctx)
	if err != nil || !clean.OK {
		t.Fatalf("a reading inside the range is not a finding: %+v %v", clean, err)
	}
	if _, err := s.DB.W.ExecContext(ctx, `DROP TRIGGER measurements_scale_range`); err != nil {
		t.Fatal(err)
	}
	var high, fraction int64
	insert := func(value float64, dst *int64) {
		t.Helper()
		if err := s.DB.W.QueryRowContext(ctx, `INSERT INTO measurements(metric_id, day, value, source, created_at)
			VALUES ((SELECT entity_id FROM entity_names WHERE name_key = 'mood'), '2026-10-02', ?, 'cli', `+nowSQL+`) RETURNING id`, value).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	insert(9, &high)
	insert(2.5, &fraction)
	got, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.OK || !reflect.DeepEqual(got.InvalidScaleValues, []int64{high, fraction}) {
		t.Fatalf("want InvalidScaleValues=[%d %d]: %+v", high, fraction, got)
	}
}

// A name key is derived from the title by the pinned algorithm of the writer. SQLite cannot compute it, so a key set
// by hand is found by Go (docs/contract/integrity-checks.md).
func TestIntegrityDetectsANameKeyThatIsNotTheKeyOfItsTitle(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Harbour", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := s.Integrity(ctx)
	if err != nil || !clean.OK {
		t.Fatalf("clean setup: %+v %v", clean, err)
	}
	var alias int64
	if err := s.DB.W.QueryRowContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, 'Straße', 'strasse-by-hand') RETURNING id`, id).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	got, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.OK || !reflect.DeepEqual(got.NameKeyMismatches, []int64{alias}) || len(got.OrphanEntities) != 0 || len(got.InvalidJournalNames) != 0 {
		t.Fatalf("want only NameKeyMismatches=[%d]: %+v", alias, got)
	}
}

// pageQueries are the statements of the first sql block of docs/contract/integrity-checks.md, the home of the
// queries, with the comments cut and the white space folded.
func pageQueries(t *testing.T) []string {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "integrity-checks.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(page), "```sql\n")
	if !ok {
		t.Fatal("the page has no sql block")
	}
	block, _, _ = strings.Cut(block, "```")
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		line, _, _ = strings.Cut(line, " -- ")
		line, _, _ = strings.Cut(line, ";   --")
		lines = append(lines, line)
	}
	var out []string
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		if folded := strings.Join(strings.Fields(statement), " "); folded != "" {
			out = append(out, folded)
		}
	}
	return out
}

// The queries of idChecks are copies of the page that is their home. A query that changes in one place only fails
// here.
func TestIntegrityQueriesAreThoseOfTheContract(t *testing.T) {
	page := pageQueries(t)
	checks := (&IntegrityResult{}).idChecks()
	const before = 2 // integrity_check and foreign_key_check come first on the page
	if len(page) < before+len(checks) {
		t.Fatalf("the page has %d statements; want at least %d", len(page), before+len(checks))
	}
	for i, c := range checks {
		if got, want := strings.Join(strings.Fields(c.query), " "), page[before+i]; got != want {
			t.Errorf("%s differs from the page:\n code: %s\n page: %s", c.name, got, want)
		}
	}
}
