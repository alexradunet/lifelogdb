package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSourceConfinement(t *testing.T) {
	for _, kind := range []string{"file", "directory", "nfc", "broken", "inside"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "source")
			writeSourceFile(t, src, "safe/Note.md", "inside")
			outside := writeSourceFile(t, base, "outside/Note.md", "OUTSIDE MARKER")
			target, link, rel := outside, "Link.md", "Link.md"
			switch kind {
			case "directory":
				target, link, rel = filepath.Dir(outside), "Link", "Link/Note.md"
			case "nfc":
				target, link, rel = filepath.Dir(outside), "Cafe\u0301", "Caf\u00e9/Note.md"
			case "broken":
				target = filepath.Join(base, "missing")
			case "inside":
				target = filepath.Join("safe", "Note.md")
			}
			if err := os.Symlink(target, filepath.Join(src, link)); err != nil {
				t.Skipf("%s link unavailable: %v", kind, err)
			}
			w, err := Open(src + ".lifelog")
			if err != nil {
				t.Fatal(err)
			}
			body, err := w.ReadSource(rel)
			if kind == "inside" {
				if err != nil || body != "inside" {
					t.Fatalf("inside read = %q, %v", body, err)
				}
				return
			}
			if err == nil || body != "" {
				t.Fatalf("escaping/broken read = %q, %v", body, err)
			}
			if kind != "broken" {
				if _, err := w.SourcePath(rel); err == nil {
					t.Fatal("SourcePath attested escape")
				}
			}
			if _, err := w.MakeLedger(); err == nil {
				t.Fatal("inventory accepted unsafe link")
			}
			if exists(w.file("ledger.md")) || exists(w.planPath()) {
				t.Fatal("refusal wrote artifacts")
			}
		})
	}
}

func TestSourceConfinementJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction case")
	}
	base := t.TempDir()
	src := filepath.Join(base, "source")
	writeSourceFile(t, src, "safe.md", "inside")
	outside := filepath.Dir(writeSourceFile(t, base, "outside/Note.md", "OUTSIDE MARKER"))
	link := filepath.Join(src, "Link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v: %s", err, output)
	}
	t.Cleanup(func() { os.Remove(link) })
	w, store := setupSourcePathWorkspace(t, src)
	if body, err := w.ReadSource("Link/Note.md"); err == nil || body != "" {
		t.Fatalf("junction read = %q, %v", body, err)
	}
	if _, err := w.SourcePath("Link/Note.md"); err == nil {
		t.Fatal("attested junction escape")
	}
	if _, err := w.MakeLedger(); err == nil {
		t.Fatal("inventory accepted junction")
	}
	if _, err := w.PlanVault(ctx, store); err == nil {
		t.Fatal("plan accepted junction")
	}
	if exists(w.file("ledger.md")) || exists(w.planPath()) {
		t.Fatal("refusal wrote artifacts")
	}
	if err := w.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	p := &Plan{Notes: []Note{{Path: "Link/Note.md", Title: "Outside", Action: "create"}}}
	if _, err := w.applyPlan(ctx, store, p, true); err == nil {
		t.Fatal("apply accepted junction")
	}
	var n int
	if err := store.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM entity_names WHERE title = 'Outside'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("refused apply created page")
	}
}
