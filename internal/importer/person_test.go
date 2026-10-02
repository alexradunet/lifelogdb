package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// a person note that is only frontmatter — synthetic, like the rest of the vault
const bobPerson = "People/Bob Person.md"

func addSource(t *testing.T, f *fixture, file, body string) {
	t.Helper()
	full := filepath.Join(f.w.Source, filepath.FromSlash(file))
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func person(t *testing.T, s *core.Store, title string) *core.Page {
	t.Helper()
	id, err := s.PageID(ctx, title)
	if err != nil {
		t.Fatalf("%s: %v", title, err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func openStore(t *testing.T, path string) *core.Store {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &core.Store{DB: d}
}

func personFacts(file string, p map[string]any, quote string) map[string]any {
	return map[string]any{"file": file, "writes": []any{map[string]any{"person": p, "quote": quote}}}
}

// A frontmatter-only person note is promoted and its birthday reaches people.birth_day: the note names its own
// title, the birthday is in its quote. Applying again, and replaying twice, writes nothing new.
func TestPersonNoteWithOnlyFrontmatter(t *testing.T) {
	f := setup(t)
	addSource(t, f, bobPerson, "---\ntype: Person\nbirthday: 1980-03-29\n---\n")
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if p := person(t, f.s, "Bob Person"); p.Type != "page" {
		t.Fatalf("the vault made a %s", p.Type)
	}
	facts := personFacts(bobPerson, map[string]any{"title": "Bob Person", "name": "Bob Person", "birth_day": "1980-03-29"}, "birthday: 1980-03-29")
	if err := f.facts(t, bobPerson, facts); err != nil {
		t.Fatalf("a person with a birth_day: %v", err)
	}
	if r, err := f.w.Check(ctx, f.s, bobPerson); err != nil || r.Summary != "1 person (1 promoted)" {
		t.Fatalf("check: %v, %v", r, err)
	}
	if _, err := f.w.Apply(ctx, f.s, bobPerson); err != nil {
		t.Fatal(err)
	}
	if p := person(t, f.s, "Bob Person"); p.Type != "person" || p.Person.Birth != "1980-03-29" || p.Person.Name != "Bob Person" {
		t.Errorf("after apply: %s %+v", p.Type, p.Person)
	}
	if r, err := f.w.Apply(ctx, f.s, bobPerson); err != nil || r.Summary != "1 person (1 existing)" {
		t.Errorf("a second apply: %v, %v", r, err)
	}
	if st, err := f.w.Status(ctx, f.s, f.trial); err != nil || len(st.Mismatches) != 0 {
		t.Errorf("status: %+v, %v", st, err)
	}

	target := filepath.Join(t.TempDir(), "life.db")
	for i := 0; i < 2; i++ {
		res, err := f.w.Replay(ctx, f.s, target)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Differences) != 0 || !res.Integrity.OK {
			t.Errorf("replay %d: %+v", i+1, res)
		}
		want := "1 person (1 promoted)"
		if i == 1 {
			want = "1 person (1 existing)"
		}
		for _, r := range res.Files {
			if r.File == bobPerson && r.Summary != want {
				t.Errorf("replay %d: %s", i+1, r.Summary)
			}
		}
	}
	ts := openStore(t, target)
	if p := person(t, ts, "Bob Person"); p.Type != "person" || p.Person.Birth != "1980-03-29" {
		t.Errorf("the replay's person: %s %+v", p.Type, p.Person)
	}
}

// A person who exists without a day is given it (updated, once); a different day is the owner's correction;
// a day not in the quote, or a title another file's quote does not name, is refused.
func TestPersonDays(t *testing.T) {
	f := setup(t)
	addSource(t, f, bobPerson, "---\ntype: Person\nbirthday: 1980-03-29\ndied: 2060-01-02\n---\n")
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if _, err := f.s.CreatePerson(ctx, "cli", "Bob Person", "", "", ""); err != nil {
		t.Fatal(err)
	}
	both := "birthday: 1980-03-29 died: 2060-01-02"
	f.facts(t, bobPerson, personFacts(bobPerson, map[string]any{"title": "Bob Person", "birth_day": "1980-03-29", "death_day": "2060-01-02"}, both))
	if r, err := f.w.Apply(ctx, f.s, bobPerson); err != nil || r.Summary != "1 person (1 updated)" {
		t.Fatalf("filling the days: %v, %v", r, err)
	}
	if p := person(t, f.s, "Bob Person"); p.Person.Birth != "1980-03-29" || p.Person.Death != "2060-01-02" {
		t.Errorf("the days: %+v", p.Person)
	}
	if r, err := f.w.Apply(ctx, f.s, bobPerson); err != nil || r.Summary != "1 person (1 existing)" {
		t.Errorf("a second apply: %v, %v", r, err)
	}

	cases := []struct {
		name, file string
		facts      map[string]any
		want       string
	}{
		{"a day not in the quote", bobPerson,
			personFacts(bobPerson, map[string]any{"title": "Bob Person", "death_day": "2060-01-02"}, "type: Person"), "not in the quote"},
		{"a day that is not a day", bobPerson,
			personFacts(bobPerson, map[string]any{"title": "Bob Person", "birth_day": "1980-3-29"}, "birthday: 1980-03-29"), "not YYYY-MM-DD"},
		{"a title another file's quote does not name", "Journal/2031-04-12.md",
			personFacts("Journal/2031-04-12.md", map[string]any{"title": "Bob Person"}, "Coffee with Cara"), "does not name"},
	}
	for _, c := range cases {
		if err := f.facts(t, c.file, c.facts); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Check(ctx, f.s, c.file); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", c.name, err, c.want)
		}
	}
	// a day the person holds is never replaced by a facts file: a correction is the owner's
	addSource(t, f, bobPerson, "---\ntype: Person\nbirthday: 1980-03-28\n---\n")
	f.facts(t, bobPerson, personFacts(bobPerson, map[string]any{"title": "Bob Person", "birth_day": "1980-03-28"}, "birthday: 1980-03-28"))
	if _, err := f.w.Check(ctx, f.s, bobPerson); err == nil || !strings.Contains(err.Error(), "correction is the owner's") {
		t.Errorf("a different birth_day: %v", err)
	}
	if p := person(t, f.s, "Bob Person"); p.Person.Birth != "1980-03-29" {
		t.Errorf("the held day changed: %+v", p.Person)
	}
}
