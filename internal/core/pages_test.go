package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/internal/photo/phototest"
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
	bob, err := s.CreatePerson(ctx, "cli", "Bob", "Bob Sample", "1990-02-30", "")
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
	person, err := s.CreatePerson(ctx, "cli", "Ana Example", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	place, err := s.CreatePlace(ctx, "cli", "Lake Example")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreatePlace(ctx, "cli", "Old Lake")
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
	_, err := s.SearchWith(expired, "one", SearchOptions{}, 10, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the cause is lost: %v", err)
	}
}

// TestSearchNewestOrdersByTheDayOfAPage: with the order newest, the hits go by the day of the page, newest first. A
// day page has the day of its title. Any other page has the local day of its created_at, in the zone of the store, so
// a plain page made now sits between a day page of the past and one of the future. The day pages here are made by
// hand (Capture) with fixed days; the expected day of the plain page is read from its created_at, not from the code.
func TestSearchNewestOrdersByTheDayOfAPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		zone *time.Location
	}{
		{"UTC", time.UTC},
		{"west of UTC", time.FixedZone("UTC-12", -12*3600)},
		{"east of UTC", time.FixedZone("UTC+14", 14*3600)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fresh(t)
			s.Zone = tc.zone
			plain, _, err := s.CreatePage(ctx, "cli", "Plain note", "needle needle needle", "")
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, plain)
			if err != nil {
				t.Fatal(err)
			}
			created, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			day := created.In(tc.zone) // the local day of the plain page
			before, after := day.AddDate(0, 0, -1).Format(time.DateOnly), day.AddDate(0, 0, 1).Format(time.DateOnly)
			if after >= "2099-03-01" || before <= "2020-03-01" {
				t.Skipf("the clock of this machine (%s) is outside 2020 to 2099", day.Format(time.DateOnly))
			}
			// A wrong zone (UTC, say) puts the plain page on the day before or after its day, so it ties with a day page
			// next to it. The day page before ranks better than the plain page and the one after ranks worse, so that
			// a tie sorts them wrong, and the test fails.
			for d, entry := range map[string]string{"2020-03-01": "needle", before: strings.Repeat("needle ", 6), after: "needle", "2099-03-01": "needle"} {
				if _, _, err := s.Capture(ctx, "cli", d, entry, nil); err != nil {
					t.Fatal(err)
				}
			}
			want := []string{"2099-03-01", after, "Plain note", before, "2020-03-01"}
			hits, err := s.SearchWith(ctx, "needle", SearchOptions{Order: SearchNewest}, 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.Title)
			}
			if !slices.Equal(got, want) {
				t.Errorf("newest first = %q, want %q", got, want)
			}
			// The paging is the same order, cut: the limit and the offset select from the sorted hits.
			page, err := s.SearchWith(ctx, "needle", SearchOptions{Order: SearchNewest}, 2, 2)
			if err != nil || len(page) != 2 || page[0].Title != "Plain note" || page[1].Title != before {
				t.Errorf("limit 2 offset 2 = %+v %v, want Plain note and %s", page, err, before)
			}
			// A page in Omit is not a hit, and the paging counts the pages that are left.
			skipped, err := s.SearchWith(ctx, "needle", SearchOptions{Order: SearchNewest, Omit: []int64{plain}}, 2, 2)
			if err != nil || len(skipped) != 2 || skipped[0].Title != before || skipped[1].Title != "2020-03-01" {
				t.Errorf("newest without the plain page, limit 2 offset 2 = %+v %v", skipped, err)
			}
		})
	}
}

