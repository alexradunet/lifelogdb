package main

import (
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
