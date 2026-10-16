package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/core"
)

// Browse (docs/plans/117-one-browse-view.md): one list with two filters, the kind of the rows and where they are
// (everything, a category, or no category), and the category tree beside it. Each kind keeps its row and its order;
// the order is fixed here, and there is no sort field.

// browseKinds are the values of the field kind; "all" is a chip only inside a category, where it is the first.
var browseKinds = append(slices.Clone(core.BrowseKinds), "all")

// chipWords are the words of each chip.
var chipWords = map[string]string{"page": "Pages", "person": "People", "place": "Places", "media": "Media",
	"document": "Documents", "metric": "Metrics", "all": "All"}

// browseRow is a row of Browse: the thing, its type and its Browse kind, and only what that kind shows.
type browseRow struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`               // entities.entity_type
	Kind     string `json:"kind"`               // page, person, place, media, document or metric
	Category bool   `json:"category,omitempty"` // a page that is a category
	Mentions *int   `json:"mentions,omitempty"` // a person, a place, a file: the live pages that link to it
	LastDay  string `json:"last_day,omitempty"` // a person, a place: the newest day page among them
	Picture  string `json:"picture,omitempty"`  // a person, a file: the address of its picture
	MIME     string `json:"mime,omitempty"`     // a file: the type of the original
	FileKind string `json:"file_kind,omitempty"`
	Day      string `json:"day,omitempty"`  // a file: its day, as the media list says it
	Unit     string `json:"unit,omitempty"` // a metric: its unit
}

// browseCounts are the counts of the chips in the scope of the list.
type browseCounts struct {
	Page     int  `json:"page"`
	Person   int  `json:"person"`
	Place    int  `json:"place"`
	Media    int  `json:"media"`
	Document int  `json:"document"`
	Metric   int  `json:"metric"`
	All      *int `json:"all,omitempty"` // inside a category only
	Ghosts   int  `json:"ghosts"`
}

// mediaCounts counts the live files of each media kind in the scope of the media list.
type mediaCounts struct {
	Image int `json:"image"`
	Video int `json:"video"`
	Audio int `json:"audio"`
}

// browseIn is the category of a list: its page, and its path in the tree (its title when nothing is filed in it).
type browseIn struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// browseQuery is what GET /browse was asked, checked: the filters as the URL names them, and the kind they come to.
type browseQuery struct {
	kind    string // the kind of the rows: the field, else its default
	named   bool   // the URL names the kind: a branch of the tree keeps it
	in      string // the title of the category, "" for none
	unfiled bool
	ghosts  bool
	media   string
}

// browseQueryOf reads the filters of GET /browse and refuses a combination that means nothing (422).
func browseQueryOf(q url.Values) (browseQuery, error) {
	b := browseQuery{kind: q.Get("kind"), in: q.Get("in"), media: q.Get("media")}
	var err error
	if b.unfiled, err = flag(q, "unfiled"); err != nil {
		return b, err
	}
	if b.ghosts, err = flag(q, "ghosts"); err != nil {
		return b, err
	}
	b.named = b.kind != ""
	switch {
	case b.named && !slices.Contains(browseKinds, b.kind):
		return b, core.Invalid("kind must be one of %s", strings.Join(browseKinds, ", "))
	case !b.named && b.in != "":
		b.kind = "all"
	case !b.named:
		b.kind = "page"
	}
	switch {
	case b.kind == "all" && b.in == "":
		return b, core.Invalid("kind all needs in: the category whose things it lists")
	case b.in != "" && b.unfiled:
		return b, core.Invalid("in and unfiled cannot be used together")
	case b.ghosts && (b.kind != "page" || b.in != "" || b.unfiled):
		return b, core.Invalid("ghosts is a list of pages: it takes no other kind, no in and no unfiled")
	case b.media != "" && b.kind != "media":
		return b, core.Invalid("media needs kind media")
	case b.media != "" && !slices.Contains(core.MediaKinds, b.media):
		return b, core.Invalid("media must be image, video or audio")
	}
	return b, nil
}

// values are the filters of the list as a query: the kind when the URL names it, then the others that are set.
// kind replaces the kind ("" keeps it), and drop removes filters.
func (b browseQuery) values(kind string, drop ...string) url.Values {
	v := url.Values{}
	switch {
	case kind != "":
		v.Set("kind", kind)
	case b.named:
		v.Set("kind", b.kind)
	}
	if b.in != "" {
		v.Set("in", b.in)
	}
	if b.unfiled {
		v.Set("unfiled", "1")
	}
	if b.ghosts {
		v.Set("ghosts", "1")
	}
	if b.media != "" {
		v.Set("media", b.media)
	}
	for _, d := range drop {
		v.Del(d)
	}
	return v
}

// browseHref is the address of Browse with a query.
func browseHref(v url.Values) string {
	if len(v) == 0 {
		return "/browse"
	}
	return "/browse?" + v.Encode()
}

// oldLists are the lists that Browse replaced (docs/plans/117-one-browse-view.md, rule 8), each with the filters of
// Browse that give its rows. A GET of one answers 308 to Browse, so a bookmark or a client keeps working; the POST
// routes of the same paths do not change.
var oldLists = map[string]url.Values{
	"/people":     {"kind": {"person"}},
	"/places":     {"kind": {"place"}},
	"/media":      {"kind": {"media"}},
	"/files":      {"kind": {"document"}},
	"/ghosts":     {"kind": {"page"}, "ghosts": {"1"}},
	"/categories": {},
}

// toBrowse is the Browse address of a GET of an old list: its filters, and the limit, the offset and the asked form
// (new) of the query. The kind of the media list (image, video, audio) is the field media of Browse.
func toBrowse(path string, q url.Values) string {
	v := url.Values{}
	for k, vs := range oldLists[path] {
		v[k] = slices.Clone(vs)
	}
	for _, k := range []string{"limit", "offset", "new"} {
		if x, ok := q[k]; ok {
			v[k] = x
		}
	}
	if path == "/media" && q.Get("kind") != "" {
		v.Set("media", q.Get("kind"))
	}
	return browseHref(v)
}

// browse is GET /browse and the action browse: the rows of one kind in one scope, the count of each chip, and the
// category tree. Inside a category the list's New form files the new page there.
func (h *server) browse(r *http.Request, _ spec, _ string) (*Entity, error) {
	b, err := browseQueryOf(r.URL.Query())
	if err != nil {
		return nil, err
	}
	limit, offset, err := pageOf(r, 200)
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	cs, err := h.s.Categories(ctx)
	if err != nil {
		return nil, err
	}
	var sc core.Scope
	var in *browseIn
	if b.in != "" {
		if in, err = h.categoryNamed(ctx, b.in, cs); err != nil {
			return nil, err
		}
		sc.In = in.ID
	}
	sc.Unfiled = b.unfiled
	counts, err := h.s.BrowseCounts(ctx, sc)
	if err != nil {
		return nil, err
	}

	self := browseHref(b.values(""))
	e := &Entity{Class: []string{"browse"}, Title: "Browse",
		Links: []Link{link("self", self, "Browse"), link("index", "/", "Home"), link("metrics", "/metrics", "Metrics")}}
	chips := core.BrowseKinds
	if in != nil { // inside a category All is the default, and its chip comes first
		chips = append([]string{"all"}, chips...)
	}
	for _, k := range chips {
		e.Links = append(e.Links, Link{Rel: []string{"kind"}, Href: browseHref(b.values(k, "ghosts", "media")), Title: chipWords[k], Class: []string{k}})
	}
	treeKind := url.Values{} // a branch keeps the kind the URL names; All needs a category
	if b.named && b.kind != "all" {
		treeKind.Set("kind", b.kind)
	}
	e.Links = append(e.Links, link("everything", browseHref(treeKind), "Everything"))
	unfiled := url.Values{"unfiled": {"1"}}
	if k := treeKind.Get("kind"); k != "" {
		unfiled.Set("kind", k)
	}
	e.Links = append(e.Links, link("unfiled", browseHref(unfiled), "Not filed"), link("ghosts", "/browse?ghosts=1&kind=page", "Ghost pages"))
	if b.kind == "media" {
		for _, m := range []struct{ rel, kind, title string }{{"pictures", "image", "Pictures"}, {"videos", "video", "Videos"}, {"recordings", "audio", "Recordings"}} {
			v := b.values("", "media")
			v.Set("media", m.kind)
			e.Links = append(e.Links, link(m.rel, browseHref(v), m.title))
		}
	}

	rows, err := h.browseRows(ctx, e, b, sc, limit, offset)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		e.Entities = append(e.Entities, Link{Rel: []string{"item"}, Href: pageHref(row.ID), Title: row.Title, Class: []string{row.Type}})
	}
	treeIn := func(title string) string {
		v := url.Values{"in": {title}}
		if k := treeKind.Get("kind"); k != "" {
			v.Set("kind", k)
		}
		return browseHref(v)
	}
	var titles []string
	for _, c := range cs {
		e.Entities = append(e.Entities, Link{Rel: []string{"category"}, Href: treeIn(c.Title), Title: c.Title, Class: []string{"category"}})
		titles = append(titles, c.Title)
	}
	e.Actions = browseActions(b, in, titles)

	props := e.Properties.(map[string]any) // paged made them
	props["kind"], props["unfiled"], props["ghosts"], props["media"] = b.kind, b.unfiled, b.ghosts, b.media
	props["categories"], props["rows"] = cs, rows
	c := browseCounts{Page: counts["page"], Person: counts["person"], Place: counts["place"], Media: counts["media"],
		Document: counts["document"], Metric: counts["metric"], Ghosts: counts["ghosts"]}
	if in != nil {
		all := counts["all"]
		c.All = &all
		props["in"] = in
	}
	props["counts"] = c
	if b.kind == "media" {
		m, err := h.s.MediaCounts(ctx, sc)
		if err != nil {
			return nil, err
		}
		props["media_counts"] = mediaCounts{Image: m["image"], Video: m["video"], Audio: m["audio"]}
	}
	return e, nil
}

// categoryNamed is the category the field in names, by any of its names: its path from the tree when it is a
// category, else the page alone. A title that names nothing is a 404. Only a live plain page can be a category
// (link_kinds and links_endpoint_types refuse anything else as the end of a part-of link), so another kind or a day
// is a 422, and a tombstone, which the tree does not draw, is a 409.
func (h *server) categoryNamed(ctx context.Context, title string, cs []core.Category) (*browseIn, error) {
	id, err := h.s.PageID(ctx, title)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, core.NotFound("no page titled %s", title)
	}
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case p.Type != "page":
		return nil, core.Invalid("%s is a %s: only a page can be a category", p.Title, p.Type)
	case p.IsDayPage:
		return nil, core.Invalid("%s is a day: a day is never a category", p.Title)
	case p.DeletedAt != "":
		return nil, core.Conflict("the category %s is deleted", p.Title)
	}
	for i := range cs {
		if cs[i].ID == id {
			return &browseIn{ID: id, Title: cs[i].Title, Path: cs[i].Path}, nil
		}
	}
	return &browseIn{ID: id, Title: p.Title, Path: p.Title}, nil
}

// browseRows reads one page of the rows of the list and links its next and previous pages, the filters kept.
func (h *server) browseRows(ctx context.Context, e *Entity, b browseQuery, sc core.Scope, limit, offset int) ([]browseRow, error) {
	q := b.values("")
	rows := []browseRow{}
	switch {
	case b.ghosts:
		found, err := h.s.Ghosts(ctx, limit+1, offset)
		if err != nil {
			return nil, err
		}
		for _, g := range paged(e, found, "/browse", q, limit, offset) {
			rows = append(rows, browseRow{ID: g.ID, Title: g.Title, Type: "page", Kind: "page"})
		}
	case b.kind == "page":
		found, err := h.s.PlainPages(ctx, sc, limit+1, offset)
		if err != nil {
			return nil, err
		}
		for _, p := range paged(e, found, "/browse", q, limit, offset) {
			rows = append(rows, browseRow{ID: p.ID, Title: p.Title, Type: "page", Kind: "page", Category: p.Category})
		}
	case b.kind == "person" || b.kind == "place":
		found, err := h.s.NamedMentions(ctx, b.kind, sc, limit+1, offset)
		if err != nil {
			return nil, err
		}
		for _, p := range paged(e, found, "/browse", q, limit, offset) {
			row := browseRow{ID: p.ID, Title: p.Title, Type: b.kind, Kind: b.kind, Mentions: &p.Mentions, LastDay: p.LastDay}
			if p.Picture != 0 {
				row.Picture = pageHref(p.Picture) + "/preview"
			}
			rows = append(rows, row)
		}
	case b.kind == "media" || b.kind == "document":
		var found []core.FileRow
		var err error
		if b.kind == "media" {
			found, err = h.s.Media(ctx, b.media, sc, limit+1, offset)
		} else {
			found, err = h.s.OtherFiles(ctx, sc, limit+1, offset)
		}
		if err != nil {
			return nil, err
		}
		for _, f := range paged(e, found, "/browse", q, limit, offset) {
			row := browseRow{ID: f.ID, Title: f.Title, Type: "file", Kind: b.kind, Mentions: &f.Mentions, MIME: f.MIME,
				FileKind: fileKind(f.MIME), Day: f.Day}
			if f.Picture {
				row.Picture = pageHref(f.ID) + "/preview"
			}
			rows = append(rows, row)
		}
	case b.kind == "metric":
		found, err := h.s.MetricRows(ctx, sc, limit+1, offset)
		if err != nil {
			return nil, err
		}
		for _, m := range paged(e, found, "/browse", q, limit, offset) {
			rows = append(rows, browseRow{ID: m.ID, Title: m.Title, Type: "metric", Kind: "metric", Unit: m.Unit})
		}
	default: // all, inside a category
		found, err := h.s.FiledIn(ctx, sc.In, limit+1, offset)
		if err != nil {
			return nil, err
		}
		for _, f := range paged(e, found, "/browse", q, limit, offset) {
			rows = append(rows, browseRow{ID: f.ID, Title: f.Title, Type: f.Type, Kind: f.Kind, Category: f.Category, FileKind: fileKind(f.MIME)})
		}
	}
	return rows, nil
}

// fileKind is the part of a file's type before the '/': image, video, audio, application, text. A stored type has
// the '/' (files_mime); a row that is not a file has no type, and no kind.
func fileKind(mime string) string {
	kind, _, _ := strings.Cut(mime, "/")
	return kind
}

// browseActions are the actions of a list: the New of its kind (a new thing is filed in the category of the list,
// rule 6), and the pick bar's. Every list but All offers file-pages; pages (the ghosts too), media and documents
// also offer tombstone-pages. People, places and metrics are deleted from their own page.
func browseActions(b browseQuery, in *browseIn, categories []string) []Action {
	var values map[string]any
	if in != nil {
		values = map[string]any{"category": in.Title}
	}
	var out []Action
	switch {
	case b.ghosts:
	case b.kind == "page":
		out = append(out, action("create-page", nil, values))
	case b.kind == "person":
		out = append(out, action("create-person", nil, values))
	case b.kind == "place":
		out = append(out, action("create-place", nil, values))
	case b.kind == "media", b.kind == "document": // a file of any kind: it goes to the list of its type
		out = append(out, action("add-file", nil, values))
	}
	if b.kind != "all" {
		out = append(out, withOptions(action("file-pages", nil, nil), "category", categories))
	}
	if b.kind == "page" || b.kind == "media" || b.kind == "document" {
		out = append(out, action("tombstone-pages", nil, nil))
	}
	return out
}
