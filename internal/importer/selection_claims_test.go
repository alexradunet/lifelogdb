package importer

import (
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestSelectedCaptureAndPositionComparability(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	original := phototest.JPEG(3, 2, photo.Meta{Taken: "2020-01-01 23:10:00", Lat: 10, Lon: 20, HasGPS: true, Orientation: 1}, true)
	if e := os.WriteFile(filepath.Join(f.w.Source, "one.jpg"), original, 0600); e != nil {
		t.Fatal(e)
	}
	writeSource(t, f, "one.json", `{"photoTakenTime":{"timestamp":"1577920200"},"geoData":{"latitude":11,"longitude":20}}`)
	for _, tc := range []struct{ offset, comparison string }{{"", "unresolved"}, {"+00:00", "comparable agreement"}, {"+02:00", "comparable disagreement"}} {
		p, e := f.w.DraftSelectedPhoto(ctx, "one.jpg", "one.json", "One.jpg", "", PhotoChoices{Capture: "exif", GPS: "none", Offset: tc.offset})
		if e != nil || p.Evidence.Comparison != tc.comparison || p.Evidence.GPSComparison != "disagreement" {
			t.Fatalf("claims %+v %v", p, e)
		}
	}
	if _, e := f.w.DraftSelectedPhoto(ctx, "one.jpg", "one.json", "One.jpg", "", PhotoChoices{Capture: "", GPS: "sidecar"}); e == nil {
		t.Fatal("implicit claim precedence")
	}
	for _, raw := range []string{`{"photoTakenTime":{"timestamp":"253402300800"}}`, `{"photoTakenTime":{"timestamp":"1.1"}}`, `{"photoTakenTime":{"timestamp":"9223372036854775808"}}`, `{"geoData":{"latitude":null,"longitude":20}}`} {
		if _, e := decodeSidePhoto([]byte(raw)); e == nil {
			t.Fatal("invalid claim admitted")
		}
	}
}
func TestSourceUnicodeValidAndInvalidArtifactControls(t *testing.T) {
	for _, tc := range []struct{ raw, activity string }{{`"rún"`, "rún"}, {`"run\ufffd"`, "run�"}, {`"run\ud83d\ude00"`, "run😀"}} {
		body := []byte(`{"fitnessActivity":` + tc.raw + `,"startTime":"2020-01-02T00:00:00Z"}`)
		if _, e := decodeSourceProfile("fit-session-object-v1", body, &FitSessionBinding{Key: "reviewed", Day: "2020-01-02", Activity: tc.activity}); e != nil {
			t.Fatalf("valid Unicode %v", e)
		}
	}
	invalid := append(append([]byte(`{"name":"`), 255), []byte(`"}`)...)
	if validateSourceJSON(invalid) == nil {
		t.Fatal("invalid raw artifact Unicode repaired")
	}
}
