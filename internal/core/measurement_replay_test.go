package core

import "testing"

func TestRecordReplayAfterSessionTombstone(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Training", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		session, _, err = tx.CaptureSession(SessionInput{Kind: "Training", Day: "2026-10-07", StartLocal: "2026-10-07T08:00:00.000"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	input := Reading{Metric: "Mood", Day: "2026-10-07", Value: 3, Key: "reading-1", SessionID: session}
	root, err := s.Record(ctx, "import:synthetic", input)
	if err != nil || root == 0 {
		t.Fatalf("initial record: id=%d err=%v", root, err)
	}
	before, err := s.Session(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.SessionLifecycle(session, before.Version, true) }); err != nil {
		t.Fatal(err)
	}
	tombstoned, err := s.Session(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	type fact struct {
		ID, MetricID, SessionID     int64
		Day, Source, Key, CreatedAt string
		Value                       float64
	}
	readFact := func() fact {
		t.Helper()
		var f fact
		if err := s.DB.R.QueryRowContext(ctx, "SELECT id,metric_id,session_id,day,source,import_key,created_at,value FROM measurements WHERE id=?", root).Scan(&f.ID, &f.MetricID, &f.SessionID, &f.Day, &f.Source, &f.Key, &f.CreatedAt, &f.Value); err != nil {
			t.Fatal(err)
		}
		return f
	}
	expectedFact := readFact()
	if again, err := s.Record(ctx, "import:synthetic", input); err != nil || again != 0 {
		t.Errorf("exact committed retry must remain a no-op after session tombstone: id=%d err=%v", again, err)
	}
	input.Key = "reading-2"
	if _, err := s.Record(ctx, "import:synthetic", input); err == nil {
		t.Error("new value accepted on tombstoned session")
	}
	var count int
	if err := s.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM measurements WHERE session_id=?", session).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry or refused new value changed facts: count=%d err=%v", count, err)
	}
	if got := readFact(); got != expectedFact {
		t.Fatalf("retry or refusal changed recorded fact: got %+v want %+v", got, expectedFact)
	}
	after, err := s.Session(ctx, session)
	if err != nil || after.Version != tombstoned.Version || after.DeletedAt != tombstoned.DeletedAt || after.DeletedAt == "" {
		t.Fatalf("retry changed session lifecycle or revision: %+v err=%v", after, err)
	}
	if retraction, _, err := s.Correct(ctx, "cli", root, nil); err != nil || retraction == 0 {
		t.Fatalf("same-scope NULL retraction remains allowed: id=%d err=%v", retraction, err)
	}
	if err := s.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM measurement_values WHERE session_id=?", session).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retraction left active value: count=%d err=%v", count, err)
	}
}
