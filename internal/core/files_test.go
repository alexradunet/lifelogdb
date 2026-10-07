package core

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"lifelog/internal/text"
)

func smallJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sha(c byte) string { return strings.Repeat(string(c), 64) }

func TestAddFileCreatesAFilePage(t *testing.T) {
	s := fresh(t)
	pic := smallJPEG(t)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04 Lake.jpg", SHA256: strings.ToUpper(sha('a')), MIME: "image/HEIC",
		Body: "The lake at dawn with [[Sam]].", Day: "2026-10-04", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	if k.Existing || k.Promoted || strings.Join(k.Sync.Created, ",") != "Sam" {
		t.Errorf("kept: %+v", k)
	}
	p, err := s.PageByID(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "file" || p.Day != "2026-10-04" || p.File == nil || p.File.SHA256 != sha('a') || p.File.MIME != "image/heic" || !p.File.Preview {
		t.Errorf("page: %+v, file %+v", p, p.File)
	}
	if got, _ := s.Preview(ctx, k.ID); !bytes.Equal(got, pic) {
		t.Error("the preview is not the one kept")
	}
	if got, _ := s.PreviewByTitle(ctx, "2026-10-04 LAKE.jpg"); !bytes.Equal(got, pic) {
		t.Error("the preview is not found by its title's key")
	}
	if got, _ := s.PreviewByTitle(ctx, "Nothing"); got != nil {
		t.Error("a title with no file has a preview")
	}
}

func TestAddFileKeepsAnOriginalOnce(t *testing.T) {
	s := fresh(t)
	k, err := s.AddFile(ctx, "api", FileIn{Title: "Memo.m4a", SHA256: sha('b'), MIME: "audio/mp4", Body: "a transcript"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddFile(ctx, "import:audio", FileIn{Title: "Other name.m4a", SHA256: sha('b'), MIME: "audio/mp4", Body: "another text"})
	if err != nil || !again.Existing || again.ID != k.ID {
		t.Fatalf("the same original from another source: %+v %v", again, err)
	}
	if id, _ := s.PageID(ctx, "Other name.m4a"); id != 0 {
		t.Error("the re-send made a page")
	}
	// a missing preview is filled; an existing one is never replaced
	pic := smallJPEG(t)
	if k2, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('b'), MIME: "audio/mp4", Preview: pic}); !k2.PreviewAdded {
		t.Errorf("a missing preview was not added: %+v", k2)
	}
	other := append(append([]byte{}, pic[:len(pic)-2]...), 0, 0xFF, 0xD9)
	if k3, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('b'), MIME: "audio/mp4", Preview: other}); k3.PreviewAdded {
		t.Error("an existing preview was replaced")
	}
	if got, _ := s.Preview(ctx, k.ID); !bytes.Equal(got, pic) {
		t.Error("the preview changed")
	}
	// a tombstoned file is left alone
	k4, _ := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"})
	if err := s.Tombstone(ctx, "cli", k4.ID); err != nil {
		t.Fatal(err)
	}
	if k5, _ := s.AddFile(ctx, "cli", FileIn{SHA256: sha('c'), MIME: "application/pdf", Preview: pic}); !k5.Existing || !k5.Deleted || k5.PreviewAdded {
		t.Errorf("a tombstoned file: %+v", k5)
	}
}

func TestAddFilePromotesAGhost(t *testing.T) {
	s := fresh(t)
	day, _, err := s.Capture(ctx, "cli", "2026-10-04", "Dawn: ![[Lake.jpg]]", nil)
	if err != nil {
		t.Fatal(err)
	}
	ghost, _ := s.PageID(ctx, "Lake.jpg")
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "lake.jpg", SHA256: sha('d'), MIME: "image/jpeg", Body: "With [[Bob]]"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, ghost)
	if !k.Promoted || k.ID != ghost || p.Type != "file" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]" || titles(p.Out, "wikilink") != "Bob" {
		t.Errorf("promoted: %+v, page %+v", k, p)
	}
	if titles(p.In, "wikilink") != "2026-10-04" {
		t.Errorf("the day's embed does not land on the file: %v (day %d)", p.In, day)
	}
	// a page with text keeps it; text on both sides is refused
	if _, _, err := s.CreatePage(ctx, "cli", "Notes.pdf", "my own words"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Notes.pdf", SHA256: sha('e'), MIME: "application/pdf", Body: "OCR text"}); status(err) != 409 {
		t.Errorf("two texts merged: %v", err)
	}
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "Notes.pdf", SHA256: sha('e'), MIME: "application/pdf"})
	if p, _ := s.PageByID(ctx, k.ID); err != nil || p.Body != "my own words" || p.Type != "file" {
		t.Errorf("a page with text, no text sent: %v %+v", err, p)
	}
}

