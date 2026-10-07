package core

import (
	"bytes"
	"testing"
)

func TestDatedFileRenameKeepsOriginalAndOldEmbed(t *testing.T) {
	s := fresh(t)
	f := photo(t, 'a', "Old.jpg", "2019-06-03", 46.1, 7.2)
	f.Body = "caption [[Subject]]"
	kept, err := s.AddFile(ctx, "cli", f)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	day := dayOf(t, s, "2019-06-03")
	if day == nil {
		t.Fatal("missing day")
	}
	body := day.Body
	image, err := s.Preview(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) == 0 {
		t.Fatal("missing preview")
	}
	if got, err := s.Rename(ctx, "cli", kept.ID, "New.jpg"); err != nil || got != kept.ID {
		t.Fatalf("dated file rename: %d %v", got, err)
	}
	after, err := s.PageByID(ctx, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != before.Body || after.Day != before.Day || after.File.SHA256 != before.File.SHA256 || after.File.MIME != before.File.MIME || after.Version == before.Version {
		t.Fatalf("file changed identity/data: %+v", after)
	}
	for _, name := range []string{"Old.jpg", "New.jpg"} {
		id, err := s.PageID(ctx, name)
		if err != nil || id != kept.ID {
			t.Fatalf("file alias %q: %d %v", name, id, err)
		}
		preview, err := s.PreviewByTitle(ctx, name)
		if err != nil || !bytes.Equal(preview, image) {
			t.Fatalf("preview alias %q: %v", name, err)
		}
	}
	again, err := s.AddFile(ctx, "cli", f)
	if err != nil || again.ID != kept.ID || !again.Existing || again.Embedded {
		t.Fatalf("same original added embed after rename: %+v %v", again, err)
	}
	day = dayOf(t, s, "2019-06-03")
	if day.Body != body || len(day.Out) != 1 || day.Out[0].ID != kept.ID {
		t.Fatalf("old embed changed: %+v", day)
	}
	p, err := s.PageByID(ctx, day.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", day.ID, p.Body, p.Version); err != nil {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, day.ID)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != kept.ID {
		t.Fatalf("old embed no longer resolves: %+v %v", p, err)
	}
}
