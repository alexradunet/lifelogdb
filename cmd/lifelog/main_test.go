package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt: a flag a command does not take is refused, never
// ignored: --dry-run belongs to file, --public to serve. The usage names every command of the table. A setup command
// of the machine (init, agentic-init) keeps the OS's interrupt, as does a line with no command; a name that is no command is an extension, which gets the interrupt through its context and passes it on; every other command takes the interrupt through its context, snapshot too, as it runs through the
// client since plan 082.
func TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt(t *testing.T) {
	for _, argv := range [][]string{{"search", "x", "--dry-run"}, {"day", "--public"}, {"search", "x", "--day", "2026-01-01"}, {"capture", "hello", "--snapshots", "x"}} {
		if _, err := parse(argv, noVars); err == nil || !strings.Contains(err.Error(), "does not take") {
			t.Errorf("%v: %v, want a refusal of the flag", argv, err)
		}
	}
	for _, argv := range [][]string{{"file", "x", "--dry-run"}, {"serve", "--public"}, {"capture", "hello", "--day", "2026-01-01"}, {"snapshot", "--snapshots", "x"}} {
		if _, err := parse(argv, noVars); err != nil {
			t.Errorf("%v: %v, want the flag taken", argv, err)
		}
	}
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"init", "--url", "http://x"}, "init does not take --url"},
		{[]string{"init", "--source", "x"}, "init does not take --source"},
		{[]string{"agentic-init", "--url", "http://x"}, "agentic-init does not take --url"},
		{[]string{"agentic-init", "--source", "x"}, "agentic-init does not take --source"},
		{[]string{"serve", "--human"}, "serve does not take --human"},
		{[]string{"mcp", "--human"}, "mcp does not take --human"},
		{[]string{"day", "--db", "a.db", "--url", "http://x"}, "--db and --url name two databases: give one"},
	} {
		if _, err := parse(tc.argv, noVars); err == nil || err.Error() != tc.want {
			t.Errorf("%v: %v, want %q", tc.argv, err, tc.want)
		}
	}
	for _, argv := range [][]string{{"init", "--db", "a.db"}, {"agentic-init", "--db", "a.db"}, {"day", "--url", "http://x", "--human"}} {
		if _, err := parse(argv, noVars); err != nil {
			t.Errorf("%v: %v, want the flags taken", argv, err)
		}
	}
	usage := usageText()
	for _, c := range commands {
		if c.usage != "" && !strings.Contains(usage, "lifelog "+c.name) {
			t.Errorf("the usage does not name %s", c.name)
		}
	}
	for _, args := range [][]string{nil, {"init"}, {"agentic-init"}, {"help"}} {
		if commandConsumesContext(opts{args: args}) {
			t.Errorf("%v takes the interrupt through its context; want the OS's interrupt", args)
		}
	}
	for _, cmd := range []string{"serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "rename", "file", "cover", "snapshot", "an-extension"} {
		if !commandConsumesContext(opts{args: []string{cmd}}) {
			t.Errorf("%s must take the interrupt through its context", cmd)
		}
	}
}

// TestAnUnknownCommandIsAnErrorAndNotTheUsage: a command that is not in the table is the error "unknown command", not a
// usageError, so main writes "lifelog: unknown command ..." and exits 1. A line with no command, the line help and a
// line that parse refuses are a usageError, so main writes the refusal (if there is one) and the usage, and exits 2.
// runArgs itself writes nothing.
func TestAnUnknownCommandIsAnErrorAndNotTheUsage(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		usage bool   // main writes the usage and exits 2; else it exits 1
		want  string // the error that main writes; "" for none
	}{
		{[]string{"unknown"}, false, `unknown command "unknown": no command of lifelog and no lifelog-unknown beside this program or on PATH (lifelog help)`},
		{[]string{"unknown", "--dry-run"}, false, `unknown command "unknown": no command of lifelog and no lifelog-unknown beside this program or on PATH (lifelog help)`},
		{nil, true, ""},
		{[]string{"help"}, true, ""},
		{[]string{"search", "x", "--dry-run"}, true, "search does not take --dry-run"},
		{[]string{"--bogus"}, true, "unknown flag --bogus"},
	} {
		e, stdout, stderr := testEnv(t)
		err := runArgs(t.Context(), e, tc.argv)
		var u usageError
		usage := errors.As(err, &u)
		var said string
		switch {
		case usage && u.err != nil:
			said = u.err.Error()
		case !usage && err != nil:
			said = err.Error()
		}
		if err == nil || usage != tc.usage || said != tc.want {
			t.Errorf("%v: %v (a usage error: %v), want %q (a usage error: %v)", tc.argv, err, usage, tc.want, tc.usage)
		}
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("%v: runArgs wrote %q and %q, want nothing", tc.argv, stdout, stderr)
		}
	}
}

