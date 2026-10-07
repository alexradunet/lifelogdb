package importer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedReservationCommitMarkerRecoveryAndSeparateProviders(t *testing.T) {
	for _, failAt := range []string{"reservation", "completion"} {
		t.Run(failAt, func(t *testing.T) {
			f := setup(t)
			f.approveRules(t, rulesBody)
			writeSource(t, f, "bound.csv", "Date,Distance (m)\n2020-01-02,10.5\n")
			mustLedger(t, f)
			registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Source distance"})
			b, e := f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"})
			if e != nil {
				t.Fatal(e)
			}
			if e = ownerApproves(f.w, preparedFile); e != nil {
				t.Fatal(e)
			}
			if failAt == "reservation" {
				if e = f.w.bindPrepared(b, true); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e = f.w.prepared(ctx, f.s, false, func() error { return errors.New("synthetic commit publication interruption") }); e == nil {
					t.Fatal("interruption swallowed")
				}
			}
			target := filepath.Join(t.TempDir(), "reserved.db")
			if _, e = f.w.Replay(ctx, f.s, target); e == nil {
				t.Fatal("reservation claimed complete")
			}
			if _, e = os.Stat(target); !os.IsNotExist(e) {
				t.Fatal("reserved replay created target")
			}
			if _, e = f.w.Prepared(ctx, f.s, false); e != nil {
				t.Fatal(e)
			}
			if importerMeasurementRows(t, f) != 1 {
				t.Fatal("recovery did not retain single root")
			}
			marker := filepath.Join(f.w.Dir, preparedBindingName(b)+".complete")
			if e = os.WriteFile(marker, []byte("corrupt"), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = f.w.Replay(ctx, f.s, target); e == nil {
				t.Fatal("corrupt completion accepted")
			}
			// A verified unchanged apply is the repair path; it checks SQL originals and recreates completion.
			if _, e = f.w.Prepared(ctx, f.s, false); e != nil {
				t.Fatal(e)
			}
			binding := filepath.Join(f.w.Dir, preparedBindingName(b))
			if e = os.WriteFile(binding, []byte("{}"), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = f.w.Prepared(ctx, f.s, false); e == nil {
				t.Fatal("corrupt reservation accepted")
			}
		})
	}
	// Same values in a truly separate synthetic provider workspace are not global duplicates.
	f := setup(t)
	f.approveRules(t, strings.Replace(rulesBody, "import:notebook", "import:other-provider", 1))
	writeSource(t, f, "bound.csv", "Date,Distance (m)\n2020-01-02,10.5\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Other provider"})
	if _, e := f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); e != nil {
		t.Fatal(e)
	}
	if e := ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
}
func TestReplayRevalidatesExternalSelectionAfterRehearsal(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "bound.csv", "Date,Distance (m)\n2020-01-02,10.5\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Source"})
	if _, e := f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); e != nil {
		t.Fatal(e)
	}
	if e := ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(t.TempDir(), "must-not-exist.db")
	_, e := f.w.replayAfterRehearsal(ctx, f.s, target, func() error {
		return os.WriteFile(filepath.Join(f.w.Source, "bound.csv"), []byte("Date,Distance (m)\n2020-01-02,99\n"), 0600)
	})
	if e == nil {
		t.Fatal("changed source after real rehearsal applied")
	}
	if _, e = os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("target changed before snapshot revalidation")
	}
}
