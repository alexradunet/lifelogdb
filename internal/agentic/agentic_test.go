package agentic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func paths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{Dir: dir, Exe: filepath.Join(dir, "lifelog.exe"), DB: filepath.Join(dir, "life.db")}
}

func read(t *testing.T, p Paths, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var allFiles = []string{
	".agents/skills/lifelog-import-notes/SKILL.md", ".agents/skills/lifelog-keep-photos/SKILL.md",
	".agents/skills/lifelog-readings-from-table/SKILL.md", ".claude/skills/lifelog-import-notes/SKILL.md",
	".claude/skills/lifelog-keep-photos/SKILL.md", ".claude/skills/lifelog-readings-from-table/SKILL.md",
	".codex/config.toml", ".mcp.json", ".pi/mcp.json", "AGENTS.md", "CLAUDE.md", "opencode.json",
}

func TestInitWritesEveryFileWithThePaths(t *testing.T) {
	p := paths(t)
	res, err := Init(p, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range res {
		if r.State != Written {
			t.Errorf("%s: %s, want written", r.Path, r.State)
		}
		got = append(got, r.Path)
	}
	if !reflect.DeepEqual(got, allFiles) {
		t.Fatalf("files %v, want %v", got, allFiles)
	}
	agents := read(t, p, "AGENTS.md")
	for _, want := range []string{"`" + p.DB + "`", "`" + p.Exe + "`", "`" + p.Dir + "`", "lifelog-import-notes", "## Privacy"} {
		if !strings.Contains(agents, want) {
			t.Errorf("AGENTS.md lacks %q", want)
		}
	}
	if read(t, p, "CLAUDE.md") != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md does not import AGENTS.md")
	}
	if left, _ := filepath.Glob(filepath.Join(p.Dir, ".agentic-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestTheMCPConfigsStartLifelogOnTheDatabase(t *testing.T) {
	p := paths(t)
	if _, err := Init(p, false); err != nil {
		t.Fatal(err)
	}
	args := func(agent string) []string { return []string{"mcp", "--agent", agent, "--db", p.DB} }
	for _, c := range []struct{ file, agent string }{{".mcp.json", "claude"}, {".pi/mcp.json", "pi"}} {
		var cfg struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(read(t, p, c.file)), &cfg); err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		s := cfg.MCPServers["lifelog"]
		if s.Command != p.Exe || !reflect.DeepEqual(s.Args, args(c.agent)) {
			t.Errorf("%s: %+v", c.file, s)
		}
	}
	var oc struct {
		MCP map[string]struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled bool     `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(read(t, p, "opencode.json")), &oc); err != nil {
		t.Fatal(err)
	}
	if s := oc.MCP["lifelog"]; s.Type != "local" || !s.Enabled || !reflect.DeepEqual(s.Command, append([]string{p.Exe}, args("opencode")...)) {
		t.Errorf("opencode.json: %+v", s)
	}
	codex := read(t, p, ".codex/config.toml")
	if !strings.HasPrefix(codex, "[mcp_servers.lifelog]\ncommand = ") || !strings.Contains(codex, `"--agent", "codex"`) {
		t.Errorf(".codex/config.toml:\n%s", codex)
	}
}

func TestPathsWithQuotesAndBackslashesStayValid(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, `my "life" \ log`)
	p := Paths{Dir: dir, Exe: `C:\Life "Log"\lifelog.exe`, DB: odd}
	files, err := Files(p)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(files[".mcp.json"], &cfg); err != nil {
		t.Fatalf(".mcp.json is not JSON: %v", err)
	}
	want := `command = "C:\\Life \"Log\"\\lifelog.exe"`
	if !strings.Contains(string(files[".codex/config.toml"]), want) {
		t.Errorf("TOML command: want %s in\n%s", want, files[".codex/config.toml"])
	}
	if got := tomlString("a\tb\x7f"); got != `"a\u0009b\u007F"` {
		t.Errorf("tomlString control characters = %s", got)
	}
}

func TestEachSkillFollowsTheSharedFormat(t *testing.T) {
	p := paths(t)
	files, err := Files(p)
	if err != nil {
		t.Fatal(err)
	}
	nameRE := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	front := regexp.MustCompile(`(?s)^---\nname: ([^\n]*)\ndescription: ([^\n]*)\n---\n`)
	n := 0
	for name, b := range files {
		if !strings.HasSuffix(name, "/SKILL.md") {
			continue
		}
		n++
		dir := filepath.Base(filepath.Dir(filepath.FromSlash(name)))
		m := front.FindStringSubmatch(string(b))
		if m == nil {
			t.Errorf("%s: no name and description frontmatter", name)
			continue
		}
		if m[1] != dir || !nameRE.MatchString(m[1]) || len(m[1]) > 64 {
			t.Errorf("%s: name %q, folder %q", name, m[1], dir)
		}
		if l := len(m[2]); l < 1 || l > 1024 {
			t.Errorf("%s: description of %d characters", name, l)
		}
	}
	if n != 6 {
		t.Errorf("%d skill files, want 3 skills in 2 folders", n)
	}
}

func TestASecondRunKeepsTheOwnersEdits(t *testing.T) {
	p := paths(t)
	if _, err := Init(p, false); err != nil {
		t.Fatal(err)
	}
	again, err := Init(p, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range again {
		if r.State != Unchanged {
			t.Errorf("a second run: %s %s", r.Path, r.State)
		}
	}
	edited := read(t, p, "AGENTS.md") + "\nThe owner's own line.\n"
	if err := os.WriteFile(filepath.Join(p.Dir, "AGENTS.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	state := func(res []Result) string {
		for _, r := range res {
			if r.Path == "AGENTS.md" {
				return r.State
			}
		}
		return ""
	}
	res, err := Init(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if state(res) != Kept || read(t, p, "AGENTS.md") != edited {
		t.Fatalf("an edited AGENTS.md: %s, text kept %v", state(res), read(t, p, "AGENTS.md") == edited)
	}
	res, err = Init(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if state(res) != Replaced || strings.Contains(read(t, p, "AGENTS.md"), "The owner's own line.") {
		t.Fatalf("--force: %s", state(res))
	}
}
