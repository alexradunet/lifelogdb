// Package phototest builds synthetic photos for tests: EXIF (a TIFF block) in either byte order, a JPEG carrying it,
// and the box tree of a HEIC carrying it (not a decodable image: its picture is a placeholder). No real photo is used.
package phototest

import (
	"bytes"
	"encoding/binary"
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

// TIFF is EXIF saying what m says: IFD0 (orientation, the pointers), the Exif IFD (DateTimeOriginal) and the GPS IFD.
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
	rat := func(v float64) []byte {
		d := math.Floor(v)
		mf := (v - d) * 60
		mi := math.Floor(mf)
		sec := (mf - mi) * 60
		var b []byte
		for _, p := range [][2]uint32{{uint32(d), 1}, {uint32(mi), 1}, {uint32(math.Round(sec * 10000)), 10000}} {
			b = append(b, u32(p[0])...)
			b = append(b, u32(p[1])...)
		}
		return b
	}
	var ifd0, exif, gps []entry
	if m.Orientation > 0 {
		ifd0 = append(ifd0, entry{0x0112, 3, 1, u16(uint16(m.Orientation))})
	}
	if m.Taken != "" {
		s := strings.Replace(m.Taken[:10], "-", ":", 2) + m.Taken[10:] + "\x00"
		exif = append(exif, entry{0x9003, 2, uint32(len(s)), []byte(s)})
	}
	if m.HasGPS {
		ns, ew := "N\x00", "E\x00"
		if m.Lat < 0 {
			ns = "S\x00"
		}
		if m.Lon < 0 {
			ew = "W\x00"
		}
		gps = []entry{{1, 2, 2, []byte(ns)}, {2, 5, 3, rat(math.Abs(m.Lat))}, {3, 2, 2, []byte(ew)}, {4, 5, 3, rat(math.Abs(m.Lon))}}
	}
	size := func(es []entry) int { return 2 + 12*len(es) + 4 }
	n0 := len(ifd0)
	if exif != nil {
		ifd0 = append(ifd0, entry{0x8769, 4, 1, nil})
	}
	if gps != nil {
		ifd0 = append(ifd0, entry{0x8825, 4, 1, nil})
	}
	offExif := 8 + size(ifd0)
	offGPS := offExif
	if exif != nil {
		offGPS += size(exif)
	}
	cursor := offGPS
	if gps != nil {
		cursor += size(gps)
	}
	for i := n0; i < len(ifd0); i++ {
		if ifd0[i].tag == 0x8769 {
			ifd0[i].data = u32(uint32(offExif))
		} else {
			ifd0[i].data = u32(uint32(offGPS))
		}
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
	if gps != nil {
		out = append(out, write(gps)...)
	}
	return append(out, dataArea...)
}

// JPEG is a w×h JPEG (a gradient) carrying m's EXIF in an APP1 segment right after its SOI marker.
func JPEG(w, h int, m photo.Meta, little bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	j := b.Bytes()
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

// HEIC is the box tree of a HEIC whose Exif item says what m says: in mdat (construction method 0), or in the meta
// box's idat (method 1).
func HEIC(m photo.Meta, little, inIdat bool) []byte {
	return HEICLocationVersion(m, little, inIdat, 1)
}

// HEICLocationVersion is HEIC with a selected iloc version. Version 0 has no construction method field, so its Exif
// item is always located in the file.
func HEICLocationVersion(m photo.Meta, little, inIdat bool, version byte) []byte {
	item := append(binary.BigEndian.AppendUint32(nil, 6), "Exif\x00\x00"...)
	item = append(item, TIFF(m, little)...)
	picture := make([]byte, 64) // a placeholder for the coded image
	exifID := uint32(102)
	pictureID := uint32(101)
	if version == 2 {
		exifID = 0x10002
		pictureID = 0x10001
	}
	if version == 0 {
		inIdat = false
	}
	infe := func(id uint32, typ string) []byte {
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
	build := func(picOff, exifOff uint32) []byte {
		iloc := []byte{version, 0, 0, 0, 0x44, 0x00} // offsets and lengths of 4 bytes, no base offset, no index
		if version == 2 {
			iloc = binary.BigEndian.AppendUint32(iloc, 2)
		} else {
			iloc = binary.BigEndian.AppendUint16(iloc, 2)
		}
		for _, it := range []struct {
			id     uint32
			method uint16
			off, n uint32
		}{{pictureID, 0, picOff, uint32(len(picture))}, {exifID, map[bool]uint16{false: 0, true: 1}[inIdat], exifOff, uint32(len(item))}} {
			if version == 2 {
				iloc = binary.BigEndian.AppendUint32(iloc, it.id)
			} else {
				iloc = binary.BigEndian.AppendUint16(iloc, uint16(it.id))
			}
			if version == 1 || version == 2 {
				iloc = binary.BigEndian.AppendUint16(iloc, it.method)
			}
			iloc = binary.BigEndian.AppendUint16(iloc, 0) // data_reference_index
			iloc = binary.BigEndian.AppendUint16(iloc, 1) // one extent
			iloc = binary.BigEndian.AppendUint32(iloc, it.off)
			iloc = binary.BigEndian.AppendUint32(iloc, it.n)
		}
		hdlr := box("hdlr", []byte{0, 0, 0, 0, 0, 0, 0, 0}, []byte("pict"), make([]byte, 13))
		iinf := box("iinf", []byte{0, 0, 0, 0, 0, 2}, infe(pictureID, "hvc1"), infe(exifID, "Exif"))
		parts := [][]byte{{0, 0, 0, 0}, hdlr, iinf, box("iloc", iloc)}
		if inIdat {
			parts = append(parts, box("idat", item))
		}
		ftyp := box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
		mdat := box("mdat", picture)
		if !inIdat {
			mdat = box("mdat", picture, item)
		}
		return append(append(ftyp, box("meta", parts...)...), mdat...)
	}
	draft := build(0, 0)
	mdatPayload := uint32(len(draft) - len(picture)) // the payload of mdat, the last box, after its 8-byte header
	if !inIdat {
		mdatPayload -= uint32(len(item))
	}
	exifOff := mdatPayload + uint32(len(picture))
	if inIdat {
		exifOff = 0
	}
	return build(mdatPayload, exifOff)
}

// HEICOverflowingLocation is a HEIC-shaped metadata fixture whose Exif item extent wraps uint64 arithmetic.
func HEICOverflowingLocation() []byte {
	infe := func(id uint16, typ string) []byte {
		b := []byte{2, 0, 0, 0}
		b = binary.BigEndian.AppendUint16(b, id)
		b = binary.BigEndian.AppendUint16(b, 0)
		return box("infe", append(append(b, typ...), 0))
	}
	iloc := []byte{1, 0, 0, 0, 0x88, 0x00} // version 1; 8-byte offsets and lengths
	iloc = binary.BigEndian.AppendUint16(iloc, 1)
	iloc = binary.BigEndian.AppendUint16(iloc, 102)
	iloc = binary.BigEndian.AppendUint16(iloc, 1) // construction method: idat
	iloc = binary.BigEndian.AppendUint16(iloc, 0) // data_reference_index
	iloc = binary.BigEndian.AppendUint16(iloc, 1) // one extent
	iloc = appendUint(iloc, 8, ^uint64(0)-1)
	iloc = appendUint(iloc, 8, 8)
	hdlr := box("hdlr", []byte{0, 0, 0, 0, 0, 0, 0, 0}, []byte("pict"), make([]byte, 13))
	iinf := box("iinf", []byte{0, 0, 0, 0, 0, 1}, infe(102, "Exif"))
	meta := box("meta", []byte{0, 0, 0, 0}, hdlr, iinf, box("iloc", iloc), box("idat", make([]byte, 16)))
	return append(box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), meta...)
}

func appendUint(b []byte, size int, v uint64) []byte {
	for i := size - 1; i >= 0; i-- {
		b = append(b, byte(v>>uint(i*8)))
	}
	return b
}

// GPSReference replaces one reference entry in a synthetic JPEG or HEIC. A zero
// tag removes the reference; an out-of-line count points past the TIFF data.
func GPSReference(b []byte, axis uint16, typ uint16, count uint32, value string, absent bool) []byte {
	b = append([]byte(nil), b...)
	start := bytes.Index(b, []byte("MM\x00\x2a"))
	if start < 0 {
		start = bytes.Index(b, []byte("II\x2a\x00"))
	}
	t := b[start:]
	var bo binary.ByteOrder = binary.BigEndian
	if string(t[:2]) == "II" {
		bo = binary.LittleEndian
	}
	off := int(bo.Uint32(t[4:]))
	gps := 0
	for i := 0; i < int(bo.Uint16(t[off:])); i++ {
		e := off + 2 + 12*i
		if bo.Uint16(t[e:]) == 0x8825 {
			gps = int(bo.Uint32(t[e+8:]))
		}
	}
	for i := 0; i < int(bo.Uint16(t[gps:])); i++ {
		e := gps + 2 + 12*i
		if bo.Uint16(t[e:]) != axis {
			continue
		}
		if absent {
			bo.PutUint16(t[e:], 0)
			break
		}
		bo.PutUint16(t[e+2:], typ)
		bo.PutUint32(t[e+4:], count)
		clear(t[e+8 : e+12])
		copy(t[e+8:e+12], value)
		if count > 4 {
			bo.PutUint32(t[e+8:], uint32(len(t)+1))
		}
		break
	}
	return b
}
