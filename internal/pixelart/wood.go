// Package pixelart draws the small pictures that a gradient cannot be. It holds one drawing: the wood tile of the
// rail (docs/plans/137-pixel-wood-and-the-focus-pass.md). A drawing is deterministic: the same call gives the same
// pixels, so a file that holds it is checked by comparing pixels with the drawing.
package pixelart

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
)

// The tile is alpha shading only: shadow pixels and warm-light pixels over a transparent ground. The colour of the
// rail then shows through in both colour schemes.
const (
	woodWidth  = 84
	woodHeight = 160
	boardWidth = 21 // four boards across one tile
	grainLines = 3  // grain lines a board
	// Dither pixels a board: one in twenty of the board's area.
	ditherPerBoard = boardWidth * woodHeight * 5 / 100
)

var (
	shadow = color.NRGBA{0, 0, 0, 0}
	light  = color.NRGBA{255, 214, 150, 0}
)

// Wood is the tile of the rail: 84 by 160 pixels, four vertical boards, each with a staggered end and two nail
// heads, grain lines and sparse dither. The grain wanders with a period of the tile's height, so the tile wraps.
func Wood() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, woodWidth, woodHeight))
	rnd := rand.New(rand.NewPCG(3, 3))
	for bx := 0; bx < woodWidth; bx += boardWidth {
		seams(img, bx)
		for range grainLines {
			grain(img, rnd, bx)
		}
		for range ditherPerBoard {
			x := bx + 2 + rnd.IntN(boardWidth-3)
			y := rnd.IntN(woodHeight)
			if rnd.Float64() < .6 {
				put(img, x, y, shadow, .07)
			} else {
				put(img, x, y, light, .07)
			}
		}
		boardEnd(img, rnd, bx)
	}
	return img
}

// seams draws a shadow line at the board's left edge, a light line beside it and a soft shadow at its right.
func seams(img *image.NRGBA, bx int) {
	for y := range woodHeight {
		put(img, bx, y, shadow, .5)
		put(img, bx+1, y, light, .07)
		put(img, bx+boardWidth-1, y, shadow, .22)
	}
}

// grain draws one grain line that wanders by a sine, with a light pixel beside it now and then.
func grain(img *image.NRGBA, rnd *rand.Rand, bx int) {
	x0 := float64(bx + 3 + rnd.IntN(boardWidth-6))
	amp := 1 + 2*rnd.Float64()
	phase := 6.28 * rnd.Float64()
	k := float64(1 + rnd.IntN(2))
	inside := func(x int) bool { return bx+1 < x && x < bx+boardWidth-1 }
	for y := range woodHeight {
		x := int(math.Round(x0 + amp*math.Sin(2*math.Pi*k*float64(y)/woodHeight+phase)))
		if inside(x) {
			put(img, x, y, shadow, .16)
		}
		if rnd.Float64() < .08 && inside(x+1) {
			put(img, x+1, y, light, .05)
		}
	}
}

// boardEnd draws a dark line across the board at a staggered height, with a light line under it and two nail heads.
func boardEnd(img *image.NRGBA, rnd *rand.Rand, bx int) {
	ey := (bx*7 + rnd.IntN(woodHeight)) % woodHeight
	for x := bx + 1; x < bx+boardWidth; x++ {
		put(img, x, ey, shadow, .55)
		put(img, x, ey+1, light, .08)
	}
	for _, x := range []int{bx + 4, bx + boardWidth - 5} {
		put(img, x, ey+4, shadow, .6)
		put(img, x, ey+3, light, .18)
	}
}

// put draws a pixel of colour c with the opacity a, wrapped at the tile's edges. A stronger pixel wins over a
// weaker one; two pixels are never blended.
func put(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	x = (x%woodWidth + woodWidth) % woodWidth
	y = (y%woodHeight + woodHeight) % woodHeight
	alpha := uint8(a * 255)
	if alpha > img.NRGBAAt(x, y).A {
		c.A = alpha
		img.SetNRGBA(x, y, c)
	}
}
