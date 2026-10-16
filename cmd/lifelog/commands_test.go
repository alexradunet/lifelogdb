package main

import (
	"errors"
	"strings"
	"testing"
)

// TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt: a flag a command does not take is refused, never
// ignored: --dry-run belongs to file, --public to serve. The usage names every command of the table. A setup command
// of the machine (init, agentic-init) keeps the OS's interrupt, as does a line with no command or a command that is not
// in the table; every other command takes the interrupt through its context, snapshot too, as it runs through the
// client since plan 082.
func TestTheCommandTableDecidesTheFlagsTheUsageAndTheInterrupt(t *testing.T) {
	for _, argv := range [][]string{{"search", "x", "--dry-run"}, {"day", "--public"}, {"search", "x", "--day", "2026-01-01"}, {"capture", "hello", "--to", "x"}} {
		if _, err := parse(argv, noVars); err == nil || !strings.Contains(err.Error(), "does not take") {
			t.Errorf("%v: %v, want a refusal of the flag", argv, err)
		}
	}
	for _, argv := range [][]string{{"file", "x", "--dry-run"}, {"serve", "--public"}, {"capture", "hello", "--day", "2026-01-01"}, {"snapshot", "--to", "x"}} {
		if _, err := parse(argv, noVars); err != nil {
			t.Errorf("%v: %v, want the flag taken", argv, err)
		}
	}
	usage := usageText()
	for _, c := range commands {
		if c.usage != "" && !strings.Contains(usage, "lifelog "+c.name) {
			t.Errorf("the usage does not name %s", c.name)
		}
	}
	for _, args := range [][]string{nil, {"init"}, {"agentic-init"}, {"help"}, {"unknown"}} {
		if commandConsumesContext(opts{args: args}) {
			t.Errorf("%v takes the interrupt through its context; want the OS's interrupt", args)
		}
	}
	for _, cmd := range []string{"serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "rename", "file", "snapshot"} {
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
		{[]string{"unknown"}, false, `unknown command "unknown" (lifelog help)`},
		{[]string{"unknown", "--dry-run"}, false, `unknown command "unknown" (lifelog help)`},
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
