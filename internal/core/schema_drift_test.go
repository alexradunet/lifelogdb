package core

import (
	"reflect"
	"strings"
	"testing"
)

// The schema of the live file is compared with the schema of a file that the embedded DDL builds
// (docs/contract/integrity-checks.md).

func TestIntegrityFindsNoSchemaDriftInAFreshFile(t *testing.T) {
	s := fresh(t)
	// ANALYZE makes sqlite_stat1, which the DDL does not name: it is not drift. The FTS5 shadow tables are there too.
	if _, err := s.DB.W.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatal(err)
	}
	got, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.SchemaDrift == nil || len(got.SchemaDrift) != 0 {
		t.Fatalf("a fresh file has the schema of the DDL: %+v", got)
	}
}

func TestIntegrityReportsSchemaDriftByName(t *testing.T) {
	s := fresh(t)
	var fixed, noUpdate string
	for name, dst := range map[string]*string{"links_fixed": &fixed, "measurements_no_update": &noUpdate} {
		if err := s.DB.R.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'trigger' AND name = ?`, name).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`DROP TRIGGER links_fixed`,                // missing
		`CREATE INDEX stray_index ON links(note)`, // extra
		`DROP TRIGGER measurements_no_update`,     // changed: the same name with another text
		strings.Replace(noUpdate, "BEGIN", "BEGIN SELECT 1;", 1),
	} {
		if _, err := s.DB.W.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	got, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"missing trigger links_fixed", "extra index stray_index", "changed trigger measurements_no_update"}
	if !reflect.DeepEqual(got.SchemaDrift, want) {
		t.Fatalf("schema drift = %q; want %q", got.SchemaDrift, want)
	}
	// The drift is a finding about the binary and the file, not damage to the data: ok is about the data.
	if !got.OK {
		t.Errorf("a missing trigger with no bad row must not make the data not ok: %+v", got)
	}
}

// Only the text of the DDL counts: a trigger written again with other white space is the same trigger.
func TestIntegritySchemaDriftFoldsWhiteSpace(t *testing.T) {
	s := fresh(t)
	var definition string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'trigger' AND name = 'links_fixed'`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	reflowed := strings.ReplaceAll(definition, "\n", "\n \t  ")
	if reflowed == definition {
		t.Fatal("the fixture trigger has one line: the test would prove nothing")
	}
	for _, q := range []string{`DROP TRIGGER links_fixed`, reflowed} {
		if _, err := s.DB.W.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Integrity(ctx)
	if err != nil || len(got.SchemaDrift) != 0 {
		t.Fatalf("white space is not drift: %+v %v", got, err)
	}
}
