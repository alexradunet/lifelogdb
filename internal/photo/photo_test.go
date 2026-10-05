package photo_test

import (
	"encoding/binary"
	"math"
	"testing"

	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestReadsAJPEGAndAHEIC(t *testing.T) {
	want := photo.Meta{Taken: "2019-06-03 07:41:12", Lat: 38.7139, Lon: -9.1394, HasGPS: true, Orientation: 6}
	south := photo.Meta{Taken: "2024-01-31 23:59:59", Lat: -33.8568, Lon: 151.2153, HasGPS: true, Orientation: 1}
	for _, little := range []bool{false, true} {
		for name, b := range map[string][]byte{
			"JPEG":         phototest.JPEG(40, 30, want, little),
			"HEIC in mdat": phototest.HEIC(want, little, false),
			"HEIC in idat": phototest.HEIC(want, little, true),
		} {
			got := photo.Read(b)
			if got.Taken != want.Taken || !got.HasGPS || !near(got.Lat, want.Lat) || !near(got.Lon, want.Lon) || got.Orientation != 6 || got.Day() != "2019-06-03" {
				t.Errorf("%s (little-endian %v): %+v", name, little, got)
			}
		}
		if got := photo.Read(phototest.JPEG(8, 8, south, little)); !near(got.Lat, south.Lat) || !near(got.Lon, south.Lon) || got.Taken != south.Taken {
			t.Errorf("south and east (little-endian %v): %+v", little, got)
		}
	}
}

func TestSaysNothingItDoesNotKnow(t *testing.T) {
	for name, b := range map[string][]byte{
		"no EXIF":         phototest.JPEG(8, 8, photo.Meta{}, false)[:0],
		"not a photo":     []byte("%PDF-1.7 hello"),
		"0°, 0°":          phototest.JPEG(8, 8, photo.Meta{Taken: "2019-06-03 07:41:12", HasGPS: true}, false),
		"a date alone":    phototest.JPEG(8, 8, photo.Meta{Taken: "2019-06-03 07:41:12"}, true),
		"a HEIC's head":   phototest.HEIC(photo.Meta{Taken: "2019-06-03 07:41:12", Lat: 1, Lon: 1, HasGPS: true}, false, false)[:200],
		"a JPEG's head":   phototest.JPEG(8, 8, photo.Meta{Taken: "2019-06-03 07:41:12", Lat: 1, Lon: 1, HasGPS: true}, false)[:40],
		"garbage in ftyp": append([]byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00"), 0xFF, 0xFF, 0xFF, 0xFF, 'm', 'e', 't', 'a'),
	} {
		got := photo.Read(b)
		if got.HasGPS || got.Orientation != 1 && got.Orientation != 0 {
			t.Errorf("%s: %+v", name, got)
		}
		if name != "a date alone" && name != "0°, 0°" && got.Taken != "" {
			t.Errorf("%s: a date from nowhere: %+v", name, got)
		}
	}
	if got := photo.Read(phototest.JPEG(8, 8, photo.Meta{Taken: "2019-06-03 07:41:12"}, true)); got.Day() != "2019-06-03" || got.HasGPS {
		t.Errorf("a date alone: %+v", got)
	}
	if (photo.Meta{}).Day() != "" {
		t.Error("no date, no day")
	}
}

func TestHEIFLocationVersions(t *testing.T) {
	want := photo.Meta{Taken: "2026-10-05 12:34:56", Lat: 38.7139, Lon: -9.1394, HasGPS: true, Orientation: 8}
	item := heifExifItem(want, true, 0)
	split := len(item) / 2
	for name, b := range map[string][]byte{
		"version 0 in file":          phototest.HEICLocationVersion(want, true, false, 0),
		"version 1 in file":          phototest.HEICLocationVersion(want, true, false, 1),
		"version 1 in idat":          phototest.HEICLocationVersion(want, true, true, 1),
		"version 2 in file":          phototest.HEICLocationVersion(want, true, false, 2),
		"version 2 in idat":          phototest.HEICLocationVersion(want, true, true, 2),
		"version 1 repeated extents": heifWithLocations(1, 4, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(split)}, {off: uint64(split), n: uint64(len(item) - split)}}}),
	} {
		got := readPhoto(t, b)
		if got.Taken != want.Taken || !got.HasGPS || !near(got.Lat, want.Lat) || !near(got.Lon, want.Lon) || got.Orientation != want.Orientation {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestHEIFLocationBounds(t *testing.T) {
	want := photo.Meta{Taken: "2026-10-05 12:34:56", Lat: 1, Lon: 2, HasGPS: true, Orientation: 6}
	item := heifExifItem(want, false, 0)
	valid := heifWithLocations(1, 4, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}})
	overlapIDAT := append([]byte{0}, item...)
	repeated := make([]byte, (1<<19)+1)
	unknown := map[string][]byte{
		"truncated fields":        valid[:len(valid)-1],
		"unsupported version":     heifWithLocations(3, 4, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}}),
		"unsupported size":        heifWithLocations(1, 3, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}}),
		"unsupported method":      heifWithLocations(1, 4, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 2, extents: []heifExtent{{n: uint64(len(item))}}}),
		"external data reference": heifWithLocations(1, 4, 4, 0, 0, item, heifLocation{id: 102, typ: "Exif", method: 1, dataRef: 7, extents: []heifExtent{{n: uint64(len(item))}}}),
		"base plus offset wraps":  heifWithLocations(1, 8, 8, 8, 0, overlapIDAT, heifLocation{id: 102, typ: "Exif", method: 1, base: ^uint64(0) - 1, extents: []heifExtent{{off: 2, n: uint64(len(item))}}}),
		"extent end wraps":        heifWithLocations(1, 8, 8, 0, 0, make([]byte, 16), heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{off: ^uint64(0) - 1, n: 8}}}),
		"over budget repeats":     heifWithLocations(1, 4, 4, 0, 0, repeated, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(repeated))}, {n: uint64(len(repeated))}}}),
		"truncated non-target extents": heifWithLocations(1, 4, 4, 0, 0, item,
			heifLocation{id: 101, typ: "hvc1", method: 0, extentCount: 3},
			heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}},
		),
		"zero-width extent flood": heifWithLocations(1, 0, 0, 0, 0, nil,
			heifLocation{id: 101, typ: "hvc1", method: 0, extentCount: 65535},
			heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{}}},
		),
	}
	for name, b := range unknown {
		got := readPhoto(t, b)
		if got.Taken != "" || got.HasGPS || got.Orientation != 1 {
			t.Errorf("%s: %+v", name, got)
		}
	}

	withSkippedImage := heifWithLocations(1, 8, 8, 0, 0, item,
		heifLocation{id: 101, typ: "hvc1", method: 0, extents: []heifExtent{{off: 1 << 40, n: 64}}},
		heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}},
	)
	if got := readPhoto(t, withSkippedImage); got.Taken != want.Taken || !got.HasGPS {
		t.Errorf("Exif after unavailable image item: %+v", got)
	}

	manyExtents := make([]heifExtent, 65535)
	withManySkippedExtents := heifWithLocations(1, 4, 4, 0, 0, item,
		heifLocation{id: 101, typ: "hvc1", method: 0, extents: manyExtents},
		heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(item))}}},
	)
	if got := readPhoto(t, withManySkippedExtents); got.Taken != want.Taken || !got.HasGPS {
		t.Errorf("Exif after many skipped extents: %+v", got)
	}

	budgetItem := heifExifItem(want, false, 1<<20-4-6-len(phototest.TIFF(want, false)))
	atBudget := heifWithLocations(1, 4, 4, 0, 0, budgetItem, heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{n: uint64(len(budgetItem))}}})
	if got := readPhoto(t, atBudget); got.Taken != want.Taken || !got.HasGPS || got.Orientation != want.Orientation {
		t.Errorf("budget boundary: %+v", got)
	}
}

