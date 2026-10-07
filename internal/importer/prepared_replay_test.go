package importer

import (
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestPreparedReplayPortableScopeCorrectedRootAndWrongTargetRefusal(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "sleep.json", `[{"logId":9007199254740993,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","minutesAsleep":420}]`)
	mustLedger(t, f)
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, _, e := tx.CreatePage("Sleep", "", "", ""); return e }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterMetric(ctx, "cli", "Asleep", "min", ""); err != nil {
		t.Fatal(err)
	}
	b, err := f.w.DraftPrepared(ctx, "sleep.json", "legacy-sleep-array-v1", "Sleep", nil, map[string]string{"asleep-minutes": "Asleep"})
	if err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	var root int64
	if err = f.s.Do(ctx, b.Source, func(tx *core.Tx) error {
		var e error
		root, e = tx.MeasurementByKey(b.Source, "Asleep", preparedKey(b.Profile, b.Records[0].Key, "asleep-minutes"))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	value := 400.0
	_, correction, err := f.s.Correct(ctx, "cli", root, &value)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.w.RecordCorrection(correction); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), "target.db")
	if err := db.Init(targetPath); err != nil {
		t.Fatal(err)
	}
	targetDB, err := db.Open(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	target := &core.Store{DB: targetDB}
	if err = target.Do(ctx, "cli", func(tx *core.Tx) error {
		for _, title := range []string{"Padding one", "Padding two", "Sleep"} {
			if _, _, _, e := tx.CreatePage(title, "", "", ""); e != nil {
				return e
			}
		}
		if _, e := tx.RegisterMetric("Asleep", "min", ""); e != nil {
			return e
		}
		_, _, e := tx.CaptureSession(core.SessionInput{Kind: "Sleep", Day: "1999-01-01", StartLocal: "1999-01-01T00:00:00.000"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = writePrepared(ctx, target, b, false); err != nil {
		t.Fatal(err)
	}
	trialSessions, err := f.s.Sessions(ctx, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	targetSessions, err := target.Sessions(ctx, "", "", true)
	if err != nil || len(targetSessions) != 2 || targetSessions[1].ID == trialSessions[0].ID {
		t.Fatalf("different entity IDs %+v %v", targetSessions, err)
	}
	if _, err = f.w.replayCorrections(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	if err = target.Do(ctx, b.Source, func(tx *core.Tx) error {
		id, e := tx.MeasurementByKey(b.Source, "Asleep", preparedKey(b.Profile, b.Records[0].Key, "asleep-minutes"))
		if e != nil {
			return e
		}
		_, v, ok, e := tx.CurrentOf(id)
		if e == nil && (!ok || v != 400) {
			t.Fatalf("corrected target %v %v", v, ok)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Deliberate target damage: remove the scoped association. The source root key/value still match.
	if _, err = targetDB.W.Exec(`DROP TRIGGER measurements_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err = targetDB.W.Exec(`UPDATE measurements SET session_id=NULL WHERE supersedes_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err = writePrepared(ctx, target, b, true); err == nil {
		t.Fatal("wrong-session target root verified")
	}
}
