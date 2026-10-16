package api

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// TestPaged: paged trims a read of limit+1 rows to the page, puts limit and offset in the properties, and adds the
// link prev when offset > 0 and next when the read had one more row. A link keeps the query of the list and gives it
// its own limit and offset.
func TestPaged(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rows          []int
		path, query   string
		limit, offset int
		want          string // the rows, the properties, then each link as rel=href
	}{
		{"the first page of more", []int{1, 2, 3, 4}, "/browse", "kind=person", 3, 0,
			"[1 2 3] map[kind:x limit:3 offset:0] self=/x next=/browse?kind=person&limit=3&offset=3"},
		{"a middle page", []int{4, 5, 6, 7}, "/browse", "kind=person", 3, 3,
			"[4 5 6] map[kind:x limit:3 offset:3] self=/x prev=/browse?kind=person&limit=3&offset=0 next=/browse?kind=person&limit=3&offset=6"},
		{"the last page", []int{7}, "/browse", "kind=person", 3, 6,
			"[7] map[kind:x limit:3 offset:6] self=/x prev=/browse?kind=person&limit=3&offset=3"},
		{"a page past the end", nil, "/browse", "kind=person", 3, 9,
			"[] map[kind:x limit:3 offset:9] self=/x prev=/browse?kind=person&limit=3&offset=6"},
		{"an offset that is not a step of limit", []int{1, 2}, "/days", "", 3, 2,
			"[1 2] map[kind:x limit:3 offset:2] self=/x prev=/days?limit=3&offset=0"},
		{"one page, all the rows", []int{1, 2, 3}, "/days", "", 3, 0,
			"[1 2 3] map[kind:x limit:3 offset:0] self=/x"},
		{"a query with its own limit and offset", []int{1, 2, 3, 4}, "/search", "q=needleword&limit=3&offset=3", 3, 3,
			"[1 2 3] map[kind:x limit:3 offset:3] self=/x prev=/search?limit=3&offset=0&q=needleword next=/search?limit=3&offset=6&q=needleword"},
	} {
		q, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		before := q.Encode()
		e := &Entity{Properties: map[string]any{"kind": "x"}, Links: []Link{link("self", "/x", "This")}}
		rows := paged(e, tc.rows, tc.path, q, tc.limit, tc.offset)
		got := fmt.Sprint(rows, " ", e.Properties)
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
	// An entity with no properties gets them.
	e := &Entity{}
	paged(e, []string{"a"}, "/days", url.Values{}, 60, 0)
	if fmt.Sprint(e.Properties) != "map[limit:60 offset:0]" || len(e.Links) != 0 {
		t.Errorf("an entity with no properties: %v %v", e.Properties, e.Links)
	}
}
