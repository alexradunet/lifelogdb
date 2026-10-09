package core

import (
	"context"
	"fmt"
	"math"
	"testing"
)

func TestSessionFullCalendarAndExactRevisionExhaustion(t *testing.T) {
	s := fresh(t)
	kind, _, err := s.CreatePage(ctx, "cli", "Calendar session", "")
	if err != nil {
		t.Fatal(err)
	}
	in := SessionInput{Kind: "Calendar session", Day: "0000-01-01", StartAt: "0000-01-01T00:00:00.000Z", EndAt: "9999-12-31T23:59:59.999Z"}
	var id int64
	if err := s.Do(ctx, "agent:synthetic", func(tx *Tx) error { var err error; id, _, err = tx.CaptureSession(in); return err }); err != nil {
		t.Fatal(err)
	}
	have, err := s.Session(ctx, id)
	if err != nil || have.ElapsedMilliseconds == nil || *have.ElapsedMilliseconds != 315569519999999 {
		t.Fatalf("full calendar %+v %v", have, err)
	}
	page, err := s.PageByID(ctx, kind)
	if err != nil || !page.SessionKind {
		t.Fatalf("kind %+v %v", page, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.PromotePeriod(kind, page.Version, nil, nil) }); err == nil {
		t.Fatal("retained kind promoted")
	}
	if _, err := s.DB.W.Exec("UPDATE sessions SET revision=? WHERE id=?", int64(math.MaxInt64), id); err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprint(int64(math.MaxInt64))
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditSession(id, token, in) }); err != nil {
		t.Fatalf("exhausted no-op %v", err)
	}
	changed := in
	changed.Day = "0000-01-02"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditSession(id, token, changed) }); err == nil {
		t.Fatal("revision wrapped")
	}
	kept, err := s.Session(ctx, id)
	if err != nil || kept.Version != token || kept.Day != in.Day {
		t.Fatalf("exhaustion lost state %+v %v", kept, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Do(canceled, "cli", func(tx *Tx) error { return tx.EditSession(id, token, changed) }); err == nil {
		t.Fatal("cancel ignored")
	}
}
