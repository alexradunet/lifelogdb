// Package photo reads what a photo's metadata says about when and where it was taken
// (docs/cookbook/place-of-a-photo.md): the EXIF of a JPEG (its APP1 segment) or of a HEIC (the Exif item of its meta
// box). It reads bytes it is handed, never more: a caller holding only a file's head gets what the head holds.
package photo

import (
	"encoding/binary"
	"math"
	"strings"
	"time"
)

// Meta is what the metadata says; the zero of each field is "it does not say".
type Meta struct {
	Taken       string  // the local date and time it was taken (EXIF DateTimeOriginal, the camera's clock), "2006-01-02 15:04:05"
	Lat, Lon    float64 // WGS84 degrees, when HasGPS
	HasGPS      bool    // a position, and not 0°, 0° (how photo metadata says "no location")
	Orientation int     // the EXIF orientation, 1-8; 1 when it has none
}

// Day is the local day it was taken (lifelog_meta.days: the calendar of the device that captured it), "" when none.
func (m Meta) Day() string {
	if len(m.Taken) < 10 {
		return ""
	}
	return m.Taken[:10]
}

// Read is the metadata of a JPEG or a HEIC; a file of any other kind, or with no metadata, says nothing.
func Read(b []byte) Meta {
	if t := jpegExif(b); t != nil {
		return parseTIFF(t)
	}
	if t := heifExif(b); t != nil {
		return parseTIFF(t)
	}
	return Meta{Orientation: 1}
}

// jpegExif is the TIFF data of a JPEG's APP1 Exif segment, nil when it has none.
func jpegExif(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0xDA || marker == 0xD9: // the image data: no header follows
			return nil
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD7: // markers without a length
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return nil
		}
		if seg := b[i+4 : i+2+n]; marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return seg[6:]
		}
		i += 2 + n
	}
	return nil
}

// heifExif is the TIFF data of a HEIC's (ISOBMFF) Exif item: found by its type in iinf, located by iloc, in the file
// (construction method 0) or in idat (1). Nil when the file is not one, or the item lies beyond b.
func heifExif(b []byte) []byte {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return nil
	}
	meta := child(b, "meta")
	if len(meta) < 4 {
		return nil
	}
	meta = meta[4:] // a full box: version and flags
	id, ok := exifItem(child(meta, "iinf"))
	if !ok {
		return nil
	}
	data := itemData(b, child(meta, "iloc"), child(meta, "idat"), id)
	if len(data) < 4 {
		return nil
	}
	off := int(binary.BigEndian.Uint32(data)) // exif_tiff_header_offset: from the end of these four bytes
	if off < 0 || 4+off >= len(data) {
		return nil
	}
	return data[4+off:]
}

// boxes calls fn with each box's type and body, in order, until fn returns false.
func boxes(b []byte, fn func(typ string, body []byte) bool) {
	for len(b) >= 8 {
		size, head := uint64(binary.BigEndian.Uint32(b)), 8
		typ := string(b[4:8])
		switch size {
		case 0:
			size = uint64(len(b))
		case 1:
			if len(b) < 16 {
				return
			}
			size, head = binary.BigEndian.Uint64(b[8:]), 16
		}
		if size < uint64(head) || size > uint64(len(b)) {
			if typ != "mdat" || len(b) <= head { // the file's head may end inside mdat: what came before it is whole
				return
			}
			size = uint64(len(b))
		}
		if !fn(typ, b[head:size]) {
			return
		}
		b = b[size:]
	}
}

func child(b []byte, typ string) []byte {
	var out []byte
	boxes(b, func(t string, body []byte) bool {
		if t == typ {
			out = body
			return false
		}
		return true
	})
	return out
}

// exifItem is the id of the item of type Exif in an iinf box.
func exifItem(iinf []byte) (uint32, bool) {
	if len(iinf) < 6 {
		return 0, false
	}
	p := 6 // version, flags, a 16-bit entry count
	if iinf[0] != 0 {
		p = 8
	}
	if p > len(iinf) {
		return 0, false
	}
	var id uint32
	found := false
	boxes(iinf[p:], func(t string, e []byte) bool {
		if t != "infe" || len(e) < 4 || e[0] < 2 {
			return true
		}
		var typ string
		switch {
		case e[0] == 2 && len(e) >= 12:
			id, typ = uint32(binary.BigEndian.Uint16(e[4:])), string(e[8:12])
		case e[0] >= 3 && len(e) >= 14:
			id, typ = binary.BigEndian.Uint32(e[4:]), string(e[10:14])
		}
		found = typ == "Exif"
		return !found
	})
	return id, found
}

