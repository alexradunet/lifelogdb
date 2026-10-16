package api

import (
	"net/http"
	"strings"
)

// Editing (docs/plans/091-the-browser-for-daily-use.md): a body rendered as the browser shows it saved, and an
// editor as tall as its body.

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
	answer(w, r, http.StatusOK, &Entity{Class: []string{"preview"}, Title: "Preview", Properties: map[string]any{"body": body}}, frame{pageOnly: true})
}

// saveBody is the save-body of a page, its body and version filled. Its textarea is as tall as the body, 12 to 40
// lines, so a long page is edited without a scroll inside a small box.
func saveBodyAction(id int64, body, version string) Action {
	a := action("save-body", idParam(id), map[string]any{"body": body, "version": version})
	a.field("body").Rows = min(max(strings.Count(body, "\n")+2, 12), 40)
	return a
}
