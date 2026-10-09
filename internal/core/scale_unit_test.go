package core

import "testing"

// A scale names its range as its unit (metrics.unit), so every metric whose unit is a range is held to whole
// numbers inside it, on a first reading and on a correction alike, not only the seeded Mood (D6, D7).
func TestAnyRangeUnitHoldsItsValuesToTheRange(t *testing.T) {
	s := fresh(t)
	for _, m := range []struct{ name, unit string }{{"Pain", "0-10"}, {"Grade", "1-100"}} {
		if _, err := s.RegisterMetric(ctx, "cli", m.name, m.unit, ""); err != nil {
			t.Fatal(err)
		}
	}
	record := func(metric string, v float64) (int64, error) {
		return s.Record(ctx, "cli", Reading{Metric: metric, Day: "2026-09-01", Value: v})
	}
	for _, c := range []struct {
		metric string
		value  float64
		ok     bool
	}{
		{"Pain", 0, true}, {"Pain", 10, true}, {"Pain", 11, false}, {"Pain", -1, false}, {"Pain", 5.5, false},
		{"Grade", 1, true}, {"Grade", 100, true}, {"Grade", 0, false}, {"Grade", 101, false},
		{"Mood", 5, true}, {"Mood", 6, false},
	} {
		_, err := record(c.metric, c.value)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s %g: %v, want it recorded", c.metric, c.value, err)
		case !c.ok && status(err) != 422:
			t.Errorf("%s %g: %v, want a 422", c.metric, c.value, err)
		}
	}
	// a correction is held to the range too, and a retraction (no value) is not a value
	id, err := record("Pain", 3)
	if err != nil {
		t.Fatal(err)
	}
	eleven := 11.0
	if _, err := s.Correct(ctx, "cli", id, &eleven); status(err) != 422 {
		t.Errorf("a correction of Pain to 11: %v, want a 422", err)
	}
	four := 4.0
	if _, err := s.Correct(ctx, "cli", id, &four); err != nil {
		t.Errorf("a correction of Pain to 4: %v", err)
	}
	// a metric without a range unit takes any finite number
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := record("Weight", 70.5); err != nil {
		t.Errorf("Weight 70.5 kg: %v", err)
	}
}

// Registering a metric whose title is already a scale, with the scale's own range or with no unit, takes the
// existing metric: a source that names Mood does not write its range, so a unitless request cannot be a conflict.
// A different unit is one, since a unit never changes (metrics_unit_fixed).
func TestRegisteringAnExistingScaleAdoptsIt(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Pain", "0-10", "how much it hurts"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, unit string
		want       int // 0: adopted
	}{
		{"Mood", "", 0}, {"mood", "1-5", 0}, {"MOOD", "", 0}, {"Mood", "kg", 409}, {"Mood", "0-10", 409}, {"Mood", "1-10", 409},
		{"pain", "", 0}, {"Pain", "0-10", 0}, {"Pain", "1-5", 409}, {"Pain", "mg", 409},
	} {
		added, err := s.RegisterMetric(ctx, "cli", c.name, c.unit, "ignored note")
		if status(err) != c.want || added {
			t.Errorf("register %q with unit %q: added %v, %v; want added false and status %d", c.name, c.unit, added, err, c.want)
		}
	}
	// the adopted metric is untouched: same unit, same page text
	var unit, body string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT m.unit, e.body FROM metrics m JOIN entities e ON e.id = m.id WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = 'pain')`).Scan(&unit, &body); err != nil {
		t.Fatal(err)
	}
	if unit != "0-10" || body != "how much it hurts" {
		t.Errorf("Pain after the adoptions: unit %q, body %q", unit, body)
	}
	// only a range is adopted: a physical unit, or none, stays what it was
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "", ""); status(err) != 409 {
		t.Errorf("Weight (kg) registered with no unit: %v, want a 409", err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "Steps", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "Steps", "0-10", ""); status(err) != 409 {
		t.Errorf("a unitless metric registered as a scale: %v, want a 409", err)
	}
}

func TestRangeUnit(t *testing.T) {
	for _, c := range []struct {
		unit   string
		lo, hi float64
		ok     bool
	}{
		{"1-5", 1, 5, true}, {"0-10", 0, 10, true}, {"1-100", 1, 100, true}, {"01-05", 1, 5, true}, {"0-1", 0, 1, true},
		{"0-100000000000000000000", 0, 1e20, true}, {"100000000000000000000-100000000000000000001", 1e20, 1e20, true},
		{"", 0, 0, false}, {"1-1", 0, 0, false}, {"5-1", 0, 0, false}, {"0-0", 0, 0, false}, {"1-", 0, 0, false},
		{"-5", 0, 0, false}, {"-", 0, 0, false}, {"1-5-7", 0, 0, false}, {"1--5", 0, 0, false}, {"1 -5", 0, 0, false},
		{"1-5 ", 0, 0, false}, {"1–5", 0, 0, false}, {"+1-5", 0, 0, false}, {"1.5-5", 0, 0, false}, {"a-b", 0, 0, false},
		{"١-٥", 0, 0, false}, {"kg", 0, 0, false}, {"mg/dL", 0, 0, false}, {"1/5", 0, 0, false}, {"2024-2025", 2024, 2025, true},
	} {
		lo, hi, ok := RangeUnit(c.unit)
		if ok != c.ok || ok && (lo != c.lo || hi != c.hi) {
			t.Errorf("RangeUnit(%q) = %g, %g, %v; want %g, %g, %v", c.unit, lo, hi, ok, c.lo, c.hi, c.ok)
		}
	}
}
