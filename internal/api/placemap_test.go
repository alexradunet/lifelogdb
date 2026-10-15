package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestPlacesListDrawsItsPointsOnAMap: a place with a point has it in its row of /places, and the sheet draws it on
// the map as a link to its page. A place with no point is in the list and not on the map. People have no map, and
// neither has a list where no place has a point.
func TestPlacesListDrawsItsPointsOnAMap(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	lake := must(c.Do(ctx, find(root, "create-place"), map[string]string{"title": "Lake"}))
	if body := page(t, h, "/places"); strings.Contains(body, `class="place-map"`) {
		t.Errorf("the places sheet draws a map with no point")
	}
	must(c.Do(ctx, find(lake, "locate"), map[string]string{"lat": "46.77", "lon": "23.59", "radius_m": "2000", "link_days": "1"}))
	nowhere := must(c.Do(ctx, find(root, "create-place"), map[string]string{"title": "Nowhere"}))

	rows, _ := props(must(c.Get(ctx, "/places")))["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want Lake and Nowhere", rows)
	}
	pt, _ := rows[0].(map[string]any)["point"].(map[string]any)
	if str(pt["lat"]) != "46.77" || str(pt["lon"]) != "23.59" || str(pt["radius_m"]) != "2000" || pt["link_days"] != true {
		t.Errorf("Lake's point = %v, want 46.77, 23.59, 2000 m, linked", pt)
	}
	if _, has := rows[1].(map[string]any)["point"]; has {
		t.Errorf("Nowhere has a point: %v", rows[1])
	}

	body := page(t, h, "/places")
	start := strings.Index(body, `<svg class="place-map"`)
	if start < 0 {
		t.Fatalf("the places sheet has no map")
	}
	svg := body[start : start+strings.Index(body[start:], "</svg>")]
	for _, s := range []string{
		`aria-label="Map of 1 places"`,
		`<use class="land" href="/assets/world-50m-v5.1.2.svg#land"/>`,
		`<a href="` + hrefOf(lake, "self") + `"><title>Lake</title>`,
		`class="area"`, // 2 km is larger than the dot on a map of 5.5 km
		`>Lake</text>`,
	} {
		if !strings.Contains(svg, s) {
			t.Errorf("the map lacks %q", s)
		}
	}
	if strings.Contains(svg, "Nowhere") || !strings.Contains(body, `href="`+hrefOf(nowhere, "self")+`"`) {
		t.Errorf("Nowhere is on the map, or not in the list")
	}
	if body := page(t, h, "/people"); strings.Contains(body, `class="place-map"`) {
		t.Errorf("the people sheet draws a map")
	}
}

// TestTheWorldOutlineIsServed: the outline the map draws is an embedded SVG with the land and the borders, and a
// browser keeps it for a year.
func TestTheWorldOutlineIsServed(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	const path = "/assets/world-50m-v5.1.2.svg"
	r := request(h, "GET", path, "", nil)
	if r.Code != http.StatusOK || r.Header().Get("Content-Type") != "image/svg+xml" ||
		r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s: %d %q %q", path, r.Code, r.Header().Get("Content-Type"), r.Header().Get("Cache-Control"))
	}
	b := r.Body.String()
	if !strings.HasPrefix(b, "<svg ") || !strings.Contains(b, `<path id="land"`) || !strings.Contains(b, `<path id="borders"`) ||
		strings.Contains(b, "<script") {
		t.Errorf("the outline is not an SVG with the land and the borders and no script: %.120s", b)
	}
	if r := request(h, "POST", path, "", nil); r.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s: %d, want 405", path, r.Code)
	}
}
