package photo_test

import (
	"encoding/binary"
	"testing"

	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestReadsAJPEGAndAHEIC(t *testing.T) {
	want := photo.Meta{Taken: "2019-06-03 07:41:12", Orientation: 6}
	south := photo.Meta{Taken: "2024-01-31 23:59:59", Orientation: 1}
	for _, little := range []bool{false, true} {
		for name, b := range map[string][]byte{
			"JPEG":         phototest.WithExif(phototest.JPEG(40, 30), want, little),
			"HEIC in mdat": phototest.HEIF(want, phototest.Layout{Little: little, Version: 1}),
			"HEIC in idat": phototest.HEIF(want, phototest.Layout{Little: little, InIdat: true, Version: 1}),
		} {
			got := photo.Read(b)
			if got.Taken != want.Taken || got.Orientation != 6 || got.Day() != "2019-06-03" {
				t.Errorf("%s (little-endian %v): %+v", name, little, got)
			}
		}
		if got := photo.Read(phototest.WithExif(phototest.JPEG(8, 8), south, little)); got.Taken != south.Taken {
			t.Errorf("south and east (little-endian %v): %+v", little, got)
		}
	}
}

func TestSaysNothingItDoesNotKnow(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":           {},
		"no EXIF":         phototest.JPEG(8, 8),
		"not a photo":     []byte("%PDF-1.7 hello"),
		"a date alone":    phototest.WithExif(phototest.JPEG(8, 8), photo.Meta{Taken: "2019-06-03 07:41:12"}, true),
		"a HEIC's head":   phototest.HEIF(photo.Meta{Taken: "2019-06-03 07:41:12"}, phototest.Layout{Version: 1})[:200],
		"a JPEG's head":   phototest.WithExif(phototest.JPEG(8, 8), photo.Meta{Taken: "2019-06-03 07:41:12"}, false)[:40],
		"garbage in ftyp": append([]byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00"), 0xFF, 0xFF, 0xFF, 0xFF, 'm', 'e', 't', 'a'),
	} {
		got := photo.Read(b)
		if got.Orientation != 1 && got.Orientation != 0 {
			t.Errorf("%s: %+v", name, got)
		}
		if name != "a date alone" && got.Taken != "" {
			t.Errorf("%s: a date from nowhere: %+v", name, got)
		}
	}
	if got := photo.Read(phototest.WithExif(phototest.JPEG(8, 8), photo.Meta{Taken: "2019-06-03 07:41:12"}, true)); got.Day() != "2019-06-03" {
		t.Errorf("a date alone: %+v", got)
	}
	if (photo.Meta{}).Day() != "" {
		t.Error("no date, no day")
	}
}

func TestHEIFLocationVersions(t *testing.T) {
	want := photo.Meta{Taken: "2026-10-05 12:34:56", Orientation: 8}
	item := heifExifItem(want, true, 0)
	split := len(item) / 2
	for name, b := range map[string][]byte{
		"version 0 in file":          phototest.HEIF(want, phototest.Layout{Little: true, Version: 0}),
		"version 1 in file":          phototest.HEIF(want, phototest.Layout{Little: true, Version: 1}),
		"version 1 in idat":          phototest.HEIF(want, phototest.Layout{Little: true, InIdat: true, Version: 1}),
		"version 2 in file":          phototest.HEIF(want, phototest.Layout{Little: true, Version: 2}),
		"version 2 in idat":          phototest.HEIF(want, phototest.Layout{Little: true, InIdat: true, Version: 2}),
		"version 1 repeated extents": phototest.HEIFItems(1, 4, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(split)}, {Off: uint64(split), N: uint64(len(item) - split)}}}),
	} {
		got := readPhoto(t, b)
		if got.Taken != want.Taken || got.Orientation != want.Orientation {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestHEIFLocationBounds(t *testing.T) {
	want := photo.Meta{Taken: "2026-10-05 12:34:56", Orientation: 6}
	item := heifExifItem(want, false, 0)
	valid := phototest.HEIFItems(1, 4, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}})
	overlapIDAT := append([]byte{0}, item...)
	repeated := make([]byte, (1<<19)+1)
	unknown := map[string][]byte{
		"truncated fields":        valid[:len(valid)-1],
		"unsupported version":     phototest.HEIFItems(3, 4, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}}),
		"unsupported size":        phototest.HEIFItems(1, 3, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}}),
		"unsupported method":      phototest.HEIFItems(1, 4, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 2, Extents: []phototest.Extent{{N: uint64(len(item))}}}),
		"external data reference": phototest.HEIFItems(1, 4, 4, 0, 0, item, phototest.Location{ID: 102, Type: "Exif", Method: 1, DataRef: 7, Extents: []phototest.Extent{{N: uint64(len(item))}}}),
		"base plus offset wraps":  phototest.HEIFItems(1, 8, 8, 8, 0, overlapIDAT, phototest.Location{ID: 102, Type: "Exif", Method: 1, Base: ^uint64(0) - 1, Extents: []phototest.Extent{{Off: 2, N: uint64(len(item))}}}),
		"extent end wraps":        phototest.HEIFItems(1, 8, 8, 0, 0, make([]byte, 16), phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{Off: ^uint64(0) - 1, N: 8}}}),
		"over budget repeats":     phototest.HEIFItems(1, 4, 4, 0, 0, repeated, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(repeated))}, {N: uint64(len(repeated))}}}),
		"truncated non-target extents": phototest.HEIFItems(1, 4, 4, 0, 0, item,
			phototest.Location{ID: 101, Type: "hvc1", Method: 0, ExtentCount: 3},
			phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}},
		),
		"zero-width extent flood": phototest.HEIFItems(1, 0, 0, 0, 0, nil,
			phototest.Location{ID: 101, Type: "hvc1", Method: 0, ExtentCount: 65535},
			phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{}}},
		),
	}
	for name, b := range unknown {
		got := readPhoto(t, b)
		if got.Taken != "" || got.Orientation != 1 {
			t.Errorf("%s: %+v", name, got)
		}
	}

	withSkippedImage := phototest.HEIFItems(1, 8, 8, 0, 0, item,
		phototest.Location{ID: 101, Type: "hvc1", Method: 0, Extents: []phototest.Extent{{Off: 1 << 40, N: 64}}},
		phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}},
	)
	if got := readPhoto(t, withSkippedImage); got.Taken != want.Taken {
		t.Errorf("Exif after unavailable image item: %+v", got)
	}

	manyExtents := make([]phototest.Extent, 65535)
	withManySkippedExtents := phototest.HEIFItems(1, 4, 4, 0, 0, item,
		phototest.Location{ID: 101, Type: "hvc1", Method: 0, Extents: manyExtents},
		phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(item))}}},
	)
	if got := readPhoto(t, withManySkippedExtents); got.Taken != want.Taken {
		t.Errorf("Exif after many skipped Extents: %+v", got)
	}

	budgetItem := heifExifItem(want, false, 1<<20-4-6-len(phototest.TIFF(want, false)))
	atBudget := phototest.HEIFItems(1, 4, 4, 0, 0, budgetItem, phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{N: uint64(len(budgetItem))}}})
	if got := readPhoto(t, atBudget); got.Taken != want.Taken || got.Orientation != want.Orientation {
		t.Errorf("budget boundary: %+v", got)
	}
}

func FuzzRead(f *testing.F) {
	meta := photo.Meta{Taken: "2026-10-05 12:34:56", Orientation: 6}
	for _, seed := range [][]byte{
		{},
		[]byte("%PDF-1.7 hello"),
		phototest.WithExif(phototest.JPEG(8, 8), meta, false),
		phototest.HEIF(meta, phototest.Layout{Version: 1}),
		phototest.HEIF(meta, phototest.Layout{InIdat: true, Version: 2}),
		phototest.HEIFItems(1, 8, 8, 0, 0, make([]byte, 16), phototest.Location{ID: 102, Type: "Exif", Method: 1, Extents: []phototest.Extent{{Off: ^uint64(0) - 1, N: 8}}}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, b []byte) { photo.Read(b) })
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
