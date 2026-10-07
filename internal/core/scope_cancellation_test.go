package core

import (
	"context"
	"database/sql"
	"errors"
	"lifelog/internal/db"
	"reflect"
	"testing"
)

func TestScopeRelocationCancellationAfterRetraction(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Steps", "steps", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Workout", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		session, _, err = tx.CaptureSession(SessionInput{Kind: "Workout", Day: "2020-01-02", StartAt: "2020-01-02T08:00:00.000Z"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	root, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Measurement(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	beforeSession, err := s.Session(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	observed := false
	err = s.Do(request, "cli", func(tx *Tx) error {
		_, err := tx.relocateReading(root, Reading{Metric: "Steps", Day: "2020-01-03", Value: 4000}, func(id int64) error {
			var parent, scope int64
			var value any
			if err := tx.tx.QueryRow("SELECT supersedes_id,session_id,value FROM measurements WHERE id=?", id).Scan(&parent, &scope, &value); err != nil {
				return err
			}
			if parent != root || scope != session || value != nil {
				return errors.New("wrong intermediate retraction")
			}
			observed = true
			cancel()
			return nil // Production must observe cancellation, not an injected hook error.
		})
		return err
	})
	// database/sql may roll back before the next statement observes cancellation.
	if !observed || request.Err() != context.Canceled || !(errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone)) {
		t.Fatalf("intermediate cancellation: observed=%v err=%v", observed, err)
	}
	check := func() {
		t.Helper()
		after, err := s.Measurement(ctx, root)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("reading changed %+v %v", after, err)
		}
		afterSession, err := s.Session(ctx, session)
		if err != nil || !reflect.DeepEqual(beforeSession, afterSession) {
			t.Fatalf("session changed %+v %v", afterSession, err)
		}
		afterCounts, err := s.Counts(ctx)
		if err != nil || !reflect.DeepEqual(counts, afterCounts) {
			t.Fatalf("counts changed %+v %v", afterCounts, err)
		}
		var n int
		if err := s.DB.R.QueryRow("SELECT count(*) FROM measurements").Scan(&n); err != nil || n != 1 {
			t.Fatalf("half-operation persisted %d %v", n, err)
		}
	}
	check()
	var seq int
	var name, path string
	if err := s.DB.R.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	check()
}
