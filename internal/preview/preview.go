// Package preview makes the picture a file page keeps (docs/decisions/D09-binary-files.md, the files table of
// docs/schema/schema.sql): a JPEG, upright, its long edge at most 1600 px, at most 1 MB, with no metadata. It reads
// what the standard library reads: JPEG, PNG and GIF.
package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

const (
	MaxEdge  = 1600    // the long edge of a preview, in pixels
	MaxBytes = 1 << 20 // the size of a preview (files_preview)
	maxArea  = 120e6   // the largest picture decoded (a 108 MP phone photo), so a crafted file cannot exhaust memory
)

// ErrFormat is a picture this package cannot read.
var ErrFormat = errors.New("a preview is made from a JPEG, PNG or GIF picture; turn other formats (HEIC, a video's frame) into a JPEG first")

// Readable reports a type a preview can be made from.
func Readable(mime string) bool {
	return mime == "image/jpeg" || mime == "image/png" || mime == "image/gif"
}

// Make decodes a picture, scales its long edge to at most MaxEdge, turns it upright by a JPEG's EXIF orientation, and
// encodes it as a JPEG of at most MaxBytes. The result carries no metadata: a photo's GPS stays out (D21).
func Make(src []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, ErrFormat
	}
	if float64(cfg.Width)*float64(cfg.Height) > maxArea {
		return nil, fmt.Errorf("the picture is %d×%d: too large to read", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	w, h := m.Bounds().Dx(), m.Bounds().Dy()
	if w == 0 || h == 0 {
		return nil, ErrFormat
	}
	dw, dh := w, h
	if long := max(w, h); long > MaxEdge {
		dw, dh = max(1, (w*MaxEdge+long/2)/long), max(1, (h*MaxEdge+long/2)/long)
	}
	out := orient(scale(m, dw, dh), orientation(src))
	for _, q := range []int{85, 75, 65, 55, 45, 35} {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, out, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if b.Len() <= MaxBytes {
			return b.Bytes(), nil
		}
	}
	return nil, errors.New("the picture does not fit in 1 MB")
}

// Check reports whether b is a preview as the schema keeps it: a JPEG of at most MaxBytes whose long edge is at most
// MaxEdge.
func Check(b []byte) error {
	if len(b) < 3 || b[0] != 0xFF || b[1] != 0xD8 || b[2] != 0xFF {
		return errors.New("a preview is a JPEG")
	}
	if len(b) > MaxBytes {
		return fmt.Errorf("a preview is at most %d bytes, not %d", MaxBytes, len(b))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("a preview is a JPEG: %v", err)
	}
	if max(cfg.Width, cfg.Height) > MaxEdge {
		return fmt.Errorf("a preview's long edge is at most %d px, not %d", MaxEdge, max(cfg.Width, cfg.Height))
	}
	return nil
}

// scale averages the source pixels that fall in each destination pixel (a box filter: what a downscale by a large
// factor needs), compositing any transparency over white, since a JPEG has none.
func scale(src image.Image, dw, dh int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	at := pixels(src)
	sum := make([]uint64, dw*4)
	flush := func(row int) {
		for x := range dw {
			s := sum[x*4 : x*4+4]
			if s[3] > 0 {
				i := dst.PixOffset(x, row)
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(s[0]/s[3]), uint8(s[1]/s[3]), uint8(s[2]/s[3]), 0xFF
			}
			s[0], s[1], s[2], s[3] = 0, 0, 0, 0
		}
	}
	row := 0
	for y := range sh {
		if ty := y * dh / sh; ty != row {
			flush(row)
			row = ty
		}
		for x := range sw {
			r, g, bl := at(b.Min.X+x, b.Min.Y+y)
			s := sum[(x*dw/sw)*4:]
			s[0] += uint64(r)
			s[1] += uint64(g)
			s[2] += uint64(bl)
			s[3]++
		}
	}
	flush(row)
	return dst
}

// pixels reads 8-bit RGB over white, with fast paths for what the decoders return.
func pixels(src image.Image) func(x, y int) (r, g, b uint8) {
	switch m := src.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint8, uint8, uint8) {
			return color.YCbCrToRGB(m.Y[m.YOffset(x, y)], m.Cb[m.COffset(x, y)], m.Cr[m.COffset(x, y)])
		}
	case *image.Gray:
		return func(x, y int) (uint8, uint8, uint8) { v := m.Pix[m.PixOffset(x, y)]; return v, v, v }
	}
	return func(x, y int) (uint8, uint8, uint8) {
		r, g, b, a := src.At(x, y).RGBA() // premultiplied: over white is c + (1 - a)
		return uint8((r + 0xFFFF - a) >> 8), uint8((g + 0xFFFF - a) >> 8), uint8((b + 0xFFFF - a) >> 8)
	}
}

// orient turns m upright for an EXIF orientation (1-8): 2 mirrored, 3 turned half, 4 flipped, 5 transposed,
// 6 turned a quarter clockwise, 7 transversed, 8 turned a quarter counter-clockwise.
func orient(m *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return m
	}
	w, h := m.Bounds().Dx(), m.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			copy(out.Pix[out.PixOffset(nx, ny):out.PixOffset(nx, ny)+4], m.Pix[m.PixOffset(x, y):m.PixOffset(x, y)+4])
		}
	}
	return out
}

// orientation is a JPEG's EXIF orientation (tag 0x0112 of IFD0 in the APP1 segment), 1 when it has none.
func orientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0xDA || marker == 0xD9: // the image data: no header follows
			return 1
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD7: // markers without a length
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		if seg := b[i+4 : i+2+n]; marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	for k := range int(bo.Uint16(t[off:])) {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
