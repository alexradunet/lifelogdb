package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"lifelog/internal/api"
)

func listActions(ctx context.Context, e env) error {
	actions, err := e.c.Catalog(ctx)
	if err != nil {
		return err
	}
	if !e.o.human {
		return api.WriteJSON(e.stdout, actions, "  ")
	}
	for _, a := range actions {
		var fs []string
		for _, f := range a.Fields {
			s := f.Name
			if !f.Required {
				s = "[" + s + "]"
			}
			fs = append(fs, s)
		}
		fmt.Fprintf(e.stdout, "%-14s %s %s\n  %s\n  fields: %s\n", a.Name, a.Method, a.Href, a.Description, strings.Join(fs, " "))
	}
	return nil
}

// show prints an entity (an error entity too: an agent reads the message and the links) and returns the error.
func show(e env) func(*api.Entity, error) error {
	return func(ent *api.Entity, err error) error {
		if ent == nil {
			return err
		}
		if e.o.human {
			human(e.stdout, ent)
		} else if perr := api.WriteJSON(e.stdout, ent, "  "); perr != nil {
			return perr
		}
		return err
	}
}

// human writes an entity as a readable summary: its title and class, the result of a write, its properties, then
// the embedded links, the links and the actions.
func human(w io.Writer, e *api.Entity) {
	fmt.Fprintf(w, "# %s  (%s)\n", e.Title, strings.Join(e.Class, " "))
	if e.Result != nil {
		b, _ := json.Marshal(e.Result)
		fmt.Fprintf(w, "result: %s\n", b)
	}
	if p, ok := e.Properties.(map[string]any); ok {
		keys := make([]string, 0, len(p))
		for k := range p {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch v := p[k].(type) {
			case string:
				if strings.Contains(v, "\n") {
					fmt.Fprintf(w, "%s:\n%s\n", k, indent(v))
				} else if v != "" {
					fmt.Fprintf(w, "%s: %s\n", k, v)
				}
			default:
				b, _ := json.Marshal(v)
				fmt.Fprintf(w, "%s: %s\n", k, b)
			}
		}
	}
	for _, l := range e.Entities {
		fmt.Fprintf(w, "  → %-12s %-28s %s\n", strings.Join(l.Rel, " "), l.Title, l.Href)
	}
	for _, l := range e.Links {
		fmt.Fprintf(w, "  ↗ %-12s %s\n", strings.Join(l.Rel, " "), l.Href)
	}
	for _, a := range e.Actions {
		fmt.Fprintf(w, "  ! %-14s %s %s\n", a.Name, a.Method, a.Href)
	}
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }
