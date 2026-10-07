package importer

import (
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
	"lifelog/internal/preview"
)

func TestSelectionActualRehearseReplayHistoryAndTrialProof(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	// Isolated synthetic workspace, with the session kind supplied by the ordinary vault path.
	if err := os.RemoveAll(f.w.Source); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.w.Source, 0700); err != nil {
		t.Fatal(err)
	}
	writeSource(t, f, "Sleep.md", "")
	writeSource(t, f, "sleep.json", `[{"logId":9007199254740993,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","minutesAsleep":420}]`)
	for _, name := range []string{"one.jpg", "two.jpg"} {
		raw := phototest.JPEG(3, 2, photo.Meta{Taken: "2020-01-02 03:04:05", Lat: 10, Lon: 20, HasGPS: true, Orientation: 1}, true)
		if name == "two.jpg" {
			raw = phototest.JPEG(4, 2, photo.Meta{Orientation: 1}, true)
		}
		if name == "one.jpg" {
			m := photo.Read(raw)
			if !m.HasGPS || m.Taken != "2020-01-02 03:04:05" {
				t.Fatal("metadata-bearing input setup failed")
			}
		}
		if err := os.WriteFile(filepath.Join(f.w.Source, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Asleep", Unit: "min", Note: "Source asleep minutes"})
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
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
	v := 400.0
	_, key, err := f.s.Correct(ctx, "cli", root, &v)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.w.RecordCorrection(key); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.jpg", "two.jpg"} {
		if _, err = f.w.DraftSelectedPhoto(ctx, name, "", name, "", PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
			t.Fatal(err)
		}
		if err = ownerApproves(f.w, selectedPhotoFile); err != nil {
			t.Fatal(err)
		}
		if _, err = f.w.SelectedPhoto(ctx, f.s, false); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "target.db")
	rehearsal, err := f.w.Rehearse(ctx, f.s, target)
	if err != nil || len(rehearsal.Failures) != 0 {
		t.Fatalf("actual rehearsal %+v %v", rehearsal, err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("rehearsal created target")
	}
	result, err := f.w.Replay(ctx, f.s, target)
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("actual replay %+v %v", result, err)
	}
	d, err := db.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var count int
	if err = d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("multiple selection retention %d %v", count, err)
	}
	var day, clock string
	var value float64
	var scope int64
	if err = d.R.QueryRow(`SELECT me.day,s.start_local,me.value,s.id FROM measurements me JOIN sessions s ON s.id=me.session_id WHERE NOT EXISTS(SELECT 1 FROM measurements child WHERE child.supersedes_id=me.id)`).Scan(&day, &clock, &value, &scope); err != nil || day != "2020-01-02" || clock != "2020-01-01T23:00:00.000" || value != 400 || scope == 0 {
		t.Fatalf("replayed meaning %s %s %g %d %v", day, clock, value, scope, err)
	}
	if _, err = f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	// Genuine metadata was in the original, not merely asserted absent on an already-stripped input.
	var picture []byte
	if err = d.R.QueryRow(`SELECT preview FROM files f JOIN entities e ON e.id=f.id JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE n.title='one.jpg'`).Scan(&picture); err != nil {
		t.Fatal(err)
	}
	if len(picture) == 0 || preview.Check(picture) != nil {
		t.Fatal("missing or invalid preview")
	}
	if meta := photo.Read(picture); meta.HasGPS || meta.Taken != "" {
		t.Fatal("EXIF retained in preview")
	}
	existing := filepath.Join(t.TempDir(), "existing.db")
	if err = db.Init(existing); err != nil {
		t.Fatal(err)
	}
	ed, e := db.Open(existing)
	if e != nil {
		t.Fatal(e)
	}
	defer ed.Close()
	es := &core.Store{DB: ed}
	if e = es.Do(ctx, "cli", func(tx *core.Tx) error {
		for _, name := range []string{"Padding one", "Padding two"} {
			if _, _, _, e := tx.CreatePage(name, "", "", ""); e != nil {
				return e
			}
		}
		_, _, e := tx.CaptureSession(core.SessionInput{Kind: "Padding one", Day: "1999-01-01", StartLocal: "1999-01-01T00:00:00.000"})
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Replay(ctx, f.s, existing); e != nil {
		t.Fatal(e)
	}
	var targetScope int64
	if e = ed.R.QueryRow(`SELECT session_id FROM measurements WHERE supersedes_id IS NULL`).Scan(&targetScope); e != nil || targetScope == scope {
		t.Fatalf("different target session ID %d %v", targetScope, e)
	}
	if _, e = ed.W.Exec(`DROP TRIGGER measurements_no_update;UPDATE measurements SET session_id=1 WHERE supersedes_id IS NULL`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Replay(ctx, f.s, existing); e == nil {
		t.Fatal("wrong existing target association replayed")
	}
	if _, err = f.s.DB.W.Exec(`DROP TRIGGER measurements_no_update; UPDATE measurements SET session_id=NULL WHERE supersedes_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	badTarget := filepath.Join(t.TempDir(), "must-not-exist.db")
	if _, err = f.w.Replay(ctx, f.s, badTarget); err == nil {
		t.Fatal("wrong trial association replayed")
	}
	if _, err = os.Stat(badTarget); !os.IsNotExist(err) {
		t.Fatal("trial refusal mutated target")
	}
}
