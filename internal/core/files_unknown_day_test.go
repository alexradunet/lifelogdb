package core

import (
	"strings"
	"testing"

	"lifelog/internal/photo/phototest"
)

// photo is a kept photo as the writer sees it: a JPEG with a preview, taken on day. Its position is never read (D21).
func photo(t *testing.T, c byte, title, day string) FileIn {
	return FileIn{Title: title, SHA256: sha(c), MIME: "image/jpeg", Taken: day, Preview: phototest.JPEG(8, 8)}
}

// TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects: a photo with no Day and no Taken has an unknown capture day. keep
// finds that from the type and the days alone: the caller does not say it.
func TestUnknownPhotoDayHasNoCaptureOrPlaceSideEffects(t *testing.T) {
	s := fresh(t)
	in := FileIn{Title: "Synthetic photo.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("a", 64), Body: "Selected picture"}
	kept, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.PageByID(ctx, kept.ID)
	if err != nil || page.Day != "" || kept.Day != "" || kept.Embedded {
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
	promoted, err := s.AddFile(ctx, "cli", FileIn{Title: "Promotion.jpg", MIME: "image/jpeg", SHA256: strings.Repeat("b", 64)})
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
	in := photo(t, 'c', "Owner.jpg", "2012-03-04")
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
	unknown := FileIn{Title: "Different.jpg", MIME: "image/jpeg", SHA256: in.SHA256, Body: "Do not replace", Preview: preview}
	before, err := countRows(ctx, s)
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
	if err != nil || again.ID != kept.ID || again.StoredDay != "2012-03-04" || again.Embedded {
		t.Fatalf("known attribution %+v %v", again, err)
	}
	after, err := s.PageByID(ctx, kept.ID)
	if err != nil || after.Day != prior.Day || after.Title != prior.Title || after.Body != prior.Body {
		t.Fatal("dedup replaced ownership")
	}
	counts, err := countRows(ctx, s)
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
