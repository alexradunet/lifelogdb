package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func statusWorkspaceFiles(t *testing.T, w *Workspace) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := filepath.WalkDir(w.Dir, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() || filepath.Ext(path) == ".db" || filepath.Ext(path) == ".db-wal" || filepath.Ext(path) == ".db-shm" {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil {
			out[path] = string(b)
		}
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestPreparedStatusCorrectedAliasSessionOnlyAndUnexplainedSession(t *testing.T) {
	f := statusSelectionWorkspace(t)
	writeSource(t, f, "sleep.json", `[{"logId":1,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","minutesAsleep":420}]`)
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Asleep", Unit: "min", Note: "Source asleep"})
	var kind int64
	if e := f.s.Do(ctx, "cli", func(tx *core.Tx) error { var e error; kind, _, _, e = tx.CreatePage("Sleep", "", "", ""); return e }); e != nil {
		t.Fatal(e)
	}
	b, e := f.w.DraftPrepared(ctx, "sleep.json", "legacy-sleep-array-v1", "Sleep", nil, map[string]string{"asleep-minutes": "Asleep"})
	if e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	var root int64
	if e = f.s.Do(ctx, b.Source, func(tx *core.Tx) error {
		var e error
		root, e = tx.MeasurementByKey(b.Source, "Asleep", preparedKey(b.Profile, b.Records[0].Key, "asleep-minutes"))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	value := 400.0
	if _, _, e = f.s.Correct(ctx, "cli", root, &value); e != nil {
		t.Fatal(e)
	}
	metric, e := f.s.PageID(ctx, "Asleep")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Rename(ctx, "cli", metric, "Renamed asleep"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Rename(ctx, "cli", kind, "Renamed sleep"); e != nil {
		t.Fatal(e)
	}
	before := statusWorkspaceFiles(t, f.w)
	good, e := f.w.Status(ctx, f.s, f.trial)
	if e != nil || len(good.Mismatches) != 0 {
		t.Fatalf("corrected alias control %+v %v", good, e)
	}
	if !reflect.DeepEqual(before, statusWorkspaceFiles(t, f.w)) {
		t.Fatal("status published workspace recovery")
	}
	if e = f.s.Do(ctx, b.Source, func(tx *core.Tx) error {
		_, _, e := tx.CaptureSession(core.SessionInput{Kind: "Sleep", Day: "2020-01-03", StartLocal: "2020-01-03T00:00:00.000", Key: "unexplained"})
		return e
	}); e != nil {
		t.Fatal(e)
	}
	bad, e := f.w.Status(ctx, f.s, f.trial)
	if e != nil || len(bad.Mismatches) == 0 {
		t.Fatal("unexplained imported session ignored")
	}
	// Missing session-only applied data is not inferred from insert feasibility.
	f2 := statusSelectionWorkspace(t)
	writeSource(t, f2, "sleep.json", `[{"logId":2,"dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00"}]`)
	mustLedger(t, f2)
	if e = f2.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, _, e := tx.CreatePage("Sleep", "", "", ""); return e }); e != nil {
		t.Fatal(e)
	}
	if _, e = f2.w.DraftPrepared(ctx, "sleep.json", "legacy-sleep-array-v1", "Sleep", nil, map[string]string{}); e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f2.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e = f2.w.Prepared(ctx, f2.s, false); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "empty.db")
	if e = db.Init(path); e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	empty := &core.Store{DB: d}
	if e = empty.Do(ctx, "cli", func(tx *core.Tx) error { _, _, _, e := tx.CreatePage("Sleep", "", "", ""); return e }); e != nil {
		t.Fatal(e)
	}
	before = statusWorkspaceFiles(t, f2.w)
	bad, e = f2.w.Status(ctx, empty, path)
	if e != nil || len(bad.Mismatches) == 0 {
		t.Fatal("missing applied session reported consistent")
	}
	var count int
	if e = d.R.QueryRow(`SELECT count(*) FROM sessions`).Scan(&count); e != nil || count != 0 {
		t.Fatal("status inserted missing session")
	}
	if !reflect.DeepEqual(before, statusWorkspaceFiles(t, f2.w)) {
		t.Fatal("session status changed receipts")
	}
}
func TestReportedCaloriesPreparedApplicationUsesDocumentedMeaning(t *testing.T) {
	f := statusSelectionWorkspace(t)
	writeSource(t, f, "daily.csv", "Date,Calories (kcal)\n2020-01-02,2100\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Reported energy", Unit: "kcal", Note: "Source-reported calories; no active/basal subtype"})
	if _, e := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"reported-calories": "Reported energy"}); e != nil {
		t.Fatal(e)
	}
	if e := ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	var value float64
	var day, key string
	var unscoped bool
	if e := f.s.DB.R.QueryRow(`SELECT value,day,import_key,session_id IS NULL FROM measurements`).Scan(&value, &day, &key, &unscoped); e != nil || value != 2100 || day != "2020-01-02" || key != preparedKey("fit-date-csv-v1", "daily:2020-01-02", "reported-calories") || !unscoped {
		t.Fatalf("source meaning %g %s %s %v %v", value, day, key, unscoped, e)
	}
}
