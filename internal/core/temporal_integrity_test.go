package core

import "testing"

func TestTemporalIntegrityDetectsActualKindAndScopeDamage(t *testing.T) {
	s := fresh(t)
	kind, _, err := s.CreatePage(ctx, "cli", "Workout", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RegisterMetric(ctx, "cli", "Steps", "steps", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := []int64{}
	for _, start := range []string{"2020-01-02T08:00:00.000Z", "2020-01-02T18:00:00.000Z"} {
		var id int64
		if err := s.Do(ctx, "cli", func(tx *Tx) error {
			var err error
			id, _, err = tx.CaptureSession(SessionInput{Kind: "Workout", Day: "2020-01-02", StartAt: start})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, id)
	}
	root, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000, SessionID: sessions[0]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("DROP TRIGGER measurements_supersede_scope"); err != nil {
		t.Fatal(err)
	}
	var damaged int64
	if err := s.DB.W.QueryRow(`INSERT INTO measurements(metric_id,session_id,day,value,supersedes_id,source,created_at) SELECT metric_id,?,'2020-01-02',3500,id,'ui',`+Now+` FROM measurements WHERE id=? RETURNING id`, sessions[1], root).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	result, err := s.Integrity(ctx)
	if err != nil || result.OK || result.ForeignKeys != 0 || !result.FullTextIndexOK || len(result.InvalidMeasurementScopes) != 1 || result.InvalidMeasurementScopes[0] != damaged {
		t.Fatalf("scope damage %+v %v", result, err)
	}
	// Existing sessions retain kind ownership through a FK, while the separate semantic query detects damage
	// even in an otherwise structurally valid database (journal promotion is not encoded by that FK).
	if _, err := s.DB.W.Exec("DROP TRIGGER sessions_kind_update"); err != nil {
		t.Fatal(err)
	}
	journal, _, err := s.CreatePage(ctx, "cli", "2020-01-02", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("UPDATE sessions SET kind_id=? WHERE id=?", journal, sessions[0]); err != nil {
		t.Fatal(err)
	}
	result, err = s.Integrity(ctx)
	if err != nil || result.OK || result.ForeignKeys != 0 || len(result.InvalidSessionKinds) != 1 || result.InvalidSessionKinds[0] != sessions[0] {
		t.Fatalf("kind damage %+v %v (original %d)", result, err, kind)
	}
}
