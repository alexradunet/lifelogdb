package api

import (
	"context"
	"strings"

	"lifelog/internal/core"
)

// metricGroup is one section of /metrics: the habits (derived from their periods, D24), a category page with the
// metrics filed in it (D26), or the metrics filed nowhere. Depth is the category's depth in the drawn tree.
type metricGroup struct {
	Title   string        `json:"title"`
	Path    string        `json:"path,omitempty"`
	Page    int64         `json:"page,omitempty"`
	Depth   int           `json:"depth"`
	Metrics []core.Metric `json:"metrics"`
}

// groupMetrics lists a habit under Habits and any other metric under each category it is filed in, in the order of
// the tree; the rest last. A category is shown when it or a category under it holds a metric.
func groupMetrics(ms []core.Metric, cs []core.Category) []metricGroup {
	habits := metricGroup{Title: "Habits"}
	filed := map[string][]core.Metric{}
	var none []core.Metric
	for _, m := range ms {
		switch {
		case m.Habit:
			habits.Metrics = append(habits.Metrics, m)
		case len(m.Categories) > 0:
			for _, p := range m.Categories {
				filed[p] = append(filed[p], m)
			}
		default:
			none = append(none, m)
		}
	}
	var out []metricGroup
	if len(habits.Metrics) > 0 {
		out = append(out, habits)
	}
	for _, c := range cs { // by path: a category comes before the categories under it
		held := false
		for p := range filed {
			if p == c.Path || strings.HasPrefix(p, c.Path+"/") {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		out = append(out, metricGroup{Title: c.Title, Path: c.Path, Page: c.ID, Depth: strings.Count(c.Path, "/"),
			Metrics: append([]core.Metric{}, filed[c.Path]...)})
	}
	if len(none) > 0 {
		out = append(out, metricGroup{Title: "Not filed", Metrics: none})
	}
	return out
}

// metricOf is the live metric that name names, by its name or by an alias its renames kept (D27, as a series is
// read); nil when it names none.
func (h *server) metricOf(ctx context.Context, name string) (*core.Metric, error) {
	id, err := h.s.PageID(ctx, name)
	if err != nil || id == 0 {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, nil
}
