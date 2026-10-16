package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lifelog init PATH makes the file at PATH, whatever $LIFELOG_DB names; --db beats PATH, and both together are a
// mistake, refused before anything is written (docs/plans/098-the-codebase-made-cohesive.md).
func TestInitHonoursThePathWhenTheEnvironmentNamesAnother(t *testing.T) {
	env := filepath.Join(t.TempDir(), "env.db")
	e, _, _ := testEnv(t)
	e.getenv = func(name string) string { return map[string]string{"LIFELOG_DB": env}[name] }
	given := filepath.Join(t.TempDir(), "given.db")
	if err := runArgs(t.Context(), e, []string{"init", given}); err != nil {
		t.Fatalf("init PATH: %v", err)
	}
	if _, err := os.Stat(given); err != nil {
		t.Errorf("init PATH did not make %s: %v", given, err)
	}
	if _, err := os.Stat(env); err == nil {
		t.Errorf("init PATH made the file $LIFELOG_DB names, %s", env)
	}

	other := filepath.Join(t.TempDir(), "other.db")
	if err := runArgs(t.Context(), e, []string{"init", filepath.Join(t.TempDir(), "unused.db"), "--db", other}); err == nil || !strings.Contains(err.Error(), "--db") {
		t.Errorf("init PATH --db OTHER is refused: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Errorf("a refused init made %s", other)
	}
}
