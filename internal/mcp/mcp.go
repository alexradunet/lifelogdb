// Package mcp serves the API's action catalog as MCP tools over stdio: one narrow tool per action (small local
// models do better with fixed tools than with free browsing), plus get_day and get to follow links. Every
// call goes through the API handler, and every result is the resource's Siren entity, links and actions included.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// Serve runs the MCP server on stdin/stdout until the client goes away.
func Serve(ctx context.Context, c *client.Client, version string) error {
	s, err := New(c, version)
	if err != nil {
		return err
	}
	return s.Run(ctx, &mcp.StdioTransport{})
}

// New builds the server from GET /actions.
func New(c *client.Client, version string) (*mcp.Server, error) {
	actions, err := c.Catalog()
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "lifelog", Version: version}, &mcp.ServerOptions{
		Instructions: "life.db is a personal life log: a journal of day pages (one per local day, titled YYYY-MM-DD), " +
			"pages linked by [[Title]] and #tag, people, places and health readings. Read before you write: get_day " +
			"and search first; every result lists the links you can follow (get) and the actions you can take next, " +
			"with their current values filled in. Nothing is ever deleted; a page title never changes.",
	})
	for _, a := range actions {
		if a.Owner {
			continue // the owner's alone: never a model's tool
		}
		s.AddTool(&mcp.Tool{Name: toolName(a.Name), Description: a.Description, InputSchema: schema(a.Fields),
			Annotations: annotations(a)}, call(c, a))
	}
	s.AddTool(&mcp.Tool{Name: "get_day", Description: "The day view of one local day (YYYY-MM-DD; default today): its journal page, places, habits and readings.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"day": map[string]any{"type": "string", "format": "date"}}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, err := arguments(req)
			if err != nil {
				return failure(err), nil
			}
			href := "/days/today"
			if d := args["day"]; d != "" {
				href = "/days/" + d
			}
			return result(c.Get(href))
		})
	s.AddTool(&mcp.Tool{Name: "get", Description: "Follow a link: fetch the resource at an href from an earlier result (e.g. /pages/12).",
		InputSchema: map[string]any{"type": "object", "required": []string{"href"},
			"properties": map[string]any{"href": map[string]any{"type": "string"}}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, err := arguments(req)
			if err != nil {
				return failure(err), nil
			}
			if !strings.HasPrefix(args["href"], "/") {
				return failure(fmt.Errorf("href must be a path such as /pages/12")), nil
			}
			return result(c.Get(args["href"]))
		})
	return s, nil
}

func toolName(action string) string { return strings.ReplaceAll(action, "-", "_") }

func annotations(a api.Action) *mcp.ToolAnnotations {
	if a.Method == "GET" || a.Name == "query" {
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	return &mcp.ToolAnnotations{}
}

// schema is a JSON Schema for an action's fields: numbers as numbers, everything else a string.
func schema(fields []api.Field) map[string]any {
	props := map[string]any{}
	req := []string{}
	for _, f := range fields {
		if f.Type == "file" { // a file is sent by the CLI or a browser; an agent sends its sha256 and mime
			continue
		}
		p := map[string]any{"type": "string"}
		switch f.Type {
		case "number":
			p["type"] = "number"
		case "date":
			p["format"] = "date"
		}
		desc := f.Title
		if f.Name == "version" {
			desc = "the page's current version, from the save_body action of the last read of the page"
		}
		if desc != "" {
			p["description"] = desc
		}
		if len(f.Options) > 0 {
			p["enum"] = f.Options
		}
		props[f.Name] = p
		if f.Required {
			req = append(req, f.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": req}
}

func call(c *client.Client, a api.Action) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := arguments(req)
		if err != nil {
			return failure(err), nil
		}
		return result(c.Do(a, args))
	}
}

// arguments flattens the tool arguments to the form values the API takes.
func arguments(req *mcp.CallToolRequest) (map[string]string, error) {
	var raw map[string]any
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
			return nil, err
		}
	}
	out := map[string]string{}
	for k, v := range raw {
		if v != nil {
			out[k] = fmt.Sprint(v)
		}
	}
	return out, nil
}

func result(e *api.Entity, err error) (*mcp.CallToolResult, error) {
	if e == nil && err != nil {
		return failure(err), nil
	}
	b, _ := json.Marshal(e)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, IsError: err != nil}, nil
}

func failure(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
