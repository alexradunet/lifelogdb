// Package api serves life.db as hypermedia: Siren-style JSON for programs, plain HTML for a browser, one
// representation behind both. A client needs no rules of its own: an action is offered only where it is legal,
// with its fields (and their current values) filled in. The Siren types and the addresses of the resources are in
// package siren, which the clients import too; package view draws an entity as a page, and reads no store. This
// file holds the catalog of the actions.
package api

import (
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

const formType = "application/x-www-form-urlencoded"

// spec is one action of the catalog: a templated href ("/pages/{id}/body") and its fields, path fields included.
// An Owner action is the owner's alone: refused to an agent:* writer and never an MCP tool. A Danger action hides or
// retracts what is already there: a browser draws its button as a danger and any other client may warn the same way;
// no surface decides this by an action's name.
type spec struct {
	Name, Title, Description, Method, Path string
	Fields                                 []siren.Field
	Owner, Danger                          bool
}

func path(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title, Required: true, In: "path"}
}

func req(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title, Required: true}
}

func opt(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title}
}

// pageField is a text field whose value is a page's title; required says whether the action needs one.
func pageField(name, title string, required bool) siren.Field {
	return siren.Field{Name: name, Type: "text", Title: title, Required: required, Pages: true}
}

// area is a multi-line field a browser draws rows lines tall.
func area(name, title string, rows int) siren.Field {
	return siren.Field{Name: name, Type: "textarea", Title: title, Rows: rows}
}

// areaReq is a multi-line field the action cannot do without.
func areaReq(name, title string, rows int) siren.Field {
	f := area(name, title, rows)
	f.Required = true
	return f
}

