package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// serveInProcess runs a command line of serve in-process with e and returns the startup line that it writes to its
// stderr. The serve stops at the end of the test; an error that it returns fails the test.
func serveInProcess(t *testing.T, e env, argv ...string) string {
	t.Helper()
	read, write := io.Pipe()
	e.stderr = write
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := runArgs(ctx, e, argv)
		write.Close()
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve shutdown timed out")
		}
		read.Close()
	})
	line := make(chan string, 1)
	go func() { s, _ := bufio.NewReader(read).ReadString('\n'); line <- s }()
	select {
	case s := <-line:
		if s == "" {
			t.Fatal("serve stopped before its startup line")
		}
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("serve startup timed out")
	}
	return ""
}

// servedAt is the scheme and the address of the startup line of serve,
// "lifelog: serving PATH on SCHEME://ADDRESS[ (NOTE)]".
func servedAt(t *testing.T, line string) (scheme, address string) {
	t.Helper()
	_, rest, on := strings.Cut(strings.TrimSpace(line), " on ")
	url, _, _ := strings.Cut(rest, " ")
	scheme, address, ok := strings.Cut(url, "://")
	if !on || !ok || address == "" {
		t.Fatalf("serve startup line: %q", line)
	}
	return scheme, address
}