func FuzzRead(f *testing.F) {
	meta := photo.Meta{Taken: "2026-10-05 12:34:56", Lat: 38.7139, Lon: -9.1394, HasGPS: true, Orientation: 6}
	for _, seed := range [][]byte{
		{},
		[]byte("%PDF-1.7 hello"),
		phototest.JPEG(8, 8, meta, false),
		phototest.HEIC(meta, false, false),
		phototest.HEICLocationVersion(meta, false, true, 2),
		heifWithLocations(1, 8, 8, 0, 0, make([]byte, 16), heifLocation{id: 102, typ: "Exif", method: 1, extents: []heifExtent{{off: ^uint64(0) - 1, n: 8}}}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, b []byte) { photo.Read(b) })
}

type heifExtent struct{ off, n uint64 }

type heifLocation struct {
	id          uint32
	typ         string
	method      uint16
	dataRef     uint16
	base        uint64
	extentCount int
	extents     []heifExtent
}

func readPhoto(t *testing.T, b []byte) (m photo.Meta) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("photo.Read panicked: %v", r)
		}
	}()
	return photo.Read(b)
}

func heifExifItem(m photo.Meta, little bool, pad int) []byte {
	tiff := phototest.TIFF(m, little)
	item := binary.BigEndian.AppendUint32(nil, uint32(6+pad))
	item = append(item, "Exif\x00\x00"...)
	item = append(item, make([]byte, pad)...)
	return append(item, tiff...)
}

