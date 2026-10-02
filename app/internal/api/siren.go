// Package api serves life.db as hypermedia: Siren-style JSON for programs, plain HTML for a browser, one
// representation behind both. A client needs no rules of its own: an action is offered only where it is legal,
// with its fields (and their current values) filled in.
package api

import (
	"strings"
)

// Entity is a Siren entity. Result is an extension: what a POST did (e.g. the wikilinks a save skipped).
type Entity struct {
	Class      []string `json:"class,omitempty"`
	Title      string   `json:"title,omitempty"`
	Properties any      `json:"properties,omitempty"`
	Entities   []Link   `json:"entities,omitempty"`
	Actions    []Action `json:"actions,omitempty"`
	Links      []Link   `json:"links,omitempty"`
	Result     any      `json:"result,omitempty"`
}

// Link is a Siren link, also used as an embedded link (a sub-entity that is only a reference).
type Link struct {
	Rel   []string `json:"rel"`
	Href  string   `json:"href"`
	Title string   `json:"title,omitempty"`
	Class []string `json:"class,omitempty"`
}

// Action is a Siren action. Description (what the action does, for an agent) and Owner (the owner's alone) are
// extensions.
type Action struct {
	Name        string  `json:"name"`
	Owner       bool    `json:"owner,omitempty"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Method      string  `json:"method"`
	Href        string  `json:"href"`
	Type        string  `json:"type,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
}

