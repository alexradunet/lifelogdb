package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"lifelog/internal/core"
)

// Editing (docs/plans/091-the-browser-for-daily-use.md): the names after [[ as a resource, a body rendered as the
// browser shows it saved, and an editor as tall as its body.

// maxTitles is the most names one read of /titles answers.
const maxTitles = 20

// titles is GET /titles?prefix=&limit=: the names of live pages that start with the prefix, in key order.
func (h *server) titles(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	if strings.TrimSpace(prefix) == "" {
		return nil, core.Invalid("missing field prefix")
	}
	limit := 10
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxTitles {
			return nil, core.Invalid("limit must be 1 to %d", maxTitles)
		}
		limit = n
	}
	ms, err := h.s.TitlesByPrefix(r.Context(), prefix, limit)
	if err != nil {
		return nil, err
	}
	self := "/titles?" + url.Values{"prefix": {prefix}, "limit": {strconv.Itoa(limit)}}.Encode()
	e := &Entity{Class: []string{"titles"}, Title: "Titles that start with " + prefix,
		Properties: map[string]any{"prefix": prefix, "limit": limit, "titles": ms},
		Links:      []Link{link("self", self, "Titles"), link("index", "/", "Home")}}
	for _, m := range ms {
		e.Entities = append(e.Entities, Link{Rel: []string{"item"}, Href: pageHref(m.PageID), Title: m.Title, Class: []string{m.Type}})
	}
	return e, nil
}

// render is POST /render: a page that shows the field body (or text, a capture's) as its page will show it saved,
// the Preview of a form that writes a body, opened in a new tab so the form keeps its words. It writes nothing. It
// is presentation, not a capability of the life log, so no catalog action names it and it answers HTML only; it
// is behind the request limits, the origin guard and the token like every other POST.
func (h *server) render(w http.ResponseWriter, r *http.Request) {
	v, err := form(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	body := v.Get("body")
	if body == "" {
		body = v.Get("text")
	}
	w.Header().Set("Cache-Control", "no-store")
	renderHTML(w, r, http.StatusOK, &Entity{Class: []string{"preview"}, Title: "Preview", Properties: map[string]any{"body": body}}, nil)
}

// saveBody is the save-body of a page, its body and version filled. Its textarea is as tall as the body, 12 to 40
// lines, so a long page is edited without a scroll inside a small box.
func saveBodyAction(id int64, body, version string) Action {
	a := action("save-body", idParam(id), map[string]any{"body": body, "version": version})
	a.field("body").Rows = min(max(strings.Count(body, "\n")+2, 12), 40)
	return a
}
