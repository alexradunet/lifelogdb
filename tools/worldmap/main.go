// worldmap writes the outline of the world that the places map draws (internal/api/html/world.md): the land and the
// borders of Natural Earth, from its GeoJSON files, as one SVG. A unit is a hundredth of a degree: x is the longitude
// and y the latitude negated, so north is up. The places map uses the same units. The paths have no colour: the
// page that draws them gives it, so they follow the light and the dark theme. It is run by hand, not by
// `go generate`, because the source files are not in git.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
)

// collection is the part of a GeoJSON FeatureCollection that the outline needs.
type collection struct {
	Features []struct {
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: worldmap LAND.geojson BORDERS.geojson OUT.svg")
	}
	land, err := paths(os.Args[1], true)
	if err != nil {
		log.Fatal(err)
	}
	borders, err := paths(os.Args[2], false)
	if err != nil {
		log.Fatal(err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-18000 -9000 36000 18000">` +
		`<!-- Made with Natural Earth (public domain), version 5.1.2, 1:50m. See world.md. -->` +
		`<path id="land" vector-effect="non-scaling-stroke" d="` + land + `"/>` +
		`<path id="borders" vector-effect="non-scaling-stroke" d="` + borders + `"/></svg>` + "\n"
	if err := os.WriteFile(os.Args[3], []byte(svg), 0o644); err != nil {
		log.Fatal(err)
	}
}

// paths reads a GeoJSON file and gives its lines as one path: each ring closed when closed is true.
func paths(name string, closed bool) (string, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	var c collection
	if err := json.Unmarshal(b, &c); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	var d strings.Builder
	for _, f := range c.Features {
		var lines [][][]float64
		switch f.Geometry.Type {
		case "LineString":
			var l [][]float64
			err = json.Unmarshal(f.Geometry.Coordinates, &l)
			lines = [][][]float64{l}
		case "MultiLineString", "Polygon":
			err = json.Unmarshal(f.Geometry.Coordinates, &lines)
		case "MultiPolygon":
			var polys [][][][]float64
			err = json.Unmarshal(f.Geometry.Coordinates, &polys)
			for _, p := range polys {
				lines = append(lines, p...)
			}
		default:
			return "", fmt.Errorf("%s: geometry %q", name, f.Geometry.Type)
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		for _, l := range lines {
			line(&d, l, closed)
		}
	}
	return d.String(), nil
}

// line writes one line in relative steps, without the steps that round to no move. A line that rounds to one
// point is not written: "l" with no pair is an error that stops the path.
func line(d *strings.Builder, l [][]float64, closed bool) {
	if len(l) < 2 {
		return
	}
	var steps strings.Builder
	x, y := unit(l[0][0]), unit(-l[0][1])
	for _, p := range l[1:] {
		nx, ny := unit(p[0]), unit(-p[1])
		if nx == x && ny == y {
			continue
		}
		fmt.Fprintf(&steps, " %d %d", nx-x, ny-y)
		x, y = nx, ny
	}
	if steps.Len() == 0 {
		return
	}
	fmt.Fprintf(d, "M%d %dl%s", unit(l[0][0]), unit(-l[0][1]), steps.String()) // each pair after "l" is a relative line to
	if closed {
		d.WriteString("z")
	}
}

func unit(deg float64) int { return int(math.Round(deg * 100)) }
