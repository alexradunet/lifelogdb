// Command lifelog-mcp is the MCP server of lifelog: the actions of the API as MCP tools on stdio. It is a client of
// the lifelog HTTP API. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"lifelog-mcp/client"
	"lifelog-mcp/mcp"
	"lifelog-mcp/siren"
)

// version is the version of this extension. It starts equal to the version of lifelog, and the two change apart.
const version = "0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "lifelog-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.ReadCloser, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lifelog-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "mcp", "the name of the agent: the server writes as agent:NAME")
	defaultURL := getenv("LIFELOG_URL") // set when it runs as "lifelog mcp"
	if defaultURL == "" {
		defaultURL = "http://127.0.0.1:7777"
	}
	base := fs.String("url", defaultURL, "address of lifelog (default $LIFELOG_URL, else http://127.0.0.1:7777); $LIFELOG_TOKEN is sent as a bearer token when it is set")
	fs.String("source", "", "refused: name the agent with --agent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	named := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { named[f.Name] = true })
	if named["source"] { // one way to name the writer of MCP: an agent's
		return errors.New("mcp writes as agent:NAME: name the agent with --agent, not --source")
	}
	c := client.Remote(*base, siren.AgentPrefix+*agent, getenv("LIFELOG_TOKEN"))
	if err := mcp.Serve(ctx, c, version, stdin, stdout); err != nil {
		return fmt.Errorf("%w (run it as `lifelog mcp`, or start `lifelog serve` and give --url)", err)
	}
	return nil
}
