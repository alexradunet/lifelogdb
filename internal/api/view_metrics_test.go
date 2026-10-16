package api

import (
	"testing"
	"time"
)

// TestMetricSectionsSayDaysByTheClockOfTheView: the cards of /metrics say a day, and whether a card is stale, against
// the today of the view's clock (the shell's), not against the property today of the resource. Here the property
// says 1 January 2026 and the shell 16 October 2026: a reading of 14 October is "2 days ago", and one of 1 July is
// stale (older than 60 days).
func TestMetricSectionsSayDaysByTheClockOfTheView(t *testing.T) {
	metric := func(id float64, name, day string) any {
		return map[string]any{"id": id, "name": name, "unit": "kg", "recent": []any{map[string]any{"id": id * 10, "day": day, "value": 70.0}}}
	}
	v := &view{Shell: &shellView{Today: "2026-10-16", Zone: time.UTC}, Props: map[string]any{"today": "2026-01-01",
		"groups": []any{map[string]any{"title": "Not filed", "depth": 0.0, "metrics": []any{metric(1, "weight", "2026-10-14"), metric(2, "ferritin", "2026-07-01")}}}}}
	s := v.MetricSections()
	if len(s) != 1 || len(s[0].Cards) != 2 {
		t.Fatalf("sections %+v, want one with two cards", s)
	}
	weight, ferritin := s[0].Cards[0], s[0].Cards[1]
	if weight.When != "2 days ago · Wed 14 Oct" || weight.Short != "2 days ago" || weight.Stale {
		t.Errorf("weight: when %q, short %q, stale %v; want 2 days ago · Wed 14 Oct, 2 days ago, not stale", weight.When, weight.Short, weight.Stale)
	}
	if ferritin.When != "Wed 1 Jul" || !ferritin.Stale {
		t.Errorf("ferritin: when %q, stale %v; want Wed 1 Jul, stale", ferritin.When, ferritin.Stale)
	}
	if s[0].Latest != "2 days ago · Wed 14 Oct" {
		t.Errorf("the section's newest day is %q, want 2 days ago · Wed 14 Oct", s[0].Latest)
	}
}
