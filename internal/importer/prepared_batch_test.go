package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/core"
)

func TestPreparedOwnerGateAtomicRetryCorrectionAndImmutableMapping(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10.5\n2020-01-03,20\n")
	mustLedger(t, f)
	if _, err := f.s.RegisterMetric(ctx, "cli", "Distance", "m", "Source distance"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterMetric(ctx, "cli", "Other distance", "m", "Other meaning"); err != nil {
		t.Fatal(err)
	}
	b, err := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err == nil {
		t.Fatal("draft bypassed owner gate")
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, true); err != nil {
		t.Fatal(err)
	}
	if importerMeasurementRows(t, f) != 0 {
		t.Fatal("check wrote readings")
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	var root int64
	if err = f.s.Do(ctx, b.Source, func(tx *core.Tx) error {
		var e error
		root, e = tx.MeasurementByKey(b.Source, "Distance", preparedKey(b.Profile, b.Records[0].Key, "distance"))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	corrected := 12.0
	if _, _, err = f.s.Correct(ctx, "cli", root, &corrected); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	if importerMeasurementRows(t, f) != 3 {
		t.Fatal("retry changed corrected root")
	}
	if _, err = f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Other distance"}); err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err == nil {
		t.Fatal("reapproval remapped an applied quantity")
	}
	if importerMeasurementRows(t, f) != 3 {
		t.Fatal("refused mapping wrote SQL")
	}
}
func TestPreparedSessionScopePortableAndPartialWorkRollback(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "sleep.json", `[{"logId":"9007199254740993","dateOfSleep":"2020-01-02","startTime":"2020-01-01T23:00:00","minutesAsleep":420,"timeInBed":480}]`)
	mustLedger(t, f)
	if err := f.s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, _, e := tx.CreatePage("Sleep", "", "", ""); return e }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterMetric(ctx, "cli", "Asleep", "min", ""); err != nil {
		t.Fatal(err)
	}
	b, err := f.w.DraftPrepared(ctx, "sleep.json", "legacy-sleep-array-v1", "Sleep", nil, map[string]string{"asleep-minutes": "Asleep", "in-bed-minutes": "In bed"})
	if err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err == nil {
		t.Fatal("missing second metric accepted")
	}
	sessions, err := f.s.Sessions(ctx, "", "", true)
	if err != nil || len(sessions) != 0 || importerMeasurementRows(t, f) != 0 {
		t.Fatal("partial batch survived refusal")
	}
	if _, err = f.s.RegisterMetric(ctx, "cli", "In bed", "min", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	sessions, err = f.s.Sessions(ctx, "", "", true)
	if err != nil || len(sessions) != 1 || sessions[0].Day != "2020-01-02" || sessions[0].StartLocal != "2020-01-01T23:00:00.000" {
		t.Fatalf("portable session %+v %v", sessions, err)
	}
	var scoped int
	if err = f.s.DB.R.QueryRow(`SELECT count(*) FROM measurements WHERE session_id=? AND day='2020-01-02'`, sessions[0].ID).Scan(&scoped); err != nil || scoped != 2 {
		t.Fatalf("scope %d %v", scoped, err)
	}
	b.Records[0].Quantities[0].Value = "421"
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("status: draft\n"), data...)
	if err = os.WriteFile(filepath.Join(f.w.Dir, preparedFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err == nil {
		t.Fatal("arbitrary caller artifact approved")
	}
}
