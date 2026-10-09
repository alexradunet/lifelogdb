package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExecutableAgenticInitAndTheDatabaseBesideIt runs the built program in a folder of its own (plan 090):
// agentic-init writes the agents' files beside it, init with no path makes life.db there, and a command with no --db
// and no LIFELOG_DB reads that file.
func TestExecutableAgenticInitAndTheDatabaseBesideIt(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "lifelog.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LIFELOG_DB=") {
			env = append(env, kv)
		}
	}
	work := t.TempDir() // the working directory is elsewhere: the folder of the program decides
	call := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = work, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	out := call("agentic-init")
	if !strings.Contains(out, "written") || !strings.Contains(out, "no life.db yet") {
		t.Fatalf("agentic-init:\n%s", out)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".mcp.json", ".codex/config.toml", "opencode.json", ".pi/mcp.json",
		".agents/skills/lifelog-import-notes/SKILL.md", ".claude/skills/lifelog-import-notes/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || !strings.Contains(string(agents), filepath.Join(dir, "life.db")) {
		t.Errorf("AGENTS.md does not name the life.db beside the program: %v", err)
	}
	call("init")
	if _, err := os.Stat(filepath.Join(dir, "life.db")); err != nil {
		t.Fatalf("init with no path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "life.db")); !os.IsNotExist(err) {
		t.Errorf("init wrote into the working directory: %v", err)
	}
	if day := call("day", "2031-04-11"); !strings.Contains(day, "2031-04-11") {
		t.Errorf("day with no --db:\n%.300s", day)
	}
	again := call("agentic-init")
	if strings.Contains(again, "written") || strings.Contains(again, "no life.db yet") || !strings.Contains(again, "unchanged") {
		t.Errorf("a second agentic-init:\n%s", again)
	}
}
