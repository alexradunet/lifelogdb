package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/db/dbtest"
)

// TestSettingsOnTheCommandLine: `lifelog do settings` writes the file beside the database (NAME.db gives
// NAME.settings.json) and `lifelog get /settings` reads it back.
func TestSettingsOnTheCommandLine(t *testing.T) {
	live := dbtest.File(t)
	e, stdout, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"do", "settings", "model_url=http://127.0.0.1:1234/v1", "model=synthetic-model", "--db", live}); err != nil {
		t.Fatal(err)
	}
	file := strings.TrimSuffix(live, ".db") + ".settings.json"
	if filepath.Dir(file) != filepath.Dir(live) {
		t.Fatalf("the file %s is not beside %s", file, live)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), "synthetic-model") {
		t.Fatalf("the settings file: %q %v", b, err)
	}
	stdout.Reset()
	if err := runArgs(t.Context(), e, []string{"get", "/settings", "--db", live}); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); !strings.Contains(out, "http://127.0.0.1:1234/v1") || !strings.Contains(out, "synthetic-model") {
		t.Errorf("lifelog get /settings: %s", out)
	}
}
