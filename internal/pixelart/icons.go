package pixelart

import (
	"image"
	"image/color"
)

// IconSize is the width and the height of a glyph, in pixels. A glyph is at the top-left corner of its cell of
// CellSize by CellSize pixels.
const IconSize = 10

// IconNames are the names of the glyphs, in the order of the cells of the icon sheet: the name at index n is the
// cell n. The view reads this list, and app.css has one position for each name.
var IconNames = []string{"today", "calendar", "browse", "people", "places", "files", "metrics", "search", "new",
	"more", "image", "video", "audio", "week", "plans", "chat"}

// IconCell gives the cell of the glyph with this name. The second value is false for a name that is not a glyph.
func IconCell(name string) (int, bool) {
	for i, n := range IconNames {
		if n == name {
			return i, true
		}
	}
	return 0, false
}

// iconInk is the one colour of the icon sheet. The sheet is a mask: only its opaque pixels count, and the page
// gives them the colour of the text.
var iconInk = color.NRGBA{0, 0, 0, 0xff}

// glyphs are the drawings of the icons, by name. One character is one pixel: X is drawn, a dot is empty.
var glyphs = map[string]drawing{
	// a sun of eight rays
	"today": {
		"....X.....",
		"..X.X.X...",
		"...XXX....",
		".XX...XX..",
		"X.X...X.X.",
		".XX...XX..",
		"...XXX....",
		"..X.X.X...",
		"....X.....",
		"..........",
	},
	// a grid with a header row
	"calendar": {
		"XXXXXXXXX.",
		"X.X.X.X.X.",
		"XXXXXXXXX.",
		"X.......X.",
		"X.X.X.X.X.",
		"X.......X.",
		"X.X.X...X.",
		"X.......X.",
		"XXXXXXXXX.",
		"..........",
	},
	// a folder with a tab
	"browse": {
		"XXXX......",
		"X...X.....",
		"XXXXXXXXX.",
		"X.......X.",
		"X.......X.",
		"X.......X.",
		"X.......X.",
		"XXXXXXXXX.",
		"..........",
		"..........",
	},
	// a head over shoulders
	"people": {
		"...XXXX...",
		"..X....X..",
		"..X....X..",
		"...XXXX...",
		"..........",
		".XXXXXXXX.",
		"X........X",
		"X........X",
		"X........X",
		"XXXXXXXXXX",
	},
	// a drop with a hollow
	"places": {
		"...XXXX...",
		"..X....X..",
		".X..XX..X.",
		".X..XX..X.",
		"..X....X..",
		"..X....X..",
		"...X..X...",
		"....XX....",
		"....XX....",
		"..........",
	},
	// a page with a folded corner
	"files": {
		".XXXXX....",
		".X....X...",
		".X....XX..",
		".X.....X..",
		".X.....X..",
		".X.....X..",
		".X.....X..",
		".X.....X..",
		".XXXXXXX..",
		"..........",
	},
	// a rising line with two breaks
	"metrics": {
		"..........",
		"........X.",
		".......XX.",
		"......X.X.",
		"..X..X..X.",
		".X.XX.....",
		"X..X......",
		"..........",
		"..........",
		"..........",
	},
	// a lens with a handle
	"search": {
		"..XXXX....",
		".X....X...",
		"X......X..",
		"X......X..",
		"X......X..",
		".X....X...",
		"..XXXX.X..",
		"......X.X.",
		".......X.X",
		"........X.",
	},
	// a cross of two pixels
	"new": {
		"....XX....",
		"....XX....",
		"....XX....",
		"XXXXXXXXXX",
		"XXXXXXXXXX",
		"....XX....",
		"....XX....",
		"....XX....",
		"..........",
		"..........",
	},
	// three pairs
	"more": {
		"..........",
		"..........",
		"..........",
		"..........",
		"XX..XX..XX",
		"XX..XX..XX",
		"..........",
		"..........",
		"..........",
		"..........",
	},
	// a frame with a hill and a sun
	"image": {
		"XXXXXXXXXX",
		"X........X",
		"X.....XX.X",
		"X.....XX.X",
		"X...X....X",
		"X..XXX.X.X",
		"X.XXXXXXXX",
		"XXXXXXXXXX",
		"..........",
		"..........",
	},
	// a frame with a triangle
	"video": {
		"XXXXXXXXXX",
		"X........X",
		"X.X......X",
		"X.XXX....X",
		"X.XXXXX..X",
		"X.XXX....X",
		"X.X......X",
		"X........X",
		"XXXXXXXXXX",
		"..........",
	},
	// a microphone on a stand
	"audio": {
		"...XXXX...",
		"...X..X...",
		"...X..X...",
		"X..XXXX..X",
		"X........X",
		".X......X.",
		"..XXXXXX..",
		"....XX....",
		"....XX....",
		"..XXXXXX..",
	},
	// a calendar with one filled row for the week
	"week": {
		"XXXXXXXXX.",
		"X.X.X.X.X.",
		"XXXXXXXXX.",
		"X.......X.",
		"XXXXXXXXX.",
		"XXXXXXXXX.",
		"X.......X.",
		"X.......X.",
		"XXXXXXXXX.",
		"..........",
	},
	// a box with a tick
	"plans": {
		"XXXXXXXX..",
		"X......X..",
		"X....X.X..",
		"X.X.X..X..",
		"X..X...X..",
		"X......X..",
		"X......X..",
		"XXXXXXXX..",
		"..........",
		"..........",
	},
	// a speech bubble with three dots
	"chat": {
		"XXXXXXXXXX",
		"X........X",
		"X........X",
		"X.X.X.X..X",
		"X........X",
		"X........X",
		"XXXXXXXXXX",
		"...XX.....",
		"..XX......",
		"..X.......",
	},
}

// Icons is the sheet of the icons: one cell of CellSize by CellSize pixels for each name of IconNames, in one row.
// The glyphs are in one opaque colour, because the page uses the sheet as a mask. The rest is transparent.
func Icons() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, len(IconNames)*CellSize, CellSize))
	for cell, name := range IconNames {
		for y, row := range glyphs[name] {
			for x := 0; x < len(row); x++ {
				if row[x] == 'X' {
					img.SetNRGBA(cell*CellSize+x, y, iconInk)
				}
			}
		}
	}
	return img
}
