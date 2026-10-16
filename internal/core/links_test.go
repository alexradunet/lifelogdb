package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSaveBodyAddsAndDeletesLinks(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Diet", "See [[Plan]] and [[Health/Diet]] and [[Diet]]", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, id)
	if titles(p.Out, "wikilink") != "Plan" {
		t.Fatalf("links %v", p.Out)
	}
	r, err := s.SaveBody(ctx, "cli", id, "nowSQL [[Other]] only", p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 0 {
		t.Errorf("skipped %v", r.Skipped)
	}
	p2, _ := s.PageByID(ctx, id)
	if titles(p2.Out, "wikilink") != "Other" {
		t.Errorf("after the re-save: %v", p2.Out)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale", p.Version); status(err) != 409 {
		t.Errorf("a save with an old version: %v", err)
	}
}

func TestSaveRevivesATombstonedTarget(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Old", "", "")
	if err := s.Tombstone(ctx, "cli", old); err != nil {
		t.Fatal(err)
	}
	_, r, err := s.CreatePage(ctx, "cli", "New", "[[old]]", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Revived, ",") != "old" || len(r.Created) != 0 {
		t.Errorf("sync %+v", r)
	}
}

// TestALinkTargetIsSkippedOnlyWhenItsSavepointRollsBack: a body save skips a wikilink target that fails, when the
// savepoint of the target rolls it back alone (cookbook/save-a-body.md, "any error in 1-3"), and the rest of the save
// commits. When the savepoint cannot roll back, the save fails as a fault, and the page keeps its body and its links.
// A trigger of the test (deliberate damage) makes the target fail when the save inserts its name: RAISE(ABORT) is a
// refusal; a function that does not exist is an error that is not a refusal; RAISE(ROLLBACK) rolls the whole
// transaction back, so the savepoint cannot roll back to its start.
func TestALinkTargetIsSkippedOnlyWhenItsSavepointRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
		skipped       bool
	}{
		{"a refusal of the DDL", `SELECT RAISE(ABORT, 'the test refuses the target')`, true},
		{"an error that is not a refusal", `SELECT no_such_function()`, true},
		{"a rollback of the whole write", `SELECT RAISE(ROLLBACK, 'the test rolls the write back')`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fresh(t)
			home, _, err := s.CreatePage(ctx, "cli", "Home", "[[Old]]", "")
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, home)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.W.ExecContext(ctx, `CREATE TEMP TRIGGER fail_the_target BEFORE INSERT ON entity_names
			    WHEN NEW.title = 'New' BEGIN `+tc.trigger+`; END`); err != nil {
				t.Fatal(err)
			}
			r, err := s.SaveBody(ctx, "cli", home, "[[New]]", p.Version)
			body, links := "[[Old]]", "Old"
			if tc.skipped {
				body, links = "[[New]]", ""
				if err != nil || strings.Join(r.Skipped, ",") != "New" || len(r.Linked) != 0 {
					t.Errorf("SaveBody = %+v, %v; want New skipped and nothing linked", r, err)
				}
			} else if err == nil || status(err) != 0 || !reflect.DeepEqual(r, Sync{}) {
				t.Errorf("SaveBody = %+v, %v (status %d); want a fault, not a refusal or a skipped target", r, err, status(err))
			}
			got, err := s.PageByID(ctx, home)
			if err != nil {
				t.Fatal(err)
			}
			if got.Body != body || titles(got.Out, "wikilink") != links {
				t.Errorf("Home: body %q, links %q; want body %q, links %q", got.Body, titles(got.Out, "wikilink"), body, links)
			}
			if id, err := s.PageID(ctx, "New"); id != 0 || err != nil {
				t.Errorf("PageID(New) = %d, %v; want no page", id, err)
			}
		})
	}
}

