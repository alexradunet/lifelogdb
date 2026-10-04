package photo_test

import (
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
