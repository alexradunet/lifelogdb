package main

import (
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// `lifelog readings PAGE METRIC` is the shortcut of readings-from-table: it writes the table's readings once, and a
// second run writes nothing.
func TestReadingsCommand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	c := client.InProcess(api.New(&core.Store{DB: d}), "cli")
	root := mustEntity(c.Get("/"))
	if _, err := c.Do(cmdAction(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(cmdAction(t, root, "create-page"), map[string]string{"title": "Weighings", "body": "| day | value |\n|---|---|\n| 2031-01-01 | 70.5 kg |\n| 2031-01-08 | 70 kg |\n"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"written": 2`, `"existing": 2`} {
		o, err := parse([]string{"--db", p, "readings", "Weighings", "Weight"})
		if err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := run(o); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, want) {
			t.Errorf("want %s in:\n%s", want, out)
		}
	}
	if o, err := parse([]string{"--db", p, "readings", "Weighings"}); err != nil {
		t.Fatal(err)
	} else if err := run(o); err == nil || !strings.Contains(err.Error(), "readings PAGE METRIC") {
		t.Errorf("one argument: %v", err)
	}
}
