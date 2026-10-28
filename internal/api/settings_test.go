package api_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// TestSettingsAreTheOwnersFile: the owner saves the model and reads it back; the file is made with the mode 0600
// through a temporary file (none is left); a field left out stays and an empty one clears; GET /actions fills no value,
// whatever the file holds.
func TestSettingsAreTheOwnersFile(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	read := func() map[string]any {
		t.Helper()
		p, _ := apitest.Must(t)(c.Get(t.Context(), "/settings")).Properties.(map[string]any)
		return p
	}
	if p := read(); p["model_url"] != "" || p["model"] != "" || p["model_key"] != nil {
		t.Errorf("the settings with no file: %v, want empty and no key", p)
	}
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	settings := apitest.Action(t, catalog, "settings")
	if !settings.Owner || settings.Method != "POST" || settings.Href != "/settings" || settings.Field("model_key").Type != "password" {
		t.Fatalf("the action settings: %+v", settings)
	}
	e, err := c.Do(t.Context(), settings, map[string]string{"model_url": " http://127.0.0.1:1234/v1 ", "model": "synthetic-model", "model_key": "synthetic-key"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Result != "Settings saved." {
		t.Errorf("the result: %v", e.Result)
	}
	if want := filepath.Join(filepath.Dir(s.DB.Path()), strings.TrimSuffix(filepath.Base(s.DB.Path()), ".db")+".settings.json"); s.SettingsPath != want {
		t.Errorf("the settings path %q, want %q", s.SettingsPath, want)
	}
	if fi, err := os.Stat(s.SettingsPath); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("the mode %v, want 0600", fi.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.SettingsPath), ".settings-*")); len(left) != 0 {
		t.Errorf("temporary files are left: %v", left)
	}
	if p := read(); p["model_url"] != "http://127.0.0.1:1234/v1" || p["model"] != "synthetic-model" || p["model_key"] != "synthetic-key" {
		t.Errorf("GET /settings: %v", p)
	}
	// The owner's form carries the values, the key too; the catalog carries none.
	form := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/settings")), "settings")
	if form.Field("model_key").Value != "synthetic-key" || form.Field("model").Value != "synthetic-model" {
		t.Errorf("the form of GET /settings: %+v", form.Fields)
	}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		for _, f := range a.Fields {
			if a.Name == "settings" && (f.Value != nil && f.Value != "") {
				t.Errorf("GET /actions fills %s with %v", f.Name, f.Value)
			}
		}
	}
	if _, err := c.Do(t.Context(), settings, map[string]string{"model": "other"}); err != nil {
		t.Fatal(err)
	}
	if p := read(); p["model"] != "other" || p["model_key"] != "synthetic-key" || p["model_url"] != "http://127.0.0.1:1234/v1" {
		t.Errorf("after a save of one field: %v", p)
	}
	if _, err := c.Do(t.Context(), settings, map[string]string{"model_key": ""}); err != nil {
		t.Fatal(err)
	}
	if p := read(); p["model_key"] != nil || p["model"] != "other" {
		t.Errorf("after an empty key: %v", p)
	}
	b, err := os.ReadFile(s.SettingsPath)
	if err != nil || strings.Contains(string(b), "model_key") {
		t.Errorf("the file after the key was cleared: %q %v", b, err)
	}
}

// TestSettingsAreRefusedToAnAgentAndAnImport: both routes answer 403 forbidden, and a refused save writes nothing.
func TestSettingsAreRefusedToAnAgentAndAnImport(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	catalog := apitest.Must(t)(client.InProcess(h, "cli").Get(t.Context(), "/actions"))
	for _, source := range []string{"agent:x", "import:x"} {
		c := client.InProcess(h, source)
		if _, err := c.Get(t.Context(), "/settings"); apitest.Status(err) != 403 {
			t.Errorf("%s GET /settings: %v, want 403", source, err)
		}
		if _, err := c.Do(t.Context(), apitest.Action(t, catalog, "settings"), map[string]string{"model": "x"}); apitest.Status(err) != 403 {
			t.Errorf("%s POST /settings: %v, want 403", source, err)
		}
	}
	if _, err := os.Stat(s.SettingsPath); !os.IsNotExist(err) {
		t.Errorf("a refused save wrote the file: %v", err)
	}
	// The catalog is readable by an agent, and carries no value.
	for _, a := range apitest.Must(t)(client.InProcess(h, "agent:x").Get(t.Context(), "/actions")).Actions {
		if a.Name == "settings" && a.Field("model_key").Value != nil {
			t.Errorf("an agent reads the key in the catalog: %v", a.Field("model_key").Value)
		}
	}
}

// TestAStoreWithNoSettingsPathRefusesBoth: a read and a write say that the store has no settings file.
func TestAStoreWithNoSettingsPathRefusesBoth(t *testing.T) {
	h := api.New(&core.Store{DB: dbtest.New(t)})
	c := client.InProcess(h, "cli")
	_, err := c.Get(t.Context(), "/settings")
	if apitest.Status(err) != 500 || !strings.Contains(err.Error(), "no settings file") {
		t.Errorf("GET /settings: %v, want an error that says there is no settings file", err)
	}
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	_, err = c.Do(t.Context(), apitest.Action(t, catalog, "settings"), map[string]string{"model": "x"})
	if apitest.Status(err) != 500 || !strings.Contains(err.Error(), "no settings file") {
		t.Errorf("POST /settings: %v, want an error that says there is no settings file", err)
	}
}

// TestSettingsPathFor: the path of the database with .db cut and .settings.json added.
func TestSettingsPathFor(t *testing.T) {
	for in, want := range map[string]string{"/a/life.db": "/a/life.settings.json", "/a/other": "/a/other.settings.json", "life.db": "life.settings.json"} {
		if got := core.SettingsPathFor(in); got != want {
			t.Errorf("SettingsPathFor(%q) = %q, want %q", in, got, want)
		}
	}
}
