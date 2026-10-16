// Package mcp serves the API's action catalog as MCP tools over stdio: one narrow tool per action (small local
// models do better with fixed tools than with free browsing), plus get_day and get to follow links. Every
// call goes through the API handler, and every result is the resource's Siren entity, links and actions included.
package mcp

import (
	"bytes"
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// Serve runs the MCP server on stdin/stdout until the client goes away.
func Serve(ctx context.Context, c *client.Client, version string) error {
	s, err := New(ctx, c, version)
	if err != nil {
		return err
	}
	return s.Run(ctx, &mcp.StdioTransport{})
}

// New builds the server from GET /actions.
func New(ctx context.Context, c *client.Client, version string) (*mcp.Server, error) {
	actions, err := c.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	list, err := tools(actions)
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "lifelog", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	for _, t := range list {
		s.AddTool(&mcp.Tool{Name: t.name, Description: t.description, InputSchema: t.schema, Annotations: annotations(t)}, call(c, t))
	}
	return s, nil
}

// annotations say what a tool does to the database: a tool that only reads (get_day, get and a GET of the catalog)
// says so; a catalog action marked Danger is destructive, as a browser draws it.
func annotations(t tool) *mcp.ToolAnnotations {
	if t.action == nil || t.action.Method == "GET" {
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	danger := t.action.Danger
	return &mcp.ToolAnnotations{DestructiveHint: &danger}
}

func call(c *client.Client, t tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := arguments(req.Params.Arguments)
		if err != nil {
			return failure(err), nil
		}
		return result(run(ctx, c, t, args))
	}
}

func result(e *api.Entity, err error) (*mcp.CallToolResult, error) {
	if e == nil && err != nil {
		return failure(err), nil
	}
	var b bytes.Buffer
	if err := api.WriteJSON(&b, e, ""); err != nil {
		return failure(err), nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSuffix(b.String(), "\n")}}, IsError: err != nil}, nil
}

func failure(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
