package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPeoplePlacesAndPromotion(t *testing.T) {
	s := fresh(t)
	day, _, _ := s.Capture(ctx, "cli", "2026-07-31", "At [[Lakeside]] with [[Ana]]", nil)
	ana, _ := s.PageID(ctx, "Ana")
	lake, _ := s.PageID(ctx, "lakeside")
	if err := s.Promote(ctx, "cli", ana, "person", "Ana Example"); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "cli", lake, "place", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "cli", day, "place", ""); status(err) != 422 {
		t.Errorf("promoting a day page: %v", err)
	}
	if err := s.Link(ctx, "cli", day, lake, "at", "evening"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", ana, lake, "at", ""); status(err) != 422 {
		t.Errorf("an at link from a person: %v", err)
	}
	d, _ := s.Day(ctx, "2026-07-31")
	found := false
	for _, r := range d.Rows {
		found = found || (r.What == "at" && r.Detail == "Lakeside")
	}
	if !found {
		t.Errorf("day view %+v", d.Rows)
	}
	bob, err := s.CreatePerson(ctx, "cli", "Bob", "Bob Sample", "1990-02-30", "", "")
	if status(err) != 422 || bob != 0 {
		t.Errorf("an impossible birth day: %v", err)
	}
	if err := s.Link(ctx, "cli", ana, lake, "friend", ""); status(err) != 422 {
		t.Errorf("friend to a place: %v", err)
	}
	p, _ := s.PageByID(ctx, ana)
	if p.Person == nil || p.Person.Name != "Ana Example" {
		t.Errorf("person %+v", p.Person)
	}
}

func TestPromotionRefusesRetainedTypedEdges(t *testing.T) {
	for _, incoming := range []bool{true, false} {
		t.Run(map[bool]string{true: "incoming", false: "outgoing"}[incoming], func(t *testing.T) {
			s := fresh(t)
			category, _, err := s.CreatePage(ctx, "cli", "Category", "retained prose", "")
			if err != nil {
				t.Fatal(err)
			}
			other, _, err := s.CreatePage(ctx, "cli", "Other", "", "")
			if err != nil {
				t.Fatal(err)
			}
			kind := "part-of"
			from, to := other, category
			if !incoming {
				kind = "at"
				from, to = category, other
				if err := s.Promote(ctx, "cli", other, "place", ""); err != nil {
					t.Fatal(err)
				}
				// an at link starts at a day page (links_endpoint_types): written with that trigger dropped, so the
				// probe isolates the endpoint rule of a type change from the day-page rule of an insert
				withoutTrigger(t, s, "links_endpoint_types", `INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'at',`+nowSQL+`,'cli')`, from, to)
			} else if err := s.Link(ctx, "cli", from, to, kind, "retained note"); err != nil {
				t.Fatal(err)
			}
			before, err := s.PageByID(ctx, category)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Promote(ctx, "cli", category, "person", "Changed"); err == nil {
				t.Fatal("promotion accepted invalid retained edge")
			}
			after, err := s.PageByID(ctx, category)
			if err != nil {
				t.Fatal(err)
			}
			if after.Type != "page" || after.Body != before.Body || after.Version != before.Version || after.Person != nil {
				t.Fatalf("refused promotion changed page: %+v", after)
			}
			var edges, details int
			if err := s.DB.R.QueryRow(`SELECT count(*) FROM links WHERE from_id=? AND to_id=? AND kind=?`, from, to, kind).Scan(&edges); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.R.QueryRow(`SELECT count(*) FROM people WHERE id=?`, category).Scan(&details); err != nil {
				t.Fatal(err)
			}
			if edges != 1 || details != 0 {
				t.Fatalf("refusal retained %d edges / %d people; want 1/0", edges, details)
			}
		})
	}
}

// The lifecycle of an entity (docs/plans/098-the-codebase-made-cohesive.md): a missing id is 404, and the same state
// again is a no-op. TestPromoteOfATypedEntity holds the answers of a promotion of what is not a plain page.
func TestEntityLifecycleStatusesAreTheCommonOnes(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Lifecycle witness", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", 999999); status(err) != 404 {
		t.Errorf("tombstone of a missing id: %v, want 404", err)
	}
	if err := s.Revive(ctx, "cli", 999999); status(err) != 404 {
		t.Errorf("revive of a missing id: %v, want 404", err)
	}
	if err := s.Promote(ctx, "cli", 999999, "place", ""); status(err) != 404 {
		t.Errorf("promote of a missing id: %v, want 404", err)
	}
	if err := s.Revive(ctx, "cli", id); err != nil {
		t.Errorf("revive of a live page is a no-op: %v", err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Errorf("tombstone of a tombstoned page is a no-op: %v", err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil || p.DeletedAt == "" {
		t.Fatalf("the page is tombstoned once: %+v %v", p, err)
	}
	if err := s.Promote(ctx, "cli", id, "place", ""); err != nil {
		t.Fatal(err)
	}
}

// TestPromoteOfATypedEntity: only a plain page is promoted. A person, a place, a metric or a file promoted to another
// type is refused by the schema (entities_promote_from_page, D20): a 422 with the sentence of the trigger. A promotion
// to the type that the entity has already changes no type, so no rule of the schema sees it: the writer refuses it
// with a 409, and it does not revive a tombstone. A refusal writes nothing.
func TestPromoteOfATypedEntity(t *testing.T) {
	s := fresh(t)
	person, err := s.CreatePerson(ctx, "cli", "Ana Example", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	place, err := s.CreatePlace(ctx, "cli", "Lake Example", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreatePlace(ctx, "cli", "Old Lake", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Steps", Unit: "n"}); err != nil {
		t.Fatal(err)
	}
	metric, err := s.PageID(ctx, "Steps")
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	const trigger = "only a plain page is promoted: a person, a place, a metric or a file keeps its type (D20)"
	for _, c := range []struct {
		name string
		id   int64
		to   string
		want int
	}{
		{"a person to a place", person, "place", 422},
		{"a place to a person", place, "person", 422},
		{"a metric to a person", metric, "person", 422},
		{"a metric to a place", metric, "place", 422},
		{"a file to a person", file.ID, "person", 422},
		{"a file to a place", file.ID, "place", 422},
		{"a person to a person", person, "person", 409},
		{"a place to a place", place, "place", 409},
		{"a tombstoned place to a place", gone, "place", 409},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := everyRow(t, s)
			err := s.Promote(ctx, "cli", c.id, c.to, "")
			if status(err) != c.want || c.want == 422 && !strings.Contains(fmt.Sprint(err), trigger) {
				t.Fatalf("status %d (%v), want %d", status(err), err, c.want)
			}
			if after := everyRow(t, s); after != before {
				t.Errorf("a refused promotion wrote:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// A search that runs out of time says so: the cause stays in the error, so the api answers 504, not 422
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestAnExpiredSearchKeepsItsCause(t *testing.T) {
	s := fresh(t)
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	_, err := s.Search(expired, "one", 10, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the cause is lost: %v", err)
	}
}