// catalog is every action the API has. mount gives each one its route, GET /actions serves it, and the CLI and the MCP
// server are built from it.
var catalog = []spec{
	{"capture", "Add to the day", "Append an entry to the journal page of a local day (created on the first capture), with an optional mood 1-5. [[Title]] and #tag in the text link pages, creating them if new.",
		"POST", "/days/{day}/capture", []siren.Field{path("day", "date", "Local day"), area("text", "Entry (CommonMark)", 4),
			{Name: "mood", Type: "number", Title: "Mood 1-5", Options: []string{"1", "2", "3", "4", "5"}}}, false, false},
	{"create-page", "New page", "Create a page with a unique title (a valid file name) and a CommonMark body. category files it in that category page (D26) in the same write, making the category when it is new.",
		"POST", "/pages", []siren.Field{req("title", "text", "Title"), area("body", "Body", 12), pageField("category", "Category (title)", false)}, false, false},
	{"save-body", "Save", "Replace a page's whole body. version must be the page's current version (from the last read); wikilinks are re-synced.",
		"POST", "/pages/{id}/body", []siren.Field{path("id", "number", "Page id"), areaReq("body", "Body", 18), req("version", "hidden", "Version")}, false, false},
	{"create-person", "New person", "Create a person: title is the permanent handle that [[wikilinks]] use (e.g. 'Sam (barber)'), name the full name. category files it in that category page (D26) in the same write, making the category when it is new.",
		"POST", "/people", []siren.Field{req("title", "text", "Handle"), opt("name", "text", "Full name"), opt("birth_day", "date", "Born"), opt("death_day", "date", "Died"),
			pageField("category", "Category (title)", false)}, false, false},
	{"create-place", "New place", "Create a place; its title is its name and its handle. category files it in that category page (D26) in the same write, making the category when it is new.",
		"POST", "/places", []siren.Field{req("title", "text", "Name"), pageField("category", "Category (title)", false)}, false, false},
	{"add-file", "New file", "Keep a file the owner picked (a recording, a PDF, a scan, a photo) as a page: title is its permanent handle, and ![[title]] in a page shows it; body is its text (a transcript, the text of a PDF, a caption). Send the original, which is read and never stored, or its sha256 and mime. A picture is made from the original when it is a JPEG, PNG or GIF; for any other (HEIC, a video's frame) send preview, a JPEG, PNG or GIF. The file's own day is day, else a photo's EXIF date, read from the original. The page of its own day shows a file that has a picture. A photo with no day of its own gets no day and goes in no day page; any other file without one gets the day it is kept. The same original again finds its page and writes nothing but a missing picture and its embed in the page of its own day. category files the page in that category page (D26) in the same write, making the category when it is new; the page of the same original is filed too, unless it is deleted.",
		"POST", "/files", []siren.Field{req("title", "text", "Title"), opt("original", "file", "The file (read, never stored)"), opt("sha256", "text", "SHA-256 of the original, when it is not sent"),
			opt("mime", "text", "Type, e.g. audio/mp4"), opt("preview", "file", "Picture, when the original is not a JPEG, PNG or GIF"), area("body", "Text: a transcript, a caption", 6), opt("day", "date", "Its day (default: a photo's EXIF date, else none; any other file: today)"),
			pageField("category", "Category (title)", false),
			{Name: "dry_run", Type: "number", Title: "Dry run: write nothing, say what keeping it would do", Options: []string{"0", "1"}}}, false, false},
	{"set-picture", "Set picture", "Make a photo this page's picture: its embed goes first in the body, and the people list shows a person's. Send a new photo as original (kept as a file with no day, titled with the page's title and today unless title names it), or name a kept picture by title.",
		"POST", "/pages/{id}/picture", []siren.Field{path("id", "number", "Page id"), {Name: "original", Type: "file", Title: "A new photo (read, never stored)", Accept: "image/*"},
			opt("title", "text", "A kept picture's title; with a new photo, its title"), opt("preview", "file", "A JPEG, PNG or GIF of the photo, when it is not one")}, false, false},
	{"promote", "Promote", "Make a plain page (e.g. one a [[wikilink]] created) a person or a place; its id and links stay.",
		"POST", "/pages/{id}/promote", []siren.Field{path("id", "number", "Page id"), {Name: "to", Type: "text", Title: "Into", Required: true, Options: []string{"person", "place"}}, opt("name", "text", "Full name (a person)")}, false, false},
	{"tombstone", "Delete", "Tombstone a page (nothing is ever removed; revive undoes it).",
		"POST", "/pages/{id}/tombstone", []siren.Field{path("id", "number", "Page id")}, false, true},
	{"revive", "Restore", "Undo a tombstone.",
		"POST", "/pages/{id}/revive", []siren.Field{path("id", "number", "Page id")}, false, false},
	{"tombstone-pages", "Delete pages", "Tombstone several pages (files among them) in one write: ids are page ids, separated by spaces or commas. An id no page has refuses the whole action; revive undoes each.",
		"POST", "/pages/tombstone", []siren.Field{req("ids", "text", "Page ids, separated by spaces or commas")}, false, true},
	{"link", "Add a link", "Add a typed link from this page to the page titled `to` (kinds: at = a day page was at a place; part-of = filed in the category whose page is `to`).",
		"POST", "/pages/{id}/links", []siren.Field{path("id", "number", "Page id"), pageField("to", "To (title)", true), req("kind", "text", "Kind"), opt("note", "text", "Note")}, false, false},
	{"unlink", "Remove a link", "Remove a typed link from this page to the page titled `to`.",
		"POST", "/pages/{id}/unlink", []siren.Field{path("id", "number", "Page id"), pageField("to", "To (title)", true), req("kind", "text", "Kind")}, false, false},
	{"file-pages", "Put in a category", "File several pages (metrics among them) in the category page titled `category`, in one write (a part-of link from each): ids are page ids, separated by spaces or commas. A missing category is made as an empty page. With move 1, every other part-of link of those pages is removed first. An id no page has refuses the whole action.",
		"POST", "/pages/file", []siren.Field{req("ids", "text", "Page ids, separated by spaces or commas"), pageField("category", "Category (title)", true),
			{Name: "move", Type: "number", Title: "Move (remove the other categories)", Options: []string{"0", "1"}}}, false, false},
	{"record", "Add a reading", "Record a reading of a registered metric for a local day. import_key makes a re-send a no-op. A view offers the metrics read in its last 60 days; /metrics lists them all.",
		"POST", "/measurements", []siren.Field{req("metric", "text", "Metric"), req("day", "date", "Day"), req("value", "number", "Value"),
			opt("taken_at", "text", "Taken at (UTC, 2026-06-09T21:14:03.482Z)"), opt("tz", "text", "Zone (IANA)"), opt("import_key", "text", "Key")}, false, false},
	{"correct", "Correct", "Correct a reading with a new value (a reading is corrected once; correct the correction after that). To remove a reading, retract it.",
		"POST", "/measurements/{id}/correct", []siren.Field{path("id", "number", "Measurement id"), req("value", "number", "Value")}, false, false},
	{"retract", "Retract", "Retract a reading that should never have existed.",
		"POST", "/measurements/{id}/retract", []siren.Field{path("id", "number", "Measurement id")}, false, true},
	{"rename", "Rename", "Select a new preferred reference name without changing this identity, body or links. Previous names remain aliases. Another identity's name and journal dates are refused.",
		"POST", "/pages/{id}/rename", []siren.Field{path("id", "number", "Page id"), req("title", "text", "New title")}, false, false},
	{"register-metric", "New metric", "Register a metric with a preferred reference name; its unit never changes ('' for a count or a 0/1 habit; a scale names its range, '1-5'); the note is the page's text. category files it in that category page (made when missing) in the same write. A metric that exists is returned as it is when the unit is the same, or when it is a scale and the unit is empty.",
		"POST", "/metrics", []siren.Field{req("name", "text", "Name"), opt("unit", "text", "Unit"), opt("note", "text", "Note"),
			pageField("category", "Category (title)", false)}, true, false},
	{"integrity-check", "Integrity", "Run the four integrity checks of the database (docs/contract/integrity-checks.md).",
		"GET", "/integrity", nil, true, false},
	{"search", "Search", "Full-text search over page titles and bodies (FTS5 syntax).",
		"GET", "/search", []siren.Field{req("q", "search", "Search")}, false, false},
	{"browse", "Browse", "List the things of one kind, with the category tree. kind is page, person, place, media, document or metric (default page), or all, which needs in. in is the title of a category: only what is filed directly in it (default kind all). unfiled 1 lists only what is in no category. ghosts 1 lists the ghost pages (kind page only). media is image, video or audio (kind media only). limit and offset select the page of rows. A combination with no meaning is refused.",
		"GET", "/browse", []siren.Field{{Name: "kind", Type: "text", Title: "Kind", Options: browseKinds},
			pageField("in", "In the category (title)", false),
			{Name: "unfiled", Type: "number", Title: "Only what is in no category", Options: []string{"0", "1"}},
			{Name: "ghosts", Type: "number", Title: "Only the ghost pages", Options: []string{"0", "1"}},
			{Name: "media", Type: "text", Title: "Media kind", Options: core.MediaKinds()},
			opt("limit", "number", "Rows on a page (1-500, default 200)"), opt("offset", "number", "Rows skipped")}, false, false},
	{"find", "Find page", "Open the page with this title (any case or normalisation).",
		"GET", "/pages", []siren.Field{pageField("title", "Title", true)}, false, false},
	{"open-day", "Go to a day", "Open the day view of a local day: its journal, the pages of that day, the places it was at and its readings.",
		"GET", "/days/go", []siren.Field{opt("day", "date", "Day (default today)")}, false, false},
	{"snapshot", "Take a snapshot", "The owner's dated copy of life.db (docs/cookbook/take-a-snapshot.md) into the server's snapshot folder, never overwriting, then its restore check. The answer names the file and the check.",
		"POST", "/snapshots", nil, true, false},
	{"rebuild-search", "Rebuild search", "The owner's repair of the full-text index: rebuild it from the pages (docs/contract/integrity-checks.md). Run it when integrity-check says the index is not ok. It reads every page and holds the write lock while it runs. The answer says how many documents the new index holds.",
		"POST", "/search/rebuild", nil, true, false},
	{"resync-links", "Resync links", "The owner's repair of the links of the bodies: every live page whose [[wikilinks]] differ from its body gets the links of its body, as a save of the same body would give them (docs/contract/integrity-checks.md). A page whose links are right is not written. A page that a body names and no page has is made, and a deleted one is revived, as a save does. The answer lists the pages rewritten.",
		"POST", "/links/resync", nil, true, false},
}

