// Command pixelart writes the pictures of internal/pixelart to internal/api/view/html: the wood tile of the rail, the
// ground tile of the page and the sprite sheet. Run it from the repository root after a change to a drawing; a test
// of internal/api/view compares each file with its drawing.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"

	"lifelog/internal/pixelart"
)

func main() {
	for _, f := range []struct {
		name string
		img  image.Image
	}{
		{"wood.png", pixelart.Wood()},
		{"ground.png", pixelart.Ground()},
		{"sprites.png", pixelart.Sprites()},
	} {
		path := "internal/api/view/html/" + f.name
		var buf bytes.Buffer
		if err := png.Encode(&buf, f.img); err != nil {
			fmt.Fprintln(os.Stderr, "pixelart:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "pixelart:", err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d bytes\n", path, buf.Len())
	}
}
