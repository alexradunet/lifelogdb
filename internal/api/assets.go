package api

// The routes that only a browser asks for: the embedded fonts, the manifest and its icon (webFiles), and the style
// page.

import (
	"bytes"
	"net/http"
	"strings"
	"time"
)

// fontTypes are the files the font route serves, by their extension: the faces and their licences.
var fontTypes = map[string]string{"woff2": "font/woff2", "txt": "text/plain; charset=utf-8"}

// fonts is GET /assets/fonts/{file}: an embedded face or licence, by its exact name (html/fonts/README.md). It is
// presentation, like /render, and not in the catalog. A file never changes under its name, so a browser keeps it
// for a year.
func fonts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ := fontTypes[name[strings.LastIndex(name, ".")+1:]]
	if typ == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := webFiles.ReadFile("html/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// styleguide is GET /styleguide: every component of the Almanac in every state, on synthetic words
// (html/styleguide.html). It is presentation, like /render, and not in the catalog. The template reads nothing from
// the entity, so the page shows nothing of the database; only the shell around it reads, as for any page.
func (h *server) styleguide(w http.ResponseWriter, r *http.Request) {
	answer(w, r, http.StatusOK, &Entity{Class: []string{"styleguide"}, Title: "The style page"}, frame{shell: h.s, pageOnly: true})
}

// install serves an embedded file of html/ for a browser's install of the app: the manifest and its icon. The
// cache says how long a browser keeps it. The file is the same for every request, so it needs no read of the store.
func install(name, typ, cache string) http.HandlerFunc {
	b := mustWebFile(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	}
}
