package core

import "testing"

func TestPromoteKeepsTheDatesOfAPersonAndRefusesThemForAPlace(t *testing.T) {
	s := fresh(t)
	_, _, _ = s.Capture(ctx, "cli", "2026-07-31", "With [[Sam]] at [[Pier]]", nil)
	sam, _ := s.PageID(ctx, "Sam")
	pier, _ := s.PageID(ctx, "Pier")
	for _, tc := range []struct{ name, birth, death string }{
		{"", "1980-02-03", "2020-01-01"}, {"", "", "2020-01-01"}, {"Full name", "", ""},
	} {
		if err := s.Promote(ctx, "cli", pier, "place", tc.name, tc.birth, tc.death); status(err) != 422 {
			t.Errorf("a place with %+v: %v, want 422", tc, err)
		}
	}
	if p, _ := s.PageByID(ctx, pier); p.Type != "page" {
		t.Errorf("a refused promotion changed the type to %s", p.Type)
	}
	if err := s.Promote(ctx, "cli", sam, "person", "Samuel Ortiz", "1980-02-03", ""); err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, sam)
	if p.Person == nil || *p.Person != (Person{Name: "Samuel Ortiz", Birth: "1980-02-03"}) {
		t.Errorf("person %+v, want Samuel Ortiz born 1980-02-03 and no death day", p.Person)
	}
}

func TestEditPersonChangesWhatIsNamedAndKeepsTheRest(t *testing.T) {
	s := fresh(t)
	id, err := s.CreatePerson(ctx, "cli", "Sam (barber)", "Samuel Ortiz", "1980-02-03", "2020-01-01")
	if err != nil {
		t.Fatal(err)
	}
	str := func(v string) *string { return &v }
	person := func() Person {
		t.Helper()
		p, err := s.PageByID(ctx, id)
		if err != nil || p.Person == nil {
			t.Fatalf("the person %d: %+v, %v", id, p, err)
		}
		return *p.Person
	}
	// A nil field stays; an empty date is removed.
	if err := s.EditPerson(ctx, "cli", id, PersonEdit{Death: str("")}); err != nil {
		t.Fatal(err)
	}
	if got, want := person(), (Person{Name: "Samuel Ortiz", Birth: "1980-02-03"}); got != want {
		t.Errorf("after removing the death day: %+v, want %+v", got, want)
	}
	if err := s.EditPerson(ctx, "cli", id, PersonEdit{Name: str("Samuel A. Ortiz"), Birth: str("1981-04-05")}); err != nil {
		t.Fatal(err)
	}
	want := Person{Name: "Samuel A. Ortiz", Birth: "1981-04-05"}
	if got := person(); got != want {
		t.Errorf("after a new name and birth day: %+v, want %+v", got, want)
	}
	// The schema refuses these, and a refused edit writes nothing.
	for name, e := range map[string]PersonEdit{
		"an empty name":        {Name: str("")},
		"an impossible day":    {Birth: str("1990-02-30")},
		"a death before birth": {Death: str("1900-01-01")},
		"a padded name":        {Name: str(" padded ")},
	} {
		if err := s.EditPerson(ctx, "cli", id, e); status(err) != 422 {
			t.Errorf("%s: %v, want 422", name, err)
		}
	}
	if got := person(); got != want {
		t.Errorf("after refused edits: %+v, want %+v", got, want)
	}
	plain, _, err := s.CreatePage(ctx, "cli", "Plain", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EditPerson(ctx, "cli", plain, PersonEdit{Name: str("X")}); status(err) != 409 {
		t.Errorf("a plain page: %v, want 409", err)
	}
	if err := s.EditPerson(ctx, "cli", 999999, PersonEdit{Name: str("X")}); status(err) != 404 {
		t.Errorf("a missing page: %v, want 404", err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if err := s.EditPerson(ctx, "cli", id, PersonEdit{Name: str("X")}); status(err) != 409 {
		t.Errorf("a deleted person: %v, want 409", err)
	}
}
