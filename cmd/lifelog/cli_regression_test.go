package main

import (
	"testing"
)

func TestCommandInterruptPolicy(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false}, {[]string{"init"}, false},
		{[]string{"help"}, false}, {[]string{"unknown"}, false},
		{[]string{"import"}, false}, {[]string{"import", "status"}, false}, // removed with the import process (plan 089)
	} {
		if got := commandConsumesContext(opts{args: tc.args}); got != tc.want {
			t.Errorf("%v: intercept=%v want=%v", tc.args, got, tc.want)
		}
	}
	// snapshot runs through the client like the rest since plan 082: an interrupt reaches it.
	for _, cmd := range []string{"serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "rename", "file", "snapshot"} {
		if !commandConsumesContext(opts{args: []string{cmd}}) {
			t.Errorf("%s must consume signal context", cmd)
		}
	}
}
