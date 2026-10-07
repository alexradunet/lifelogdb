package core

import (
	"errors"
	"testing"
)

func TestMeasurementScopesCorrectionsAndAtomicRelocation(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Steps", "steps", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Workout", ""); err != nil {
		t.Fatal(err)
	}
	sessions := []int64{}
	for _, start := range []string{"2020-01-02T08:00:00.000Z", "2020-01-02T18:00:00.000Z"} {
		var id int64
		if err := s.Do(ctx, "cli", func(tx *Tx) error {
			var e error
			id, _, e = tx.CaptureSession(SessionInput{Kind: "Workout", Day: "2020-01-02", StartAt: start})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, id)
	}
	daily, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 10000})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000, SessionID: sessions[0]})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 2000, SessionID: sessions[1]})
	if err != nil {
		t.Fatal(err)
	}
	unassociated, err := s.Series(ctx, "Steps", "2020-01-01", "2020-01-02")
	if err != nil || len(unassociated) != 1 || unassociated[0].ID != daily || unassociated[0].Value != 10000 || unassociated[0].Scope != "unassociated" {
		t.Fatalf("unassociated %+v %v", unassociated, err)
	}
	all, err := s.SeriesScope(ctx, "Steps", "2020-01-01", "2020-01-02", "all", 0, false)
	if err != nil || len(all) != 3 {
		t.Fatalf("all %+v %v", all, err)
	}
	value := 3500.0
	var corrected int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error { var e error; corrected, e = tx.Correct(a, &value); return e }); err != nil {
		t.Fatal(err)
	}
	row, err := s.Measurement(ctx, corrected)
	if err != nil || row.SessionID != sessions[0] || row.Day != "2020-01-02" || row.Supersedes != a {
		t.Fatalf("same-scope %+v %v", row, err)
	}
	result, err := s.RelocateReading(ctx, "cli", corrected, Reading{Metric: "Steps", Day: "2020-01-03", Value: 3500, SessionID: sessions[1]})
	if err != nil {
		t.Fatal(err)
	}
	retraction, err := s.Measurement(ctx, result.RetractionID)
	if err != nil || !retraction.Retraction || retraction.SessionID != sessions[0] || retraction.Supersedes != corrected {
		t.Fatalf("retraction %+v %v", retraction, err)
	}
	replacement, err := s.Measurement(ctx, result.ReplacementID)
	if err != nil || replacement.Supersedes != 0 || replacement.SessionID != sessions[1] || replacement.Day != "2020-01-03" {
		t.Fatalf("new root %+v %v", replacement, err)
	}
	if _, err := s.RelocateReading(ctx, "cli", corrected, Reading{Metric: "Steps", Day: "2020-01-02", Value: 3500}); status(err) != 409 {
		t.Fatalf("stale %v", err)
	}
	// Fault after the retraction INSERT, before replacement publication; all SQL rolls back.
	if _, err := s.DB.W.Exec(`CREATE TRIGGER fail_scope_replacement BEFORE INSERT ON measurements WHEN NEW.supersedes_id IS NULL AND NEW.value=999 BEGIN SELECT RAISE(ABORT,'injected replacement failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RelocateReading(ctx, "cli", b, Reading{Metric: "Steps", Day: "2020-01-02", Value: 999}); err == nil {
		t.Fatal("accepted injected failure")
	}
	kept, err := s.Measurement(ctx, b)
	if err != nil || !kept.Current || kept.SupersededBy != 0 || kept.SessionID != sessions[1] {
		t.Fatalf("partial relocation %+v %v", kept, err)
	}
	session, err := s.Session(ctx, sessions[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.SessionLifecycle(session.ID, session.Version, true) }); err != nil {
		t.Fatal(err)
	}
	active, err := s.SeriesScope(ctx, "Steps", "2020-01-01", "2020-01-03", "all", 0, false)
	if err != nil || len(active) != 1 || active[0].ID != daily {
		t.Fatalf("active %+v %v", active, err)
	}
	historical, err := s.SeriesScope(ctx, "Steps", "2020-01-01", "2020-01-03", "all", 0, true)
	if err != nil || len(historical) != 3 {
		t.Fatalf("historical %+v %v", historical, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { _, err := tx.Correct(b, &value); return err }); err == nil {
		t.Fatal("restored value into tombstoned session")
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { _, err := tx.Correct(b, nil); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestScopeRelocationRejectsKeyedAncestorsAgentsAndRollback(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Training", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var e error
		session, _, e = tx.CaptureSession(SessionInput{Kind: "Training", Day: "2020-01-02", StartLocal: "2020-01-02T08:00:00.000"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	root, err := s.Record(ctx, "import:synthetic", Reading{Metric: "Weight", Day: "2020-01-02", Value: 70, Key: "9007199254740993"})
	if err != nil {
		t.Fatal(err)
	}
	value := 71.0
	var leaf int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error { var e error; leaf, e = tx.Correct(root, &value); return e }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RelocateReading(ctx, "cli", leaf, Reading{Metric: "Weight", Day: "2020-01-02", Value: 71, SessionID: session}); err == nil {
		t.Fatal("keyed imported ancestor bypass")
	}
	allowed, err := s.RelocationAllowed(ctx, leaf)
	if err != nil || allowed {
		t.Fatalf("availability %v %v", allowed, err)
	}
	ordinary, err := s.Record(ctx, "cli", Reading{Metric: "Weight", Day: "2020-01-02", Value: 72})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RelocateReading(ctx, "agent:test", ordinary, Reading{Metric: "Weight", Day: "2020-01-02", Value: 72, SessionID: session}); err == nil {
		t.Fatal("agent bypass")
	}
	failure := errors.New("rollback after complete relocation")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.RelocateReading(ordinary, Reading{Metric: "Weight", Day: "2020-01-02", Value: 72, SessionID: session}); err != nil {
			return err
		}
		return failure
	}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	have, err := s.Measurement(ctx, ordinary)
	if err != nil || !have.Current || have.SupersededBy != 0 {
		t.Fatalf("rollback %+v %v", have, err)
	}
}
