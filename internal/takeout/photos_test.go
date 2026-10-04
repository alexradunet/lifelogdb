package takeout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

// export writes a synthetic Google Photos folder: every sidecar form, a duplicate, an edited copy, a video, a file with
// no sidecar, a sidecar with no file, an album holding a photo of a year folder. No real export is read.
func export(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(rel string, b []byte) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	side := func(title, geo, exifGeo, desc string, people bool) []byte {
		s := `{"title": "` + title + `", "description": "` + desc + `", "photoTakenTime": {"timestamp": "1559547672", "formatted": "x"},` +
			` "geoData": ` + geo + `, "geoDataExif": ` + exifGeo
		if people {
			s += `, "people": [{"name": "Secret Person"}]`
		}
		return []byte(s + `}`)
	}
	zero := `{"latitude": 0.0, "longitude": 0.0, "altitude": 0.0}`
	lisbon := `{"latitude": 38.7139, "longitude": -9.1394, "altitude": 10.0}`
	jpg := phototest.JPEG(32, 24, photo.Meta{Taken: "2019-06-03 07:41:12", Lat: 38.7139, Lon: -9.1394, HasGPS: true}, true)
	y := "Photos from 2019/"
	put(y+"IMG_1.jpg", jpg)
	put(y+"IMG_1.jpg.json", side("IMG_1.jpg", lisbon, lisbon, "A secret caption", true))
	put(y+"IMG_2.HEIC", phototest.HEIC(photo.Meta{Taken: "2019-06-04 10:00:00", Lat: 41.1, Lon: -8.6, HasGPS: true}, false, false))
	put(y+"IMG_2.HEIC.supplemental-metadata.json", side("IMG_2.HEIC", zero, lisbon, "", false))
	put(y+"PXL_20190603_074112345.MP.jpg", phototest.JPEG(8, 8, photo.Meta{Taken: "2019-06-03 08:00:00"}, false))
	put(y+"PXL_20190603_074112345.MP.jpg.supplemental-met.json", side("PXL_20190603_074112345.MP.jpg", zero, zero, "", false))
	put(y+"IMG_3(1).jpg", phototest.JPEG(9, 9, photo.Meta{}, false))
	put(y+"IMG_3.jpg(1).json", side("IMG_3.jpg", zero, zero, "", false))
	put(y+"IMG_4.jpg", phototest.JPEG(10, 10, photo.Meta{}, false))
	put(y+"IMG_4-edited.jpg", phototest.JPEG(11, 11, photo.Meta{}, false))
	put(y+"IMG_4.jpg.json", side("IMG_4.jpg", zero, zero, "", false))
	long := "a_very_long_name_that_google_cut_short_in_the_sidecar.jpg"
	put(y+long, phototest.JPEG(12, 12, photo.Meta{}, false))
	put(y+long[:44]+".json", side(long, zero, zero, "", false))
	put(y+"VID_5.mp4", []byte("not really a video"))
	put(y+"VID_5.mp4.json", side("VID_5.mp4", lisbon, zero, "", false))
	put(y+"lonely.jpg", phototest.JPEG(13, 13, photo.Meta{}, false))
	put(y+"orphan.json", side("gone.jpg", zero, zero, "", false))
	a := "Trip to Secret Place/"
	put(a+"metadata.json", []byte(`{"title": "Trip to Secret Place", "description": "", "access": "protected"}`))
	put(a+"IMG_1.jpg", jpg)
	put(a+"IMG_1.jpg.json", side("IMG_1.jpg", lisbon, lisbon, "A secret caption", false))
	return root
}

