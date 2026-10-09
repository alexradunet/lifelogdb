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
// extension) is how many lines a browser draws of a textarea.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Title    string   `json:"title,omitempty"`
	Value    any      `json:"value,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	In       string   `json:"in,omitempty"`
	Rows     int      `json:"rows,omitempty"`
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

// area is a multi-line field a browser draws rows lines tall.
func area(name, title string, rows int) Field {
	return Field{Name: name, Type: "textarea", Title: title, Rows: rows}
}

// areaReq is a multi-line field the action cannot do without.
func areaReq(name, title string, rows int) Field {
	f := area(name, title, rows)
	f.Required = true
	return f
}

// dangerous names the catalog's destructive actions: those that hide or retract what is already there. A browser
// draws their button as a danger and any other client may warn the same way; no surface decides this by an
// action's name. Every name here is a catalog action (TestDangerIsACatalogAction).
var dangerous = map[string]bool{
	"tombstone": true, "tombstone-session": true, "tombstone-task": true, "tombstone-occurrence": true,
	"retract": true, "relocate-reading": true,
}

// catalog is every action the API has. GET /actions serves it, and the CLI and the MCP server are built from it.
var catalog = []spec{
	{"relocate-reading", "Move reading scope", "Owner-only atomic same-metric scope correction of a current unkeyed non-imported chain: retract old scope and append independent replacement, never a cross-scope supersedes edge.", "POST", "/measurements/{id}/relocate", []Field{path("id", "number", "Expected current leaf id"), req("metric", "text", "Same metric"), req("day", "date", "Replacement reading day"), req("value", "number", "Replacement value"), opt("session_id", "number", "New session id (blank unassociated)"), opt("taken_at", "text", "Replacement UTC instant"), opt("tz", "text", "Replacement supplied zone"), opt("captured_with_id", "number", "Replacement capture provenance")}, true},
	{"capture-session", "Record session", "Record a lightweight session with independent reporting day and explicit UTC or unresolved local endpoints. Zone labels are UNVERIFIED claims; no host timezone inference.", "POST", "/sessions", append(append([]Field{}, sessionFields...), opt("import_key", "text", "Exact source identity")), false},
	{"edit-session", "Edit session", "Replace session metadata using its current revision; identity/provenance never change.", "POST", "/sessions/{id}/edit", append([]Field{path("id", "number", "Session id"), req("version", "hidden", "Version")}, sessionFields...), false},
	{"tombstone-session", "Tombstone session", "Hide active session results, retaining facts and historical access.", "POST", "/sessions/{id}/tombstone", []Field{path("id", "number", "Session id"), req("version", "hidden", "Version")}, false},
	{"revive-session", "Revive session", "Restore the same session identity and current facts.", "POST", "/sessions/{id}/revive", []Field{path("id", "number", "Session id"), req("version", "hidden", "Version")}, false},
	{"create-period", "Record period", "Record a named historical span, with optional uncertain boundaries; classifications use ordinary part-of links.", "POST", "/periods", []Field{req("title", "text", "Title"), area("body", "Body", 5), opt("start_boundary", "text", "Start boundary (blank unknown)"), opt("end_boundary", "text", "End boundary (blank unknown; .. ongoing)")}, false},
	{"edit-period", "Edit boundaries", "Replace period boundaries using the current named-object version.", "POST", "/pages/{id}/period", []Field{path("id", "number", "Period id"), req("version", "hidden", "Version"), opt("start_boundary", "text", "Start boundary"), opt("end_boundary", "text", "End boundary")}, false},
	{"promote-period", "Record span", "Make this plain non-journal page a period without replacing its identity, body or links.", "POST", "/pages/{id}/promote-period", []Field{path("id", "number", "Page id"), req("version", "hidden", "Version"), opt("start_boundary", "text", "Start boundary"), opt("end_boundary", "text", "End boundary")}, false},
	{"capture", "Capture", "Append an entry to the journal page of a local day (created on the first capture), with an optional mood 1-5. [[Title]] and #tag in the text link pages, creating them if new.",
		"POST", "/days/{day}/capture", []Field{path("day", "date", "Local day, YYYY-MM-DD"), area("text", "Entry (CommonMark)", 4), opt("mood", "number", "Mood 1-5")}, false},
	{"create-page", "New page", "Create a page with a unique title (a valid file name) and a CommonMark body.",
		"POST", "/pages", []Field{req("title", "text", "Title"), area("body", "Body", 12)}, false},
	{"save-body", "Save", "Replace a page's whole body. version must be the page's current version (from the last read); wikilinks are re-synced.",
		"POST", "/pages/{id}/body", []Field{path("id", "number", "Page id"), areaReq("body", "Body", 18), req("version", "hidden", "Version")}, false},
	{"create-person", "New person", "Create a person: title is the permanent handle that [[wikilinks]] use (e.g. 'Sam (barber)'), name the full name.",
		"POST", "/people", []Field{req("title", "text", "Handle"), opt("name", "text", "Full name"), opt("birth_day", "date", "Born"), opt("death_day", "date", "Died")}, false},
	{"create-place", "New place", "Create a place; its title is its name and its handle.",
		"POST", "/places", []Field{req("title", "text", "Name")}, false},
	{"add-file", "Keep a file", "Keep a file the owner picked (a recording, a PDF, a scan, a photo) as a page: title is its permanent handle, and ![[title]] in a day page shows it; body is its text (a transcript, the text of a PDF, a caption). Send the original, which is read and never stored, or its sha256 and mime. A picture is made from the original when it is a JPEG, PNG or GIF; for any other (HEIC, a video's frame) send preview, a JPEG, PNG or GIF. A photo's own day (its EXIF date) and position are read from the original: its day page gets an at link to the place it was taken in, and shows it; a position near no place is reported, and at names the place, which takes the position as its point. The same original again finds its page and writes nothing but a missing picture, its place and its day.",
		"POST", "/files", []Field{req("title", "text", "Title"), opt("original", "file", "The file (read, never stored)"), opt("sha256", "text", "SHA-256 of the original, when it is not sent"),
			opt("mime", "text", "Type, e.g. audio/mp4"), opt("preview", "file", "Picture, when the original is not a JPEG, PNG or GIF"), area("body", "Text: a transcript, a caption", 6), opt("day", "date", "Its day (default: the photo's own, else today)"),
			opt("at", "text", "The place it was taken at (names it)"), opt("radius", "number", "That place's radius in metres, when it takes this position (default 250)"),
			{Name: "dry_run", Type: "number", Title: "Dry run: write nothing, say what keeping it would do", Options: []string{"0", "1"}}}, false},
	{"locate", "Locate", "Give this place its point and radius, or move them: a photo kept inside the circle is matched to it, the smallest circle first. link_days 0 marks a place recognised and never linked (home, work).",
		"POST", "/pages/{id}/locate", []Field{path("id", "number", "Place id"), req("lat", "number", "Latitude"), req("lon", "number", "Longitude"), req("radius_m", "number", "Radius (m)"),
			{Name: "link_days", Type: "number", Title: "Link its days", Options: []string{"1", "0"}}}, false},
	{"promote", "Promote", "Make a plain page (e.g. one a [[wikilink]] created) a person or a place; its id and links stay.",
		"POST", "/pages/{id}/promote", []Field{path("id", "number", "Page id"), {Name: "to", Type: "text", Title: "Into", Required: true, Options: []string{"person", "place"}}, opt("name", "text", "Full name (a person)")}, false},
	{"tombstone", "Delete", "Tombstone a page (nothing is ever removed; revive undoes it).",
		"POST", "/pages/{id}/tombstone", []Field{path("id", "number", "Page id")}, false},
	{"revive", "Revive", "Undo a tombstone.",
		"POST", "/pages/{id}/revive", []Field{path("id", "number", "Page id")}, false},
	{"link", "Link", "Add a typed link from this page to the page titled `to` (kinds: at = a day page was at a place; about; located-in; parent-of; part-of = filed in the category whose page is `to`; friend; family; related).",
		"POST", "/pages/{id}/links", []Field{path("id", "number", "Page id"), req("to", "text", "To (title)"), req("kind", "text", "Kind"), opt("note", "text", "Note")}, false},
	{"unlink", "Unlink", "Remove a typed link from this page to the page titled `to`.",
		"POST", "/pages/{id}/unlink", []Field{path("id", "number", "Page id"), req("to", "text", "To (title)"), req("kind", "text", "Kind")}, false},
	{"record", "Record", "Record a reading of a registered metric for a local day. import_key makes a re-send a no-op. A view offers the metrics read in its last 60 days; /metrics lists them all.",
		"POST", "/measurements", []Field{req("metric", "text", "Metric"), req("day", "date", "Day"), req("value", "number", "Value"),
			opt("session_id", "number", "Session scope (blank unassociated, not a daily total)"), opt("taken_at", "text", "Taken at (UTC, 2026-06-09T21:14:03.482Z)"), opt("tz", "text", "Zone (IANA)"), opt("import_key", "text", "Key")}, false},
	{"readings-from-table", "Readings from a table", "Turn the table of a page into readings of a registered metric: each row with a day (YYYY-MM-DD) and a plain number is one reading of that day. The unit (in the cell, a unit column, or the header in ( ) or [ ]) must be the metric's; nothing is converted. A sign (<5), a word, a comma decimal, another unit or a second value for one day is reported, not written. A second run writes nothing.",
		"POST", "/measurements/from-table", []Field{req("page", "text", "Page (title)"), req("metric", "text", "Metric"), opt("column", "text", "Value column, when the table has several")}, false},
	{"correct", "Correct", "Correct a reading with a new value (a reading is corrected once; correct the correction after that).",
		"POST", "/measurements/{id}/correct", []Field{path("id", "number", "Measurement id"), req("value", "number", "Value")}, false},
	{"retract", "Retract", "Retract a reading that should never have existed.",
		"POST", "/measurements/{id}/retract", []Field{path("id", "number", "Measurement id")}, false},
	{"rename", "Rename", "Select a new preferred reference name without changing this identity, body or links. Previous names remain aliases. Another identity's name and journal dates are refused.",
		"POST", "/pages/{id}/rename", []Field{path("id", "number", "Page id"), req("title", "text", "New title")}, false},
	{"register-metric", "New metric", "Register a metric with a preferred reference name; its unit never changes ('' only for a 0/1 habit; a scale names its range, '1-5'); the note is the page's text. A metric that exists is returned as it is when the unit is the same, or when it is a scale and the unit is empty.",
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
		"POST", "/query", []Field{areaReq("sql", "SQL", 6)}, false},
	{"snapshot", "Take a snapshot", "The owner's dated copy of life.db (docs/cookbook/take-a-snapshot.md) into the server's snapshot folder, never overwriting, then its restore check. The answer names the file and the check.",
		"POST", "/snapshots", nil, true},
	// Planning (docs/contract/planning.md): explicit tasks, their occurrences, the deadlines of a window.
	{"create-task", "New task", "Create a task: a one-off (due on due_day, or undated), or a series when repeat_unit is day, week, month or year, every repeat_every units from anchor_day, until repeat_until_day. project names an existing page as its context. reminder_time (HH:MM) and reminder_zone (IANA) are the default reminder of each occurrence. Planning never records what happened.",
		"POST", "/tasks", []Field{req("label", "text", "Label"), opt("project", "text", "Project page (title)"), opt("repeat_unit", "text", "Repeat: day, week, month or year (blank: one-off)"),
			opt("repeat_every", "number", "Every N units"), opt("anchor_day", "date", "First occurrence"), opt("repeat_until_day", "date", "Last occurrence (inclusive)"),
			opt("reminder_time", "text", "Reminder clock (HH:MM)"), opt("reminder_zone", "text", "Reminder zone (IANA)"), opt("due_day", "date", "Due (a one-off)"), opt("import_key", "text", "Key")}, false},
	{"edit-task", "Edit task", "Change a task's label, project or default reminder; its cadence is fixed (stop-task shortens a series).",
		"POST", "/tasks/{id}/edit", []Field{path("id", "number", "Task id"), req("version", "hidden", "Version"), req("label", "text", "Label"), opt("project", "text", "Project page (title)"),
			opt("reminder_time", "text", "Reminder clock (HH:MM)"), opt("reminder_zone", "text", "Reminder zone (IANA)")}, false},
	{"stop-task", "Stop series", "End a series on a day (inclusive): its open slots after it are skipped, done ones kept. An end only shortens.",
		"POST", "/tasks/{id}/stop", []Field{path("id", "number", "Task id"), req("version", "hidden", "Version"), req("through", "date", "Last day")}, false},
	{"tombstone-task", "Delete task", "Tombstone a task: all its work and reminders stop; revive-task undoes it.",
		"POST", "/tasks/{id}/tombstone", []Field{path("id", "number", "Task id"), req("version", "hidden", "Version")}, false},
	{"revive-task", "Revive task", "Undo a task's tombstone.",
		"POST", "/tasks/{id}/revive", []Field{path("id", "number", "Task id"), req("version", "hidden", "Version")}, false},
	{"capture-occurrence", "Capture occurrence", "Write one occurrence of a task: key is a slot day of the series (due on that day unless due_day says otherwise) or once for a one-off. state open, done or skipped; completed_at an instant, or now, for a done one; reminder_mode inherit, off, or at with reminder_at. A slot written before is returned as it is.",
		"POST", "/tasks/{id}/occurrences", []Field{path("id", "number", "Task id"), req("task_version", "hidden", "Task version"), req("key", "text", "Slot (a day, or once)"), opt("due_day", "date", "Due"),
			{Name: "state", Type: "text", Title: "State", Options: []string{"open", "done", "skipped"}}, opt("completed_at", "text", "Completed at (UTC instant, or now)"),
			{Name: "reminder_mode", Type: "text", Title: "Reminder", Options: []string{"inherit", "off", "at"}}, opt("reminder_at", "text", "Reminder at (UTC instant, with at)"), opt("import_key", "text", "Key")}, false},
	{"edit-occurrence", "Edit occurrence", "Change an occurrence's due day (blank: undated), state, completion instant (now for this instant) or reminder choice, with both current versions. Reopening clears completion.",
		"POST", "/tasks/{id}/occurrences/{key}/edit", []Field{path("id", "number", "Task id"), path("key", "text", "Occurrence key"), req("task_version", "hidden", "Task version"), req("version", "hidden", "Version"),
			opt("due_day", "date", "Due"), {Name: "state", Type: "text", Title: "State", Options: []string{"open", "done", "skipped"}}, opt("completed_at", "text", "Completed at (UTC instant, or now)"),
			{Name: "reminder_mode", Type: "text", Title: "Reminder", Options: []string{"inherit", "off", "at"}}, opt("reminder_at", "text", "Reminder at (UTC instant, with at)")}, false},
	{"tombstone-occurrence", "Delete occurrence", "Tombstone one occurrence; its slot stays suppressed. revive-occurrence undoes it.",
		"POST", "/tasks/{id}/occurrences/{key}/tombstone", []Field{path("id", "number", "Task id"), path("key", "text", "Occurrence key"), req("task_version", "hidden", "Task version"), req("version", "hidden", "Version")}, false},
	{"revive-occurrence", "Revive occurrence", "Undo an occurrence's tombstone.",
		"POST", "/tasks/{id}/occurrences/{key}/revive", []Field{path("id", "number", "Task id"), path("key", "text", "Occurrence key"), req("task_version", "hidden", "Task version"), req("version", "hidden", "Version")}, false},
	{"deadlines", "Deadlines", "The occurrences of every task due in a window (default: 30 days back to 90 days on), open ones unless state says otherwise. Overdue is this read with an earlier from, never a stored flag.",
		"GET", "/deadlines", []Field{opt("from", "date", "From"), opt("through", "date", "Through"), {Name: "state", Type: "text", Title: "State", Options: []string{"open", "done", "skipped", "all"}},
			{Name: "include_deleted", Type: "number", Title: "Include tombstoned", Options: []string{"0", "1"}}}, false},
}

func specOf(name string) spec {
	for _, s := range catalog {
		if s.Name == name {
			return s
		}
	}
	panic("no action " + name)
}

// action instantiates a catalog action: path fields fill the href and drop out, values prefill the rest.
func action(name string, params map[string]string, values map[string]any) Action {
	s := specOf(name)
	a := Action{Name: s.Name, Title: s.Title, Description: s.Description, Method: s.Method, Href: s.Path, Owner: s.Owner,
		Danger: dangerous[s.Name]}
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
	a := Action{Name: s.Name, Title: s.Title, Description: s.Description, Method: s.Method, Href: s.Path, Fields: s.Fields,
		Owner: s.Owner, Danger: dangerous[s.Name]}
	if s.Method == "POST" {
		a.Type = formType
	}
	return a
}
