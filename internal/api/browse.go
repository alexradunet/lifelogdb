package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

// Browse (docs/plans/117-one-browse-view.md): one list with two filters, the kind of the rows and where they are
// (everything, a category, or no category), and the category tree beside it. Each kind keeps its row and its order;
// the order is fixed here, and there is no sort field.

// browseKind is what Browse knows of one kind of row. A new kind is one row of browseKindTable, with its read in core.
// The kinds that core lists are the rows; "all" is the row of the list of a category.
type browseKind struct {
	chip         string // the words of the chip
	fileable     bool   // the pick bar offers file-pages
	tombstonable bool   // the pick bar offers tombstone-pages; the other kinds are deleted from their own page
	read         func(h *server, ctx context.Context, rd browseRead) ([]browseRow, pageProps, error)
}

// browseRead is what the read of a kind gets: the entity that takes the links to the next and previous pages, the
// filters, the scope and the page asked for.
type browseRead struct {
	e             *siren.Entity
	b             browseQuery
	sc            core.Scope
	limit, offset int
}

// browseKindTable has a row for each of the browseKinds. The action that makes a thing of a kind on its list is
// siren.NewActionOf, because the rail of the browser reads it too.
var browseKindTable = map[string]browseKind{
	"page":     {chip: "Pages", fileable: true, tombstonable: true, read: (*server).pageRows},
	"person":   {chip: "People", fileable: true, read: (*server).namedRows},
	"place":    {chip: "Places", fileable: true, read: (*server).namedRows},
	"media":    {chip: "Media", fileable: true, tombstonable: true, read: (*server).fileRows},
	"document": {chip: "Documents", fileable: true, tombstonable: true, read: (*server).fileRows},
	"metric":   {chip: "Metrics", fileable: true, read: (*server).metricRows},
	"all":      {chip: "All", read: (*server).filedRows},
}

// browseKinds are the values of the field kind; "all" is a chip only inside a category, where it is the first.
var browseKinds = append(core.BrowseKinds(), "all")

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

// browseProps are the properties of the browse answer. The view draws the list and the tree from them.
type browseProps struct {
	pageProps
	Kind        string          `json:"kind"`
	Unfiled     bool            `json:"unfiled"`
	Ghosts      bool            `json:"ghosts"`
	Media       string          `json:"media"`
	Categories  []core.Category `json:"categories"`
	Rows        []browseRow     `json:"rows"`
	In          *browseIn       `json:"in,omitempty"`
	Counts      browseCounts    `json:"counts"`
	MediaCounts *mediaCounts    `json:"media_counts,omitempty"` // kind media only
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
	case b.media != "" && !slices.Contains(core.MediaKinds(), b.media):
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
	return siren.BrowseHref(v)
}

// browse is GET /browse and the action browse: the rows of one kind in one scope, the count of each chip, and the
// category tree. Inside a category the list's New form files the new page there.
func (h *server) browse(r *http.Request, _ spec, _ string) (*siren.Entity, error) {
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

	self := siren.BrowseHref(b.values(""))
	e := &siren.Entity{Class: []string{"browse"}, Title: "Browse",
		Links: []siren.Link{link("self", self, "Browse"), link("index", "/", "Home"), link("metrics", "/metrics", "Metrics")}}
	chips := core.BrowseKinds()
	if in != nil { // inside a category All is the default, and its chip comes first
		chips = append([]string{"all"}, chips...)
	}
	for _, k := range chips {
		e.Links = append(e.Links, siren.Link{Rel: []string{"kind"}, Href: siren.BrowseHref(b.values(k, "ghosts", "media")), Title: browseKindTable[k].chip, Class: []string{k}})
	}
	treeKind := url.Values{} // a branch keeps the kind the URL names; All needs a category
	if b.named && b.kind != "all" {
		treeKind.Set("kind", b.kind)
	}
	e.Links = append(e.Links, link("everything", siren.BrowseHref(treeKind), "Everything"))
	unfiled := url.Values{"unfiled": {"1"}}
	if k := treeKind.Get("kind"); k != "" {
		unfiled.Set("kind", k)
	}
	e.Links = append(e.Links, link("unfiled", siren.BrowseHref(unfiled), "Not filed"), link("ghosts", "/browse?ghosts=1&kind=page", "Ghost pages"))
	if b.kind == "media" {
		for _, m := range []struct{ rel, kind, title string }{{"pictures", "image", "Pictures"}, {"videos", "video", "Videos"}, {"recordings", "audio", "Recordings"}} {
			v := b.values("", "media")
			v.Set("media", m.kind)
			e.Links = append(e.Links, link(m.rel, siren.BrowseHref(v), m.title))
		}
	}

	rows, pg, err := h.browseRows(ctx, browseRead{e: e, b: b, sc: sc, limit: limit, offset: offset})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		e.Entities = append(e.Entities, siren.Link{Rel: []string{"item"}, Href: siren.PageHref(row.ID), Title: row.Title, Class: []string{row.Type}})
	}
	treeIn := func(title string) string {
		v := url.Values{"in": {title}}
		if k := treeKind.Get("kind"); k != "" {
			v.Set("kind", k)
		}
		return siren.BrowseHref(v)
	}
	var titles []string
	for _, c := range cs {
		e.Entities = append(e.Entities, siren.Link{Rel: []string{"category"}, Href: treeIn(c.Title), Title: c.Title, Class: []string{"category"}})
		titles = append(titles, c.Title)
	}
	e.Actions = browseActions(b, in, titles)

	props := browseProps{pageProps: pg, Kind: b.kind, Unfiled: b.unfiled, Ghosts: b.ghosts, Media: b.media,
		Categories: cs, Rows: rows, In: in}
	props.Counts = browseCounts{Page: counts["page"], Person: counts["person"], Place: counts["place"], Media: counts["media"],
		Document: counts["document"], Metric: counts["metric"], Ghosts: counts["ghosts"]}
	if in != nil {
		all := counts["all"]
		props.Counts.All = &all
	}
	if b.kind == "media" {
		m, err := h.s.MediaCounts(ctx, sc)
		if err != nil {
			return nil, err
		}
		props.MediaCounts = &mediaCounts{Image: m["image"], Video: m["video"], Audio: m["audio"]}
	}
	e.Properties = props
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

