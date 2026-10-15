package core

import "testing"

func TestIntegrityDetectsRestoredGuardSemanticDamage(t *testing.T) {
	for _, damage := range []string{"correction cycle", "journal alias", "journal owner"} {
		t.Run(damage, func(t *testing.T) {
			s := fresh(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.DB.W.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			bypass := func(trigger string, change func()) {
				t.Helper()
				var definition string
				if err := s.DB.R.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?", trigger).Scan(&definition); err != nil {
					t.Fatal(err)
				}
				exec("DROP TRIGGER " + trigger)
				change()
				exec(definition)
			}
			page := func(name string) int64 {
				t.Helper()
				id, _, err := s.CreatePage(ctx, "cli", name, "", "")
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			switch damage {
			case "correction cycle":
				root, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-01", Value: 3})
				if err != nil {
					t.Fatal(err)
				}
				value := 4.0
				child, err := s.Correct(ctx, "cli", root, &value)
				if err != nil {
					t.Fatal(err)
				}
				bypass("measurements_no_update", func() { exec("UPDATE measurements SET supersedes_id=? WHERE id=?", child, root) })
			case "journal alias":
				id := page("2026-10-01")
				bypass("entity_names_day_insert", func() { exec("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Extra','extra')", id) })
			case "journal owner":
				id := page("Ordinary")
				bypass("entity_names_day_insert", func() {
					exec("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'2026-10-01','2026-10-01')", id)
				})
			}
			got, err := s.Integrity(ctx)
			if err != nil || got.OK || got.ForeignKeys != 0 || !got.FullTextIndexOK || len(got.IntegrityCheck) != 1 || got.IntegrityCheck[0] != "ok" {
				t.Fatalf("semantic damage with its guards restored must be reported independently of structural checks: %+v, %v", got, err)
			}
			var diagnostic []int64
			want := 1
			switch damage {
			case "correction cycle":
				diagnostic, want = got.InvalidMeasurementChains, 2
			case "journal alias", "journal owner":
				diagnostic = got.InvalidJournalNames
			}
			if len(diagnostic) != want {
				t.Fatalf("%s diagnostic IDs=%v; want %d", damage, diagnostic, want)
			}
		})
	}
}
