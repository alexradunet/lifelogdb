package api

import (
	"context"
	"time"

	"lifelog/internal/core"
)

// glanceDays is how many days, the day itself the last, a reading's trend covers.
const glanceDays = 14

// trend is a metric read on a day: its unit, and its current unassociated readings of the glanceDays days through
// the day, oldest first (the series of the cookbook).
type trend struct {
	Metric   string       `json:"metric"`
	Unit     string       `json:"unit"`
	Readings []trendPoint `json:"readings"`
}

// trendPoint is one reading of a trend.
type trendPoint struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// trends are the trends of the metrics with a current reading among a day's readings, in the order of those
// readings.
func (h *server) trends(ctx context.Context, day string, readings []core.Reading) ([]trend, error) {
	out := []trend{}
	seen := map[string]bool{}
	var names []string
	for _, r := range readings {
		if !seen[r.Metric] {
			seen[r.Metric] = true
			names = append(names, r.Metric)
		}
	}
	if len(names) == 0 {
		return out, nil
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	units := map[string]string{}
	for _, m := range ms {
		units[m.Name] = m.Unit
	}
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, err
	}
	from := t.AddDate(0, 0, -glanceDays).Format(time.DateOnly) // a series is (from, to]
	if !core.IsDay(from) {
		from = "0000-01-01" // the first days a date can name: the trend starts the day after
	}
	for _, name := range names {
		rows, err := h.s.Series(ctx, name, from, day)
		if err != nil {
			return nil, err
		}
		tr := trend{Metric: name, Unit: units[name], Readings: []trendPoint{}}
		for _, r := range rows {
			tr.Readings = append(tr.Readings, trendPoint{Day: r.Day, Value: r.Value})
		}
		out = append(out, tr)
	}
	return out, nil
}
