package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestIconsAreDeterministicAndHaveTheirSize(t *testing.T) {
	a, b := Icons(), Icons()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two calls of Icons draw different pixels")
	}
	if got, want := a.Bounds(), image.Rect(0, 0, 240, 16); got != want {
		t.Errorf("bounds %v, want %v", got, want)
	}
}

// Every glyph is 10 by 10 characters, has ink, and the sheet has one colour only.
func TestEachIconIsATenByTenGlyphInOneColour(t *testing.T) {
	img := Icons()
	for cell, name := range IconNames {
		d, ok := glyphs[name]
		if !ok || len(d) != IconSize {
			t.Fatalf("%s: %d rows, want %d", name, len(d), IconSize)
		}
		ink := 0
		for y, row := range d {
			if len(row) != IconSize {
				t.Errorf("%s row %d is %d wide, want %d", name, y, len(row), IconSize)
			}
			for x := 0; x < len(row); x++ {
				got := img.NRGBAAt(cell*CellSize+x, y)
				if row[x] == 'X' {
					ink++
					if got != iconInk {
						t.Errorf("%s (%d,%d) is %v, want the ink", name, x, y, got)
					}
				} else if got != (color.NRGBA{}) {
					t.Errorf("%s (%d,%d) is %v, want empty", name, x, y, got)
				}
			}
		}
		if ink == 0 {
			t.Errorf("%s has no ink", name)
		}
	}
	if len(glyphs) != len(IconNames) {
		t.Errorf("%d drawings for %d names", len(glyphs), len(IconNames))
	}
}

func TestIconCellIsTheIndexOfTheName(t *testing.T) {
	for want, name := range []string{"today", "calendar", "browse", "people", "places", "files", "metrics", "search",
		"new", "more", "image", "video", "audio", "week", "plans"} {
		if got, ok := IconCell(name); !ok || got != want {
			t.Errorf("IconCell(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	if _, ok := IconCell("nope"); ok {
		t.Error("IconCell answers for a name that is not a glyph")
	}
	// Known pixels, read by position as app.css reads them: the cross of "new" at its centre, the dots of "more".
	img := Icons()
	new, _ := IconCell("new")
	more, _ := IconCell("more")
	if img.NRGBAAt(new*CellSize+4, 0) != iconInk || img.NRGBAAt(new*CellSize+3, 0).A != 0 {
		t.Error("the cross of new is not where its drawing puts it")
	}
	if img.NRGBAAt(more*CellSize+9, 5) != iconInk || img.NRGBAAt(more*CellSize+2, 5).A != 0 {
		t.Error("the dots of more are not where their drawing puts them")
	}
	// The tick of plans: its lowest point is at (3,4), and (3,3) is empty.
	plans, _ := IconCell("plans")
	if img.NRGBAAt(plans*CellSize+3, 4) != iconInk || img.NRGBAAt(plans*CellSize+3, 3).A != 0 {
		t.Error("the tick of plans is not where its drawing puts it")
	}
}
