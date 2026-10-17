package api

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/siren"
)

// TestPaged: paged trims a read of limit+1 rows to the page, gives back limit and offset as properties, and adds the
// link prev when offset > 0 and next when the read had one more row. A link keeps the query of the list and gives it
// its own limit and offset.
func TestPaged(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rows          []int
		path, query   string
		limit, offset int
		want          string // the rows, the properties (limit and offset), then each link as rel=href
	}{
		{"the first page of more", []int{1, 2, 3, 4}, "/browse", "kind=person", 3, 0,
			"[1 2 3] {3 0} self=/x next=/browse?kind=person&limit=3&offset=3"},
		{"a middle page", []int{4, 5, 6, 7}, "/browse", "kind=person", 3, 3,
			"[4 5 6] {3 3} self=/x prev=/browse?kind=person&limit=3&offset=0 next=/browse?kind=person&limit=3&offset=6"},
		{"the last page", []int{7}, "/browse", "kind=person", 3, 6,
			"[7] {3 6} self=/x prev=/browse?kind=person&limit=3&offset=3"},
		{"a page past the end", nil, "/browse", "kind=person", 3, 9,
			"[] {3 9} self=/x prev=/browse?kind=person&limit=3&offset=6"},
		{"an offset that is not a step of limit", []int{1, 2}, "/days", "", 3, 2,
			"[1 2] {3 2} self=/x prev=/days?limit=3&offset=0"},
		{"one page, all the rows", []int{1, 2, 3}, "/days", "", 3, 0,
			"[1 2 3] {3 0} self=/x"},
		{"a query with its own limit and offset", []int{1, 2, 3, 4}, "/search", "q=needleword&limit=3&offset=3", 3, 3,
			"[1 2 3] {3 3} self=/x prev=/search?limit=3&offset=0&q=needleword next=/search?limit=3&offset=6&q=needleword"},
	} {
		q, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		before := q.Encode()
		e := &siren.Entity{Links: []siren.Link{link("self", "/x", "This")}}
		rows, props := paged(e, tc.rows, tc.path, q, tc.limit, tc.offset)
		got := fmt.Sprint(rows, " ", props)
		for _, l := range e.Links {
			got += fmt.Sprintf(" %s=%s", strings.Join(l.Rel, ","), l.Href)
		}
		if got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
		if q.Encode() != before {
			t.Errorf("%s: paged changed the query of the list to %s", tc.name, q.Encode())
		}
	}
}

// TestReadPage: readPage asks the read for one row more than the page, so the page is trimmed and the next link
// exists; a failed read gives its error and adds no link.
func TestReadPage(t *testing.T) {
	var asked [2]int
	read := func(limit, offset int) ([]int, error) {
		asked = [2]int{limit, offset}
		return []int{1, 2, 3}, nil
	}
	e := &siren.Entity{}
	rows, props, err := readPage(e, "/days", nil, 2, 4, read)
	if err != nil {
		t.Fatal(err)
	}
	if asked != [2]int{3, 4} {
		t.Errorf("the read was asked for limit %d offset %d, want 3 and 4", asked[0], asked[1])
	}
	if fmt.Sprint(rows, props) != "[1 2] {2 4}" || len(e.Links) != 2 {
		t.Errorf("page %v props %v links %v", rows, props, e.Links)
	}
	e = &siren.Entity{}
	failed := func(int, int) ([]int, error) { return nil, fmt.Errorf("no read") }
	if _, _, err := readPage(e, "/days", nil, 2, 4, failed); err == nil || len(e.Links) != 0 {
		t.Errorf("a failed read: err %v, links %v", err, e.Links)
	}
}
