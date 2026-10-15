package api

// The embedded assets a browser gets: the fonts, the style page, the manifest and the icon, the sidebar's icons.

import (
	"bytes"
	"html/template"
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
	b, err := views.ReadFile("html/fonts/" + name)
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
	h.inShell(w, r, http.StatusOK, &Entity{Class: []string{"styleguide"}, Title: "The style page"})
}

// install serves an embedded file of html/ for a browser's install of the app: the manifest and its icon. The
// cache says how long a browser keeps it. The file is the same for every request, so it needs no read of the store.
func install(name, typ, cache string) http.HandlerFunc {
	b := mustView(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	}
}

// icons are the sidebar's line icons (the Icons of the design system): 16 px, a 1.5 px stroke in the colour of
// their text.
var icons = map[string]string{
	"today":      `<circle cx="8" cy="8" r="3"/><path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6 13 13M3 13l1.4-1.4M11.6 4.4 13 3"/>`,
	"calendar":   `<rect x="2" y="3" width="12" height="11" rx="2"/><path d="M2 6.5h12M5 1.5v3M11 1.5v3"/>`,
	"due":        `<circle cx="8" cy="8" r="6"/><path d="M5.5 8.2l1.8 1.8 3.3-3.6"/>`,
	"tasks":      `<path d="M6 4h8M6 8h8M6 12h8M2 4h1M2 8h1M2 12h1"/>`,
	"categories": `<path d="M2 3.5h4l1.5 1.5H14v8.5H2z"/><path d="M2 7.5h12"/>`,
	"people":     `<circle cx="8" cy="5.5" r="2.5"/><path d="M3 14c.6-2.8 2.6-4.2 5-4.2s4.4 1.4 5 4.2"/>`,
	"places":     `<path d="M8 14.5s4.5-4.2 4.5-8a4.5 4.5 0 0 0-9 0c0 3.8 4.5 8 4.5 8z"/><circle cx="8" cy="6.5" r="1.6"/>`,
	"files":      `<path d="M4 1.5h5l3 3v10H4z"/><path d="M9 1.5v3h3"/>`,
	"media":      `<rect x="2" y="3" width="12" height="10" rx="1.5"/><circle cx="5.5" cy="6.5" r="1.3"/><path d="M2.5 11.5l3.5-3.5 2.5 2.5 2-2 3 3"/>`,
	"metrics":    `<path d="M1.5 9h3l2-5 3 9 2-4h3"/>`,
	"habits":     `<path d="M3 6a5 5 0 0 1 9-1.5M13 10a5 5 0 0 1-9 1.5M12 1.5v3h-3M4 14.5v-3h3"/>`,
	"ghosts":     `<path d="M4 1.5h5l3 3v10H4z" stroke-dasharray="2 2"/>`,
	"search":     `<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/>`,
	"new":        `<path d="M8 3v10M3 8h10"/>`,
	"chat":       `<path d="M2 3.5h12v7.5H7l-3 2.5V11H2z"/>`,
	"more":       `<circle cx="3.5" cy="8" r=".9"/><circle cx="8" cy="8" r=".9"/><circle cx="12.5" cy="8" r=".9"/>`,
	// the kinds of the media list, on a blank tile: a picture, a video, a recording
	"image": `<rect x="2" y="3" width="12" height="10" rx="1.5"/><circle cx="5.5" cy="6.5" r="1.3"/><path d="M2.5 11.5l3.5-3.5 2.5 2.5 2-2 3 3"/>`,
	"video": `<rect x="1.5" y="4" width="9" height="8" rx="1.5"/><path d="M10.5 7l4-2v6l-4-2z"/>`,
	"audio": `<rect x="6" y="1.5" width="4" height="8" rx="2"/><path d="M3.5 7.5a4.5 4.5 0 0 0 9 0M8 12v2.5M5.5 14.5h5"/>`,
}

// icon is an icon of the sidebar as inline SVG, hidden from a reader: an icon is always beside its word.
func icon(name string) template.HTML {
	p, ok := icons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 16 16" aria-hidden="true">` + p + `</svg>`)
}
