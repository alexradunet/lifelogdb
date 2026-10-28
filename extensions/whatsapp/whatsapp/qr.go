package whatsapp

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// quiet is the width of the quiet zone around a code, in modules. The QR standard asks for four.
const quiet = 4

// Render draws a QR code of text as Unicode half blocks: one line holds two rows of modules. A dark module is
// drawn as a filled half, a light module as a blank. The code has a quiet zone of four modules on each side.
// Draw the lines in black on white; Login does so with a colour code, so the code reads on any terminal theme.
func Render(text string) ([]string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("qr code: %w", err)
	}
	size := code.Size
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < size && y < size && code.Black(x, y)
	}
	total := size + 2*quiet
	lines := make([]string, 0, (total+1)/2)
	for y := 0; y < total; y += 2 {
		var b strings.Builder
		for x := 0; x < total; x++ {
			switch top, bottom := dark(x, y), dark(x, y+1); {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		lines = append(lines, b.String())
	}
	return lines, nil
}