func TestScanPairsEverySidecarForm(t *testing.T) {
	media, sidecars, _, err := Scan(export(t))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range media {
		got[m.Path] = m.Form + " → " + m.Sidecar
	}
	y := "Photos from 2019/"
	for p, want := range map[string]string{
		y + "IMG_1.jpg":                     FormName + " → " + y + "IMG_1.jpg.json",
		y + "IMG_2.HEIC":                    FormSupplemental + " → " + y + "IMG_2.HEIC.supplemental-metadata.json",
		y + "PXL_20190603_074112345.MP.jpg": FormTruncated + " → " + y + "PXL_20190603_074112345.MP.jpg.supplemental-met.json",
		y + "IMG_3(1).jpg":                  FormDuplicate + " → " + y + "IMG_3.jpg(1).json",
		y + "IMG_4.jpg":                     FormName + " → " + y + "IMG_4.jpg.json",
		y + "IMG_4-edited.jpg":              FormEdited + " → " + y + "IMG_4.jpg.json",
		y + "a_very_long_name_that_google_cut_short_in_the_sidecar.jpg": FormTitle + " → " + y + "a_very_long_name_that_google_cut_short_in_th.json",
		y + "VID_5.mp4":                  FormName + " → " + y + "VID_5.mp4.json",
		y + "lonely.jpg":                 FormNone + " → ",
		"Trip to Secret Place/IMG_1.jpg": FormName + " → Trip to Secret Place/IMG_1.jpg.json",
	} {
		if got[p] != want {
			t.Errorf("%s: %q, want %q", p, got[p], want)
		}
	}
	if len(media) != 10 {
		t.Errorf("%d media files, want 10", len(media))
	}
	if _, ok := sidecars["Trip to Secret Place/metadata.json"]; ok {
		t.Error("an album's metadata.json was read as a photo's sidecar")
	}
	if s := sidecars[y+"IMG_1.jpg.json"]; s == nil || !s.GeoData.Known() || s.Description != "A secret caption" {
		t.Errorf("a sidecar's fields: %+v", s)
	}
}

func TestInventoryCountsAndNamesNothing(t *testing.T) {
	inv, err := Take(export(t))
	if err != nil {
		t.Fatal(err)
	}
	want := Inventory{Folders: 2, YearFolders: 1, MetadataFolders: 1, JSONFiles: 10, Sidecars: 9, SharedSidecars: 1, Unclaimed: 1,
		GeoData: 3, GeoDataExifOnly: 1, NoPosition: 5, TakenTime: 9, Descriptions: 2, WithPeople: 1, SameNameAndSize: 1}
	if inv.Folders != want.Folders || inv.YearFolders != want.YearFolders || inv.MetadataFolders != want.MetadataFolders ||
		inv.JSONFiles != want.JSONFiles || inv.Sidecars != want.Sidecars || inv.SharedSidecars != want.SharedSidecars ||
		inv.Unclaimed != want.Unclaimed || inv.GeoData != want.GeoData || inv.GeoDataExifOnly != want.GeoDataExifOnly ||
		inv.NoPosition != want.NoPosition || inv.TakenTime != want.TakenTime || inv.Descriptions != want.Descriptions ||
		inv.WithPeople != want.WithPeople || inv.SameNameAndSize != want.SameNameAndSize {
		t.Errorf("counts: %+v", inv)
	}
	if inv.Media[".jpg"] != 8 || inv.Media[".heic"] != 1 || inv.Media[".mp4"] != 1 {
		t.Errorf("media by extension: %v", inv.Media)
	}
	if inv.FoundBy[FormName] != 4 || inv.FoundBy[FormNone] != 1 || inv.FoundBy[FormTitle] != 1 || inv.FoundBy[FormTruncated] != 1 {
		t.Errorf("found by: %v", inv.FoundBy)
	}
	if e := inv.Exif["jpeg"]; e.Files != 8 || e.Date != 3 || e.GPS != 2 {
		t.Errorf("JPEG EXIF: %+v", e)
	}
	if e := inv.Exif["heic"]; e.Files != 1 || e.Date != 1 || e.GPS != 1 {
		t.Errorf("HEIC EXIF: %+v", e)
	}
	if inv.Keys["photoTakenTime"] != 9 || inv.Keys["people"] != 1 {
		t.Errorf("keys: %v", inv.Keys)
	}
	b, _ := json.Marshal(inv)
	for _, secret := range []string{"Secret", "IMG_", "PXL", "lonely", "38.71", "1559547672", "caption", "2019-06", "Lisbon"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the inventory names %q: %s", secret, b)
		}
	}
}
