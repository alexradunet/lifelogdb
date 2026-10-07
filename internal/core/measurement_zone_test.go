package core

import (
	"strings"
	"testing"
)

func TestMeasurementZoneValidationBeforeWrites(t *testing.T) {
	for _, zone := range []string{"UTC\x00", "UTC\x00hidden", "\x00UTC", "Europe Berlin", strings.Repeat("x", 65)} {
		t.Run(zone, func(t *testing.T) {
			s := fresh(t)
			input := Reading{Metric: "Mood", Day: "2020-01-01", Value: 3, TZ: zone}
			id, err := s.Record(ctx, "cli", input)
			if status(err) != 422 || id != 0 {
				t.Errorf("Record invalid zone: id=%d err=%v", id, err)
			}
			var count int
			if err := s.DB.R.QueryRow("SELECT count(*) FROM measurements").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("invalid Record persisted %d measurements", count)
			}
			root, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2020-01-01", Value: 3})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.RelocateReading(ctx, "cli", root, input)
			if status(err) != 422 || result.RetractionID != 0 || result.ReplacementID != 0 {
				t.Errorf("RelocateReading invalid zone: result=%+v err=%v", result, err)
			}
			if err := s.DB.R.QueryRow("SELECT count(*) FROM measurements WHERE supersedes_id=?", root).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("invalid relocation retracted root with %d corrections", count)
			}
		})
	}
}

func TestMeasurementZoneValidControls(t *testing.T) {
	s := fresh(t)
	for _, zone := range []string{"", "UTC", "Europe/Bucharest", "Etc/GMT+5", "America/Port-au-Prince", strings.Repeat("x", 64)} {
		id, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2020-01-01", Value: 3, TZ: zone})
		if err != nil {
			t.Fatalf("Record(%q): %v", zone, err)
		}
		row, err := s.Measurement(ctx, id)
		if err != nil || row.TZ != zone {
			t.Errorf("stored zone got %+v, %v; want %q", row, err, zone)
		}
	}
}
