package importer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedCommitPublicationRecoveryAndImmutablePair(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if e := os.WriteFile(filepath.Join(f.w.Source, "one.jpg"), syntheticSelectedJPEG(t), 0600); e != nil {
		t.Fatal(e)
	}
	mustLedger(t, f)
	p, e := f.w.DraftSelectedPhoto(ctx, "one.jpg", "", "One.jpg", "", PhotoChoices{Capture: "none", GPS: "none"})
	if e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, selectedPhotoFile); e != nil {
		t.Fatal(e)
	}
	kept, e := f.w.selectedPhoto(ctx, f.s, false, func() error { return errors.New("synthetic selected commit publication interruption") })
	if e == nil || kept.ID == 0 {
		t.Fatal("selected commit failure was not reported")
	}
	var count int
	if e = f.s.DB.R.QueryRow(`SELECT count(*) FROM files`).Scan(&count); e != nil || count != 1 {
		t.Fatalf("actual committed file %d %v", count, e)
	}
	if photos, e := f.w.appliedSelectedPhotos(ctx); e != nil || len(photos) != 0 {
		t.Fatal("reservation claimed completion")
	}
	recovered, e := f.w.SelectedPhoto(ctx, f.s, false)
	if e != nil || recovered.ID != kept.ID || !recovered.Existing {
		t.Fatalf("verified recovery %+v %v", recovered, e)
	}
	marker := filepath.Join(f.w.Dir, selectedBindingName(p)+".complete")
	if e = os.WriteFile(marker, []byte("bad"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.appliedSelectedPhotos(ctx); e == nil {
		t.Fatal("corrupt completion accepted")
	}
	if _, e = f.w.SelectedPhoto(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.DraftSelectedPhoto(ctx, "one.jpg", "", "One.jpg", "", PhotoChoices{Capture: "owner", Day: "2020-01-02", GPS: "none"}); e != nil {
		t.Fatal(e)
	}
	if e = ownerApproves(f.w, selectedPhotoFile); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.SelectedPhoto(ctx, f.s, false); e == nil {
		t.Fatal("reapproval repaired prior selected binding")
	}
	if e = f.s.DB.R.QueryRow(`SELECT count(*) FROM files`).Scan(&count); e != nil || count != 1 {
		t.Fatal("changed pair wrote SQL")
	}
}
