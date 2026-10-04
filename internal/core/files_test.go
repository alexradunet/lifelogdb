package core

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"strings"
	"testing"
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
	if _, err := s.Rename(ctx, "cli", k.ID, "Lake 2.jpg"); status(err) != 409 {
		t.Errorf("a file page was renamed: %v", err)
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
