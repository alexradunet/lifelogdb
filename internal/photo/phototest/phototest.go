// Package phototest builds synthetic photos for tests: EXIF (a TIFF block) in either byte order, a JPEG with or
// without it, and the box tree of a HEIF file carrying it (not a decodable image: its picture is a placeholder). No
// real photo is used.
package phototest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"

	"lifelog/internal/photo"
)

type entry struct {
	tag, typ uint16
	count    uint32
	data     []byte
}

// TIFF is EXIF saying what m says: IFD0 (orientation, the pointers) and the Exif IFD (DateTimeOriginal).
func TIFF(m photo.Meta, little bool) []byte {
	var bo interface {
		binary.ByteOrder
		binary.AppendByteOrder
	} = binary.BigEndian
	head := []byte("MM\x00\x2a")
	if little {
		bo, head = binary.LittleEndian, []byte("II\x2a\x00")
	}
	u16 := func(v uint16) []byte { return bo.AppendUint16(nil, v) }
	u32 := func(v uint32) []byte { return bo.AppendUint32(nil, v) }
	var ifd0, exif []entry
	if m.Orientation > 0 {
		ifd0 = append(ifd0, entry{0x0112, 3, 1, u16(uint16(m.Orientation))})
	}
	if m.Taken != "" {
		s := strings.Replace(m.Taken[:10], "-", ":", 2) + m.Taken[10:] + "\x00"
		exif = append(exif, entry{0x9003, 2, uint32(len(s)), []byte(s)})
	}
	size := func(es []entry) int { return 2 + 12*len(es) + 4 }
	n0 := len(ifd0)
	if exif != nil {
		ifd0 = append(ifd0, entry{0x8769, 4, 1, nil})
	}
	offExif := 8 + size(ifd0)
	cursor := offExif
	if exif != nil {
		cursor += size(exif)
	}
	for i := n0; i < len(ifd0); i++ {
		ifd0[i].data = u32(uint32(offExif)) // the only pointer is the one to the Exif IFD
	}
	var dataArea []byte
	write := func(es []entry) []byte {
		b := u16(uint16(len(es)))
		for _, e := range es {
			b = append(b, u16(e.tag)...)
			b = append(b, u16(e.typ)...)
			b = append(b, u32(e.count)...)
			if len(e.data) <= 4 {
				b = append(b, append(append([]byte{}, e.data...), make([]byte, 4-len(e.data))...)...)
			} else {
				b = append(b, u32(uint32(cursor+len(dataArea)))...)
				dataArea = append(dataArea, e.data...)
			}
		}
		return append(b, 0, 0, 0, 0) // no next IFD
	}
	out := append(append(head, u32(8)...), write(ifd0)...)
	if exif != nil {
		out = append(out, write(exif)...)
	}
	return append(out, dataArea...)
}

// JPEG is a w×h JPEG (a gradient) with no metadata. It panics with the error of jpeg.Encode when the encode fails (a
// side of 65,536 pixels or more): such a size is a defect of the test.
func JPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		panic(fmt.Errorf("phototest: a %d×%d JPEG: %w", w, h, err))
	}
	return b.Bytes()
}

// WithExif is the JPEG j with m's EXIF in an APP1 segment right after its SOI marker.
func WithExif(j []byte, m photo.Meta, little bool) []byte {
	seg := append([]byte("Exif\x00\x00"), TIFF(m, little)...)
	out := append([]byte{}, j[:2]...)
	out = append(out, 0xFF, 0xE1, byte((len(seg)+2)>>8), byte(len(seg)+2))
	out = append(out, seg...)
	return append(out, j[2:]...)
}

func box(typ string, body ...[]byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, 0)
	b = append(b, typ...)
	for _, x := range body {
		b = append(b, x...)
	}
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	return b
}

// Extent is one run of an item's bytes, as the iloc box says it.
type Extent struct{ Off, N uint64 }

// Location is one item of a HEIF file: its iinf entry (ID and Type) and its iloc entry. ExtentCount, when not zero,
// is the count that the iloc box says, whatever Extents holds.
type Location struct {
	ID          uint32
	Type        string
	Method      uint16
	DataRef     uint16
	Base        uint64
	ExtentCount int
	Extents     []Extent
}