// TestSearchNewestKeepsRankThenIDInADay: pages of one day keep the order of the best match, then the id, as the order
// best does.
func TestSearchNewestKeepsRankThenIDInADay(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Alpha", "needle", ""); err != nil {
		t.Fatal(err)
	}
	beta, _, err := s.CreatePage(ctx, "cli", "Beta", "needle needle needle needle", "") // the better match, the later id
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2099-01-01", "needle", nil); err != nil {
		t.Fatal(err)
	}
	best, err := s.SearchWith(ctx, "needle", SearchOptions{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	newest, err := s.SearchWith(ctx, "needle", SearchOptions{Order: SearchNewest}, 10, 0)
	if err != nil || len(newest) != 3 || newest[0].Title != "2099-01-01" {
		t.Fatalf("newest = %+v %v, want the day page of 2099 first", newest, err)
	}
	if best[0].ID != beta {
		t.Fatalf("best puts %q first, want Beta: the setup does not rank Beta first", best[0].Title)
	}
	if newest[1].ID != beta || newest[2].Title != "Alpha" {
		t.Errorf("newest puts %q then %q after the day page, want Beta then Alpha (the order of best)", newest[1].Title, newest[2].Title)
	}
	// A page of no hits is empty, as with best: a limit of 0, or an offset after the last hit.
	for _, c := range []struct{ limit, offset int }{{0, 0}, {10, 3}} {
		if got, err := s.SearchWith(ctx, "needle", SearchOptions{Order: SearchNewest}, c.limit, c.offset); err != nil || len(got) != 0 {
			t.Errorf("newest with limit %d offset %d = %+v %v, want no hits", c.limit, c.offset, got, err)
		}
	}
}

// TestSearchRefusesAnUnknownOrder: the order is one of the SearchOrders. Anything else is a 422 before any read.
func TestSearchRefusesAnUnknownOrder(t *testing.T) {
	s := fresh(t)
	for _, order := range []string{"oldest", "NEWEST", "rank; DROP TABLE entities", " newest"} {
		if _, err := s.SearchWith(ctx, "needle", SearchOptions{Order: order}, 10, 0); status(err) != 422 {
			t.Errorf("order %q: status %d (%v), want 422", order, status(err), err)
		}
	}
	for _, order := range []string{"", "best", "newest"} {
		if _, err := s.SearchWith(ctx, "needle", SearchOptions{Order: order}, 10, 0); err != nil {
			t.Errorf("order %q: %v, want none", order, err)
		}
	}
}

// TestSearchByKind: a hit has its kind (a day page is a day, a metric a page), the counts are the hits of each kind
// with their sum, a kind keeps only its hits in either order, and another kind is a 422. A hit's picture is its
// cover, or a file's own preview.
func TestSearchByKind(t *testing.T) {
	s := streamStore(t)
	captured(t, s, "2026-10-01", "A lantern by the lake.", nil)
	if _, _, err := s.CreatePage(ctx, "cli", "Shed", "A lantern.", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePerson(ctx, "cli", "Ana", "", "", ""); err != nil {
		t.Fatal(err)
	}
	ana := must[int64](t)(s.PageID(ctx, "Ana"))
	page := must[*Page](t)(s.PageByID(ctx, ana))
	if _, err := s.SaveBody(ctx, "cli", ana, "Ana keeps a lantern.", page.Version); err != nil {
		t.Fatal(err)
	}
	must[bool](t)(s.CreateMetric(ctx, "cli", NewMetric{Name: "Lantern hours", Note: "A lantern lit."}))
	pic := must[Kept](t)(s.AddFile(ctx, "cli", FileIn{Title: "Lantern.jpg", SHA256: strings.Repeat("c", 64), MIME: "image/jpeg", Body: "A lantern at night.", Preview: phototest.JPEG(8, 8)}))
	if _, err := s.SetCoverOf(ctx, "cli", ana, "Lantern.jpg"); err != nil {
		t.Fatal(err)
	}

	hits := must[[]Hit](t)(s.SearchWith(ctx, "lantern", SearchOptions{}, 50, 0))
	kinds := map[string]string{}
	for _, h := range hits {
		kinds[h.Title] = h.Kind
		if want := map[string]int64{"Ana": pic.ID, "Lantern.jpg": pic.ID}[h.Title]; h.Picture != want {
			t.Errorf("the picture of %s is %d, want %d", h.Title, h.Picture, want)
		}
	}
	want := map[string]string{"2026-10-01": "day", "Shed": "page", "Ana": "person", "Lantern hours": "page", "Lantern.jpg": "file"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Errorf("the kinds of the hits: %v, want %v", kinds, want)
	}
	for _, order := range SearchOrders() {
		only := must[[]Hit](t)(s.SearchWith(ctx, "lantern", SearchOptions{Order: order, Kind: "page"}, 50, 0))
		var titles []string
		for _, h := range only {
			titles = append(titles, h.Title)
		}
		if slices.Sort(titles); strings.Join(titles, ",") != "Lantern hours,Shed" {
			t.Errorf("the pages, %s: %v", order, titles)
		}
	}
	if _, err := s.SearchWith(ctx, "lantern", SearchOptions{Kind: "metric"}, 50, 0); status(err) != 422 {
		t.Errorf("the kind metric: %v, want a 422", err)
	}
}
