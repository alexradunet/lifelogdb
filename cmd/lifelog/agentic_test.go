package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api"
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

// agenticFolder runs init and agentic-init in-process for a program in a folder of its own, dir. The life.db and the
// files of the agents are then beside the program, exe. The env has no LIFELOG_DB.
func agenticFolder(t *testing.T) (e env, dir, exe string) {
	t.Helper()
	dir = t.TempDir()
	exe = filepath.Join(dir, "lifelog.exe")
	e, _, _ = testEnv(t)
	e.executable = func() (string, error) { return exe, nil }
	for _, argv := range [][]string{{"init"}, {"agentic-init"}} {
		if err := runArgs(t.Context(), e, argv); err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
	}
	return e, dir, exe
}

// TestTheMCPConfigsStartTheMCPServerOfThisProgram: each MCP config that agentic-init writes starts this program with
// the line "mcp --agent NAME --db FILE". NAME is the agent of its framework and FILE is the life.db beside the program.
// The program runs that line and serves its tools on the file.
func TestTheMCPConfigsStartTheMCPServerOfThisProgram(t *testing.T) {
	e, dir, exe := agenticFolder(t)
	for _, c := range []struct{ file, agent string }{
		{".mcp.json", "claude"}, {".pi/mcp.json", "pi"}, {"opencode.json", "opencode"}, {".codex/config.toml", "codex"},
	} {
		command, args := mcpServerOf(t, dir, c.file)
		want := []string{"mcp", "--agent", c.agent, "--db", filepath.Join(dir, "life.db")}
		if command != exe || !slices.Equal(args, want) {
			t.Errorf("%s: %s %q, want %s %q", c.file, command, args, exe, want)
			continue
		}
		if tools := mcpToolNames(t, e, args); !tools["find"] {
			t.Errorf("%s: the server of its line lists the tools %v, without find", c.file, tools)
		}
	}
}

