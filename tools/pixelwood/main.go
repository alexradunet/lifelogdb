// Command pixelwood writes the wood tile of the rail to internal/api/view/html/wood.png. Run it from the repository
// root after a change to internal/pixelart; a test of internal/api/view compares the file with the drawing.
package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"

	"lifelog/internal/pixelart"
)

const out = "internal/api/view/html/wood.png"

func main() {
	var buf bytes.Buffer
	if err := png.Encode(&buf, pixelart.Wood()); err != nil {
		fmt.Fprintln(os.Stderr, "pixelwood:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "pixelwood:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d bytes\n", out, buf.Len())
}