// maxHEIFItemData bounds iloc reconstruction to the metadata head the API keeps. A photo's position is only a
// transient input to D21 place matching, so metadata outside the supplied head stays unknown rather than being fetched.
const maxHEIFItemData = 1 << 20

// itemData is an item's bytes, by its iloc entry; nil when they are not all in file (the head given) or idat.
func itemData(file, iloc, idat []byte, item uint32) []byte {
	if len(iloc) < 8 {
		return nil
	}
	v := iloc[0]
	if v > 2 {
		return nil
	}
	r := reader{b: iloc, p: 4}
	sizes := r.n(2)
	offSize, lenSize, baseSize, idxSize := int(sizes>>12&15), int(sizes>>8&15), int(sizes>>4&15), int(sizes&15)
	if v == 0 {
		idxSize = 0
	}
	if !validItemLocationSize(offSize) || !validItemLocationSize(lenSize) || !validItemLocationSize(baseSize) || !validItemLocationSize(idxSize) {
		return nil
	}
	var count uint64
	if v == 2 {
		count = r.n(4)
	} else {
		count = r.n(2)
	}
	if r.bad {
		return nil
	}
	for range count {
		var id uint64
		if v == 2 {
			id = r.n(4)
		} else {
			id = r.n(2)
		}
		method := uint64(0)
		if v == 1 || v == 2 {
			method = r.n(2) & 15
		}
		dataRef := r.n(2)
		base := r.n(baseSize)
		extents := r.n(2)
		if r.bad {
			return nil
		}
		target := uint32(id) == item
		extentBytes := idxSize + offSize + lenSize
		if !target {
			skip := extents * uint64(extentBytes)
			if extentBytes != 0 && skip/uint64(extentBytes) != extents || !r.skip(skip) {
				return nil
			}
			continue
		}
		if dataRef != 0 {
			return nil
		}
		var out []byte
		for range extents {
			r.n(idxSize)
			rel := r.n(offSize)
			n := r.n(lenSize)
			if r.bad {
				return nil
			}
			if !target {
				continue
			}
			src := file
			switch method {
			case 0:
			case 1:
				src = idat
			default:
				return nil
			}
			if n == 0 || base > math.MaxUint64-rel {
				return nil
			}
			off := base + rel
			if off > uint64(len(src)) || n > uint64(len(src))-off || n > uint64(maxHEIFItemData-len(out)) {
				return nil
			}
			start, end := int(off), int(off+n)
			out = append(out, src[start:end]...)
		}
		if target {
			return out
		}
	}
	return nil
}

func validItemLocationSize(size int) bool {
	return size == 0 || size == 2 || size == 4 || size == 8
}

// reader reads big-endian numbers of 0, 2, 4 or 8 bytes; past the end it reads 0 and marks bad.
type reader struct {
	b   []byte
	p   int
	bad bool
}

func (r *reader) n(size int) uint64 {
	if size == 0 {
		return 0
	}
	if r.p+size > len(r.b) {
		r.bad = true
		return 0
	}
	var v uint64
	for _, c := range r.b[r.p : r.p+size] {
		v = v<<8 | uint64(c)
	}
	r.p += size
	return v
}

func (r *reader) skip(n uint64) bool {
	if n > uint64(len(r.b)-r.p) {
		r.bad = true
		return false
	}
	r.p += int(n)
	return true
}

