package importer

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"lifelog/internal/core"
)

// A Fit daily aggregate of two days. The hand-checked whole values: 1789.3456789012 -> 1789, 5432.5 -> 5433 and
// 72.5 -> 73 (half away from zero), 0.4 -> 0, 10.49 -> 10, 61.5 -> 62; steps are not rounded.
const roundCSV = "Date,Calories (kcal),Distance (m),Average heart rate (bpm),Step count\n" +
	"2031-04-11,1789.3456789012,5432.5,72.5,8123\n" +
	"2031-04-12,0.4,10.49,61.5,0\n"

const roundMetrics = `{"reported-calories":"Calories","distance":"Distance","mean-heart-rate":"Mean heart rate","steps":"Steps"}`

func setupRound(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "daily.csv", roundCSV)
	mustLedger(t, f)
	for _, m := range [][2]string{{"Calories", "kcal"}, {"Distance", "m"}, {"Mean heart rate", "bpm"}, {"Steps", "steps"}} {
		if _, err := f.s.RegisterMetric(ctx, "cli", m[0], m[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// preparedValue reads the stored value of one prepared reading by its key.
func preparedValue(t *testing.T, f *fixture, b *PreparedBatch, metric, day, code string) float64 {
	t.Helper()
	var id int64
	if err := f.s.DryRun(ctx, b.Source, func(tx *core.Tx) error {
		var err error
		id, err = tx.MeasurementByKey(b.Source, metric, preparedKey(b.Profile, "daily:"+day, code))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var value float64
	if err := f.s.DB.R.QueryRowContext(ctx, `SELECT value FROM measurements WHERE id = ?`, id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestPreparedRoundsChosenQuantitiesToWholeUnits(t *testing.T) {
	f := setupRound(t)
	b, err := f.w.DraftPreparedJSON(ctx, "daily.csv", "fit-date-csv-v1", "", "", roundMetrics, `["reported-calories","mean-heart-rate","distance"]`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"distance", "mean-heart-rate", "reported-calories"}; !reflect.DeepEqual(b.Round, want) {
		t.Fatalf("round = %v, want the sorted list %v", b.Round, want)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		metric, day, code string
		want              float64
	}{
		{"Calories", "2031-04-11", "reported-calories", 1789},
		{"Distance", "2031-04-11", "distance", 5433},
		{"Mean heart rate", "2031-04-11", "mean-heart-rate", 73},
		{"Steps", "2031-04-11", "steps", 8123},
		{"Calories", "2031-04-12", "reported-calories", 0},
		{"Distance", "2031-04-12", "distance", 10},
		{"Mean heart rate", "2031-04-12", "mean-heart-rate", 62},
		{"Steps", "2031-04-12", "steps", 0},
	} {
		if got := preparedValue(t, f, b, c.metric, c.day, c.code); got != c.want {
			t.Errorf("%s on %s = %v, want %v", c.metric, c.day, got, c.want)
		}
	}
	if n := importerMeasurementRows(t, f); n != 8 {
		t.Fatalf("readings = %d, want 8", n)
	}
	// a second apply finds every rounded reading as it is and writes nothing
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	if n := importerMeasurementRows(t, f); n != 8 {
		t.Fatalf("a second apply changed the readings: %d rows, want 8", n)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Mismatches) != 0 {
		t.Fatalf("status after a rounded apply: mismatches %v", st.Mismatches)
	}

	// a different choice after the apply is a changed interpretation: refused, nothing written
	if _, err = f.w.DraftPreparedJSON(ctx, "daily.csv", "fit-date-csv-v1", "", "", roundMetrics, `["distance"]`); err != nil {
		t.Fatal(err)
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err == nil {
		t.Fatal("a changed rounding after the apply was accepted")
	}
	if got := preparedValue(t, f, b, "Calories", "2031-04-11", "reported-calories"); got != 1789 || importerMeasurementRows(t, f) != 8 {
		t.Fatalf("a refused rounding changed the readings: calories %v, rows %d", got, importerMeasurementRows(t, f))
	}
}

func TestPreparedWithoutRoundKeepsTheSourceValue(t *testing.T) {
	f := setupRound(t)
	b, err := f.w.DraftPreparedJSON(ctx, "daily.csv", "fit-date-csv-v1", "", "", roundMetrics, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Round != nil {
		t.Fatalf("round = %v, want none", b.Round)
	}
	body, err := os.ReadFile(f.w.file(preparedFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"round"`)) {
		t.Fatal("a batch without rounding names round in prepared.md")
	}
	if err = ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	if got := preparedValue(t, f, b, "Calories", "2031-04-11", "reported-calories"); got != 1789.3456789012 {
		t.Fatalf("calories = %v, want the source value 1789.3456789012", got)
	}
}

func TestPreparedRoundRefusesWhatIsNotAQuantityOnce(t *testing.T) {
	f := setupRound(t)
	for _, round := range []string{
		`["calories"]`,                  // not a quantity code of the file
		`["distance","distance"]`,       // a code twice
		`"distance"`,                    // not a list
		`["distance",1]`,                // not a list of codes
		`["distance"] ["steps"]`,        // two values
		`{"distance":true}`,             // an object
		`["distance","mean-heart-rate"`, // not JSON
	} {
		if _, err := f.w.DraftPreparedJSON(ctx, "daily.csv", "fit-date-csv-v1", "", "", roundMetrics, round); err == nil {
			t.Errorf("round %s was accepted", round)
		}
		if _, err := os.Stat(f.w.file(preparedFile)); !os.IsNotExist(err) {
			t.Fatalf("round %s: a refused draft wrote prepared.md", round)
		}
	}
}
