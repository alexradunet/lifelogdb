package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lifelog init PATH makes the file at PATH, whatever $LIFELOG_DB names; --db beats PATH, and both together are a
// mistake, refused before anything is written (docs/plans/098-the-codebase-made-cohesive.md).
func TestInitHonoursThePathWhenTheEnvironmentNamesAnother(t *testing.T) {
	env := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("LIFELOG_DB", env)
	given := filepath.Join(t.TempDir(), "given.db")
	o, err := parse([]string{"init", given})
	if err != nil {
		t.Fatal(err)
	}
	if err := runContext(context.Background(), o); err != nil {
		t.Fatalf("init PATH: %v", err)
	}
	if _, err := os.Stat(given); err != nil {
		t.Errorf("init PATH did not make %s: %v", given, err)
	}
	if _, err := os.Stat(env); err == nil {
		t.Errorf("init PATH made the file $LIFELOG_DB names, %s", env)
	}

	other := filepath.Join(t.TempDir(), "other.db")
	o, err = parse([]string{"init", filepath.Join(t.TempDir(), "unused.db"), "--db", other})
	if err != nil {
		t.Fatal(err)
	}
	if err := runContext(context.Background(), o); err == nil || !strings.Contains(err.Error(), "--db") {
		t.Errorf("init PATH --db OTHER is refused: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Errorf("a refused init made %s", other)
	}
}

// A flag a command does not take is refused, never ignored: --dry-run belongs to file, --public to serve. The usage
// names every command of the table.
func TestAFlagACommandDoesNotTakeIsRefused(t *testing.T) {
	for _, argv := range [][]string{{"search", "x", "--dry-run"}, {"day", "--public"}, {"search", "x", "--day", "2026-01-01"}, {"capture", "hello", "--to", "x"}} {
		if _, err := parse(argv); err == nil || !strings.Contains(err.Error(), "does not take") {
			t.Errorf("%v: %v, want a refusal of the flag", argv, err)
		}
	}
	for _, argv := range [][]string{{"file", "x", "--dry-run"}, {"serve", "--public"}, {"capture", "hello", "--day", "2026-01-01"}, {"due", "--to", "2026-02-01"}, {"snapshot", "--to", "x"}} {
		if _, err := parse(argv); err != nil {
			t.Errorf("%v: %v, want the flag taken", argv, err)
		}
	}
	usage := usageText()
	for _, c := range commands {
		if c.usage != "" && !strings.Contains(usage, "lifelog "+c.name) {
			t.Errorf("the usage does not name %s", c.name)
		}
	}
}