// Field is a Siren field. In "path" (an extension) marks a field of the catalog's templated href.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Title    string   `json:"title,omitempty"`
	Value    any      `json:"value,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	In       string   `json:"in,omitempty"`
}

const formType = "application/x-www-form-urlencoded"

// spec is one action of the catalog: a templated href ("/pages/{id}/body") and its fields, path fields included.
// An Owner action is the owner's alone: refused to an agent:* writer and never an MCP tool.
type spec struct {
	Name, Title, Description, Method, Path string
	Fields                                 []Field
	Owner                                  bool
}

func path(name, typ, title string) Field {
	return Field{Name: name, Type: typ, Title: title, Required: true, In: "path"}
}
func req(name, typ, title string) Field {
	return Field{Name: name, Type: typ, Title: title, Required: true}
}
func opt(name, typ, title string) Field { return Field{Name: name, Type: typ, Title: title} }

// catalog is every action the API has. GET /actions serves it, and the CLI and the MCP server are built from it.
var catalog = []spec{
	{"capture", "Capture", "Append an entry to the journal page of a local day (created on the first capture), with an optional mood 1-5. [[Title]] and #tag in the text link pages, creating them if new.",
		"POST", "/days/{day}/capture", []Field{path("day", "date", "Local day, YYYY-MM-DD"), opt("text", "textarea", "Entry (CommonMark)"), opt("mood", "number", "Mood 1-5")}, false},
	{"create-page", "New page", "Create a page with a unique title (a valid file name) and a CommonMark body.",
		"POST", "/pages", []Field{req("title", "text", "Title"), opt("body", "textarea", "Body")}, false},
	{"save-body", "Save", "Replace a page's whole body. version must be the page's current version (from the last read); wikilinks are re-synced.",
		"POST", "/pages/{id}/body", []Field{path("id", "number", "Page id"), req("body", "textarea", "Body"), req("version", "hidden", "Version")}, false},
	{"create-person", "New person", "Create a person: title is the permanent handle that [[wikilinks]] use (e.g. 'Sam (barber)'), name the full name.",
		"POST", "/people", []Field{req("title", "text", "Handle"), opt("name", "text", "Full name"), opt("birth_day", "date", "Born"), opt("death_day", "date", "Died")}, false},
	{"create-place", "New place", "Create a place; its title is its name and its handle.",
		"POST", "/places", []Field{req("title", "text", "Name")}, false},
	{"promote", "Promote", "Make a plain page (e.g. one a [[wikilink]] created) a person or a place; its id and links stay.",
		"POST", "/pages/{id}/promote", []Field{path("id", "number", "Page id"), {Name: "to", Type: "text", Title: "Into", Required: true, Options: []string{"person", "place"}}, opt("name", "text", "Full name (a person)")}, false},
	{"tombstone", "Delete", "Tombstone a page (nothing is ever removed; revive undoes it).",
		"POST", "/pages/{id}/tombstone", []Field{path("id", "number", "Page id")}, false},
	{"revive", "Revive", "Undo a tombstone.",
		"POST", "/pages/{id}/revive", []Field{path("id", "number", "Page id")}, false},
	{"link", "Link", "Add a typed link from this page to the page titled `to` (kinds: at = a day page was at a place; about; located-in; parent-of; friend; family; related).",
		"POST", "/pages/{id}/links", []Field{path("id", "number", "Page id"), req("to", "text", "To (title)"), req("kind", "text", "Kind"), opt("note", "text", "Note")}, false},
	{"unlink", "Unlink", "Remove a typed link from this page to the page titled `to`.",
		"POST", "/pages/{id}/unlink", []Field{path("id", "number", "Page id"), req("to", "text", "To (title)"), req("kind", "text", "Kind")}, false},
	{"record", "Record", "Record a reading of a registered metric for a local day. import_key makes a re-send a no-op.",
		"POST", "/measurements", []Field{req("metric", "text", "Metric"), req("day", "date", "Day"), req("value", "number", "Value"),
			opt("taken_at", "text", "Taken at (UTC, 2026-06-09T21:14:03.482Z)"), opt("tz", "text", "Zone (IANA)"), opt("import_key", "text", "Key")}, false},
	{"correct", "Correct", "Correct a reading with a new value (a reading is corrected once; correct the correction after that).",
		"POST", "/measurements/{id}/correct", []Field{path("id", "number", "Measurement id"), req("value", "number", "Value")}, false},
	{"retract", "Retract", "Retract a reading that should never have existed.",
		"POST", "/measurements/{id}/retract", []Field{path("id", "number", "Measurement id")}, false},
	{"rename", "Rename", "Give a plain page a new title: the text moves to the page with that title and this page becomes a #REDIRECT stub. Into an existing title only when this page is empty.",
		"POST", "/pages/{id}/rename", []Field{path("id", "number", "Page id"), req("title", "text", "New title")}, false},
	{"register-metric", "New metric", "Register a metric: a lowercase snake_case name and a unit that never changes ('' for a unitless 1-5 scale or a 0/1 habit).",
		"POST", "/metrics", []Field{req("name", "text", "Name"), opt("unit", "text", "Unit"), opt("note", "text", "Note")}, true},
	{"start-habit", "Start habit", "Make a unitless metric a habit from a day on (end_day: the last day of a past period).",
		"POST", "/metrics/{name}/periods", []Field{path("name", "text", "Metric"), req("start_day", "date", "From"), opt("end_day", "date", "Until")}, false},
	{"stop-habit", "Stop habit", "End a habit's open period on a day (inclusive).",
		"POST", "/metrics/{name}/stop", []Field{path("name", "text", "Metric"), req("day", "date", "Last day")}, false},
	{"check-in", "Check in", "Record a habit for a day: done 1, not done 0.",
		"POST", "/metrics/{name}/check-in", []Field{path("name", "text", "Habit"), req("day", "date", "Day"), {Name: "done", Type: "number", Title: "Done", Required: true, Options: []string{"1", "0"}}}, false},
	{"integrity-check", "Integrity", "Run the four integrity checks of the database (docs/contract/integrity-checks.md).",
		"GET", "/integrity", nil, false},
	{"search", "Search", "Full-text search over page titles and bodies (FTS5 syntax).",
		"GET", "/search", []Field{req("q", "search", "Search")}, false},
	{"find", "Find page", "Open the page with this title (any case or normalisation).",
		"GET", "/pages", []Field{req("title", "text", "Title")}, false},
	{"query", "Query", "Run one read-only SQL statement (SQLite; see docs/cookbook for the canonical queries). At most 500 rows.",
		"POST", "/query", []Field{req("sql", "textarea", "SQL")}, false},
}

func specOf(name string) spec {
	for _, s := range append(catalog, importCatalog...) {
		if s.Name == name {
			return s
		}
	}
	panic("no action " + name)
}

// action instantiates a catalog action: path fields fill the href and drop out, values prefill the rest.
func action(name string, params map[string]string, values map[string]any) Action {
	s := specOf(name)
	a := Action{Name: s.Name, Title: s.Title, Description: s.Description, Method: s.Method, Href: s.Path, Owner: s.Owner}
	if s.Method == "POST" {
		a.Type = formType
	}
	for _, f := range s.Fields {
		if f.In == "path" {
			a.Href = strings.ReplaceAll(a.Href, "{"+f.Name+"}", params[f.Name])
			continue
		}
		if v, ok := values[f.Name]; ok {
			f.Value = v
		}
		a.Fields = append(a.Fields, f)
	}
	return a
}

// templated is a catalog action as /actions shows it: the href keeps its {placeholders}.
func templated(s spec) Action {
	a := Action{Name: s.Name, Title: s.Title, Description: s.Description, Method: s.Method, Href: s.Path, Fields: s.Fields, Owner: s.Owner}
	if s.Method == "POST" {
		a.Type = formType
	}
	return a
}
