package core

import (
	"fmt"
	"testing"
)

func TestStableRenameKeepsIdentityAndOldReferences(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "First name", "original [[Target]]", "")
	if err != nil {
		t.Fatal(err)
	}
	mention, _, err := s.CreatePage(ctx, "cli", "Mention", "[[First name]]", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Second name", "Final name", "FINAL NAME", "First name"} {
		got, err := s.Rename(ctx, "cli", id, name)
		if err != nil || got != id {
			t.Fatalf("rename %q = %d, %v; want stable %d", name, got, err, id)
		}
	}
	for _, name := range []string{"First name", "Second name", "Final name"} {
		got, err := s.PageID(ctx, name)
		if err != nil || got != id {
			t.Fatalf("lookup %q = %d, %v; want %d", name, got, err, id)
		}
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != before.Body || after.Version == before.Version || len(after.In) != 1 || after.In[0].ID != mention {
		t.Fatalf("rename changed identity/prose/backlinks: %+v", after)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale [[Forbidden]]", before.Version); status(err) != 409 {
		t.Fatalf("name mutation stale token = %v", err)
	}
	p, err := s.PageByID(ctx, mention)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveBody(ctx, "cli", mention, "[[First name]] ![[Second name|display]] [[Final name]]", p.Version)
	if err != nil || len(r.Linked) != 1 {
		t.Fatalf("aliases not deduplicated: %+v %v", r, err)
	}
	p, err = s.PageByID(ctx, mention)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != id {
		t.Fatalf("alias edges = %+v %v", p, err)
	}
}

func TestReferenceGrammarRejectsUnaddressableNamesAtomically(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Lab [old]", "_old_", "a`b`c", "R&amp;D", "a`b`c"} {
		if _, _, err := s.CreatePage(ctx, "cli", name, "[[Forbidden]]", ""); status(err) != 422 {
			t.Fatalf("unaddressable name %q = %v; want 422", name, err)
		}
		if id, err := s.PageID(ctx, name); err != nil || id != 0 {
			t.Fatalf("rejected name persisted: %d %v", id, err)
		}
	}
	if id, err := s.PageID(ctx, "Forbidden"); err != nil || id != 0 {
		t.Fatalf("rejected create had side effects: %d %v", id, err)
	}
}

// TestAWriteToAPageThatIsNotLive: every write that needs a live page gives one answer for a page that does not exist,
// a 404 "no page N", and one for a tombstoned page, a 409 that names its id and its title: page N "Title" is deleted:
// revive it first (livePage). The owner thus reads which page, also when the write named it by its title (a category).
func TestAWriteToAPageThatIsNotLive(t *testing.T) {
	s := fresh(t)
	live, _, err := s.CreatePage(ctx, "cli", "Live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Ana.jpg", SHA256: sha('a'), MIME: "image/jpeg", Preview: smallJPEG(t)}); err != nil {
		t.Fatal(err)
	}
	const missing = 9999
	want := fmt.Sprintf(`page %d "Old Group" is deleted: revive it first`, gone)
	writes := []struct {
		name  string
		write func(id int64) error
	}{
		{"save-body", func(id int64) error { _, err := s.SaveBody(ctx, "cli", id, "words", "1"); return err }},
		{"rename", func(id int64) error { _, err := s.Rename(ctx, "cli", id, "New Title"); return err }},
		{"file-pages", func(id int64) error { _, err := s.FilePages(ctx, "cli", []int64{id}, "Group", false); return err }},
		{"set-picture", func(id int64) error {
			_, err := s.SetPicture(ctx, "cli", id, "2026-10-16", FileIn{SHA256: sha('b'), MIME: "image/jpeg", Preview: smallJPEG(t)})
			return err
		}},
		{"set-picture-of", func(id int64) error { _, err := s.SetPictureOf(ctx, "cli", id, "Ana.jpg"); return err }},
	}
	for _, w := range writes {
		if err := w.write(missing); status(err) != 404 || err.Error() != "no page 9999" {
			t.Errorf("%s of a missing page: %v (status %d), want 404 %q", w.name, err, status(err), "no page 9999")
		}
		if err := w.write(gone); status(err) != 409 || err.Error() != want {
			t.Errorf("%s of a tombstoned page: %v (status %d), want 409 %q", w.name, err, status(err), want)
		}
	}
	// a tombstoned page as the category of a filing
	if _, err := s.FilePages(ctx, "cli", []int64{live}, "Old Group", false); status(err) != 409 || err.Error() != want {
		t.Errorf("file-pages in a tombstoned category: %v (status %d), want 409 %q", err, status(err), want)
	}
}

// TestTheLivePageCheckDoesNotReadTheBody: livePage gives the title, the version and the day-page flag of a live page,
// and leaves the body empty. body is the last column of entities, so a write that checks a long page (each id of a
// bulk filing) does not read the overflow pages of its body.
func TestTheLivePageCheckDoesNotReadTheBody(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Long", "words of the body", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		p, err := tx.livePage(id)
		if err != nil {
			return err
		}
		if p.ID != id || p.Title != "Long" || p.Version != "1" || p.DayPage || p.Body != "" {
			t.Errorf("livePage(%d) = %+v; want the page Long at version 1, not a day page, with no body read", id, *p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
