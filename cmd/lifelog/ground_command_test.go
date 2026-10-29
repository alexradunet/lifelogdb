package main

import (
	"strings"
	"testing"

	"lifelog/internal/db/dbtest"
)

// TestTheGroundOnTheCommandLine: `lifelog do settings ground=embers` is saved; a floor outside the four is refused,
// and the file keeps the old one.
func TestTheGroundOnTheCommandLine(t *testing.T) {
	live := dbtest.File(t)
	e, stdout, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"do", "settings", "ground=embers", "--db", live}); err != nil {
		t.Fatal(err)
	}
	err := runArgs(t.Context(), e, []string{"do", "settings", "ground=flagstones", "--db", live})
	if err == nil || !strings.Contains(err.Error(), "ground") {
		t.Fatalf("ground=flagstones: %v, want a refusal that names the field", err)
	}
	stdout.Reset()
	if err := runArgs(t.Context(), e, []string{"get", "/settings", "--db", live}); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); !strings.Contains(out, `"ground": "embers"`) {
		t.Errorf("lifelog get /settings: %s", out)
	}
}