func specOf(name string) spec {
	for _, s := range catalog {
		if s.Name == name {
			return s
		}
	}
	panic("no action " + name)
}

// base is a spec's action before its fields: what action and templated share.
func (s spec) base() siren.Action {
	a := siren.Action{Name: s.Name, Title: s.Title, Description: s.Description, Method: s.Method, Href: s.Path, Owner: s.Owner, Danger: s.Danger}
	if s.Method == "POST" {
		a.Type = formType
	}
	return a
}

// action instantiates a catalog action: path fields fill the href (escaped for a path) and drop out, values prefill
// the rest.
func action(name string, params map[string]string, values map[string]any) siren.Action {
	s := specOf(name)
	a := s.base()
	for _, f := range s.Fields {
		if f.In == "path" {
			a.Href = strings.ReplaceAll(a.Href, "{"+f.Name+"}", url.PathEscape(params[f.Name]))
			continue
		}
		if v, ok := values[f.Name]; ok {
			f.Value = v
		}
		a.Fields = append(a.Fields, f)
	}
	return a
}

// templated is a catalog action as /actions shows it: the href keeps its {placeholders}. The fields are a copy, so a
// caller that changes them leaves the catalog as it is.
func templated(s spec) siren.Action {
	a := s.base()
	a.Fields = slices.Clone(s.Fields)
	return a
}

func link(rel, href, title string) siren.Link {
	return siren.Link{Rel: []string{rel}, Href: href, Title: title}
}

func selfOf(e *siren.Entity) string {
	if l := e.LinkByRel("self"); l != nil {
		return l.Href
	}
	return ""
}

func withOptions(a siren.Action, field string, opts []string) siren.Action {
	if f := a.Field(field); f != nil {
		f.Options = opts
	}
	return a
}

func withResult(e *siren.Entity, err error) func(any) (*siren.Entity, error) {
	return func(res any) (*siren.Entity, error) {
		if e != nil {
			e.Result = res
		}
		return e, err
	}
}
