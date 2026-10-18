package api

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// fileKind is the part of a type before the '/'. A row that is not a file has no type and no kind. files_mime refuses
// a type with no '/' in the file; fileKind gives such a value whole, and does not stop the list.
func TestFileKind(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": "image", "application/pdf": "application", "": "", "noslash": "noslash"} {
		if got := fileKind(mime); got != want {
			t.Errorf("fileKind(%q) = %q, want %q", mime, got, want)
		}
	}
}

// TestBrowseQueryOf: the filters of GET /browse, each as browseQueryOf reads it: the kind (its default when the URL
// names none), whether the URL names it, and the other filters. A combination that means nothing is a 422 with its
// sentence.
func TestBrowseQueryOf(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"", "kind=page named=false in= unfiled=false ghosts=false media="},
		{"kind=person", "kind=person named=true in= unfiled=false ghosts=false media="},
		{"in=Family", "kind=all named=false in=Family unfiled=false ghosts=false media="},
		{"in=Family&kind=all", "kind=all named=true in=Family unfiled=false ghosts=false media="},
		{"in=Family&kind=page", "kind=page named=true in=Family unfiled=false ghosts=false media="},
		{"kind=place&unfiled=1", "kind=place named=true in= unfiled=true ghosts=false media="},
		{"unfiled=0", "kind=page named=false in= unfiled=false ghosts=false media="},
		{"ghosts=1", "kind=page named=false in= unfiled=false ghosts=true media="},
		{"kind=media&media=image", "kind=media named=true in= unfiled=false ghosts=false media=image"},
		{"kind=all", "422 kind all needs in: the category whose things it lists"},
		{"in=Family&unfiled=1", "422 in and unfiled cannot be used together"},
		{"ghosts=1&kind=person", "422 ghosts is a list of pages: it takes no other kind, no in and no unfiled"},
		{"ghosts=1&in=Family", "422 ghosts is a list of pages: it takes no other kind, no in and no unfiled"},
		{"ghosts=1&unfiled=1", "422 ghosts is a list of pages: it takes no other kind, no in and no unfiled"},
		{"media=image", "422 media needs kind media"},
		{"kind=page&media=image", "422 media needs kind media"},
		{"kind=media&media=pdf", "422 media must be image, video or audio"},
		{"kind=media&media=document", "422 media must be image, video or audio"}, // a kind of Browse, not a kind of media
		{"kind=pages", "422 kind must be one of page, person, place, media, document, metric, all"},
		{"unfiled=yes", "422 unfiled must be 0 or 1"},
		{"ghosts=2", "422 ghosts must be 0 or 1"},
	} {
		q, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		b, err := browseQueryOf(q)
		got := fmt.Sprintf("kind=%s named=%v in=%s unfiled=%v ghosts=%v media=%s", b.kind, b.named, b.in, b.unfiled, b.ghosts, b.media)
		var ce *core.Error
		if errors.As(err, &ce) {
			got = fmt.Sprint(ce.Status, " ", ce.Msg)
		} else if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("browseQueryOf(%q) = %s, want %s", tc.query, got, tc.want)
		}
	}
}

// TestBrowseActions: the actions of each list of Browse. The New of the kind of the list (none for a metric, all and
// the ghosts) takes the category of the list; file-pages, on every list but All, offers the categories; pages, the
// ghosts, media and documents offer tombstone-pages; every list but the ghosts offers random-page.
func TestBrowseActions(t *testing.T) {
	family := &browseIn{ID: 7, Title: "Family", Path: "Family"}
	categories := []string{"Cousins", "Family"}
	for _, tc := range []struct {
		name string
		b    browseQuery
		in   *browseIn
		want string // each action, with the value of its field category and the options of that field
	}{
		{"pages", browseQuery{kind: "page"}, nil, "create-page file-pages[Cousins Family] tombstone-pages random-page"},
		{"people", browseQuery{kind: "person"}, nil, "create-person file-pages[Cousins Family] random-page"},
		{"places", browseQuery{kind: "place"}, nil, "create-place file-pages[Cousins Family] random-page"},
		{"unfiled places", browseQuery{kind: "place", unfiled: true}, nil, "create-place file-pages[Cousins Family] random-page"},
		{"media", browseQuery{kind: "media"}, nil, "add-file file-pages[Cousins Family] tombstone-pages random-page"},
		{"documents", browseQuery{kind: "document"}, nil, "add-file file-pages[Cousins Family] tombstone-pages random-page"},
		{"metrics", browseQuery{kind: "metric"}, nil, "file-pages[Cousins Family] random-page"},
		{"the ghosts", browseQuery{kind: "page", ghosts: true}, nil, "file-pages[Cousins Family] tombstone-pages"},
		{"all in Family", browseQuery{kind: "all", in: "Family"}, family, "random-page"},
		{"pages in Family", browseQuery{kind: "page", in: "Family"}, family, "create-page=Family file-pages[Cousins Family] tombstone-pages random-page"},
		{"people in Family", browseQuery{kind: "person", in: "Family"}, family, "create-person file-pages[Cousins Family] random-page"},
		{"media in Family", browseQuery{kind: "media", in: "Family"}, family, "add-file file-pages[Cousins Family] tombstone-pages random-page"},
	} {
		var got []string
		for _, a := range browseActions(tc.b, tc.in, categories) {
			s := a.Name
			if f := a.Field("category"); f != nil && f.Value != nil {
				s += "=" + fmt.Sprint(f.Value)
			}
			if f := a.Field("category"); f != nil && f.Options != nil {
				s += fmt.Sprint(f.Options)
			}
			got = append(got, s)
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, strings.Join(got, " "), tc.want)
		}
	}
}

// TestBrowseKindTable: each kind that the field kind accepts has a row with its chip words and its read. A kind that
// core lists without a row would panic at the first request for it.
func TestBrowseKindTable(t *testing.T) {
	for _, k := range browseKinds {
		row, ok := browseKindTable[k]
		if !ok || row.chip == "" || row.read == nil {
			t.Errorf("kind %q has no complete row in browseKindTable: %+v", k, row)
		}
	}
	if len(browseKindTable) != len(browseKinds) {
		t.Errorf("browseKindTable has %d rows for %d kinds", len(browseKindTable), len(browseKinds))
	}
}