// HEIFItems is the ftyp and meta boxes of a HEIF file with the items at the places that entries say. The iloc box has
// the version, and fields of offSize, lenSize, baseSize and idxSize bytes. idat, when not nil, is the meta box's idat.
// The bytes of an item that lie in mdat are for the caller to append.
func HEIFItems(version byte, offSize, lenSize, baseSize, idxSize int, idat []byte, entries ...Location) []byte {
	iloc := []byte{version, 0, 0, 0, byte(offSize<<4 | lenSize), byte(baseSize<<4 | idxSize)}
	if version == 2 {
		iloc = binary.BigEndian.AppendUint32(iloc, uint32(len(entries)))
	} else {
		iloc = binary.BigEndian.AppendUint16(iloc, uint16(len(entries)))
	}
	for _, e := range entries {
		if version == 2 {
			iloc = binary.BigEndian.AppendUint32(iloc, e.ID)
		} else {
			iloc = binary.BigEndian.AppendUint16(iloc, uint16(e.ID))
		}
		if version == 1 || version == 2 {
			iloc = binary.BigEndian.AppendUint16(iloc, e.Method)
		}
		iloc = binary.BigEndian.AppendUint16(iloc, e.DataRef)
		iloc = appendUint(iloc, baseSize, e.Base)
		count := len(e.Extents)
		if e.ExtentCount != 0 {
			count = e.ExtentCount
		}
		iloc = binary.BigEndian.AppendUint16(iloc, uint16(count))
		for _, x := range e.Extents {
			iloc = appendUint(iloc, idxSize, 0)
			iloc = appendUint(iloc, offSize, x.Off)
			iloc = appendUint(iloc, lenSize, x.N)
		}
	}
	hdlr := box("hdlr", []byte{0, 0, 0, 0, 0, 0, 0, 0}, []byte("pict"), make([]byte, 13))
	iinf := binary.BigEndian.AppendUint16([]byte{0, 0, 0, 0}, uint16(len(entries)))
	for _, e := range entries {
		iinf = append(iinf, infe(e.ID, e.Type)...)
	}
	parts := [][]byte{{0, 0, 0, 0}, hdlr, box("iinf", iinf), box("iloc", iloc)}
	if idat != nil {
		parts = append(parts, box("idat", idat))
	}
	return append(box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), box("meta", parts...)...)
}

func infe(id uint32, typ string) []byte {
	var b []byte
	if id > math.MaxUint16 {
		b = []byte{3, 0, 0, 0}
		b = binary.BigEndian.AppendUint32(b, id)
	} else {
		b = []byte{2, 0, 0, 0}
		b = binary.BigEndian.AppendUint16(b, uint16(id))
	}
	b = binary.BigEndian.AppendUint16(b, 0)
	return box("infe", append(append(b, typ...), 0))
}

// Layout says how HEIF writes its Exif item.
type Layout struct {
	Little bool // the EXIF in little-endian byte order
	// InIdat puts the item in the meta box's idat (construction method 1), not in mdat (method 0). An iloc box of
	// version 0 has no construction method, so its item is always in mdat.
	InIdat   bool
	Version  byte // the version of the iloc box: 0, 1 or 2
	Overflow bool // the item's offset is 2^64−2, so the end of its extent wraps uint64 arithmetic: no reader finds m
}

// HEIF is the box tree of a HEIC whose Exif item says what m says, written as l says. Its other item is the picture,
// a placeholder in mdat.
func HEIF(m photo.Meta, l Layout) []byte {
	item := append(binary.BigEndian.AppendUint32(nil, 6), "Exif\x00\x00"...)
	item = append(item, TIFF(m, l.Little)...)
	picture := make([]byte, 64) // a placeholder for the coded image
	exifID := uint32(102)
	pictureID := uint32(101)
	if l.Version == 2 {
		exifID = 0x10002
		pictureID = 0x10001
	}
	inIdat := l.InIdat && l.Version != 0
	size := 4 // offsets and lengths of 4 bytes
	if l.Overflow {
		size = 8 // of 8 bytes, to hold the offset 2^64−2
	}
	var idat []byte
	method := uint16(0)
	if inIdat {
		idat, method = item, 1
	}
	build := func(picOff, exifOff uint64) []byte {
		head := HEIFItems(l.Version, size, size, 0, 0, idat,
			Location{ID: pictureID, Type: "hvc1", Extents: []Extent{{picOff, uint64(len(picture))}}},
			Location{ID: exifID, Type: "Exif", Method: method, Extents: []Extent{{exifOff, uint64(len(item))}}})
		if inIdat {
			return append(head, box("mdat", picture)...)
		}
		return append(head, box("mdat", picture, item)...)
	}
	picOff := uint64(len(build(0, 0)) - len(picture)) // the payload of mdat, the last box, after its 8-byte header
	if !inIdat {
		picOff -= uint64(len(item))
	}
	exifOff := picOff + uint64(len(picture))
	if inIdat {
		exifOff = 0
	}
	if l.Overflow {
		exifOff = math.MaxUint64 - 1
	}
	return build(picOff, exifOff)
}

func appendUint(b []byte, size int, v uint64) []byte {
	for i := size - 1; i >= 0; i-- {
		b = append(b, byte(v>>uint(i*8)))
	}
	return b
}
