package core

import "testing"

// The measurement-corrections check finds a correction of another metric even when the trigger that refuses it is
// gone: the semantic query does not trust the triggers of the file it checks.
func TestIntegrityDetectsACorrectionOfAnotherMetric(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Steps", "Floors"} {
		if _, err := s.RegisterMetric(ctx, "cli", name, "count", ""); err != nil {
			t.Fatal(err)
		}
	}
	root, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("DROP TRIGGER measurements_supersede_metric"); err != nil {
		t.Fatal(err)
	}
	var damaged int64
	if err := s.DB.W.QueryRow(`INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) SELECT (SELECT entity_id FROM entity_names WHERE name_key='floors'),'2020-01-02',3,?,'ui',`+nowSQL+` RETURNING id`, root).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	result, err := s.Integrity(ctx)
	if err != nil || result.OK || result.ForeignKeys != 0 || !result.FullTextIndexOK || len(result.InvalidMeasurementCorrections) != 1 || result.InvalidMeasurementCorrections[0] != damaged {
		t.Fatalf("correction damage: %+v %v", result, err)
	}
}
