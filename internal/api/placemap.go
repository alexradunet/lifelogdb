package api

import (
	"cmp"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// worldOutline is the land and the borders that the places map draws under its points (html/world.md). A changed
// outline gets a new name, because a browser keeps it for a year.
const worldOutline = "/assets/world-50m-v5.1.2.svg"

// The units of the places map are those of the outline: a hundredth of a degree, north up. A unit of latitude is
// metresPerUnit metres. The map scales the longitude by the cosine of its middle latitude, so a unit across is the
// same distance as a unit down, near the middle of the map.
const (
	metresPerUnit = 1113.2
	mapMinSpan    = 5.0   // about 5.5 km down: the map never shows less, so the places of one town are apart
	mapAspect     = 2.0   // the width of the map over its height
	mapWidthPx    = 720.0 // the width a dot and a word are sized for; the map scales to its column
)

// placeMap draws the places of a list that have a point as an inline SVG: the world outline, and a dot a place,
// each a link to its page with its title. A place's circle (radius_m) shows when it is larger than its dot. The view
// holds every point and every circle. It is empty when no place has a point. A circle is not drawn across the 180th
// meridian, as the place match does not match across it.
//
// A name is written beside its dot, on the side towards the middle, so it stays in the view. The places most
// mentioned get their names first; a name that would cover a name already written is left out, and its dot keeps
// the title that a browser shows on hover.
func placeMap(rows any) template.HTML {
	type place struct {
		id, title  string
		lat, lon   float64
		radiusUnit float64
		mentions   float64
	}
	list, _ := rows.([]any)
	var places []place
	for _, it := range list {
		m, _ := it.(map[string]any)
		pt, _ := m["point"].(map[string]any)
		lat, ok1 := pt["lat"].(float64)
		lon, ok2 := pt["lon"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		r, _ := pt["radius_m"].(float64)
		title, _ := m["title"].(string)
		mentions, _ := m["mentions"].(float64)
		places = append(places, place{num(m["id"]), title, lat, lon, r / metresPerUnit, mentions})
	}
	if len(places) == 0 {
		return ""
	}
	minLat, maxLat := places[0].lat, places[0].lat
	for _, p := range places {
		minLat, maxLat = min(minLat, p.lat), max(maxLat, p.lat)
	}
	k := math.Cos((minLat + maxLat) / 2 * math.Pi / 180)
	x := func(p place) float64 { return p.lon * 100 * k }
	y := func(p place) float64 { return -p.lat * 100 }

	x0, x1, y0, y1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, p := range places {
		x0, x1 = min(x0, x(p)-p.radiusUnit), max(x1, x(p)+p.radiusUnit)
		y0, y1 = min(y0, y(p)-p.radiusUnit), max(y1, y(p)+p.radiusUnit)
	}
	w, h := (x1-x0)*1.2, (y1-y0)*1.2 // a tenth of the span free on each side
	w, h = max(w, mapMinSpan*mapAspect), max(h, mapMinSpan)
	w, h = max(w, h*mapAspect), max(h, w/mapAspect)
	cx, cy := (x0+x1)/2, (y0+y1)/2
	px := w / mapWidthPx // a pixel, in units

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="place-map" viewBox="%.2f %.2f %.2f %.2f" aria-label="Map of %d places">`,
		cx-w/2, cy-h/2, w, h, len(places))
	fmt.Fprintf(&sb, `<g transform="scale(%.6f 1)"><use class="land" href="%s#land"/><use class="borders" href="%s#borders"/></g>`,
		k, worldOutline, worldOutline)
	dot := 5 * px
	for _, p := range places {
		if p.radiusUnit > dot {
			fmt.Fprintf(&sb, `<a href="/pages/%s"><title>%s</title><circle class="area" cx="%.2f" cy="%.2f" r="%.2f"/></a>`,
				p.id, template.HTMLEscapeString(p.title), x(p), y(p), p.radiusUnit)
		}
	}
	type box struct{ x0, y0, x1, y1 float64 }
	var written []box
	byMentions := slices.Clone(places)
	slices.SortStableFunc(byMentions, func(a, b place) int { return cmp.Compare(b.mentions, a.mentions) })
	for _, p := range byMentions {
		fmt.Fprintf(&sb, `<a href="/pages/%s"><title>%s</title><circle class="dot" cx="%.2f" cy="%.2f" r="%.2f"/>`,
			p.id, template.HTMLEscapeString(p.title), x(p), y(p), dot)
		// a letter of the 13 px face is about 7 px wide
		lw, tx, anchor := float64(utf8.RuneCountInString(p.title))*7*px, x(p)+dot*1.6, "start"
		b := box{tx, y(p) - 9*px, tx + lw, y(p) + 4*px}
		if x(p) > cx {
			tx, anchor = x(p)-dot*1.6, "end"
			b.x0, b.x1 = tx-lw, tx
		}
		free := true
		for _, o := range written {
			if b.x0 < o.x1 && o.x0 < b.x1 && b.y0 < o.y1 && o.y0 < b.y1 {
				free = false
				break
			}
		}
		if free {
			written = append(written, b)
			fmt.Fprintf(&sb, `<text x="%.2f" y="%.2f" font-size="%.2f" text-anchor="%s">%s</text>`, tx, y(p)+4*px, 13*px,
				anchor, template.HTMLEscapeString(p.title))
		}
		sb.WriteString(`</a>`)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
