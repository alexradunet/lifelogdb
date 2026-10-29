package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheTableOfTheSurfacesIsTheCatalog keeps the README's table "Every action on every surface" equal to the catalog
// (AGENTS.md, "Every capability on every surface"): one row an action, in the order of GET /actions; its route; the
// CLI's do of it, and each shortcut a command of the program; its MCP tool, or none for an owner's action; and the
// place of its form in the browser.
func TestTheTableOfTheSurfacesIsTheCatalog(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(b), "\n## Every action on every surface\n")
	if !ok {
		t.Fatal("README.md has no section Every action on every surface")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	row := regexp.MustCompile("(?m)^\\| `([a-z-]+)` \\| (.*) \\| (.*) \\| (.*) \\| (.*) \\|$")
	rows := row.FindAllStringSubmatch(section, -1)

	c, _, _ := apitest.Fresh(t, "cli")
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions
	if len(rows) != len(catalog) {
		t.Errorf("the table has %d rows, the catalog %d actions", len(rows), len(catalog))
	}
	backticked := regexp.MustCompile("`([^`]+)`")
	shown := map[string]string{} // the shortcuts that the table shows, each with the action of its row
	for i, a := range catalog {
		if i >= len(rows) {
			break
		}
		r := rows[i]
		if r[1] != a.Name {
			t.Errorf("row %d is %s, want %s: the rows follow the catalog", i+1, r[1], a.Name)
			continue
		}
		if want := "`" + a.Method + " " + a.Href + "`"; r[2] != want {
			t.Errorf("%s: the API is %s, want %s", a.Name, r[2], want)
		}
		cli := backticked.FindAllStringSubmatch(r[3], -1)
		if len(cli) == 0 || cli[0][1] != "do "+a.Name {
			t.Errorf("%s: the CLI is %s, want do %s first", a.Name, r[3], a.Name)
		}
		for _, s := range cli[min(1, len(cli)):] {
			if commandNamed(s[1]) == nil {
				t.Errorf("%s: the CLI names %s, which is no command of the program", a.Name, s[1])
				continue
			}
			shown[s[1]] = a.Name
			if w := commandNamed(s[1]).wraps; w != a.Name {
				t.Errorf("%s: the CLI names %s, which wraps %q", a.Name, s[1], w)
			}
		}
		want := "`" + strings.ReplaceAll(a.Name, "-", "_") + "`"
		if a.Owner {
			want = "none, the owner's"
		}
		if r[4] != want {
			t.Errorf("%s: the MCP tool is %s, want %s", a.Name, r[4], want)
		}
		if strings.TrimSpace(r[5]) == "" {
			t.Errorf("%s: the table names no place in the browser", a.Name)
		}
	}
	for _, c := range commands {
		if c.wraps != "" && shown[c.name] != c.wraps {
			t.Errorf("%s wraps %s, and the row of %s does not name it", c.name, c.wraps, c.wraps)
		}
	}
}
