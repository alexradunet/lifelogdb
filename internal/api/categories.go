package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"lifelog/internal/core"
)

// metricGroup is one section of /metrics: the habits (derived from their periods, D24), a category with the
// metrics filed in it (D26), or the metrics filed nowhere. Depth is the category's depth in the tree.
type metricGroup struct {
	Title   string        `json:"title"`
	Path    string        `json:"path,omitempty"`
	Depth   int           `json:"depth"`
	Metrics []core.Metric `json:"metrics"`
}

// groupMetrics lists each metric once: a habit under Habits, any other in its category, in the order of the
// tree, the rest last. A category is shown when it or a category under it holds a metric.
func groupMetrics(ms []core.Metric, cs []core.Category) []metricGroup {
	habits := metricGroup{Title: "Habits"}
	filed := map[string][]core.Metric{}
	var none []core.Metric
	for _, m := range ms {
		switch {
		case m.Habit:
			habits.Metrics = append(habits.Metrics, m)
		case m.Category != "":
			filed[m.Category] = append(filed[m.Category], m)
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
		names := strings.Split(c.Path, "/")
		out = append(out, metricGroup{Title: strings.ReplaceAll(names[len(names)-1], "_", " "), Path: c.Path,
			Depth: len(names) - 1, Metrics: append([]core.Metric{}, filed[c.Path]...)})
	}
	if len(none) > 0 {
		out = append(out, metricGroup{Title: "Not filed", Metrics: none})
	}
	return out
}

func (h *server) registerCategory(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "path"); err != nil {
		return nil, err
	}
	if _, err := h.s.RegisterCategory(r.Context(), src, v.Get("path"), v.Get("note")); err != nil {
		return nil, err
	}
	return h.metrics(r)
}

func (h *server) fileMetric(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if _, err := h.s.FileMetric(r.Context(), src, r.PathValue("name"), v.Get("category")); err != nil {
		return nil, err
	}
	return h.metricEntity(r, r.PathValue("name"))
}

// fileAction offers the registered categories to file metric in, with the one it is in now.
func (h *server) fileAction(ctx context.Context, metric, now string) Action {
	a := action("file-metric", map[string]string{"name": url.PathEscape(metric)}, map[string]any{"category": now})
	if cs, err := h.s.Categories(ctx); err == nil {
		paths := []string{""}
		for _, c := range cs {
			paths = append(paths, c.Path)
		}
		a = withOptions(a, "category", paths)
	}
	return a
}

// category is the path of the category metric is filed in, "" for none.
func (h *server) category(ctx context.Context, metric string) string {
	ms, _ := h.s.Metrics(ctx)
	for _, m := range ms {
		if m.Name == metric {
			return m.Category
		}
	}
	return ""
}