func TestCommandCancellation(t *testing.T) {
	for _, args := range [][]string{{"get", "blocked"}, {"capture", "synthetic"}, {"search", "synthetic"}, {"actions"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(stopped)
				case <-release:
				}
			}))
			defer srv.Close()
			// This independent escape must run before server shutdown even on Fatal.
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			e, _, _ := testEnv(t)
			e.o = opts{args: args, url: srv.URL, source: "cli"}
			go func() { done <- runContext(ctx, e) }()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("not started")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("handler not canceled")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("command did not return after cancellation")
			}
		})
	}
}

// lifelog init PATH makes the file at PATH, whatever $LIFELOG_DB names; --db beats PATH, and both together are a
// mistake, refused before anything is written (docs/plans/098-the-codebase-made-cohesive.md).
func TestInitHonoursThePathWhenTheEnvironmentNamesAnother(t *testing.T) {
	named := filepath.Join(t.TempDir(), "env.db")
	e, _, _ := testEnv(t)
	e.getenv = func(name string) string { return map[string]string{"LIFELOG_DB": named}[name] }
	given := filepath.Join(t.TempDir(), "given.db")
	if err := runArgs(t.Context(), e, []string{"init", given}); err != nil {
		t.Fatalf("init PATH: %v", err)
	}
	if _, err := os.Stat(given); err != nil {
		t.Errorf("init PATH did not make %s: %v", given, err)
	}
	if _, err := os.Stat(named); err == nil {
		t.Errorf("init PATH made the file $LIFELOG_DB names, %s", named)
	}

	other := filepath.Join(t.TempDir(), "other.db")
	if err := runArgs(t.Context(), e, []string{"init", filepath.Join(t.TempDir(), "unused.db"), "--db", other}); err == nil || !strings.Contains(err.Error(), "--db") {
		t.Errorf("init PATH --db OTHER is refused: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Errorf("a refused init made %s", other)
	}
}

// TestMCPNamesItsWriterOneWay: the MCP server writes as agent:NAME, which --agent names; a --source on its line is
// refused, never overridden in silence (docs/plans/131-the-pareto-pass.md, part 2).
func TestMCPNamesItsWriterOneWay(t *testing.T) {
	cmd := commandNamed("mcp")
	o, err := parse([]string{"mcp", "--agent", "codex"}, noVars)
	if err == nil {
		o, err = cmd.prepare(o)
	}
	if err != nil || o.source != "agent:codex" {
		t.Errorf("mcp --agent codex: source %q, %v; want agent:codex", o.source, err)
	}
	o, err = parse([]string{"mcp", "--source", "cli"}, noVars)
	if err == nil {
		_, err = cmd.prepare(o)
	}
	if err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Errorf("mcp --source cli: %v, want a refusal that names --agent", err)
	}
}

// TestAUsageErrorNamesTheCommandAndARepeatedFieldIsRefused: the error of use is the first line of the usage of the
// command, from the table; `do` refuses a field given twice instead of keeping the last one.
func TestAUsageErrorNamesTheCommandAndARepeatedFieldIsRefused(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"rename", "1"}, "rename PAGE-ID NEW-TITLE"},
		{[]string{"get"}, "get PATH"},
		{[]string{"file"}, "file PATH [--title T]"},
		{[]string{"do", "capture", "text=a", "text=b"}, "text is given twice"},
	} {
		e, _, _ := testEnv(t)
		file := filepath.Join(t.TempDir(), "x.db")
		if err := runArgs(t.Context(), e, []string{"init", file}); err != nil {
			t.Fatal(err)
		}
		err := runArgs(t.Context(), e, append(tc.argv, "--db", file))
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%v: %v, want %q", tc.argv, err, tc.want)
		}
	}
}