// browseRows reads one page of the rows of the list and links its next and previous pages, the filters kept. The
// ghosts are pages that no page links to: their list is not a kind of its own.
func (h *server) browseRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	if rd.b.ghosts {
		return h.ghostRows(ctx, rd)
	}
	return browseKindTable[rd.b.kind].read(h, ctx, rd)
}

// readList reads the page of a list of Browse that rd asks for, with the filters kept in its links.
func readList[T any](rd browseRead, read func(limit, offset int) ([]T, error)) ([]T, pageProps, error) {
	return readPage(rd.e, "/browse", rd.b.values(""), rd.limit, rd.offset, read)
}

func (h *server) ghostRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	found, props, err := readList(rd, func(limit, offset int) ([]core.Ghost, error) { return h.s.Ghosts(ctx, limit, offset) })
	rows := []browseRow{}
	for _, g := range found {
		rows = append(rows, browseRow{ID: g.ID, Title: g.Title, Type: "page", Kind: "page"})
	}
	return rows, props, err
}

func (h *server) pageRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	found, props, err := readList(rd, func(limit, offset int) ([]core.PageRow, error) { return h.s.PlainPages(ctx, rd.sc, limit, offset) })
	rows := []browseRow{}
	for _, p := range found {
		rows = append(rows, browseRow{ID: p.ID, Title: p.Title, Type: "page", Kind: "page", Category: p.Category})
	}
	return rows, props, err
}

// namedRows are the people or the places, as rd.b.kind says.
func (h *server) namedRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	kind := rd.b.kind
	found, props, err := readList(rd, func(limit, offset int) ([]core.NamedRow, error) {
		return h.s.NamedMentions(ctx, kind, rd.sc, limit, offset)
	})
	rows := []browseRow{}
	for _, p := range found {
		row := browseRow{ID: p.ID, Title: p.Title, Type: kind, Kind: kind, Mentions: &p.Mentions, LastDay: p.LastDay}
		if p.Picture != 0 {
			row.Picture = siren.PageHref(p.Picture) + "/preview"
		}
		rows = append(rows, row)
	}
	return rows, props, err
}

// fileRows are the files, as rd.b.kind says: the media (one media kind, or all) or the documents.
func (h *server) fileRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	kind := rd.b.kind
	found, props, err := readList(rd, func(limit, offset int) ([]core.FileRow, error) {
		if kind == "media" {
			return h.s.Media(ctx, rd.b.media, rd.sc, limit, offset)
		}
		return h.s.OtherFiles(ctx, rd.sc, limit, offset)
	})
	rows := []browseRow{}
	for _, f := range found {
		row := browseRow{ID: f.ID, Title: f.Title, Type: "file", Kind: kind, Mentions: &f.Mentions, MIME: f.MIME,
			FileKind: fileKind(f.MIME), Day: f.Day}
		if f.Picture {
			row.Picture = siren.PageHref(f.ID) + "/preview"
		}
		rows = append(rows, row)
	}
	return rows, props, err
}

func (h *server) metricRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	found, props, err := readList(rd, func(limit, offset int) ([]core.MetricRow, error) { return h.s.MetricRows(ctx, rd.sc, limit, offset) })
	rows := []browseRow{}
	for _, m := range found {
		rows = append(rows, browseRow{ID: m.ID, Title: m.Title, Type: "metric", Kind: "metric", Unit: m.Unit})
	}
	return rows, props, err
}

// filedRows are the things filed directly in the category of the list (kind all).
func (h *server) filedRows(ctx context.Context, rd browseRead) ([]browseRow, pageProps, error) {
	found, props, err := readList(rd, func(limit, offset int) ([]core.FiledRow, error) { return h.s.FiledIn(ctx, rd.sc.In, limit, offset) })
	rows := []browseRow{}
	for _, f := range found {
		rows = append(rows, browseRow{ID: f.ID, Title: f.Title, Type: f.Type, Kind: f.Kind, Category: f.Category, FileKind: fileKind(f.MIME)})
	}
	return rows, props, err
}

// fileKind is the part of a file's type before the '/': image, video, audio, application, text. A stored type has
// the '/' (files_mime); a row that is not a file has no type, and no kind.
func fileKind(mime string) string {
	kind, _, _ := strings.Cut(mime, "/")
	return kind
}

// browseActions are the actions of a list: the New of its kind (a new thing is filed in the category of the list,
// rule 6), and the pick bar's. The ghosts have no New. Every list but All offers file-pages; pages (the ghosts too),
// media and documents also offer tombstone-pages. People, places and metrics are deleted from their own page.
func browseActions(b browseQuery, in *browseIn, categories []string) []siren.Action {
	var out []siren.Action
	if name := siren.NewActionOf(b.kind); name != "" && !b.ghosts {
		var values map[string]any
		if in != nil {
			values = map[string]any{"category": in.Title}
		}
		out = append(out, action(name, nil, values))
	}
	k := browseKindTable[b.kind]
	if k.fileable {
		out = append(out, withOptions(action("file-pages", nil, nil), "category", categories))
	}
	if k.tombstonable {
		out = append(out, action("tombstone-pages", nil, nil))
	}
	return out
}
