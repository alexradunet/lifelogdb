package core

import (
	"testing"
)

func TestStableRenameKeepsIdentityAndOldReferences(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "First name", "original [[Target]]")
	if err != nil {
		t.Fatal(err)
	}
	mention, _, err := s.CreatePage(ctx, "cli", "Mention", "[[First name]]")
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
		if _, _, err := s.CreatePage(ctx, "cli", name, "[[Forbidden]]"); status(err) != 422 {
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
