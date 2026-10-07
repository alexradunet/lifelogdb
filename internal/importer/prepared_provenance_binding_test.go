package importer

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAppliedPreparedSourceCannotBeReassigned(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "bound.csv", "Date,Distance (m)\n2020-01-02,10.5\n")
	mustLedger(t, f)
	if _, err := f.s.RegisterMetric(ctx, "cli", "Distance", "m", "Source-reported distance"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	var source, day string
	var value float64
	if err := f.s.DB.R.QueryRow(`SELECT source,day,value FROM measurements`).Scan(&source, &day, &value); err != nil || source != "import:notebook" || day != "2020-01-02" || value != 10.5 {
		t.Fatalf("original interpretation control: %s %s %g %v", source, day, value, err)
	}
	before, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := filepath.Glob(filepath.Join(f.w.Dir, ".prepared-binding-*"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		after, e := f.s.Counts(ctx)
		if e != nil {
			t.Fatal(e)
		}
		later, e := filepath.Glob(filepath.Join(f.w.Dir, ".prepared-binding-*"))
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(bindings, later) {
			t.Errorf("reassignment changed SQL/binding state: counts=%+v => %+v; bindings=%d => %d", before, after, len(bindings), len(later))
		}
	}()
	f.approveRules(t, strings.Replace(rulesBody, "import:notebook", "import:reassigned", 1))
	if _, err = f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
		// Early refusal is also valid: the established file/namespace binding cannot be reinterpreted here.
		return
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		return
	}
	_, applyErr := f.w.Prepared(ctx, f.s, false)
	if applyErr == nil {
		t.Error("reapproving the same applied file under a new namespace created a second interpretation")
	}
}