func TestFilePromotionDay(t *testing.T) {
	s := fresh(t)
	dayLists := func(day, title string) bool {
		t.Helper()
		d, err := s.Day(ctx, day)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range d.Rows {
			if strings.HasPrefix(r.What, "page") && r.Detail == title {
				return true
			}
		}
		return false
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-04", "Dawn: ![[Lake.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	ghost, _ := s.PageID(ctx, "Lake.jpg")
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "lake.jpg", SHA256: sha('0'), MIME: "image/jpeg", Body: "With [[Bob]].", Day: "2026-10-04"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, ghost)
	if !k.Promoted || k.ID != ghost || p.Type != "file" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]." || p.Day != "2026-10-04" || titles(p.In, "wikilink") != "2026-10-04" || titles(p.Out, "wikilink") != "Bob" {
		t.Errorf("explicit-day promotion: kept %+v, page %+v", k, p)
	}
	if !dayLists("2026-10-04", "Lake.jpg") {
		t.Error("the promoted explicit-day file is absent from its day view")
	}
	again, err := s.AddFile(ctx, "cli", FileIn{Title: "Other Lake.jpg", SHA256: sha('0'), MIME: "image/jpeg", Day: "2026-10-06"})
	if err != nil || !again.Existing || again.ID != ghost {
		t.Fatalf("re-send by hash: %+v %v", again, err)
	}
	if p, _ = s.PageByID(ctx, ghost); p.Day != "2026-10-04" || p.Title != "Lake.jpg" || p.Body != "With [[Bob]]." {
		t.Errorf("re-send changed the promoted file page: %+v", p)
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-05", "Trip: ![[Taken.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	takenGhost, _ := s.PageID(ctx, "Taken.jpg")
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "taken.jpg", SHA256: sha('1'), MIME: "image/jpeg", Taken: "2026-10-05"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ = s.PageByID(ctx, takenGhost); !k.Promoted || p.Day != "2026-10-05" || !dayLists("2026-10-05", "Taken.jpg") {
		t.Errorf("taken-day promotion: kept %+v, page %+v", k, p)
	}

	var datedGhost int64
	if err := s.Do(ctx, "cli", func(t *Tx) (err error) {
		datedGhost, _, err = t.insertPage("page", "Already dated.jpg", text.TitleKey("Already dated.jpg"), "2026-10-01", "", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "Already dated.jpg", SHA256: sha('2'), MIME: "image/jpeg", Day: "2026-10-07"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ = s.PageByID(ctx, datedGhost); !k.Promoted || p.Day != "2026-10-01" || !dayLists("2026-10-01", "Already dated.jpg") || dayLists("2026-10-07", "Already dated.jpg") {
		t.Errorf("promotion overwrote an existing day: kept %+v, page %+v", k, p)
	}

	if _, _, err := s.Capture(ctx, "cli", "2026-10-08", "Dry: ![[Dry.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	dryGhost, _ := s.PageID(ctx, "Dry.jpg")
	k, err = s.TryAddFile(ctx, "cli", FileIn{Title: "dry.jpg", SHA256: sha('3'), MIME: "image/jpeg", Day: "2026-10-08"})
	if err != nil || !k.Promoted || k.ID != 0 {
		t.Fatalf("dry-run promotion report: %+v %v", k, err)
	}
	if p, _ = s.PageByID(ctx, dryGhost); p.Type != "page" || p.Day != "" || p.File != nil {
		t.Errorf("dry-run promotion wrote to storage: %+v", p)
	}

	var conflictID int64
	if err := s.Do(ctx, "cli", func(t *Tx) (err error) {
		conflictID, _, err = t.insertPage("page", "Conflict.pdf", text.TitleKey("Conflict.pdf"), "2026-10-02", "my own words", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Conflict.pdf", SHA256: sha('4'), MIME: "application/pdf", Day: "2026-10-09", Body: "OCR text"}); status(err) != 409 {
		t.Fatalf("body conflict: %v", err)
	}
	if p, _ = s.PageByID(ctx, conflictID); p.Type != "page" || p.Day != "2026-10-02" || p.Body != "my own words" || p.File != nil {
		t.Errorf("body conflict changed storage: %+v", p)
	}

	s = fresh(t)
	placeID(t, s, "Lakeside", Point{46.1, 7.2, 300, true})
	if _, _, err := s.Capture(ctx, "cli", "2099-01-01", "Undated: ![[scan.jpg]]", nil); err != nil {
		t.Fatal(err)
	}
	scanGhost, _ := s.PageID(ctx, "scan.jpg")
	before := Today()
	k, err = s.AddFile(ctx, "cli", FileIn{Title: "scan.jpg", SHA256: sha('5'), MIME: "image/jpeg", Lat: 46.1, Lon: 7.2, HasGPS: true, Preview: smallJPEG(t)})
	after := Today()
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.PageByID(ctx, scanGhost)
	if !k.Promoted || k.ID != scanGhost {
		t.Errorf("undated keep did not promote the ghost in place: %+v (ghost %d)", k, scanGhost)
	}
	if p.Day != before && p.Day != after {
		t.Errorf("undated keep used day %q, outside [%q,%q]", p.Day, before, after)
	}
	if k.Day != "" || k.Place != "Lakeside" || k.Linked || k.Embedded {
		t.Errorf("undated photo got its own-day effects: %+v", k)
	}
	if dayID, _ := s.PageID(ctx, p.Day); dayID != 0 {
		t.Errorf("undated photo made an automatic day page %d for %s", dayID, p.Day)
	}
}

func TestAddFileRefuses(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", ""); err != nil {
		t.Fatal(err)
	}
	var ex *ExistsError
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Bob Sample", SHA256: sha('a'), MIME: "image/jpeg"}); !errors.As(err, &ex) {
		t.Errorf("a person's title: %v", err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-04", "x", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "2026-10-04", SHA256: sha('a'), MIME: "image/jpeg"}); !errors.As(err, &ex) {
		t.Errorf("a day page's title: %v", err)
	}
	for name, f := range map[string]FileIn{
		"a short hash":     {Title: "A", SHA256: sha('a')[:63], MIME: "image/jpeg"},
		"a hash not hex":   {Title: "A", SHA256: sha('g'), MIME: "image/jpeg"},
		"no type":          {Title: "A", SHA256: sha('a'), MIME: ""},
		"a bad type":       {Title: "A", SHA256: sha('a'), MIME: "jpeg"},
		"a bad title":      {Title: "a/b", SHA256: sha('a'), MIME: "image/jpeg"},
		"a bad day":        {Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Day: "2026-13-01"},
		"a PNG as preview": {Title: "A", SHA256: sha('a'), MIME: "image/jpeg", Preview: []byte("\x89PNG\r\n\x1a\n")},
	} {
		if _, err := s.AddFile(ctx, "cli", f); status(err) != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: sha('f'), MIME: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed, err := s.Rename(ctx, "cli", k.ID, "Lake 2.jpg"); err != nil || renamed != k.ID {
		t.Fatalf("stable file rename: %d %v", renamed, err)
	}
	if old, err := s.PageID(ctx, "Lake.jpg"); err != nil || old != k.ID {
		t.Fatalf("old file handle: %d %v", old, err)
	}
	if err := s.Promote(ctx, "cli", k.ID, "person", ""); status(err) != 409 {
		t.Errorf("a file page was promoted: %v", err)
	}
	if r, err := s.Integrity(ctx); err != nil || !r.OK {
		t.Errorf("integrity: %+v %v", r, err)
	}
}

func TestMimeOf(t *testing.T) {
	for name, want := range map[string]string{"a.JPG": "image/jpeg", "IMG_1.heic": "image/heic", "memo.m4a": "audio/mp4", "x.pdf": "application/pdf"} {
		if got := MimeOf(name, nil); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if got := MimeOf("noext", []byte("%PDF-1.7\n")); got != "application/pdf" {
		t.Errorf("sniffed: %s", got)
	}
	if got := MimeOf("noext", []byte("plain words")); got != "text/plain" {
		t.Errorf("sniffed text: %s", got)
	}
	if NormalMIME("Text/Plain; charset=utf-8") != "text/plain" || NormalMIME("a/b/c") != "" || NormalMIME("") != "" {
		t.Error("NormalMIME")
	}
}
