package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests build the real lifelog, lifelog-mcp and lifelog-agentic-init once, and copy them into a folder of its own
// for each test: lifelog-agentic-init writes into the folder of lifelog. `lifelog agentic-init` then holds the
// discovery of step 1 of plan 135, the environment of an extension (extensions/README.md) and the extension itself.

var (
	binDir   string // the folder with the three programs, built once
	buildErr error  // why binDir has none; the tests fail with it
	skip     string // why the tests cannot build: no go command, no sources
)

func TestMain(m *testing.M) {
	code := 1
	func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			skip = fmt.Sprintf("no go command to build lifelog: %v", err)
			code = m.Run()
			return
		}
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err == nil {
			_, err = os.Stat(filepath.Join(root, "cmd", "lifelog"))
		}
		if err != nil {
			skip = fmt.Sprintf("the lifelog sources are not at ../..: %v", err)
			code = m.Run()
			return
		}
		binDir, err = os.MkdirTemp("", "lifelog-agentic-test-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer os.RemoveAll(binDir)
		for _, args := range [][]string{
			{"build", "-C", root, "-o", filepath.Join(binDir, exeName("lifelog")), "./cmd/lifelog"},
			{"build", "-C", filepath.Join("..", "mcp"), "-o", filepath.Join(binDir, exeName("lifelog-mcp")), "./cmd/lifelog-mcp"},
			{"build", "-o", filepath.Join(binDir, exeName("lifelog-agentic-init")), "./cmd/lifelog-agentic-init"},
		} {
			if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("go %v: %v\n%s", args, err, out)
				break
			}
		}
		code = m.Run()
	}()
	os.Exit(code)
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// folder is a program folder of its own with copies of the three programs, and an empty working directory.
type folder struct {
	t         *testing.T
	dir, work string
	lifelog   string
	db        string
}

func newFolder(t *testing.T) *folder {
	t.Helper()
	if skip != "" {
		t.Skip(skip)
	}
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir()) // lifelog resolves symbolic links in the path of its program
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lifelog", "lifelog-mcp", "lifelog-agentic-init"} {
		b, err := os.ReadFile(filepath.Join(binDir, exeName(name)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, exeName(name)), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &folder{t: t, dir: dir, work: t.TempDir(), lifelog: filepath.Join(dir, exeName("lifelog")), db: filepath.Join(dir, "life.db")}
}

// env is the environment of a child: the inherited one without any variable that could point it at another file or
// server than the one of the test.
func env() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "LIFELOG_") })
}

// run runs a program with the stripped environment in the empty working directory. It returns the exit status.
func (f *folder) run(program string, args ...string) (stdout, stderr string, status int) {
	f.t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = f.work, env()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	switch err := cmd.Run().(type) {
	case nil:
		status = 0
	case *exec.ExitError:
		status = err.ExitCode()
	default:
		f.t.Fatalf("%s %v: %v", program, args, err)
	}
	return out.String(), errb.String(), status
}

// ok runs `lifelog ARGS` and fails the test unless it exits 0.
func (f *folder) ok(args ...string) string {
	f.t.Helper()
	out, errb, status := f.run(f.lifelog, args...)
	if status != 0 {
		f.t.Fatalf("lifelog %v: status %d\n%s\n%s", args, status, out, errb)
	}
	return out
}

