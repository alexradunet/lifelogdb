// Package siren is the hypermedia of the API: the Siren types that every answer is made of (an entity, its links, its
// actions and their fields), the header that names the writer of a request, the JSON of an answer, and the addresses
// of the resources that the API serves and that its view links. The API (internal/api), its view (internal/api/view),
// the clients (internal/client, cmd/lifelog) and core (for the codes of an error) import it. It imports only the
// standard library, so a client of the types gets none of the server with them.
package siren

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

// The prefixes of the writers that are not the owner: an agent (the MCP server) writes as AgentPrefix+name, an import
// as ImportPrefix+name. The owner-only actions are refused to both (api.requireOwner).
const (
	AgentPrefix  = "agent:"
	ImportPrefix = "import:"
)

// The codes of the error entity (property code; API.md, Errors; core.Error.Code). A client branches on a code, so a
// string never changes. They are constants so that the code that makes one (core, the API) and the code that picks
// words for it (the view) cannot drift apart: a renamed code fails to compile instead of drawing an empty alert.
// The API gives the general code of a status when a failure names none (codeOf).
const (
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeTooLarge           = "too_large"
	CodeUnsupportedMedia   = "unsupported_media" // a multipart body sent to an action that takes no file
	CodeInvalid            = "invalid"
	CodeTimeout            = "timeout"
	CodeInternal           = "internal"
	CodeExists             = "exists"               // a title in use (core.ExistsError)
	CodeCrossOrigin        = "cross_origin"         // a browser write from an origin that the server does not trust
	CodeTokenRequired      = "token_required"       // a request with no token, where the server needs one
	CodeStaleVersion       = "stale_version"        // a page saved from a version that is not the current one
	CodeSnapshotRefused    = "snapshot_refused"     // the snapshot action refused its target
	CodeRestoreCheckFailed = "restore_check_failed" // a snapshot that did not restore
	// CodeBusy is the code of a write that met a locked file (SQLITE_BUSY): the CLI and the service write one file,
	// and a write can wait longer than busy_timeout for the other.
	CodeBusy = "busy"
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

// Action is a Siren action. Description (what the action does, for an agent), Owner (the owner's alone) and
// Danger (it hides or retracts what is there) are extensions.
type Action struct {
	Name        string  `json:"name"`
	Owner       bool    `json:"owner,omitempty"`
	Danger      bool    `json:"danger,omitempty"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Method      string  `json:"method"`
	Href        string  `json:"href"`
	Type        string  `json:"type,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
}

// Field is a Siren field. In "path" (an extension) marks a field of the catalog's templated href; Rows (an
// extension) is how many lines a browser draws of a textarea; Pages (an extension) says the value is a page's
// title, so a browser can offer the names of the pages as it does after [[; Accept (an extension) is what a file
// field takes, as the accept attribute of a browser's file input ("audio/*").
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Title    string   `json:"title,omitempty"`
	Value    any      `json:"value,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	In       string   `json:"in,omitempty"`
	Rows     int      `json:"rows,omitempty"`
	Pages    bool     `json:"pages,omitempty"`
	Accept   string   `json:"accept,omitempty"`
}

// LinkByRel is the entity's link with that rel, nil when it has none.
func (e *Entity) LinkByRel(rel string) *Link {
	for i := range e.Links {
		for _, r := range e.Links[i].Rel {
			if r == rel {
				return &e.Links[i]
			}
		}
	}
	return nil
}

// Field is the action's field with that name, nil when it has none.
func (a *Action) Field(name string) *Field {
	for i := range a.Fields {
		if a.Fields[i].Name == name {
			return &a.Fields[i]
		}
	}
	return nil
}

// WriteJSON writes v to w as the API's JSON: with no HTML escapes, so a body reads as it was written (< > & stay as
// they are); indented by indent ("" for none); with a newline at the end. The API, the CLI and the MCP server write
// their JSON with it.
func WriteJSON(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	return enc.Encode(v)
}

// PageHref is the address of a page.
func PageHref(id int64) string { return "/pages/" + strconv.FormatInt(id, 10) }

// CoverHref is the address of the choice of a page's cover.
func CoverHref(id int64) string { return PageHref(id) + "/cover" }

// DayHref is the address of a day.
func DayHref(day string) string { return "/days/" + day }

// MeasurementHref is the address of a reading.
func MeasurementHref(id int64) string { return "/measurements/" + strconv.FormatInt(id, 10) }

// MetricHref is the address of a metric, by its name.
func MetricHref(name string) string { return "/metrics/" + url.PathEscape(name) }

// MonthHref is the address of the month of t.
func MonthHref(t time.Time) string { return "/months/" + t.Format("2006-01") }

// MonthOf parses a month, YYYY-MM; ok is false for anything else.
func MonthOf(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01", s)
	return t, err == nil && t.Format("2006-01") == s
}

// YearHref is the address of a year.
func YearHref(year int) string { return fmt.Sprintf("/years/%04d", year) }

// IsYear reports a year that has a page: 0001 to 9999. The year 0000 holds days, but it has no page (API.md).
func IsYear(year int) bool { return 1 <= year && year <= 9999 }

// BrowseHref is the address of Browse with a query.
func BrowseHref(v url.Values) string {
	if len(v) == 0 {
		return "/browse"
	}
	return "/browse?" + v.Encode()
}
