package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"lifelog-whatsapp/client"
	"lifelog-whatsapp/siren"
)

// This file is a copy of extensions/mcp/mcp/tools.go without its instructions: the model gets the catalog as it is.

// tool is one function a model may call: an action of the catalog, or the read get.
type tool struct {
	name        string
	description string
	schema      map[string]any
	action      *siren.Action // the action that the tool runs; nil for get
}

// tools is the tool list of the catalog: one tool for each action that is not the owner's, then get. The owner's
// actions are never a tool (AGENTS.md). A tool name that two actions share is refused.
func tools(actions []siren.Action) ([]tool, error) {
	seen := map[string]string{"get": "built-in tool get"}
	var list []tool
	for i := range actions {
		a := &actions[i]
		if a.Owner {
			continue
		}
		name := toolName(a.Name)
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("tool name %q from action %q collides with %s", name, a.Name, prev)
		}
		seen[name] = fmt.Sprintf("action %q", a.Name)
		list = append(list, tool{name: name, description: a.Description, schema: inputSchema(a.Fields), action: a})
	}
	return append(list,
		tool{name: "get", description: "Follow a link: fetch the resource at an href from an earlier result (e.g. /pages/12).",
			schema: map[string]any{"type": "object", "required": []string{"href"},
				"properties": map[string]any{"href": map[string]any{"type": "string"}}}},
	), nil
}

// toolName is the tool name of an action: a hyphen is not allowed in every model's function names.
func toolName(action string) string { return strings.ReplaceAll(action, "-", "_") }

// run calls a tool through the client with its arguments, as arguments gives them.
func run(ctx context.Context, c *client.Client, t tool, args map[string]string) (*siren.Entity, error) {
	if t.action != nil {
		return c.Do(ctx, *t.action, args)
	}
	if !strings.HasPrefix(args["href"], "/") {
		return nil, errors.New("href must be a path such as /pages/12")
	}
	return c.Get(ctx, args["href"])
}

// inputSchema is a JSON Schema for an action's fields: numbers as numbers, everything else a string. A date takes a
// local day or the word today (API.md, "The entity"), so its schema has no format date, which takes no word, and no
// pattern, which would repeat the schema's day CHECK. Its description adds the hint to the title, inside the title's own brackets when it ends
// with them. The title stays without the hint: it is also the label of the browser's date picker, in which a person
// cannot type the word.
func inputSchema(fields []siren.Field) map[string]any {
	props := map[string]any{}
	req := []string{}
	for _, f := range fields {
		p := map[string]any{"type": "string"}
		desc := f.Title
		switch f.Type {
		case "file": // the agent sends the file, as the CLI does (client.Do)
			desc = "The absolute path of a local file: " + desc
		case "number":
			p["type"] = "number"
		case "date":
			if strings.HasSuffix(desc, ")") {
				desc = strings.TrimSuffix(desc, ")") + "; YYYY-MM-DD or today)"
			} else {
				desc = strings.TrimSpace(desc + " (YYYY-MM-DD or today)")
			}
		}
		if desc != "" {
			p["description"] = desc
		}
		if len(f.Options) > 0 {
			options := make([]any, 0, len(f.Options))
			for _, option := range f.Options {
				if f.Type == "number" { // the catalog's numbers parse: TestEveryNumericOptionIsANumber
					options = append(options, json.Number(option))
				} else {
					options = append(options, option)
				}
			}
			p["enum"] = options
		}
		props[f.Name] = p
		if f.Required {
			req = append(req, f.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": req}
}

// arguments flattens a model's JSON arguments to the form values the API takes: a number as its JSON text, which the
// API's own parsers read, as they read the CLI's and a browser's.
func arguments(raw []byte) (map[string]string, error) {
	var args map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&args); err != nil {
			return nil, err
		}
	}
	out := map[string]string{}
	for k, v := range args {
		switch v := v.(type) {
		case nil:
		case string:
			out[k] = v
		case json.Number:
			out[k] = string(v)
		default:
			return nil, fmt.Errorf("unsupported argument %s", k)
		}
	}
	return out, nil
}