func heifWithLocations(version byte, offSize, lenSize, baseSize, idxSize int, idat []byte, entries ...heifLocation) []byte {
	iloc := []byte{version, 0, 0, 0, byte(offSize<<4 | lenSize), byte(baseSize<<4 | idxSize)}
	if version == 2 {
		iloc = binary.BigEndian.AppendUint32(iloc, uint32(len(entries)))
	} else {
		iloc = binary.BigEndian.AppendUint16(iloc, uint16(len(entries)))
	}
	for _, e := range entries {
		if version == 2 {
			iloc = binary.BigEndian.AppendUint32(iloc, e.id)
		} else {
			iloc = binary.BigEndian.AppendUint16(iloc, uint16(e.id))
		}
		if version == 1 || version == 2 {
			iloc = binary.BigEndian.AppendUint16(iloc, e.method)
		}
		iloc = binary.BigEndian.AppendUint16(iloc, e.dataRef)
		iloc = appendSized(iloc, baseSize, e.base)
		extentCount := len(e.extents)
		if e.extentCount != 0 {
			extentCount = e.extentCount
		}
		iloc = binary.BigEndian.AppendUint16(iloc, uint16(extentCount))
		for _, ex := range e.extents {
			iloc = appendSized(iloc, idxSize, 0)
			iloc = appendSized(iloc, offSize, ex.off)
			iloc = appendSized(iloc, lenSize, ex.n)
		}
	}
	hdlr := heifTestBox("hdlr", []byte{0, 0, 0, 0, 0, 0, 0, 0}, []byte("pict"), make([]byte, 13))
	iinf := []byte{0, 0, 0, 0}
	iinf = binary.BigEndian.AppendUint16(iinf, uint16(len(entries)))
	for _, e := range entries {
		iinf = append(iinf, heifTestInfe(e.id, e.typ)...)
	}
	parts := [][]byte{{0, 0, 0, 0}, hdlr, heifTestBox("iinf", iinf), heifTestBox("iloc", iloc)}
	if idat != nil {
		parts = append(parts, heifTestBox("idat", idat))
	}
	return append(heifTestBox("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), heifTestBox("meta", parts...)...)
}

func appendSized(b []byte, size int, v uint64) []byte {
	for i := size - 1; i >= 0; i-- {
		b = append(b, byte(v>>uint(i*8)))
	}
	return b
}

func heifTestInfe(id uint32, typ string) []byte {
	var b []byte
	if id > math.MaxUint16 {
		b = []byte{3, 0, 0, 0}
		b = binary.BigEndian.AppendUint32(b, id)
	} else {
		b = []byte{2, 0, 0, 0}
		b = binary.BigEndian.AppendUint16(b, uint16(id))
	}
	b = binary.BigEndian.AppendUint16(b, 0)
	return heifTestBox("infe", append(append(b, typ...), 0))
}

func heifTestBox(typ string, body ...[]byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, 0)
	b = append(b, typ...)
	for _, x := range body {
		b = append(b, x...)
	}
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	return b
}

func TestGPSReferences(t *testing.T) {
	for _, little := range []bool{false, true} {
		for _, lat := range []float64{-12, 0, 12} {
			for _, lon := range []float64{-34, 0, 34} {
				want := photo.Meta{Taken: "2026-10-05 12:34:56", Orientation: 6, Lat: lat, Lon: lon, HasGPS: true}
				for name, b := range map[string][]byte{"JPEG": phototest.JPEG(8, 8, want, little), "HEIC mdat": phototest.HEIC(want, little, false), "HEIC idat": phototest.HEIC(want, little, true)} {
					got := photo.Read(b)
					if got.HasGPS != (lat != 0 || lon != 0) || !near(got.Lat, lat) || !near(got.Lon, lon) {
						t.Fatalf("%s little=%v valid: %+v", name, little, got)
					}
					for _, axis := range []uint16{1, 3} {
						for _, bad := range []struct {
							name, value string
							typ         uint16
							count       uint32
							absent      bool
						}{
							{"absent", "", 2, 2, true}, {"empty", "", 2, 2, false}, {"wrong axis", "E\x00", 2, 2, false}, {"text", "South", 2, 4, false}, {"prefix", "Sx", 2, 2, false}, {"type", "S\x00", 1, 2, false}, {"short count", "S", 2, 1, false}, {"long count", "S\x00\x00", 2, 3, false}, {"truncated", "", 2, 8, false}, {"lowercase", "s\x00", 2, 2, false},
						} {
							if axis == 3 && bad.name == "wrong axis" {
								bad.value = "N\x00"
							}
							got = photo.Read(phototest.GPSReference(b, axis, bad.typ, bad.count, bad.value, bad.absent))
							if got.HasGPS || got.Lat != 0 || got.Lon != 0 || got.Taken != want.Taken || got.Orientation != want.Orientation {
								t.Errorf("%s little=%v axis=%d %s: %+v", name, little, axis, bad.name, got)
							}
						}
					}
				}
			}
		}
	}
}
