package api

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/core"
)

// The media list and the files list (docs/plans/110-the-media-list.md): the pictures, the videos and the recordings
// the owner keeps, newest day first, drawn as tiles; then every other file (a PDF, a text), as rows. add-file keeps
// a file of any kind.

// mediaAccept is what the file input of the media list's add-file takes: the kinds the list shows.
const mediaAccept = "image/*,video/*,audio/*"

// fileRow is a row of the media list or the files list: a file the owner keeps.
type fileRow struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`              // the first part of its type: image, video, audio; application, text
	MIME     string `json:"mime"`              // of the original
	Picture  string `json:"picture,omitempty"` // the address of its picture, when it keeps one
	Day      string `json:"day,omitempty"`     // its own day, else the day of the newest day page that shows it
	Mentions int    `json:"mentions"`
}

// mediaCounts counts the live files of each kind the list shows.
type mediaCounts struct {
	Image int `json:"image"`
	Video int `json:"video"`
	Audio int `json:"audio"`
}

func (h *server) media(r *http.Request) (*Entity, error) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !slices.Contains(core.MediaKinds, kind) {
		return nil, core.Invalid("kind must be image, video or audio")
	}
	limit, offset, err := pageOf(r, 200)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	self := "/media"
	if kind != "" {
		q.Set("kind", kind)
		self += "?kind=" + kind
	}
	add := action("add-file", nil, nil)
	for i := range add.Fields {
		if add.Fields[i].Name == "original" {
			add.Fields[i].Accept = mediaAccept
		}
	}
	e := &Entity{Class: []string{"media"}, Title: "Media",
		Links: []Link{link("self", self, "Media"), link("index", "/", "Home"), link("pictures", "/media?kind=image", "Pictures"),
			link("videos", "/media?kind=video", "Videos"), link("recordings", "/media?kind=audio", "Recordings")},
		Actions: []Action{add, action("tombstone-pages", nil, nil)}} // the list deletes several at once: a browser draws a box on each tile
	counts, err := h.s.MediaCounts(r.Context())
	if err != nil {
		return nil, err
	}
	found, err := h.s.Media(r.Context(), kind, limit+1, offset)
	if err != nil {
		return nil, err
	}
	found = paged(e, found, "/media", q, limit, offset)
	props := e.Properties.(map[string]any)
	props["kind"], props["rows"] = kind, h.fileRows(e, found)
	props["counts"] = mediaCounts{Image: counts["image"], Video: counts["video"], Audio: counts["audio"]}
	return e, nil
}

// files is the list of every file that is not media: a PDF, a text, a scan's words.
func (h *server) files(r *http.Request) (*Entity, error) {
	limit, offset, err := pageOf(r, 200)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"files"}, Title: "Files",
		Links:   []Link{link("self", "/files", "Files"), link("index", "/", "Home"), link("media", "/media", "Media")},
		Actions: []Action{action("add-file", nil, nil), action("tombstone-pages", nil, nil)}}
	found, err := h.s.OtherFiles(r.Context(), limit+1, offset)
	if err != nil {
		return nil, err
	}
	found = paged(e, found, "/files", nil, limit, offset)
	e.Properties.(map[string]any)["rows"] = h.fileRows(e, found)
	return e, nil
}

// fileRows are the rows of a list of files, each also an item link of the entity.
func (h *server) fileRows(e *Entity, found []core.FileRow) []fileRow {
	rows := []fileRow{}
	for _, m := range found {
		row := fileRow{ID: m.ID, Title: m.Title, Kind: m.MIME[:strings.Index(m.MIME, "/")], MIME: m.MIME, Day: m.Day, Mentions: m.Mentions}
		if m.Picture {
			row.Picture = pageHref(m.ID) + "/preview"
		}
		rows = append(rows, row)
		e.Entities = append(e.Entities, Link{Rel: []string{"item"}, Href: pageHref(m.ID), Title: m.Title, Class: []string{"file"}})
	}
	return rows
}

// FileWords says what a row of a list of files is, and when: "picture · Wed 14 May", "recording · no day yet",
// "application/pdf · Mon 12 May" (a file of another kind is named by its type).
func (v *view) FileWords(kind, mime, day any) string {
	words, ok := map[string]string{"image": "picture", "video": "video", "audio": "recording"}[kind.(string)]
	if !ok {
		words, _ = mime.(string)
	}
	if d, _ := day.(string); d != "" {
		return words + " · " + v.clock().day(d)
	}
	return words + " · no day yet"
}

// MediaCount says a count of the media list in words: "3 pictures", "1 video", "no recordings".
func (v *view) MediaCount(n any, one, many string) string {
	c, _ := n.(float64)
	switch c {
	case 0:
		return "no " + many
	case 1:
		return "1 " + one
	}
	return num(c) + " " + many
}
