package preview

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"
)

// halves is a w×h picture, its left half red and its right half blue, with noise so it compresses like a photo.
func halves(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for y := range h {
		for x := range w {
			n := uint8(r.Intn(40))
			c := color.RGBA{200 + n/2, n, n, 255}
			if x >= w/2 {
				c = color.RGBA{n, n, 200 + n/2, 255}
			}
			m.SetRGBA(x, y, c)
		}
	}
	return m
}

func encodeJPEG(t *testing.T, m image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withOrientation splices an APP1 Exif segment with orientation o (big-endian TIFF) after a JPEG's SOI marker.
func withOrientation(j []byte, o uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // header, IFD0 at 8
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.BigEndian.AppendUint16(tiff, 3) // SHORT
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0) // value padding, next IFD = 0
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(seg)+2))
	out := append([]byte{}, j[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, j[2:]...)
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	if err := Check(b); err != nil {
		t.Fatalf("not a preview: %v", err)
	}
	m, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func reddish(c color.Color) bool { r, _, b, _ := c.RGBA(); return r > 2*b }
func bluish(c color.Color) bool  { r, _, b, _ := c.RGBA(); return b > 2*r }

func TestMakeScalesTheLongEdge(t *testing.T) {
	for _, sz := range [][4]int{{4000, 3000, 1600, 1200}, {3000, 4000, 1200, 1600}, {800, 600, 800, 600}, {5000, 7, 1600, 2}} {
		b, err := Make(encodeJPEG(t, halves(sz[0], sz[1])))
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, b)
		if got := m.Bounds().Size(); got.X != sz[2] || got.Y != sz[3] {
			t.Errorf("%dx%d became %v, want %dx%d", sz[0], sz[1], got, sz[2], sz[3])
		}
		if !reddish(m.At(m.Bounds().Dx()/4, m.Bounds().Dy()/2)) || !bluish(m.At(3*m.Bounds().Dx()/4, m.Bounds().Dy()/2)) {
			t.Errorf("%dx%d: the halves did not survive the scaling", sz[0], sz[1])
		}
	}
}

func TestMakeTurnsAJPEGUpright(t *testing.T) {
	src := encodeJPEG(t, halves(40, 20)) // red left, blue right
	if orientation(src) != 1 {
		t.Fatal("a JPEG without EXIF has orientation 1")
	}
	for o, want := range map[uint16][2]string{
		6: {"red", "blue"}, // a quarter clockwise: the left half goes to the top
		8: {"blue", "red"}, // a quarter counter-clockwise: the left half goes to the bottom
		3: {"blue", "red"}, // half a turn: the left half goes to the right
	} {
		j := withOrientation(src, o)
		if got := orientation(j); got != int(o) {
			t.Fatalf("orientation %d read as %d", o, got)
		}
		b, err := Make(j)
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, b)
		w, h := m.Bounds().Dx(), m.Bounds().Dy()
		var first, second color.Color
		if o == 3 {
			if w != 40 || h != 20 {
				t.Fatalf("orientation 3: %dx%d, want 40x20", w, h)
			}
			first, second = m.At(10, 10), m.At(30, 10)
		} else {
			if w != 20 || h != 40 {
				t.Fatalf("orientation %d: %dx%d, want 20x40", o, w, h)
			}
			first, second = m.At(10, 10), m.At(10, 30)
		}
		is := map[string]func(color.Color) bool{"red": reddish, "blue": bluish}
		if !is[want[0]](first) || !is[want[1]](second) {
			t.Errorf("orientation %d: want %s then %s", o, want[0], want[1])
		}
		if orientation(b) != 1 {
			t.Errorf("orientation %d: the preview keeps EXIF", o)
		}
	}
}

func TestMakeReadsPNGAndGIF(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 10, 10)) // transparent: over white
	var p bytes.Buffer
	png.Encode(&p, m)
	b, err := Make(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r, g, bl, _ := decode(t, b).At(5, 5).RGBA(); r < 0xF000 || g < 0xF000 || bl < 0xF000 {
		t.Errorf("a transparent PNG is not white: %x %x %x", r, g, bl)
	}
	var g bytes.Buffer
	gif.Encode(&g, halves(30, 30), nil)
	if b, err = Make(g.Bytes()); err != nil {
		t.Fatal(err)
	}
	decode(t, b)
}

func TestMakeRefuses(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("not a picture"), {0xFF, 0xD8, 0xFF, 0xE0}} {
		if _, err := Make(b); err == nil {
			t.Errorf("%q made a preview", b)
		}
	}
}

func TestCheck(t *testing.T) {
	small := encodeJPEG(t, halves(16, 16))
	if err := Check(small); err != nil {
		t.Errorf("a small JPEG: %v", err)
	}
	var p bytes.Buffer
	png.Encode(&p, halves(4, 4))
	for name, b := range map[string][]byte{
		"a PNG":           p.Bytes(),
		"empty":           {},
		"too wide":        encodeJPEG(t, halves(1601, 10)),
		"over 1 MB":       append(append([]byte{}, small...), make([]byte, MaxBytes)...),
		"JPEG bytes only": {0xFF, 0xD8, 0xFF, 0xE0, 0, 0},
	} {
		if Check(b) == nil {
			t.Errorf("%s passed", name)
		}
	}
	if !Readable("image/jpeg") || !Readable("image/png") || Readable("image/heic") || Readable("video/mp4") {
		t.Error("Readable")
	}
}
