package core

import (
	"strings"
	"testing"
)

// TestSetPictureKeepsThePhotoAndShowsItFirst: a new photo is kept as a file with no day and no place, and its embed
// goes first in the page's body, so it is the person's picture. The same picture again writes nothing. A kept file
// is named by its title. A title taken that day gets a number.
func TestSetPictureKeepsThePhotoAndShowsItFirst(t *testing.T) {
	s := fresh(t)
	pic := smallJPEG(t)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", ana, "Met at school.", "1"); err != nil {
		t.Fatal(err)
	}
	first, err := s.SetPicture(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	body := func() string {
		t.Helper()
		p, err := s.PageByID(ctx, ana)
		if err != nil {
			t.Fatal(err)
		}
		return p.Body
	}
	if got, want := body(), "![[Ana 2026-10-15.jpg]]\n\nMet at school."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	f, err := s.PageByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "file" || f.Title != "Ana 2026-10-15.jpg" || f.Day != "" {
		t.Errorf("the file page = %s %q day %q, want a file with no day", f.Type, f.Title, f.Day)
	}
	var days int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM entities WHERE entity_type = 'page' AND day IS NOT NULL`).Scan(&days); err != nil || days != 0 {
		t.Errorf("day pages = %d, %v: a picture of a person is not put in a day", days, err)
	}

	if _, err := s.SetPicture(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic}); err != nil {
		t.Fatal(err)
	}
	if got := body(); strings.Count(got, "![[") != 1 {
		t.Errorf("the same picture again changed the body: %q", got)
	}

	second, err := s.SetPicture(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := body(), "![[Ana 2026-10-15 2.jpg]]\n\n![[Ana 2026-10-15.jpg]]\n\nMet at school."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	rows, err := s.NamedMentions(ctx, "person", Scope{}, 10, 0)
	if err != nil || len(rows) != 1 || rows[0].Picture != second.ID {
		t.Errorf("NamedMentions = %+v, %v, want the picture %d", rows, err, second.ID)
	}

	// A kept file by its title: the older one goes first again.
	if _, err := s.SetPictureOf(ctx, "cli", ana, "Ana 2026-10-15.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := body(); !strings.HasPrefix(got, "![[Ana 2026-10-15.jpg]]\n\n") {
		t.Errorf("body = %q, want the older picture first", got)
	}
}

// TestSetPictureRefuses: a page that does not exist, a deleted page, a picture with no preview and a title that is
// not a kept picture are refused, and nothing is written.
func TestSetPictureRefuses(t *testing.T) {
	s := fresh(t)
	pic := smallJPEG(t)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreatePerson(ctx, "cli", "Gone", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Plain", "words", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		err    error
		status int
	}{
		{"no page", func() error {
			_, e := s.SetPicture(ctx, "cli", 999, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
			return e
		}(), 404},
		{"deleted page", func() error {
			_, e := s.SetPicture(ctx, "cli", gone, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
			return e
		}(), 409},
		{"no preview", func() error {
			_, e := s.SetPicture(ctx, "cli", ana, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/heic"})
			return e
		}(), 422},
		{"a file with no picture", func() error { _, e := s.SetPictureOf(ctx, "cli", ana, "Scan.pdf"); return e }(), 422},
		{"not a file", func() error { _, e := s.SetPictureOf(ctx, "cli", ana, "Plain"); return e }(), 422},
		{"no such title", func() error { _, e := s.SetPictureOf(ctx, "cli", ana, "Nothing.jpg"); return e }(), 422},
	} {
		if status(c.err) != c.status {
			t.Errorf("%s: %v (status %d), want %d", c.name, c.err, status(c.err), c.status)
		}
	}
	var files int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM files`).Scan(&files); err != nil || files != 1 {
		t.Errorf("files = %d, %v, want only Scan.pdf", files, err)
	}
	if p, err := s.PageByID(ctx, ana); err != nil || p.Body != "" {
		t.Errorf("Ana's body = %q, %v, want it empty", p.Body, err)
	}
}

// TestNamedMentionsPictureIsTheFirstEmbedThatIsAKeptPicture: a title that names a plain page is skipped, a person
// with no embed has no picture, and every person gets the file of their own first picture.
func TestNamedMentionsPictureIsTheFirstEmbedThatIsAKeptPicture(t *testing.T) {
	s := fresh(t)
	pic := smallJPEG(t)
	a, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreatePerson(ctx, "cli", "Bo", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePerson(ctx, "cli", "Cy", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Plain", "Only words.", ""); err != nil {
		t.Fatal(err)
	}
	pa, err := s.SetPicture(ctx, "cli", a, "2026-10-15", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := s.SetPicture(ctx, "cli", b, "2026-10-15", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: pic})
	if err != nil {
		t.Fatal(err)
	}
	// Bo's body now starts with the picture; put a plain page's embed before it.
	if _, err := s.SaveBody(ctx, "cli", b, "![[Plain]]\n\n![[Bo 2026-10-15.jpg]]", "2"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.NamedMentions(ctx, "person", Scope{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Ana": pa.ID, "Bo": pb.ID, "Cy": 0}
	if len(rows) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d people", rows, len(want))
	}
	for _, r := range rows {
		if r.Picture != want[r.Title] {
			t.Errorf("%s: Picture = %d, want %d", r.Title, r.Picture, want[r.Title])
		}
	}
}
