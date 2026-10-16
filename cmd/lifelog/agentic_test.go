package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgenticInitAndTheDatabaseBesideTheProgram runs the command lines in-process, with the program in a folder of its
// own (plan 090): agentic-init writes the agents' files beside it, init with no path makes life.db there and not in
// the working directory, and a command with no --db and no LIFELOG_DB reads that file.
func TestAgenticInitAndTheDatabaseBesideTheProgram(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "lifelog.exe")
	if err := os.WriteFile(exe, nil, 0o755); err != nil { // a stand-in for the program: only its path counts
		t.Fatal(err)
	}
	e, stdout, _ := testEnv(t) // no LIFELOG_DB in its environment
	e.executable = func() (string, error) { return exe, nil }
	call := func(args ...string) string {
		t.Helper()
		stdout.Reset()
		if err := runArgs(t.Context(), e, args); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stdout)
		}
		return stdout.String()
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
	// init names the one file it makes: the life.db beside the program, not one in the working directory.
	if out := call("init"); out != "created "+filepath.Join(dir, "life.db")+"\n" {
		t.Errorf("init with no path: %q, want the life.db beside the program", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "life.db")); err != nil {
		t.Fatalf("init with no path: %v", err)
	}
	if day := call("day", "2031-04-11"); !strings.Contains(day, "2031-04-11") {
		t.Errorf("day with no --db:\n%.300s", day)
	}
	again := call("agentic-init")
	if strings.Contains(again, "written") || strings.Contains(again, "no life.db yet") || !strings.Contains(again, "unchanged") {
		t.Errorf("a second agentic-init:\n%s", again)
	}
}
