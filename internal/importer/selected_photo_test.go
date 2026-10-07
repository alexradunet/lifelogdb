package importer

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/photo"
)

func syntheticSelectedJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestSelectedPhotoOwnerPairUnknownDayFingerprintAndOriginalPreservation(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	original := syntheticSelectedJPEG(t)
	if err := os.WriteFile(filepath.Join(f.w.Source, "selected.jpg"), original, 0600); err != nil {
		t.Fatal(err)
	}
	writeSource(t, f, "supplement.json", `{"title":"Truncated other title","photoTakenTime":{"timestamp":"1577920200"},"creationTime":{"timestamp":"1700000000"},"geoData":{"latitude":10,"longitude":20},"url":"https://invalid.example/not-followed"}`)
	mustLedger(t, f)
	p, err := f.w.DraftSelectedPhoto(ctx, "selected.jpg", "supplement.json", "Selected.jpg", "Caption", PhotoChoices{Capture: "sidecar", GPS: "sidecar"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Evidence.SourceCapture != "2020-01-01T23:10:00.000Z" || p.Evidence.SourceCreation == "" || p.Evidence.Comparison != "unresolved" || p.Evidence.SidecarTitle != "Truncated other title" {
		t.Fatalf("interpretation %+v", p)
	}
	if _, err = f.w.SelectedPhoto(ctx, f.s, false); err == nil {
		t.Fatal("draft pair applied")
	}
	if err = ownerApproves(f.w, selectedPhotoFile); err != nil {
		t.Fatal(err)
	}
	before, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.SelectedPhoto(ctx, f.s, true); err != nil {
		t.Fatal(err)
	}
	after, err := f.s.Counts(ctx)
	if err != nil || after.Pages != before.Pages {
		t.Fatal("photo dry run wrote")
	}
	kept, err := f.w.SelectedPhoto(ctx, f.s, false)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.s.PageByID(ctx, kept.ID)
	if err != nil || page.Day != "" || kept.Linked || kept.Embedded || kept.PointSet {
		t.Fatalf("unknown day %+v %+v %v", kept, page, err)
	}
	preview, err := f.s.Preview(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := photo.Read(preview)
	if meta.HasGPS || meta.Taken != "" {
		t.Fatal("preview retained metadata")
	}
	if _, err = f.w.SelectedPhoto(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(f.w.Source, "selected.jpg")); err != nil || !bytes.Equal(raw, original) {
		t.Fatal("original modified")
	}
	writeSource(t, f, "supplement.json", `{"photoTakenTime":{"timestamp":"1577920201"}}`)
	if _, err = f.w.SelectedPhoto(ctx, f.s, false); err == nil {
		t.Fatal("sidecar replacement after review applied")
	}
}
func TestSelectedPhotoExplicitDayOffsetAndCreationNotCapture(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if err := os.WriteFile(filepath.Join(f.w.Source, "selected.jpg"), syntheticSelectedJPEG(t), 0600); err != nil {
		t.Fatal(err)
	}
	writeSource(t, f, "creation.json", `{"creationTime":{"timestamp":"1577920200"}}`)
	mustLedger(t, f)
	if _, err := f.w.DraftSelectedPhoto(ctx, "selected.jpg", "creation.json", "Selected.jpg", "", PhotoChoices{Capture: "sidecar", GPS: "none"}); err == nil {
		t.Fatal("creation became capture")
	}
	writeSource(t, f, "capture.json", `{"photoTakenTime":{"timestamp":"1577920200"}}`)
	if _, err := f.w.DraftSelectedPhoto(ctx, "selected.jpg", "capture.json", "Selected.jpg", "", PhotoChoices{Capture: "sidecar", GPS: "none", At: "Must not create"}); err == nil {
		t.Fatal("unknown day selected place")
	}
	if _, err := f.w.DraftSelectedPhoto(ctx, "selected.jpg", "capture.json", "Selected.jpg", "", PhotoChoices{Capture: "sidecar", GPS: "none", Offset: "+02:00"}); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, selectedPhotoFile); err != nil {
		t.Fatal(err)
	}
	kept, err := f.w.SelectedPhoto(ctx, f.s, false)
	if err != nil || kept.Day != "2020-01-02" {
		t.Fatalf("explicit offset day %+v %v", kept, err)
	}
}