func (f *folder) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(name)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func TestTheFilesAndTheDatabaseBesideTheProgram(t *testing.T) {
	f := newFolder(t)
	out := f.ok("agentic-init")
	if !strings.Contains(out, "written") {
		t.Fatalf("agentic-init:\n%s", out)
	}
	if want := "no life.db yet: lifelog init " + f.db + " makes it\n"; !strings.Contains(out, want) {
		t.Errorf("agentic-init does not suggest lifelog init:\n%s\nwant the line %q", out, want)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".mcp.json", ".codex/config.toml", "opencode.json", ".pi/mcp.json",
		".agents/skills/lifelog-import-notes/SKILL.md", ".claude/skills/lifelog-import-notes/SKILL.md",
		".agents/skills/lifelog-extension/SKILL.md", ".claude/skills/lifelog-extension/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(f.dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !strings.Contains(f.read("AGENTS.md"), f.db) {
		t.Errorf("AGENTS.md does not name the life.db beside the program")
	}
	if out := f.ok("init"); out != "created "+f.db+"\n" {
		t.Errorf("init with no path: %q, want the life.db beside the program", out)
	}
	again := f.ok("agentic-init")
	if strings.Contains(again, "written") || strings.Contains(again, "no life.db yet") || !strings.Contains(again, "unchanged") {
		t.Errorf("a second agentic-init:\n%s", again)
	}
	if files, err := os.ReadDir(f.work); err != nil || len(files) != 0 {
		t.Errorf("the commands wrote into the working directory: %v %v", files, err)
	}
}

func TestTheExtensionRefusesToRunAlone(t *testing.T) {
	f := newFolder(t)
	tool := filepath.Join(f.dir, exeName("lifelog-agentic-init"))
	_, stderr, status := f.run(tool)
	want := "lifelog-agentic-init: run it as lifelog agentic-init: it needs LIFELOG_EXE and LIFELOG_DB\n"
	if status != 1 || stderr != want {
		t.Errorf("without the environment: status %d, stderr %q, want 1 and %q", status, stderr, want)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 3 {
		t.Errorf("it wrote files: %v", entries)
	}
	_, stderr, status = f.run(f.lifelog, "agentic-init", "extra")
	if status != 1 || !strings.Contains(stderr, `unexpected argument "extra"`) {
		t.Errorf("a positional argument: status %d, stderr %q, want a refusal", status, stderr)
	}
}

// TestTheMCPConfigsStartTheMCPServerOfThisProgram: each MCP config that agentic-init writes starts this program with
// the line "mcp --agent NAME --db FILE". NAME is the agent of its framework and FILE is the life.db beside the program.
// The test then runs each line as the framework would: the program finds lifelog-mcp beside itself, and the server
// lists the tools of the agent. The MCP framing is written by hand (one JSON object per line), so that the module
// needs no SDK.
func TestTheMCPConfigsStartTheMCPServerOfThisProgram(t *testing.T) {
	f := newFolder(t)
	f.ok("init")
	f.ok("agentic-init")
	for _, c := range []struct{ file, agent string }{
		{".mcp.json", "claude"}, {".pi/mcp.json", "pi"}, {"opencode.json", "opencode"}, {".codex/config.toml", "codex"},
	} {
		t.Run(c.agent, func(t *testing.T) {
			command, args := f.mcpServerOf(c.file)
			want := []string{"mcp", "--agent", c.agent, "--db", f.db}
			if command != f.lifelog || !slices.Equal(args, want) {
				t.Fatalf("%s: %s %q, want %s %q", c.file, command, args, f.lifelog, want)
			}
			tools := listTools(t, f, command, args)
			for _, name := range []string{"find", "capture"} {
				if !tools[name] {
					t.Errorf("no tool %q in %v", name, slices.Sorted(maps.Keys(tools)))
				}
			}
			if tools["snapshot"] {
				t.Errorf("the owner's action snapshot is a tool")
			}
		})
	}
}

// listTools starts the MCP server on stdio and speaks the protocol by hand: initialize, the notification
// initialized, tools/list.
func listTools(t *testing.T, f *folder, command string, args []string) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute) // a guard against a deadlock
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir, cmd.Env = f.work, env()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		io.Copy(io.Discard, stdout)
		cmd.Wait() // the server ends when its stdin closes
	}()
	lines := bufio.NewReader(stdout)
	send := func(v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stdin.Write(append(b, '\n')); err != nil {
			t.Fatalf("%v\n%s", err, stderr.String())
		}
	}
	// answer reads lines until the response with the id; a server may send notifications before it.
	answer := func(id int) json.RawMessage {
		t.Helper()
		for {
			line, err := lines.ReadBytes('\n')
			if err != nil {
				t.Fatalf("waiting for the answer %d: %v\n%s", id, err, stderr.String())
			}
			var msg struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(line, &msg); err != nil {
				t.Fatalf("not a JSON line: %q", line)
			}
			if msg.ID != nil && *msg.ID == id {
				if msg.Error != nil {
					t.Fatalf("answer %d is an error: %s", id, msg.Error)
				}
				return msg.Result
			}
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "agentic-test", "version": "0"}}})
	answer(1)
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(answer(2), &listed); err != nil {
		t.Fatal(err)
	}
	tools := map[string]bool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = true
	}
	return tools
}

