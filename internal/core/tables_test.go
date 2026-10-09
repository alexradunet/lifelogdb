package core

import (
	"context"
	"strings"
	"testing"
)

// tableValues reads back the readings readings-from-table wrote for a metric: day and value, in day order.
func tableValues(t *testing.T, s *Store, metric string) map[string]float64 {
	t.Helper()
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT me.day, me.value FROM measurement_values me
		WHERE me.source = ? AND me.metric_id = (SELECT entity_id FROM entity_names WHERE name_key = lower(?))`, TableSource, metric)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var day string
		var v float64
		if err := rows.Scan(&day, &v); err != nil {
			t.Fatal(err)
		}
		out[day] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReadingsFromTableWritesEachPlainRowOnce(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	if _, err := s.RegisterMetric(ctx, "cli", "Ferritin", "ng/mL", ""); err != nil {
		t.Fatal(err)
	}
	body := "# Ferritin\n\nIron stores.\n\n| date | value |\n|---|---|\n" +
		"| 2031-03-01 | 48 ng/mL |\n" + // the unit in the cell
		"| 2031-04-01 | 52.5 ng/mL |\n" +
		"| 2031-05-01 | <5 ng/mL |\n" + // a sign: kept as text
		"| 2031-06-01 | normal |\n" + // a word
		"| 2031-07-01 | 4,5 ng/mL |\n" + // a comma decimal
		"| 2031-08-01 | 40 µg/L |\n" + // another unit: never converted
		"| 2031-04-01 | 53 ng/mL |\n" + // a second value of one day
		"| early 2031 | 41 ng/mL |\n" + // no day
		"| 2031-09-01 |  |\n" // no value: nothing
	if _, _, err := s.CreatePage(ctx, "cli", "Ferritin results", body); err != nil {
		t.Fatal(err)
	}
	res, err := s.ReadingsFromTable(ctx, "Ferritin results", "Ferritin", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 2 || res.Existing != 0 || len(res.Reported) != 6 {
		t.Fatalf("result %+v", res)
	}
	want := map[string]string{
		"2031-05-01": "not a plain number", "2031-06-01": "not a plain number", "2031-07-01": "not a plain number",
		"2031-08-01": `the unit "µg/L" is not the metric's unit "ng/mL"`, "2031-04-01": "a second value for this day",
		"early 2031": "no day",
	}
	for _, r := range res.Reported {
		if !strings.HasPrefix(r.Reason, want[r.Day]) || want[r.Day] == "" {
			t.Errorf("row %d (%s, %q): %q", r.Row, r.Day, r.Cell, r.Reason)
		}
	}
	if got := tableValues(t, s, "Ferritin"); len(got) != 2 || got["2031-03-01"] != 48 || got["2031-04-01"] != 52.5 {
		t.Errorf("readings %v", got)
	}
	again, err := s.ReadingsFromTable(ctx, "Ferritin results", "Ferritin", "")
	if err != nil || again.Written != 0 || again.Existing != 2 {
		t.Fatalf("a second run: %+v, %v", again, err)
	}
	if got := tableValues(t, s, "Ferritin"); len(got) != 2 {
		t.Errorf("a second run changed the readings: %v", got)
	}
}

func TestReadingsFromTableTakesTheUnitFromTheHeaderOrAUnitColumn(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	if _, err := s.RegisterMetric(ctx, "cli", "Hemoglobin", "g/dL", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "ESR", "mm/h", ""); err != nil {
		t.Fatal(err)
	}
	header := "| Day | Hemoglobin (g/dL) | Other [mmol/L] |\n|---|---|---|\n| 2031-03-01 | 14.2 | 5 |\n| 2031-03-02 | 13 | 6 |\n"
	unitCol := "| when | ESR | unit |\n|---|---|---|\n| 2031-03-01 | 12 | mm/h |\n| 2031-03-02 | 9 | mm/hr |\n"
	for title, body := range map[string]string{"Blood count": header, "ESR results": unitCol} {
		if _, _, err := s.CreatePage(ctx, "cli", title, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReadingsFromTable(ctx, "Blood count", "Hemoglobin", ""); err == nil || !strings.Contains(err.Error(), "2 value columns") {
		t.Errorf("two value columns and no column named: %v", err)
	}
	res, err := s.ReadingsFromTable(ctx, "Blood count", "Hemoglobin", "hemoglobin")
	if err != nil || res.Written != 2 || len(res.Reported) != 0 || res.Column != "Hemoglobin (g/dL)" {
		t.Fatalf("the unit in the header: %+v, %v", res, err)
	}
	res, err = s.ReadingsFromTable(ctx, "ESR results", "ESR", "")
	if err != nil || res.Written != 1 || len(res.Reported) != 1 || !strings.Contains(res.Reported[0].Reason, `"mm/hr"`) {
		t.Fatalf("a unit column: %+v, %v", res, err)
	}
	if got := tableValues(t, s, "Hemoglobin"); got["2031-03-01"] != 14.2 || got["2031-03-02"] != 13 {
		t.Errorf("hemoglobin %v", got)
	}
}

func TestReadingsFromTableRefusesWhatItCannotRead(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "No table", "Only words.\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Bare numbers", "| date | weight |\n|---|---|\n| 2031-01-01 | 70 |\n"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ page, metric, column, want string }{
		{"Missing page", "Weight", "", "no page"},
		{"No table", "Weight", "", "no table with a column of days"},
		{"Bare numbers", "Height", "", "no metric"},
		{"Bare numbers", "Weight", "mass", `no column "mass"`},
	} {
		if _, err := s.ReadingsFromTable(ctx, c.page, c.metric, c.column); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s / %s: %v, want %q", c.page, c.metric, err, c.want)
		}
	}
	// a number with no unit anywhere, for a metric with a unit: reported, never assumed
	res, err := s.ReadingsFromTable(ctx, "Bare numbers", "Weight", "")
	if err != nil || res.Written != 0 || len(res.Reported) != 1 || !strings.HasPrefix(res.Reported[0].Reason, "no unit") {
		t.Fatalf("no unit: %+v, %v", res, err)
	}
}

func TestHasTable(t *testing.T) {
	for body, want := range map[string]bool{
		"| a | b |\n|---|---|\n| 1 | 2 |\n": true,
		"a | b, not a table\n":              false,
		"no pipes at all\n":                 false,
	} {
		if got := HasTable(body); got != want {
			t.Errorf("HasTable(%q) = %v", body, got)
		}
	}
}
