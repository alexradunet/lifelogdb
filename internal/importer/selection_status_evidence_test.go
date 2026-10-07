package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStatusDistinguishesReservedCompletedAndCorruptEvidenceWithoutPublication(t *testing.T) {
	f := statusSelectionWorkspace(t)
	writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Source"})
	b, e := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"})
	if e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	check := func() {
		t.Helper()
		before := statusWorkspaceFiles(t, f.w)
		if _, e := f.w.Status(ctx, f.s, f.trial); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(before, statusWorkspaceFiles(t, f.w)) {
			t.Fatal("status published evidence")
		}
	}
	check()
	if e = f.w.bindPrepared(b, true); e != nil {
		t.Fatal(e)
	}
	check()
	if importerMeasurementRows(t, f) != 0 {
		t.Fatal("reserved status inserted data")
	}
	if complete, e := f.w.preparedCompletion(b); e != nil || complete {
		t.Fatal("reservation became completion")
	}
	if _, e = f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	good, e := f.w.Status(ctx, f.s, f.trial)
	if e != nil || len(good.Mismatches) != 0 {
		t.Fatal("completed control mismatch")
	}
	marker := filepath.Join(f.w.Dir, preparedBindingName(b)+".complete")
	if e = os.WriteFile(marker, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	before := statusWorkspaceFiles(t, f.w)
	bad, e := f.w.Status(ctx, f.s, f.trial)
	if e != nil || len(bad.Mismatches) == 0 {
		t.Fatal("corrupt completion reported consistent")
	}
	if !reflect.DeepEqual(before, statusWorkspaceFiles(t, f.w)) {
		t.Fatal("status repaired corruption")
	}
	if e = os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	bad, e = f.w.Status(ctx, f.s, f.trial)
	if e != nil || len(bad.Mismatches) == 0 {
		t.Fatal("done ledger inferred completion from reservation")
	}
}