// mcpServerOf is the command and the arguments of the server "lifelog" in the MCP config name of the folder.
// opencode.json must also say that the server is local and enabled.
func (f *folder) mcpServerOf(name string) (command string, args []string) {
	t := f.t
	t.Helper()
	b := []byte(f.read(name))
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

// helpOf is what the built `lifelog help` says: the words of each command and the flags that its lines name, and the
// global flags. A usage line is "  lifelog WORDS [--flag X] ...", a continuation line starts with spaces only.
type helpOf struct {
	flags  map[string][]string // command -> its flags
	global []string
}

func parseHelp(text string) helpOf {
	h := helpOf{flags: map[string][]string{}}
	flagRE := regexp.MustCompile(`--[a-z][a-z-]*`)
	current, inGlobal := "", false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "Global flags"):
			inGlobal, current = true, ""
		case inGlobal:
			h.global = append(h.global, flagRE.FindString(line))
		case strings.HasPrefix(line, "  lifelog "):
			current = ""
			words := strings.Fields(strings.TrimPrefix(line, "  lifelog "))
			if len(words) > 0 && regexp.MustCompile(`^[a-z]+$`).MatchString(words[0]) { // not NAME, [PATH] or --flag
				current = words[0]
				h.flags[current] = append(h.flags[current], flagRE.FindAllString(line, -1)...)
			}
		case current != "" && strings.HasPrefix(line, "   "):
			h.flags[current] = append(h.flags[current], flagRE.FindAllString(line, -1)...)
		default:
			current = ""
		}
	}
	h.global = slices.DeleteFunc(h.global, func(s string) bool { return s == "" })
	return h
}

