package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestWoodIsDeterministic(t *testing.T) {
	a, b := Wood(), Wood()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two calls of Wood draw different pixels")
	}
}

func TestWoodHasTheSizeOfTheTile(t *testing.T) {
	if got, want := Wood().Bounds(), image.Rect(0, 0, 84, 160); got != want {
		t.Errorf("bounds %v, want %v", got, want)
	}
}

// The left edge of every board is a shadow line of the opacity 0.5 (127 of 255): no other mark reaches that column
// of the board, so the whole edge holds it. The tile also holds marks other than the seams.
func TestWoodHasSeamsAtTheBoardEdges(t *testing.T) {
	img := Wood()
	for bx := 0; bx < 84; bx += 21 {
		for y := range 160 {
			if got, want := img.NRGBAAt(bx, y), (color.NRGBA{0, 0, 0, 127}); got != want {
				t.Fatalf("pixel (%d,%d) is %v, want the shadow %v", bx, y, got, want)
			}
		}
	}
	marked := 0
	for y := range 160 {
		for x := range 84 {
			if img.NRGBAAt(x, y).A > 0 {
				marked++
			}
		}
	}
	if marked <= 4*160 {
		t.Errorf("%d pixels are drawn, want more than the four seams (%d)", marked, 4*160)
	}
}
