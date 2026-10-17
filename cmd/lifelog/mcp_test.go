package main

import (
	"io"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/db/dbtest"
)

// TestMCPCommandServesOnTheStdioOfItsEnv: lifelog mcp reads its requests from the stdin of its env and writes its
// answers to the stdout of its env. The tools are the catalog's, with no owner's action. When the client closes the
// stdin, the command ends with no error.
func TestMCPCommandServesOnTheStdioOfItsEnv(t *testing.T) {
	live := dbtest.File(t)
	e, _, _ := testEnv(t)
	names := mcpToolNames(t, e, []string{"mcp", "--agent", "test", "--db", live})
	if !names["find"] || names["register_metric"] || names["snapshot"] || names["rebuild_search"] || names["resync_links"] {
		t.Errorf("the tools of lifelog mcp: %v; want find, and no register_metric, no snapshot, no rebuild_search and no resync_links", names)
	}
}

// mcpToolNames runs the command line argv of lifelog mcp in-process with e, with an MCP client on the other ends of its
// stdin and stdout, and returns the names of the tools that the server lists. Then the client closes the stdin, and the
// command must end with no error.
func mcpToolNames(t *testing.T, e env, argv []string) map[string]bool {
	t.Helper()
	requests, requestWriter := io.Pipe()
	answerReader, answers := io.Pipe()
	e.stdin, e.stdout = requests, answers
	done := make(chan error, 1)
	go func() {
		err := runArgs(t.Context(), e, argv)
		answers.Close()
		done <- err
	}()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "lifelog-test", Version: "0"}, nil).
		Connect(t.Context(), &mcpsdk.IOTransport{Reader: answerReader, Writer: requestWriter}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(t.Context(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if err := session.Close(); err != nil {
		t.Errorf("close the session: %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("lifelog mcp after the client closed its stdin: %v", err)
	}
	return names
}