// parseTIFF reads IFD0 (orientation, the Exif and GPS IFD pointers), the Exif IFD (DateTimeOriginal) and the GPS IFD.
func parseTIFF(t []byte) Meta {
	m := Meta{Orientation: 1}
	if len(t) < 8 {
		return m
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return m
	}
	ifd0 := entries(t, bo, int(bo.Uint32(t[4:])))
	if v, ok := ifd0[0x0112]; ok {
		if o := int(v.short(t, bo)); o >= 1 && o <= 8 {
			m.Orientation = o
		}
	}
	if p, ok := ifd0[0x8769]; ok {
		exif := entries(t, bo, int(p.long(t, bo)))
		for _, tag := range []uint16{0x9003, 0x9004} { // DateTimeOriginal, else DateTimeDigitized
			if v, ok := exif[tag]; ok {
				if s, err := time.Parse("2006:01:02 15:04:05", strings.TrimRight(v.ascii(t, bo), "\x00 ")); err == nil {
					m.Taken = s.Format("2006-01-02 15:04:05")
					break
				}
			}
		}
	}
	if p, ok := ifd0[0x8825]; ok {
		gps := entries(t, bo, int(p.long(t, bo)))
		lat, okLat := gps[0x0002].degrees(t, bo)
		lon, okLon := gps[0x0004].degrees(t, bo)
		latRef := gps[0x0001].reference(t, bo, 'N', 'S')
		lonRef := gps[0x0003].reference(t, bo, 'E', 'W')
		if okLat && okLon && latRef != 0 && lonRef != 0 {
			if latRef == 'S' {
				lat = -lat
			}
			if lonRef == 'W' {
				lon = -lon
			}
			if lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && (lat != 0 || lon != 0) {
				m.Lat, m.Lon, m.HasGPS = lat, lon, true
			}
		}
	}
	return m
}

// entry is one IFD entry: its type, count, and the 4 bytes holding the value or its offset.
type entry struct {
	typ   uint16
	count uint32
	raw   []byte
}

func entries(t []byte, bo binary.ByteOrder, off int) map[uint16]entry {
	out := map[uint16]entry{}
	if off < 8 || off+2 > len(t) {
		return out
	}
	for k := range int(bo.Uint16(t[off:])) {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			break
		}
		out[bo.Uint16(t[e:])] = entry{bo.Uint16(t[e+2:]), bo.Uint32(t[e+4:]), t[e+8 : e+12]}
	}
	return out
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}

// value is the entry's bytes: inline when they fit in 4, else at the offset.
func (e entry) value(t []byte, bo binary.ByteOrder) []byte {
	n := typeSize[e.typ] * int(e.count)
	if n == 0 || e.raw == nil {
		return nil
	}
	if n <= 4 {
		return e.raw[:n]
	}
	off := int(bo.Uint32(e.raw))
	if off < 0 || off+n > len(t) {
		return nil
	}
	return t[off : off+n]
}

func (e entry) short(t []byte, bo binary.ByteOrder) uint16 {
	if v := e.value(t, bo); e.typ == 3 && len(v) >= 2 {
		return bo.Uint16(v)
	}
	return 0
}

func (e entry) long(t []byte, bo binary.ByteOrder) uint32 {
	if v := e.value(t, bo); e.typ == 4 && len(v) >= 4 {
		return bo.Uint32(v)
	}
	return 0
}

func (e entry) ascii(t []byte, bo binary.ByteOrder) string {
	if e.typ != 2 {
		return ""
	}
	return string(e.value(t, bo))
}

// reference requires the EXIF ASCII count of two: a hemisphere and its NUL terminator.
func (e entry) reference(t []byte, bo binary.ByteOrder, positive, negative byte) byte {
	if e.typ != 2 || e.count != 2 {
		return 0
	}
	v := e.value(t, bo)
	if len(v) != 2 || v[1] != 0 || (v[0] != positive && v[0] != negative) {
		return 0
	}
	return v[0]
}

// degrees is a GPS coordinate: three rationals, degrees, minutes and seconds.
func (e entry) degrees(t []byte, bo binary.ByteOrder) (float64, bool) {
	v := e.value(t, bo)
	if e.typ != 5 || e.count != 3 || len(v) != 24 {
		return 0, false
	}
	var d [3]float64
	for i := range 3 {
		num, den := bo.Uint32(v[8*i:]), bo.Uint32(v[8*i+4:])
		if den == 0 {
			return 0, false
		}
		d[i] = float64(num) / float64(den)
	}
	deg := d[0] + d[1]/60 + d[2]/3600
	return deg, !math.IsNaN(deg) && !math.IsInf(deg, 0)
}