func TestLinkFieldsImmutable(t *testing.T) {
	s := fresh(t)
	a, _, err := s.CreatePage(ctx, "cli", "Page A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreatePage(ctx, "cli", "Page B", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", a, b, "part-of", "old note"); err != nil {
		t.Fatal(err)
	}
	var id int64
	var created string
	if err := s.DB.R.QueryRow(`SELECT id,created_at FROM links WHERE from_id=? AND to_id=? AND kind='part-of'`, a, b).Scan(&id, &created); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`id=id+1000`, `created_at='2020-01-01T00:00:00.000Z'`, `from_id=to_id`, `to_id=from_id`, `kind='wikilink'`, `source='api'`} {
		if _, err := s.DB.W.Exec(`UPDATE links SET `+set+` WHERE id=?`, id); err == nil {
			t.Fatalf("immutable update accepted: %s", set)
		}
	}
	var gotID int64
	var gotCreated string
	if err := s.DB.R.QueryRow(`SELECT id,created_at FROM links WHERE from_id=? AND to_id=? AND kind='part-of'`, a, b).Scan(&gotID, &gotCreated); err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotCreated != created {
		t.Fatal("immutable refusal changed persisted link")
	}
	note := func() any {
		t.Helper()
		var n any
		if err := s.DB.R.QueryRow(`SELECT note FROM links WHERE id=?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := s.DB.W.Exec(`UPDATE links SET note='new note' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if got := note(); got != "new note" {
		t.Fatalf("the note is the one field that changes: %v", got)
	}
	rollback := errors.New("rollback note")
	if err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE links SET note='rollback' WHERE id=?`, id); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("rollback: %v", err)
	}
	if got := note(); got != "new note" {
		t.Fatalf("a rolled back note edit changed the note: %v", got)
	}
}

// TestALinkOfAKindThatTheRegistryDoesNotHave: redirect is not a kind of link_kinds (D8). The trigger
// links_endpoint_types refuses a link of a kind that the registry does not have: a 422 with its sentence. A link from
// a page that does not exist is a 404, whatever its kind. No such link exists, so a request to remove one is a 404.
// Each refusal writes nothing.
func TestALinkOfAKindThatTheRegistryDoesNotHave(t *testing.T) {
	s := fresh(t)
	a, _, err := s.CreatePage(ctx, "cli", "Page A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreatePage(ctx, "cli", "Page B", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const unregistered = "link kind is not registered in link_kinds"
	for _, c := range []struct {
		name   string
		write  func() error
		status int
		says   string // a part of the message
	}{
		{"link redirect", func() error { return s.Link(ctx, "cli", a, b, "redirect", "") }, 422, unregistered},
		{"link a kind with no row", func() error { return s.Link(ctx, "cli", a, b, "no-such-kind", "") }, 422, unregistered},
		{"link redirect from no page", func() error { return s.Link(ctx, "cli", 9999, b, "redirect", "") }, 404, "no page 9999"},
		{"unlink redirect", func() error { return s.Unlink(ctx, "cli", a, b, "redirect") }, 404, fmt.Sprintf("no redirect link from %d to %d", a, b)},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := everyRow(t, s)
			if err := c.write(); status(err) != c.status || !strings.Contains(fmt.Sprint(err), c.says) {
				t.Errorf("got %v (status %d), want %d with %q", err, status(err), c.status, c.says)
			}
			if after := everyRow(t, s); after != before {
				t.Errorf("the refusal wrote:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestLinkKindsFromMatchesTheRegistry(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, from_types FROM link_kinds WHERE kind <> 'wikilink' ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string][]string{}
	for rows.Next() {
		var kind string
		var from *string
		if err := rows.Scan(&kind, &from); err != nil {
			t.Fatal(err)
		}
		for _, typ := range []string{"page", "person", "place", "file", "metric", "category", "no-such-type"} {
			if from == nil || strings.Contains(","+*from+",", ","+typ+",") {
				want[typ] = append(want[typ], kind)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want["page"]) == 0 {
		t.Fatal("the registry offers a page no kind")
	}
	for typ, kinds := range want {
		got, err := s.LinkKindsFrom(ctx, typ)
		if err != nil || strings.Join(got, ",") != strings.Join(kinds, ",") {
			t.Errorf("%s: got %v want %v (%v)", typ, got, kinds, err)
		}
	}
	// A type written as an injection is a value: it matches only the kinds open to any type, like an unknown type.
	if got, err := s.LinkKindsFrom(ctx, "page' OR '1'='1"); err != nil || strings.Join(got, ",") != strings.Join(want["no-such-type"], ",") {
		t.Errorf("injection: %v %v want %v", got, err, want["no-such-type"])
	}
}
