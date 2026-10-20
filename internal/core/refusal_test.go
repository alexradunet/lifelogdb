package core

import (
	"database/sql"
	"errors"
	"testing"
)

// TestARefusalOfTheFileHasNoDriverWords: the text of a refused write is what SQLite says (the name of a CHECK, the
// message of a trigger) without the driver's prefix and its result code.
func TestARefusalOfTheFileHasNoDriverWords(t *testing.T) {
	s := fresh(t)
	id, err := s.CreatePerson(ctx, "cli", "Sam", "Samuel Ortiz", "1980-02-03", "")
	if err != nil {
		t.Fatal(err)
	}
	str := func(v string) *string { return &v }
	write := func(q string, args ...any) error {
		return refused(s.DB.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q, args...); return err }))
	}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a CHECK", s.EditPerson(ctx, "cli", id, PersonEdit{Death: str("1970-01-01")}), "CHECK constraint failed: people_death_day_order"},
		{"a trigger", write("DELETE FROM entity_names"), "names remain reserved, including tombstones"},
	} {
		var e *Error
		if !errors.As(tc.err, &e) || e.Status != 422 {
			t.Errorf("%s: %v, want a 422", tc.name, tc.err)
			continue
		}
		if e.Msg != tc.want {
			t.Errorf("%s: the message is %q, want %q", tc.name, e.Msg, tc.want)
		}
	}
}

func TestPlainRefusal(t *testing.T) {
	for in, want := range map[string]string{
		"constraint failed: CHECK constraint failed: a_b (275)": "CHECK constraint failed: a_b",
		"constraint failed: a trigger said no (1811)":           "a trigger said no",
		"UNIQUE constraint failed: t.c":                         "UNIQUE constraint failed: t.c",
		"a (b) message (12)\nwith two lines (3)":                "a (b) message (12)\nwith two lines",
	} {
		if got := plainRefusal(in); got != want {
			t.Errorf("plainRefusal(%q) = %q, want %q", in, got, want)
		}
	}
}
