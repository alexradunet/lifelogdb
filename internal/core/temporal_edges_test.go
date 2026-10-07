package core

import (
	"reflect"
	"testing"
)

func TestTemporalNULStorageRefusal(t *testing.T) {
	for _, col := range []string{"start_boundary", "end_boundary"} {
		for _, nominal := range []string{"2018?", "2018-09~", "2018-09-15%"} {
			t.Run(col+"/"+nominal, func(t *testing.T) {
				s := fresh(t)
				var id int64
				if err := s.Do(ctx, "cli", func(tx *Tx) error {
					var err error
					id, err = tx.CreatePeriod("Synthetic period", "", nil, nil)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				before, err := s.PageByID(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				bad := nominal + string(rune(0)) + "hidden"
				if _, err := periodBoundary(&bad, col == "end_boundary"); err == nil {
					t.Fatal("writer accepted malformed probe")
				}
				err = s.Do(ctx, "cli", func(tx *Tx) error {
					_, err := tx.tx.Exec("UPDATE periods SET "+col+"=? WHERE id=?", bad, id)
					return err
				})
				after, readErr := s.PageByID(ctx, id)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if err == nil || !reflect.DeepEqual(before, after) {
					t.Errorf("DDL accepted NUL-tailed %s=%q: err=%v; state unchanged=%v", col, bad, err, reflect.DeepEqual(before, after))
				}
			})
		}
	}
	for _, col := range []string{"start_offset", "end_offset", "start_zone_unverified", "end_zone_unverified"} {
		t.Run(col, func(t *testing.T) {
			s := fresh(t)
			if _, _, err := s.CreatePage(ctx, "cli", "Synthetic workout", ""); err != nil {
				t.Fatal(err)
			}
			in := SessionInput{Kind: "Synthetic workout", Day: "2020-01-01", StartAt: "2020-01-01T00:00:00.000Z", EndAt: "2020-01-01T01:00:00.000Z"}
			var id int64
			if err := s.Do(ctx, "cli", func(tx *Tx) error { var err error; id, _, err = tx.CaptureSession(in); return err }); err != nil {
				t.Fatal(err)
			}
			bad := "+00:00" + string(rune(0)) + "hidden"
			if col == "start_zone_unverified" || col == "end_zone_unverified" {
				bad = "UTC" + string(rune(0)) + "hidden invalid label"
			}
			writer := in
			switch col {
			case "start_offset":
				writer.StartOffset = bad
			case "end_offset":
				writer.EndOffset = bad
			case "start_zone_unverified":
				writer.StartZoneUnverified = bad
			case "end_zone_unverified":
				writer.EndZoneUnverified = bad
			}
			if err := validateSession(writer); err == nil {
				t.Fatal("writer accepted malformed probe")
			}
			before, err := s.Session(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			err = s.Do(ctx, "cli", func(tx *Tx) error {
				_, err := tx.tx.Exec("UPDATE sessions SET "+col+"=? WHERE id=?", bad, id)
				return err
			})
			after, readErr := s.Session(ctx, id)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err == nil || !reflect.DeepEqual(before, after) {
				t.Errorf("DDL accepted NUL-tailed %s=%q: err=%v; state unchanged=%v", col, bad, err, reflect.DeepEqual(before, after))
			}
		})
	}
}

func TestPeriodCategoryMustBeLiveWhenSelected(t *testing.T) {
	s := fresh(t)
	category, _, err := s.CreatePage(ctx, "cli", "Study category", "")
	if err != nil {
		t.Fatal(err)
	}
	var kept, newPeriod int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		kept, err = tx.CreatePeriod("First study", "", nil, nil)
		if err != nil {
			return err
		}
		newPeriod, err = tx.CreatePeriod("Second study", "", nil, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", kept, category, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", category); err != nil {
		t.Fatal(err)
	}
	var links int
	if err := s.DB.R.QueryRow("SELECT count(*) FROM links WHERE from_id=? AND to_id=? AND kind='part-of'", kept, category).Scan(&links); err != nil || links != 1 {
		t.Fatalf("retained edge: %d %v", links, err)
	}

	journal, _, err := s.CreatePage(ctx, "cli", "2020-01-02", "")
	if err != nil {
		t.Fatal(err)
	}
	var person int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		person, _, err = tx.CreatePerson("Not a category", "Person", "", "", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []int64{journal, person} {
		before, err := s.PageByID(ctx, newPeriod)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Link(ctx, "cli", newPeriod, target, "part-of", ""); err == nil {
			t.Fatal("invalid new category accepted")
		}
		after, err := s.PageByID(ctx, newPeriod)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("category refusal changed state %+v %v", after, err)
		}
	}
	// Retry of the retained selection is a no-op, not a new selection or revival.
	existing, err := s.PageByID(ctx, kept)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", kept, category, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	retried, err := s.PageByID(ctx, kept)
	if err != nil || !reflect.DeepEqual(existing, retried) {
		t.Fatalf("retained retry changed state %+v %v", retried, err)
	}
	before, err := s.PageByID(ctx, newPeriod)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Link(ctx, "cli", newPeriod, category, "part-of", "")
	after, readErr := s.PageByID(ctx, newPeriod)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || !reflect.DeepEqual(before, after) {
		t.Errorf("new category selection accepted tombstone: err=%v, state unchanged=%v", err, reflect.DeepEqual(before, after))
	}
	target, readErr := s.PageByID(ctx, category)
	if readErr != nil || target.DeletedAt == "" {
		t.Fatalf("selection revived category %+v %v", target, readErr)
	}

}
