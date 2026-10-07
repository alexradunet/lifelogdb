package core

import (
	"strings"
	"testing"
)

func TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects(t *testing.T) {
	s := fresh(t)
	in := FileIn{Title: "Synthetic photo.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("a", 64), Body: "Selected picture", UnknownCaptureDay: true, HasGPS: true, Lat: 10, Lon: 20}
	before, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refused := in
	refused.At = "Uncreated place"
	if _, err := s.AddFile(ctx, "cli", refused); err == nil {
		t.Fatal("unknown day created selected place")
	}
	after, err := s.Counts(ctx)
	if err != nil || after.Pages != before.Pages || after.Links["at"] != before.Links["at"] {
		t.Fatalf("refusal effects %+v %v", after, err)
	}
	kept, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.PageByID(ctx, kept.ID)
	if err != nil || page.Day != "" || kept.Day != "" || kept.Linked || kept.Embedded || kept.PointSet {
		t.Fatalf("unknown capture %+v %+v %v", kept, page, err)
	}
	var null bool
	if err := s.DB.R.QueryRow("SELECT day IS NULL FROM entities WHERE id=?", kept.ID).Scan(&null); err != nil || !null {
		t.Fatalf("day is not SQL NULL %v %v", null, err)
	}
	in.Body = "Retry must preserve owner text"
	again, err := s.AddFile(ctx, "cli", in)
	if err != nil || again.ID != kept.ID {
		t.Fatalf("retry %+v %v", again, err)
	}
	page, err = s.PageByID(ctx, kept.ID)
	if err != nil || page.Body != "Selected picture" || page.Day != "" {
		t.Fatalf("retry changed owner %+v %v", page, err)
	}
	var plain int64
	err = s.Do(ctx, "cli", func(tx *Tx) error {
		var e error
		plain, _, _, e = tx.CreatePage("Promotion.jpg", "Owner body", "2012-03-04", "")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.PageByID(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := s.AddFile(ctx, "cli", FileIn{Title: "Promotion.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("b", 64), UnknownCaptureDay: true})
	if err != nil || promoted.ID != plain || promoted.StoredDay != original.Day || promoted.Day != "" {
		t.Fatalf("promotion attribution %+v %v", promoted, err)
	}
	page, err = s.PageByID(ctx, plain)
	if err != nil || page.Day != "2012-03-04" || page.Day != original.Day || page.Body != original.Body {
		t.Fatalf("promotion changed owner %+v %v", page, err)
	}
}

func TestUnknownPhotoDryRunDedupKnownDayMissingPreviewAndTombstone(t *testing.T) {
	s := fresh(t)
	in := photo(t, 'c', "Owner.jpg", "2012-03-04", 0, 0)
	in.HasGPS = false
	preview := in.Preview
	in.Preview = nil
	kept, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown := FileIn{Title: "Different.jpg", MIME: "image/jpeg", SHA256: in.SHA256, Body: "Do not replace", UnknownCaptureDay: true, Preview: preview, HasGPS: true, Lat: 30, Lon: 40}
	before, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tried, err := s.TryAddFile(ctx, "cli", unknown)
	if err != nil || tried.ID != kept.ID || tried.StoredDay != "2012-03-04" || tried.Day != "" {
		t.Fatalf("dry dedup %+v %v", tried, err)
	}
	raw, err := s.Preview(ctx, kept.ID)
	if err != nil || raw != nil {
		t.Fatal("dry run filled preview")
	}
	again, err := s.AddFile(ctx, "cli", unknown)
	if err != nil || again.ID != kept.ID || again.StoredDay != "2012-03-04" || again.Embedded || again.Linked || again.PointSet {
		t.Fatalf("known attribution %+v %v", again, err)
	}
	after, err := s.PageByID(ctx, kept.ID)
	if err != nil || after.Day != prior.Day || after.Title != prior.Title || after.Body != prior.Body {
		t.Fatal("dedup replaced ownership")
	}
	counts, err := s.Counts(ctx)
	if err != nil || counts.Pages != before.Pages || counts.Links["at"] != before.Links["at"] {
		t.Fatal("unknown dedup incidental writes")
	}
	if err = s.Tombstone(ctx, "cli", kept.ID); err != nil {
		t.Fatal(err)
	}
	again, err = s.AddFile(ctx, "cli", unknown)
	if err != nil || !again.Deleted || again.StoredDay != prior.Day {
		t.Fatalf("tombstone retry %+v %v", again, err)
	}
	after, err = s.PageByID(ctx, kept.ID)
	if err != nil || after.DeletedAt == "" || after.Day != prior.Day {
		t.Fatal("unknown retry revived tombstone")
	}
}