// TestTheAgentsTextsNameWhatTheProgramHas: the texts that agentic-init writes (AGENTS.md, CLAUDE.md and the skills)
// name only what the program has. So a rename in the program fails here, and not in the session of an agent.
//   - The sentence of AGENTS.md that calls actions the owner's only names the tool names of all the owner's actions
//     of the catalog (lifelog actions), and no one of them is a tool. The scan of the texts reads AGENTS.md without
//     that sentence, so an owner's name in any other sentence fails.
//   - A backticked name that looks like a tool (lowercase words joined by _) is a tool (an action that is not the
//     owner's, with _ for -, or get), a command of the help of lifelog (commandWords), or a word of notTools.
//   - A backticked command line is a line with a command: "lifelog" or the program, then its words, or a short form (a
//     lowercase word and more words, such as "page TITLE"). The command is the first word that is not a global flag
//     or its value. It is a command of the help, or mcp, agentic-init or gphotos. Its flags are global flags, flags
//     that the help names for it, --agent for mcp and --force for agentic-init. A backticked flag alone is a global
//     flag or a flag of the last command line before it in the same text.
func TestTheAgentsTextsNameWhatTheProgramHas(t *testing.T) {
	f := newFolder(t)
	f.ok("init")
	f.ok("agentic-init")
	// The usage goes to stderr, and the status of `lifelog help` is 2.
	usageOut, usage, status := f.run(f.lifelog, "help")
	if status != 2 || usageOut != "" {
		t.Fatalf("lifelog help: status %d, stdout %q", status, usageOut)
	}
	help := parseHelp(usage)
	var actions []struct {
		Name  string `json:"name"`
		Owner bool   `json:"owner"`
	}
	if err := json.Unmarshal([]byte(f.ok("actions", "--db", f.db)), &actions); err != nil {
		t.Fatal(err)
	}
	// The tools of lifelog-mcp are the actions that are not the owner's, and get.
	owners, tools := map[string]bool{}, map[string]bool{"get": true} // the tool names: a hyphen of the action's name is a _
	for _, a := range actions {
		if a.Owner {
			owners[strings.ReplaceAll(a.Name, "-", "_")] = true
		} else {
			tools[strings.ReplaceAll(a.Name, "-", "_")] = true
		}
	}
	if len(owners) == 0 || len(tools) < 10 || len(help.flags) < 10 || len(help.global) == 0 {
		t.Fatalf("the program says too little: %d owner actions, %d tools, %d commands, %d global flags", len(owners), len(tools), len(help.flags), len(help.global))
	}
	// extensionFlags are the flags of the extensions that the texts name. Their commands are no lines of the help.
	extensionFlags := map[string][]string{"mcp": {"--agent"}, "agentic-init": {"--force"}, "gphotos": nil}
	flagsOf := func(cmd string) ([]string, bool) {
		if fl, ok := help.flags[cmd]; ok {
			return fl, true
		}
		fl, ok := extensionFlags[cmd]
		return fl, ok
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
	valueFlags := []string{"--db", "--url", "--source"} // the global flags that take a value

	agents := f.read("AGENTS.md")
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

	skills, err := filepath.Glob(filepath.Join(f.dir, ".agents", "skills", "*", "SKILL.md"))
	if err != nil || len(skills) == 0 {
		t.Fatalf("no skill: %v", err)
	}
	texts := []string{"AGENTS.md", "CLAUDE.md"}
	for _, s := range skills {
		rel, err := filepath.Rel(f.dir, s)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, filepath.ToSlash(rel))
	}
	used := map[string]bool{}
	for _, name := range texts {
		text := f.read(name)
		if name == "AGENTS.md" {
			text = strings.Replace(text, m[0], "", 1)
		}
		var lastName string // the command of the last command line of the text
		var lastFlags []string
		for _, b := range backticked.FindAllStringSubmatch(text, -1) {
			span, w := b[1], words(b[1])
			switch {
			case toolLike.MatchString(span):
				used[span] = true
				switch {
				case commandWords[span]:
					if _, ok := flagsOf(span); !ok {
						t.Errorf("%s: `%s` is not a command of the program", name, span)
					}
				case !tools[span] && !notTools[span]:
					t.Errorf("%s: `%s` is not a tool of lifelog-mcp", name, span)
				}
			case len(w) > 1 && (w[0] == f.lifelog || lowercase.MatchString(w[0])):
				if w[0] == "lifelog" || w[0] == f.lifelog {
					w = w[1:] // a full line: the program, then its words
				}
				if len(w) > 0 && w[0] == "NAME" { // the line of an extension in general: lifelog NAME ARGS...
					lastName, lastFlags = "", nil
					continue
				}
				cmd, flags, err := commandLine(w, valueFlags, help.global, flagsOf)
				if err != nil {
					t.Errorf("%s: `%s` is not a command line of the program: %v", name, span, err)
					continue
				}
				lastName, lastFlags = cmd, flags
			case strings.HasPrefix(span, "--"):
				flag, _, _ := strings.Cut(w[0], "=")
				if !slices.Contains(help.global, flag) && !slices.Contains(lastFlags, flag) {
					t.Errorf("%s: `%s`: %s is not a global flag or a flag of the command before it, %q", name, span, flag, lastName)
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

// commandLine reads a command line of a text, given without the program: it skips the global flags (and the values
// of those that take one) before the command, takes the command, and checks every flag of the line.
func commandLine(w, valueFlags, global []string, flagsOf func(string) ([]string, bool)) (cmd string, flags []string, err error) {
	for i := 0; i < len(w); i++ {
		flag, _, hasValue := strings.Cut(w[i], "=")
		switch {
		case strings.HasPrefix(w[i], "--"):
			if !slices.Contains(global, flag) {
				return "", nil, fmt.Errorf("%s is not a global flag, and it comes before the command", flag)
			}
			if slices.Contains(valueFlags, flag) && !hasValue {
				i++
			}
		case cmd == "":
			var ok bool
			if flags, ok = flagsOf(w[i]); !ok {
				return "", nil, fmt.Errorf("the program has no command %q", w[i])
			}
			cmd = w[i]
		}
		if cmd != "" {
			break
		}
	}
	if cmd == "" {
		return "", nil, fmt.Errorf("the line has no command")
	}
	for _, x := range w {
		if flag, _, _ := strings.Cut(x, "="); strings.HasPrefix(x, "--") && !slices.Contains(global, flag) && !slices.Contains(flags, flag) {
			return "", nil, fmt.Errorf("%s is not a global flag or a flag of %s", flag, cmd)
		}
	}
	return cmd, flags, nil
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

// TestAgenticInitAtAFailureOrAKeptFile runs the command at two of its branches:
//   - A file is where a folder of the agents' files must be: the command prints the files that it wrote before it,
//     then fails and names the file that it cannot write. It writes no file after it.
//   - A file has another text: without --force the command keeps the file and prints one line that says how to
//     replace it, also for two kept files. With --force it replaces the file and does not print that line.
func TestAgenticInitAtAFailureOrAKeptFile(t *testing.T) {
	t.Run("a file where a folder must be", func(t *testing.T) {
		f := newFolder(t)
		if err := os.WriteFile(filepath.Join(f.dir, ".codex"), []byte("not a folder\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, status := f.run(f.lifelog, "agentic-init")
		if status != 1 || !strings.Contains(stderr, ".codex/config.toml") {
			t.Fatalf("agentic-init: status %d, stderr %q, want 1 and the error of .codex/config.toml", status, stderr)
		}
		// In path order, the skills come before .codex/config.toml: the command wrote each one, and printed it.
		skills, err := filepath.Glob(filepath.Join(f.dir, ".*", "skills", "*", "SKILL.md"))
		if err != nil || len(skills) == 0 {
			t.Fatalf("no skill was written: %v", err)
		}
		var want []string
		for _, s := range skills {
			want = append(want, fmt.Sprintf("%-9s %s", "written", s))
		}
		got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("the command printed\n%s\nwant the lines of the skills that it wrote:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if _, err := os.Stat(filepath.Join(f.dir, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("AGENTS.md comes after the failure, and it is there: %v", err)
		}
	})
	t.Run("a kept file", func(t *testing.T) {
		f := newFolder(t)
		f.ok("agentic-init")
		agents, claude := filepath.Join(f.dir, "AGENTS.md"), filepath.Join(f.dir, "CLAUDE.md")
		program := f.read("AGENTS.md")
		own := "The owner's own words.\n"
		for _, name := range []string{agents, claude} {
			if err := os.WriteFile(name, []byte(own), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		const hint = "a kept file has another text; lifelog agentic-init --force replaces it\n"
		out := f.ok("agentic-init")
		for _, name := range []string{agents, claude} {
			if !strings.Contains(out, fmt.Sprintf("%-9s %s\n", "kept", name)) {
				t.Errorf("agentic-init does not say that it kept %s:\n%s", name, out)
			}
		}
		if n := strings.Count(out, hint); n != 1 {
			t.Errorf("agentic-init prints the line of a kept file %d times, want 1:\n%s", n, out)
		}
		if f.read("AGENTS.md") != own {
			t.Errorf("agentic-init changed a kept file")
		}
		out = f.ok("agentic-init", "--force")
		if !strings.Contains(out, fmt.Sprintf("%-9s %s\n", "replaced", agents)) || strings.Contains(out, hint) {
			t.Errorf("agentic-init --force:\n%s", out)
		}
		if f.read("AGENTS.md") != program {
			t.Errorf("agentic-init --force did not write the text of the program")
		}
	})
}
