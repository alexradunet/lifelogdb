package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/siren"
)

// TestAgenticInitAndTheDatabaseBesideTheProgram runs the command lines in-process, with the program in a folder of its
// own (plan 090): agentic-init writes the agents' files beside it, init with no path makes life.db there and not in
// the working directory, and a command with no --db and no LIFELOG_DB reads that file. The working directory is an
// empty folder of the test, so a command that wrote into it would not write into the repository.
func TestAgenticInitAndTheDatabaseBesideTheProgram(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
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
	untouched := func(after string) {
		t.Helper()
		if files, err := os.ReadDir(work); err != nil || len(files) != 0 {
			t.Errorf("%s wrote into the working directory: %v %v", after, files, err)
		}
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
	untouched("agentic-init and init")
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
	untouched("the second agentic-init and day")
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
//   - The sentence of AGENTS.md that calls actions the owner's only names the tool names of all the owner's actions
//     of the catalog, and no one of them is a tool. The scan of the texts reads AGENTS.md without that sentence, so
//     an owner's name in any other sentence fails.
//   - A backticked name that looks like a tool (lowercase words joined by _) is a tool of lifelog mcp, a command of
//     commandWords, or a word of notTools.
//   - A backticked command line is a line that the parse of the program takes, with a command of the table: a full
//     line ("lifelog" or the program, then its words) or a short form (a lowercase word and more words, such as
//     "page TITLE"). A backticked flag alone is a global flag or a flag of the last command line before it in the
//     same text.
func TestTheAgentsTextsNameWhatTheProgramHas(t *testing.T) {
	e, dir, exe := agenticFolder(t)
	live := filepath.Join(dir, "life.db")
	tools := mcpToolNames(t, e, []string{"mcp", "--agent", "test", "--db", live})
	var stdout bytes.Buffer
	e.stdout = &stdout
	if err := runArgs(t.Context(), e, []string{"actions", "--db", live}); err != nil {
		t.Fatal(err)
	}
	var actions []siren.Action
	if err := json.Unmarshal(stdout.Bytes(), &actions); err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{} // the tool names of the owner's actions: a hyphen of the action's name is a _
	for _, a := range actions {
		if a.Owner {
			owners[strings.ReplaceAll(a.Name, "-", "_")] = true
		}
	}
	// commandWords are the backticked words of the texts that look like a tool and are a command of the program:
	// the short form of a command line with no words after the command.
	commandWords := map[string]bool{
		"actions": true, // lifelog actions
	}
	// notTools are the other backticked words of the texts that look like a tool and are not one.
	notTools := map[string]bool{
		"lifelog": true, // the name of the server in the MCP configs
		"day":     true, // a field of capture
		"text":    true, // a field of capture
		"created": true, // a list of the answer of capture
		"skipped": true, // a list of the answer of capture
		"at":      true, // the kind of a link from a day page to a place
	}
	toolLike := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	lowercase := regexp.MustCompile(`^[a-z]`)
	backticked := regexp.MustCompile("`([^`\n]+)`")

	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	agents := read("AGENTS.md")
	m := regexp.MustCompile(`the owner's only: ([^.]*)\.`).FindStringSubmatch(agents)
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
		text := read(name)
		if name == "AGENTS.md" {
			text = strings.Replace(text, m[0], "", 1)
		}
		var last command // the command of the last command line of the text
		for _, b := range backticked.FindAllStringSubmatch(text, -1) {
			span, w := b[1], words(b[1])
			switch {
			case toolLike.MatchString(span):
				used[span] = true
				switch {
				case commandWords[span]:
					if commandNamed(span) == nil {
						t.Errorf("%s: `%s` is not a command of the program", name, span)
					}
				case !tools[span] && !notTools[span]:
					t.Errorf("%s: `%s` is not a tool of lifelog mcp", name, span)
				}
			case len(w) > 1 && (w[0] == exe || lowercase.MatchString(w[0])):
				if w[0] == "lifelog" || w[0] == exe {
					w = w[1:] // a full line: the program, then its words
				}
				cmd, err := commandOf(w)
				if err != nil {
					t.Errorf("%s: `%s` is not a command line of the program: %v", name, span, err)
					continue
				}
				last = *cmd
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
	for _, list := range []map[string]bool{commandWords, notTools} {
		for word := range list {
			if !used[word] {
				t.Errorf("no text names `%s`: remove it from the test", word)
			}
		}
	}
}

// commandOf is the command of a command line of a text, given without the program: the parse of the program takes
// the line, and its first word is a command of the table.
func commandOf(line []string) (*command, error) {
	o, err := parse(line, noVars)
	if err != nil {
		return nil, err
	}
	if len(o.args) == 0 {
		return nil, errors.New("the line has no command")
	}
	cmd := commandNamed(o.args[0])
	if cmd == nil {
		return nil, fmt.Errorf("the program has no command %q", o.args[0])
	}
	return cmd, nil
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

// TestAgenticInitAtAFailureOrAKeptFile runs agentic-init in-process at each of its branches:
//   - The path of the program is not known: the command fails and prints nothing, with and without --db.
//   - --db is relative and the working directory is gone: the command fails, because the files name the database by
//     an absolute path.
//   - A file is where a folder of the agents' files must be: the command prints the files that it wrote before it,
//     then fails and names the file that it cannot write. It writes no file after it.
//   - A file has another text: without --force the command keeps the file and prints one line that says how to
//     replace it, also for two kept files. With --force it replaces the file and does not print that line.
func TestAgenticInitAtAFailureOrAKeptFile(t *testing.T) {
	// inFolder is the env of a program in the folder dir, and the output of the env.
	inFolder := func(t *testing.T, dir string) (env, *bytes.Buffer) {
		e, stdout, _ := testEnv(t)
		e.executable = func() (string, error) { return filepath.Join(dir, "lifelog"), nil }
		return e, stdout
	}
	t.Run("the path of the program is not known", func(t *testing.T) {
		for _, argv := range [][]string{{"agentic-init"}, {"agentic-init", "--db", filepath.Join(t.TempDir(), "life.db")}} {
			e, stdout, _ := testEnv(t)
			lost := errors.New("synthetic: no path")
			e.executable = func() (string, error) { return "", lost }
			if err := runArgs(t.Context(), e, argv); !errors.Is(err, lost) {
				t.Errorf("%v: %v, want the error of the path of the program", argv, err)
			}
			if stdout.Len() != 0 {
				t.Errorf("%v printed %q", argv, stdout)
			}
		}
	})
	t.Run("a relative --db and no working directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not remove the working directory of a process")
		}
		dir := t.TempDir()
		e, stdout := inFolder(t, dir)
		gone := t.TempDir()
		t.Chdir(gone)
		if err := os.Remove(gone); err != nil {
			t.Fatal(err)
		}
		// The error is the one of the working directory: it is not there.
		if err := runArgs(t.Context(), e, []string{"agentic-init", "--db", "life.db"}); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("agentic-init with a relative --db and no working directory: %v, want the error of the working directory", err)
		}
		if files, err := os.ReadDir(dir); err != nil || len(files) != 0 || stdout.Len() != 0 {
			t.Errorf("the command wrote %v (%v) and printed %q", files, err, stdout)
		}
	})
	t.Run("a file where a folder must be", func(t *testing.T) {
		dir := t.TempDir()
		e, stdout := inFolder(t, dir)
		if err := os.WriteFile(filepath.Join(dir, ".codex"), []byte("not a folder\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := runArgs(t.Context(), e, []string{"agentic-init"})
		if err == nil || !strings.Contains(err.Error(), ".codex/config.toml") {
			t.Fatalf("agentic-init: %v, want the error of .codex/config.toml", err)
		}
		// In path order, the skills come before .codex/config.toml: the command wrote each one, and printed it.
		skills, err := filepath.Glob(filepath.Join(dir, ".*", "skills", "*", "SKILL.md"))
		if err != nil || len(skills) == 0 {
			t.Fatalf("no skill was written: %v", err)
		}
		var want []string
		for _, s := range skills {
			want = append(want, fmt.Sprintf("%-9s %s", "written", s))
		}
		got := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("the command printed\n%s\nwant the lines of the skills that it wrote:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("AGENTS.md comes after the failure, and it is there: %v", err)
		}
	})
	t.Run("a kept file", func(t *testing.T) {
		dir := t.TempDir()
		e, stdout := inFolder(t, dir)
		run := func(argv ...string) string {
			t.Helper()
			stdout.Reset()
			if err := runArgs(t.Context(), e, argv); err != nil {
				t.Fatalf("%v: %v\n%s", argv, err, stdout)
			}
			return stdout.String()
		}
		run("agentic-init")
		agents, claude := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md")
		program, err := os.ReadFile(agents)
		if err != nil {
			t.Fatal(err)
		}
		own := []byte("The owner's own words.\n")
		for _, name := range []string{agents, claude} {
			if err := os.WriteFile(name, own, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		const hint = "a kept file has another text; lifelog agentic-init --force replaces it\n"
		out := run("agentic-init")
		for _, name := range []string{agents, claude} {
			if !strings.Contains(out, fmt.Sprintf("%-9s %s\n", "kept", name)) {
				t.Errorf("agentic-init does not say that it kept %s:\n%s", name, out)
			}
		}
		if n := strings.Count(out, hint); n != 1 {
			t.Errorf("agentic-init prints the line of a kept file %d times, want 1:\n%s", n, out)
		}
		if b, err := os.ReadFile(agents); err != nil || !bytes.Equal(b, own) {
			t.Errorf("agentic-init changed a kept file: %q %v", b, err)
		}
		out = run("agentic-init", "--force")
		if !strings.Contains(out, fmt.Sprintf("%-9s %s\n", "replaced", agents)) || strings.Contains(out, hint) {
			t.Errorf("agentic-init --force:\n%s", out)
		}
		if b, err := os.ReadFile(agents); err != nil || !bytes.Equal(b, program) {
			t.Errorf("agentic-init --force did not write the text of the program: %q %v", b, err)
		}
	})
}
