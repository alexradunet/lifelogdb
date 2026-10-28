// Package siren holds the Siren types that the answers of the lifelog API are made of, as far as the WhatsApp agent
// reads them: an entity, its links, its actions and their fields, the header that names the writer of a request, and the
// JSON of an answer. It is a copy of extensions/mcp/siren, the part that this program reads, because an extension never imports
// lifelog/internal. The contract is API.md of lifelog.
package siren

import (
	"encoding/json"
	"io"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

// AgentPrefix starts the name of the writer of an agent: the WhatsApp agent writes as AgentPrefix+name. The API refuses
// the owner-only actions to such a writer.
const AgentPrefix = "agent:"

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

// Field is a Siren field. In "path" (an extension) marks a field of the catalog's templated href. The other
// extensions of the API (rows, pages, accept) are for a browser, and this program does not read them.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Title    string   `json:"title,omitempty"`
	Value    any      `json:"value,omitempty"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	In       string   `json:"in,omitempty"`
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
// they are); indented by indent ("" for none); with a newline at the end.
func WriteJSON(w io.Writer, v any, indent string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	return enc.Encode(v)
}
