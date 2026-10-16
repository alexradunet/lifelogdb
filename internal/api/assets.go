package api

// The routes that only a browser asks for: the embedded fonts, the manifest and its icon (webFiles), and the style
// page.

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// fontTypes are the files the font route serves, by their extension: the faces and their licences.
var fontTypes = map[string]string{"woff2": "font/woff2", "txt": "text/plain; charset=utf-8"}

// fonts is GET /assets/fonts/{file}: an embedded face or licence, by its exact name (html/fonts/README.md). It is
// presentation, like /render, and not in the catalog. A file never changes under its name, so a browser keeps it
// for a year.
func fonts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ := fontTypes[name[strings.LastIndex(name, ".")+1:]]
	if typ == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := webFiles.ReadFile("html/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// styleguide is GET /styleguide: every component of the Almanac in every state, drawn by the real templates on
// synthetic words (html/styleguide.html). It is presentation, like /render, and not in the catalog. Its entity is
// styleEntity, which reads nothing, so the page shows nothing of the database; only the shell around it reads, as
// for any page.
func (h *server) styleguide(w http.ResponseWriter, r *http.Request) {
	answer(w, r, http.StatusOK, styleEntity(), frame{shell: h.s, pageOnly: true})
}

// styleEntity is the entity of the style page. Its properties are synthetic, each part in the shape of the resource
// whose template draws it: a day, a month, the metrics, a series and the facts of a page. Its actions are actions of
// the catalog that send to the style page itself by GET, so a form of the page changes nothing.
func styleEntity() *Entity {
	reading := func(id float64, day string, value float64) map[string]any {
		return map[string]any{"id": id, "day": day, "value": value}
	}
	var days []any // October 2026, with three days written
	for d := 1; d <= 31; d++ {
		days = append(days, map[string]any{"day": fmt.Sprintf("2026-10-%02d", d), "page": d == 1 || d == 5 || d == 12})
	}
	timed := reading(34, "2026-10-12", 71)
	timed["taken_at"] = "2026-10-12T06:48:00.000Z"
	props := map[string]any{
		"today": "2026-10-13",
		"day": map[string]any{"day": "2026-10-12", "mood": []any{4.0},
			"journal": "### A walk in Lisbon\n\nI walked with [[Sam]] along the river. It was warm. #walk The weight was `71.0` kg.\n\n" +
				"- Coffee at the corner\n- The old tram\n- Dinner at eight\n",
			"on_this_day": []any{
				map[string]any{"day": "2025-10-12", "first_line": "A walk with Sam", "mood": []any{4.0}},
				map[string]any{"day": "2024-10-12", "first_line": "The first rain of the autumn", "mood": []any{3.0}},
			},
			"readings": []any{
				map[string]any{"id": 1.0, "metric": "weight", "value": 71.0},
				map[string]any{"id": 2.0, "metric": "steps", "value": 8200.0},
				map[string]any{"id": 5.0, "metric": "steps", "value": 8400.0},
			},
			"trends": []any{
				map[string]any{"metric": "weight", "unit": "kg", "readings": []any{reading(0, "2026-10-06", 71.4), reading(0, "2026-10-08", 71.2),
					reading(0, "2026-10-10", 71.1), reading(0, "2026-10-12", 71)}},
				map[string]any{"metric": "steps", "unit": "", "readings": []any{reading(0, "2026-10-07", 6000), reading(0, "2026-10-09", 7400),
					reading(0, "2026-10-12", 8400)}},
			}},
		"month": map[string]any{"name": "October 2026", "written": 3.0, "days": days},
		"metrics": map[string]any{
			"groups": []any{map[string]any{"title": "Bloodwork", "path": "Bloodwork", "page": 7.0, "depth": 0.0,
				"metrics": []any{map[string]any{"id": 1.0}, map[string]any{"id": 2.0}, map[string]any{"id": 3.0}}}},
			"metrics": []any{
				map[string]any{"id": 1.0, "name": "Vitamin D", "unit": "ng/mL", "recent": []any{reading(11, "2026-07-01", 24),
					reading(12, "2026-08-01", 29), reading(13, "2026-09-01", 33.5), reading(14, "2026-10-01", 38)}},
				map[string]any{"id": 2.0, "name": "Ferritin", "unit": "ug/L", "note": "Iron stores", "recent": []any{reading(21, "2025-04-12", 49),
					reading(22, "2026-04-12", 85)}},
				map[string]any{"id": 3.0, "name": "TSH", "unit": "uIU/mL", "recent": []any{}},
			}},
		"series": map[string]any{"unit": "kg", "to": "2026-10-12", "readings": []any{reading(31, "2025-10-20", 72),
			reading(32, "2026-01-15", 70), reading(33, "2026-05-10", 71.5), timed}},
		"facts": map[string]any{"title": "A walk in Lisbon", "version": 3.0, "created_at": "2026-10-12T06:48:00.000Z"},
	}
	at := func(name string, values map[string]any) Action {
		a := action(name, nil, values)
		a.Method, a.Href = "GET", "/styleguide"
		return a
	}
	return &Entity{Class: []string{"styleguide"}, Title: "The style page", Properties: props,
		Links: []Link{link("self", "/styleguide", "The style page"), link("prev", "/styleguide", "Previous"), link("next", "/styleguide", "Next")},
		Actions: []Action{at("capture", nil), at("save-body", map[string]any{"body": "A walk in Lisbon", "version": "3"}),
			at("record", map[string]any{"day": "2026-10-13"}), at("create-person", nil), at("browse", nil)}}
}

// styleParts are the parts of the style page that the methods of a real page's view give: the tiles of a day, the
// sections of /metrics and the folds of a series. Each comes from its part of the synthetic properties of
// styleEntity, on the clock of this view.
type styleParts struct {
	Tiles    []readingTile
	Sections []metricSection
	Series   metricReadings
}

// StyleParts are the parts of the style page that the methods of a real page's view give.
func (v *view) StyleParts() styleParts {
	props, _ := v.Props.(map[string]any)
	part := func(key string) *view {
		p, _ := props[key].(map[string]any)
		return &view{Entity: v.Entity, Shell: v.Shell, Props: p}
	}
	return styleParts{Tiles: part("day").ReadingTiles(), Sections: part("metrics").MetricSections(), Series: part("series").MetricReadings()}
}

// install serves an embedded file of html/ for a browser's install of the app: the manifest and its icon. The
// cache says how long a browser keeps it. The file is the same for every request, so it needs no read of the store.
func install(name, typ, cache string) http.HandlerFunc {
	b := mustWebFile(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	}
}
