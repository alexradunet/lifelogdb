package core

import "testing"

func TestIntegrityDetectsRestoredGuardSemanticDamage(t *testing.T) {
	for _, damage := range []string{"missing mirror", "mirror note", "correction cycle", "habit overlap", "habit value", "journal alias", "journal owner"} {
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
				id, _, err := s.CreatePage(ctx, "cli", name, "")
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			switch damage {
			case "missing mirror", "mirror note":
				a, b := page("One"), page("Two")
				if err := s.Link(ctx, "cli", a, b, "related", "shared"); err != nil {
					t.Fatal(err)
				}
				if damage == "missing mirror" {
					bypass("links_mirror_delete", func() { exec("DELETE FROM links WHERE from_id=? AND to_id=?", b, a) })
				} else {
					bypass("links_mirror_note", func() { exec("UPDATE links SET note=NULL WHERE from_id=? AND to_id=?", b, a) })
				}
			case "correction cycle":
				root, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-01", Value: 3})
				if err != nil {
					t.Fatal(err)
				}
				value := 4.0
				child, _, err := s.Correct(ctx, "cli", root, &value)
				if err != nil {
					t.Fatal(err)
				}
				bypass("measurements_no_update", func() { exec("UPDATE measurements SET supersedes_id=? WHERE id=?", child, root) })
			case "habit overlap", "habit value":
				if _, err := s.RegisterMetric(ctx, "cli", "Stretch", "", ""); err != nil {
					t.Fatal(err)
				}
				if err := s.StartHabit(ctx, "cli", "Stretch", "2026-10-01", "2026-10-03"); err != nil {
					t.Fatal(err)
				}
				if damage == "habit overlap" {
					if err := s.StartHabit(ctx, "cli", "Stretch", "2026-10-04", ""); err != nil {
						t.Fatal(err)
					}
					bypass("habit_periods_check_update", func() { exec("UPDATE habit_periods SET end_day='2026-10-04' WHERE start_day='2026-10-01'") })
				} else {
					// Deliberately bypass the writer's numeric-range validation, not a DDL guard.
					exec("INSERT INTO measurements(metric_id,day,value,source,created_at) SELECT metric_id,'2026-10-02',2,'ui'," + Now + " FROM habit_periods")
				}
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
			case "missing mirror":
				diagnostic = got.InvalidSymmetricLinks
			case "mirror note":
				diagnostic, want = got.InvalidSymmetricLinks, 2
			case "correction cycle":
				diagnostic, want = got.InvalidMeasurementChains, 2
			case "habit overlap":
				diagnostic, want = got.InvalidHabitPeriods, 2
			case "habit value":
				diagnostic = got.InvalidHabitReadings
			case "journal alias", "journal owner":
				diagnostic = got.InvalidJournalNames
			}
			if len(diagnostic) != want {
				t.Fatalf("%s diagnostic IDs=%v; want %d", damage, diagnostic, want)
			}
		})
	}
}
