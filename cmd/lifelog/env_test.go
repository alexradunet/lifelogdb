package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv is the env of a command line that a test runs in-process: an environment with no variable, a program in a
// folder of its own, the clock of this device, and the output in the two buffers that it returns.
func testEnv(t *testing.T) (e env, stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	exe := filepath.Join(t.TempDir(), "lifelog")
	return env{
		stdin:      io.NopCloser(strings.NewReader("")),
		stdout:     stdout,
		stderr:     stderr,
		getenv:     noVars,
		executable: func() (string, error) { return exe, nil },
	}, stdout, stderr
}

// noVars is the getenv of an environment with no variable.
func noVars(string) string { return "" }
