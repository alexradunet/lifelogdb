// Package mcp serves the API's action catalog as MCP tools over stdio: one narrow tool per action (small local
// models do better with fixed tools than with free browsing), plus get_day and get to follow links. Every
// call goes through the API handler, and every result is the resource's Siren entity, links and actions included.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
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
	tools, err := Tools(actions, func(a api.Action) bool { return !a.Owner }) // the owner's alone: never a model's tool
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "lifelog", Version: version}, &mcp.ServerOptions{
		Instructions: Instructions,
	})
	for _, t := range tools {
		s.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Schema, Annotations: annotations(t)}, call(c, t))
	}
	return s, nil
}

// annotations say what a tool does to the database: a tool that only reads says so; a catalog action marked Danger is
// destructive, as a browser draws it.
func annotations(t Tool) *mcp.ToolAnnotations {
	if t.ReadOnly() {
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	danger := t.Action.Danger
	return &mcp.ToolAnnotations{DestructiveHint: &danger}
}

func call(c *client.Client, t Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := Arguments(req.Params.Arguments)
		if err != nil {
			return failure(err), nil
		}
		return result(Call(ctx, c, t, args))
	}
}

func result(e *api.Entity, err error) (*mcp.CallToolResult, error) {
	if e == nil && err != nil {
		return failure(err), nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // a body reads as it was written, as the API and the CLI give it
	if err := enc.Encode(e); err != nil {
		return failure(err), nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSuffix(b.String(), "\n")}}, IsError: err != nil}, nil
}

func failure(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
