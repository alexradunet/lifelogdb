package importer

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestPreparedCancellationAfterObservedWriteAndPublicationRecovery(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10\n2020-01-03,20\n")
	mustLedger(t, f)
	if _, err := f.s.RegisterMetric(ctx, "cli", "Distance", "m", ""); err != nil {
		t.Fatal(err)
	}
	b, err := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"})
	if err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	observed := 0
	if _, err = writePreparedAfter(request, f.s, b, false, func(tx *core.Tx) error {
		id, e := tx.MeasurementByKey(b.Source, "Distance", preparedKey(b.Profile, b.Records[0].Key, "distance"))
		if e != nil {
			return e
		}
		row, e := tx.MeasurementRow(id)
		if e != nil {
			return e
		}
		if id == 0 || row.Value == nil || *row.Value != 10 || row.Source != b.Source {
			t.Fatalf("actual intermediate root %+v", row)
		}
		observed++
		cancel()
		return nil
	}); (!errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone)) || observed != 1 {
		t.Errorf("request cancellation %d %v", observed, err)
	}
	if importerMeasurementRows(t, f) != 0 {
		t.Fatal("partial canceled write committed")
	}
	reopened, err := db.Open(f.trial)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err = reopened.R.QueryRow(`SELECT count(*) FROM measurements`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reopened cancellation %d %v", count, err)
	}
	ledger := filepath.Join(f.w.Dir, "ledger.md")
	saved := ledger + ".saved"
	report, err := f.w.prepared(ctx, f.s, false, func() error {
		if e := os.Rename(ledger, saved); e != nil {
			return e
		}
		return os.Mkdir(ledger, 0700)
	})
	if err == nil || report == nil || !report.Applied || importerMeasurementRows(t, f) != 2 {
		t.Fatalf("publication boundary %+v %v", report, err)
	}
	if err = os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(saved, ledger); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, &core.Store{DB: reopened}, false); err != nil {
		t.Fatal(err)
	}
	if importerMeasurementRows(t, f) != 2 {
		t.Fatal("publication retry duplicated SQL")
	}
	lines, _, err := f.w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.File == "daily.csv" {
			found = l.State == "x"
		}
	}
	if !found {
		t.Fatal("publication not recovered")
	}
}
