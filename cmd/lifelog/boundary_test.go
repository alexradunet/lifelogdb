package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two tests of this file keep the rings of plan 135 (AGENTS.md, "The four rings"): the core is the file, the
// writer and the catalog, and everything that names a third party or talks to one is an extension. Both read the Go
// source of internal/ and cmd/ that is not a test.

// thirdParties are the products and companies the core never names. A thing that names one is an extension; the
// change that moves it out adds its name here.
var thirdParties = []string{"google", "apple", "telegram", "claude", "codex", "opencode", "anthropic", "openai", "modelcontextprotocol"}

// dials are the ways a Go program talks to a server. Only internal/client, the client of lifelog's own server, may
// use them: so every integration with the outside world is an extension by force.
var dials = []string{"http.Client", "http.DefaultClient", "http.Get(", "http.Post(", "http.NewRequest", "net.Dial"}

func TestTheCoreNamesNoThirdParty(t *testing.T) {
	for path, text := range coreSources(t) {
		lower := strings.ToLower(text)
		for _, name := range thirdParties {
			if strings.Contains(lower, name) {
				t.Errorf("%s names %q: the core names no third party (AGENTS.md, the four rings)", path, name)
			}
		}
	}
}

func TestTheCoreDialsOnlyItsServer(t *testing.T) {
	for path, text := range coreSources(t) {
		if strings.HasPrefix(filepath.ToSlash(path), "internal/client/") {
			continue
		}
		for _, d := range dials {
			if strings.Contains(text, d) {
				t.Errorf("%s uses %s: outside internal/client the core dials nothing (AGENTS.md, the four rings)", path, d)
			}
		}
	}
}

// coreSources is the Go source of internal/ and cmd/ that is not a test, by its path relative to the repo root.
func coreSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[rel] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) < 20 {
		t.Fatalf("read %d source files; the walk missed the tree", len(out))
	}
	return out
}