// mcpServerOf is the command and the arguments of the server "lifelog" in the MCP config name of the folder dir.
// opencode.json must also say that the server is local and enabled.
func mcpServerOf(t *testing.T, dir, name string) (command string, args []string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	switch name {
	case ".mcp.json", ".pi/mcp.json":
		var cfg struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := cfg.MCPServers["lifelog"]
		return s.Command, s.Args
	case "opencode.json":
		var cfg struct {
			MCP map[string]struct {
				Type    string   `json:"type"`
				Command []string `json:"command"`
				Enabled bool     `json:"enabled"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := cfg.MCP["lifelog"]
		if s.Type != "local" || !s.Enabled || len(s.Command) == 0 {
			t.Fatalf("%s: %+v, want a local server that is enabled", name, s)
		}
		return s.Command[0], s.Command[1:]
	case ".codex/config.toml":
		// agentic-init writes three lines: the table, then command = STRING and args = [STRING, ...]. Its strings
		// escape only \\, \" and \uXXXX, which JSON reads in the same way, so each value is read as JSON.
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) != 3 || lines[0] != "[mcp_servers.lifelog]" {
			t.Fatalf("%s:\n%s", name, b)
		}
		for _, line := range lines[1:] {
			key, value, _ := strings.Cut(line, " = ")
			var err error
			switch key {
			case "command":
				err = json.Unmarshal([]byte(value), &command)
			case "args":
				err = json.Unmarshal([]byte(value), &args)
			default:
				err = fmt.Errorf("unknown key %q", key)
			}
			if err != nil {
				t.Fatalf("%s: %s: %v", name, line, err)
			}
		}
		return command, args
	}
	t.Fatalf("%s: not an MCP config", name)
	return "", nil
}

// TestTheAgentsTextsNameWhatTheProgramHas: the texts that agentic-init writes (AGENTS.md, CLAUDE.md and the skills)
// name only what the program has. So a rename in the program fails here, and not in the session of an agent.
//   - A backticked name that looks like a tool (lowercase words joined by _) is a tool of lifelog mcp, a name that
//     AGENTS.md calls the owner's only, or a word of notTools.
//   - The names that AGENTS.md calls the owner's only are the tool names of all the owner's actions of the catalog,
//     and no one of them is a tool.
//   - A backticked command line ("lifelog" or the program, then its words) is a line that the parse of the program
//     takes, with a command of the table. A backticked flag alone is a global flag or a flag of the last command line
//     before it in the same text.
func TestTheAgentsTextsNameWhatTheProgramHas(t *testing.T) {
	e, dir, exe := agenticFolder(t)
	live := filepath.Join(dir, "life.db")
	tools := mcpToolNames(t, e, []string{"mcp", "--agent", "test", "--db", live})
	var stdout bytes.Buffer
	e.stdout = &stdout
	if err := runArgs(t.Context(), e, []string{"actions", "--db", live}); err != nil {
		t.Fatal(err)
	}
	var actions []api.Action
	if err := json.Unmarshal(stdout.Bytes(), &actions); err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{} // the tool names of the owner's actions: a hyphen of the action's name is a _
	for _, a := range actions {
		if a.Owner {
			owners[strings.ReplaceAll(a.Name, "-", "_")] = true
		}
	}
	// notTools are the backticked words of the texts that look like a tool and are not one.
	notTools := map[string]bool{
		"lifelog": true, // the name of the server in the MCP configs
		"actions": true, // the command lifelog actions
		"day":     true, // a field of capture
		"text":    true, // a field of capture
		"created": true, // a list of the answer of capture
		"skipped": true, // a list of the answer of capture
		"at":      true, // the kind of a link from a day page to a place
	}
	toolLike := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	backticked := regexp.MustCompile("`([^`\n]+)`")

	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	m := regexp.MustCompile(`the owner's only: ([^.]*)\.`).FindStringSubmatch(read("AGENTS.md"))
	if m == nil {
		t.Fatal("AGENTS.md calls no action the owner's only")
	}
	ownerOnly := map[string]bool{}
	for _, b := range backticked.FindAllStringSubmatch(m[1], -1) {
		ownerOnly[b[1]] = true
		if tools[b[1]] {
			t.Errorf("AGENTS.md calls %s the owner's only, and it is a tool", b[1])
		}
	}
	if !maps.Equal(ownerOnly, owners) {
		t.Errorf("AGENTS.md calls %v the owner's only; the owner's actions are %v", ownerOnly, owners)
	}

	skills, err := filepath.Glob(filepath.Join(dir, ".agents", "skills", "*", "SKILL.md"))
	if err != nil || len(skills) == 0 {
		t.Fatalf("no skill: %v", err)
	}
	texts := []string{"AGENTS.md", "CLAUDE.md"}
	for _, s := range skills {
		rel, err := filepath.Rel(dir, s)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, filepath.ToSlash(rel))
	}
	used := map[string]bool{}
	for _, name := range texts {
		var last command // the command of the last command line of the text
		for _, b := range backticked.FindAllStringSubmatch(read(name), -1) {
			span, w := b[1], words(b[1])
			switch {
			case toolLike.MatchString(span):
				used[span] = true
				if !tools[span] && !ownerOnly[span] && !notTools[span] {
					t.Errorf("%s: `%s` is not a tool of lifelog mcp", name, span)
				}
			case len(w) > 1 && (w[0] == "lifelog" || w[0] == exe):
				o, err := parse(w[1:], noVars)
				if err != nil || len(o.args) == 0 || commandNamed(o.args[0]) == nil {
					t.Errorf("%s: `%s` is not a command line of the program: %v", name, span, err)
					continue
				}
				last = *commandNamed(o.args[0])
			case strings.HasPrefix(span, "--"):
				flag, _, _ := strings.Cut(w[0], "=")
				if !slices.Contains(globalFlags, flag) && !slices.Contains(last.flags, flag) {
					t.Errorf("%s: `%s`: %s is not a global flag or a flag of the command before it, %q", name, span, flag, last.name)
				}
			default:
				for _, x := range w {
					if strings.HasPrefix(x, "--") {
						t.Errorf("%s: `%s` names the flag %s outside a command line", name, span, x)
					}
				}
			}
		}
	}
	for word := range notTools {
		if !used[word] {
			t.Errorf("no text names `%s`: remove it from notTools", word)
		}
	}
}

// words splits a command line of a text at its spaces. A word in quotes keeps its spaces and loses its quotes.
func words(line string) []string {
	var out []string
	for i, part := range strings.Split(line, `"`) {
		if i%2 == 1 {
			out = append(out, part)
		} else {
			out = append(out, strings.Fields(part)...)
		}
	}
	return out
}
