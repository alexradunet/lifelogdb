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

	"lifelog-mcp/mcp"
	"lifelog/client"
	"lifelog/siren"
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
	// The address is $LIFELOG_URL, which `lifelog mcp` sets, or --url. There is no default: a guess such as
	// 127.0.0.1:7777 could be the owner's service, and a stale config with a missing file would then write there.
	base := fs.String("url", getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL); $LIFELOG_TOKEN is sent as a bearer token when it is set")
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
	if *base == "" {
		return errors.New("no LIFELOG_URL and no --url: run it as `lifelog mcp` on a life.db that exists, or start `lifelog serve` and give --url")
	}
	c := client.Remote(*base, siren.AgentPrefix+*agent, getenv("LIFELOG_TOKEN"))
	s, err := mcp.New(ctx, c, version)
	if err != nil {
		return fmt.Errorf("%w (run it as `lifelog mcp`, or start `lifelog serve` and give --url)", err)
	}
	if err := mcp.Run(ctx, s, stdin, stdout); err != nil && !errors.Is(err, context.Canceled) {
		return err // an interrupt ends the session cleanly: the signal is the owner's word
	}
	return nil
}
