package api

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// row is a row of /places as the view reads it: JSON numbers are float64s.
func row(id float64, title string, mentions, lat, lon, radius float64) map[string]any {
	return map[string]any{"id": id, "title": title, "mentions": mentions,
		"point": map[string]any{"lat": lat, "lon": lon, "radius_m": radius, "link_days": true}}
}

func viewBox(t *testing.T, svg string) (x, y, w, h float64) {
	t.Helper()
	i := strings.Index(svg, `viewBox="`)
	if i < 0 {
		t.Fatalf("no viewBox in %.200s", svg)
	}
	if _, err := fmt.Sscanf(svg[i+len(`viewBox="`):], "%g %g %g %g", &x, &y, &w, &h); err != nil {
		t.Fatal(err)
	}
	return
}

// TestPlaceMapHoldsEveryPointAndCircle: the view holds each point with its whole circle, in units of a hundredth of a
// degree with the longitude scaled by the cosine of the middle latitude, and it is twice as wide as it is high.
func TestPlaceMapHoldsEveryPointAndCircle(t *testing.T) {
	// Lisbon and Bucharest, with circles of 10 and 12 km; the middle latitude is 41.575°, its cosine 0.74809.
	svg := string(placeMap([]any{row(1, "Lisbon", 0, 38.72, -9.14, 10000), row(2, "Bucharest", 0, 44.43, 26.10, 12000)}))
	x, y, w, h := viewBox(t, svg)
	if math.Abs(w/h-2) > 1e-3 {
		t.Errorf("the view is %g by %g, want twice as wide as high", w, h)
	}
	k := 0.74809
	for _, p := range []struct{ lat, lon, r float64 }{{38.72, -9.14, 10000 / 1113.2}, {44.43, 26.10, 12000 / 1113.2}} {
		px, py := p.lon*100*k, -p.lat*100
		if px-p.r < x || px+p.r > x+w || py-p.r < y || py+p.r > y+h {
			t.Errorf("the circle at %g, %g is not in the view %g %g %g %g", p.lat, p.lon, x, y, w, h)
		}
	}
	if !strings.Contains(svg, `scale(0.7480`) {
		t.Errorf("the outline is not scaled by the cosine of 41.575°: %.300s", svg)
	}

	// A lone place: the view is never less than 5 units high, about 5.5 km.
	_, _, w, h = viewBox(t, string(placeMap([]any{row(3, "Café", 0, 46.77, 23.59, 50)})))
	if math.Abs(h-5) > 1e-6 || math.Abs(w-10) > 1e-6 {
		t.Errorf("a lone place's view is %g by %g, want 10 by 5", w, h)
	}
	if placeMap([]any{map[string]any{"id": 4.0, "title": "Nowhere"}}) != "" || placeMap(nil) != "" {
		t.Errorf("a list with no point draws a map")
	}
}

// TestPlaceMapLeavesOutANameThatWouldCoverAnother: of two places at almost one point, the more mentioned one has its
// name written, and the other has only its dot and its title. A title is escaped.
func TestPlaceMapLeavesOutANameThatWouldCoverAnother(t *testing.T) {
	svg := string(placeMap([]any{
		row(1, "A <quiet> café", 1, 46.7700, 23.5900, 50),
		row(2, "Library", 9, 46.7701, 23.5901, 50),
		row(3, "Far", 0, 46.8000, 23.6500, 50),
	}))
	for _, s := range []string{
		`<title>A &lt;quiet&gt; café</title>`, `<title>Library</title>`, `>Library</text>`, `>Far</text>`,
		`href="/pages/1"`, `href="/pages/2"`, `href="/pages/3"`,
	} {
		if !strings.Contains(svg, s) {
			t.Errorf("the map lacks %q", s)
		}
	}
	if strings.Contains(svg, `café</text>`) {
		t.Errorf("the less mentioned name is written over the other")
	}
	// Far is east of the middle: its name ends at its dot, so it stays in the view.
	if !strings.Contains(svg, `text-anchor="end">Far</text>`) {
		t.Errorf("the name of the place in the east is not on the left of its dot")
	}
}
