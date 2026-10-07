package tests

import (
	"path/filepath"
	"testing"
)

func TestCanonicalIdentityFixtureOwnershipAndRollback(t *testing.T) {
	d := realDocs()
	s := &S{name: "identity-fixture", d: d, ddl: d.DDL(), dir: t.TempDir()}
	defer s.close()
	file := filepath.Join(s.dir, "ownership.db")
	c := s.freshWith(F{Path: file})
	id := c.pageW("Actual fixture name", "2031-01-02", "unaltered body")
	if c.tab("SELECT e.id,e.day,e.body,n.title,n.name_key FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE e.id=?", id) != ids(id)+"|2031-01-02|unaltered body|Actual fixture name|actual fixture name" {
		t.Fatal("identity fixture did not preserve its actual spelling/prose/day")
	}
	if c.n("SELECT count(*) FROM entity_names WHERE entity_id=?", id) != 1 {
		t.Fatal("fixture created an extra alias")
	}
	c.must("BEGIN IMMEDIATE")
	person := c.named("person", "Outer transaction person", M{"name": "Synthetic Person"})
	if c.n("SELECT count(*) FROM people WHERE id=?", person) != 1 {
		t.Fatal("typed extension absent")
	}
	before := c.n("SELECT count(*) FROM entities")
	if _, err := c.tryIdentity("page", "Actual fixture name", nil); err == nil {
		t.Fatal("duplicate owned name accepted")
	}
	if c.n("SELECT count(*) FROM entities") != before {
		t.Fatal("failed name insertion leaked an owner")
	}
	c.must("ROLLBACK")
	if c.n("SELECT count(*) FROM entities WHERE id=?", person) != 0 || c.n("SELECT count(*) FROM entity_names WHERE entity_id=?", person) != 0 {
		t.Fatal("nested fixture committed its caller's transaction")
	}
	c.must("BEGIN IMMEDIATE")
	day := c.dayPage("2031-02-03", "journal")
	c.must("COMMIT")
	if c.n("SELECT count(*) FROM entity_names WHERE entity_id=?", day) != 1 {
		t.Fatal("day fixture does not have exactly its date name")
	}
	if c.n("SELECT count(*) FROM pragma_foreign_key_check") != 0 {
		t.Fatal("fixture ownership violated foreign keys")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := s.readOnly(file)
	if reopened.n("SELECT count(*) FROM entities WHERE id IN (?,?)", id, day) != 2 || reopened.n("SELECT count(*) FROM entity_names WHERE entity_id IN (?,?)", id, day) != 2 {
		t.Fatal("owned fixtures did not persist")
	}
	if reopened.n("SELECT count(*) FROM entity_names WHERE name_key='outer transaction person'") != 0 || reopened.n("SELECT count(*) FROM people WHERE name='Synthetic Person'") != 0 {
		t.Fatal("rolled-back fixture persisted")
	}
	if reopened.str("SELECT body FROM entities WHERE id=?", id) != "unaltered body" {
		t.Fatal("reopen changed fixture prose")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}
