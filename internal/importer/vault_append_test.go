package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestVaultAppendBoundary(t *testing.T) {
	const note = "Met [[Synthetic friend]]."
	for _, tc := range []struct {
		name, old, incoming string
		append              bool
	}{
		{"interior", "Before " + note + " after.", note, true},
		{"partial final line", "Before " + note, note, true},
		{"single newline", "Before\n" + note, note, true},
		{"entire body", note, note, false},
		{"complete suffix", "Before\n\n" + note, note, false},
		{"trailing newlines", "Before\n\n" + note + "\n\n", note + "\n\n", false},
		{"missing source newline", "Before\n\n" + note, note + "\n", true},
		{"extra body newline", "Before\n\n" + note + "\n", note, true},
		{"empty", "Before", "", false},
		{"whitespace", "Before", " \n\t", false},
		{"empty existing", "", note, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.approveRules(t, rulesBody)
			const title = "2031-04-12"
			const source = "Journal/2031-04-12.md"
			path, err := f.w.SourcePath(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.incoming), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.s.CreatePage(ctx, "cli", title, tc.old); err != nil {
				t.Fatal(err)
			}
			p := &Plan{Notes: []Note{{Path: source, Title: title, Day: title, Action: "append"}}}
			if err := writeJSON(f.w.planPath(), p); err != nil {
				t.Fatal(err)
			}
			want := tc.old
			if tc.append {
				if want != "" {
					want += "\n\n"
				}
				want += tc.incoming
			}
			check := func(s *core.Store) {
				t.Helper()
				id, err := s.PageID(ctx, title)
				if err != nil {
					t.Fatal(err)
				}
				page, err := s.PageByID(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if page.Body != want {
					t.Fatalf("body = %q, want %q", page.Body, want)
				}
				links := 0
				for _, e := range page.Out {
					if e.Kind == "wikilink" {
						links++
					}
				}
				expected := 0
				if strings.Contains(want, "[[Synthetic friend]]") {
					expected = 1
				}
				if links != expected {
					t.Fatalf("links = %d, want %d", links, expected)
				}
			}
			res, err := f.w.ApplyVault(ctx, f.s)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if tc.append {
				expected = 1
			}
			if res.Appended != expected {
				t.Fatalf("appended = %d, want %d", res.Appended, expected)
			}
			check(f.s)
			// Lose the receipt, then recover it from the exact capture result.
			p, _, err = f.w.LoadPlan()
			if err != nil {
				t.Fatal(err)
			}
			if !p.Notes[0].Appended {
				t.Fatal("missing append receipt")
			}
			p.Notes[0].Appended = false
			if err := writeJSON(f.w.planPath(), p); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				res, err = f.w.ApplyVault(ctx, f.s)
				if err != nil {
					t.Fatal(err)
				}
				if res.Appended != 0 || res.Same != 1 {
					t.Fatalf("retry: %+v", res)
				}
				check(f.s)
			}
			p, _, err = f.w.LoadPlan()
			if err != nil || !p.Notes[0].Appended {
				t.Fatalf("recovered receipt: %+v, %v", p, err)
			}
			// Replay into a fresh target with the same original journal prose, not the trial receipt.
			target := filepath.Join(t.TempDir(), "life.db")
			if err := db.Init(target); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(target)
			if err != nil {
				t.Fatal(err)
			}
			ts := &core.Store{DB: d}
			if _, _, err := ts.CreatePage(ctx, "cli", title, tc.old); err != nil {
				t.Fatal(err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				replay, err := f.w.Replay(ctx, f.s, target)
				if err != nil {
					t.Fatal(err)
				}
				n := expected
				if i == 1 {
					n = 0
				}
				if replay.Vault.Appended != n || !replay.Integrity.OK {
					t.Fatalf("replay: %+v", replay)
				}
				d, err := db.Open(target)
				if err != nil {
					t.Fatal(err)
				}
				check(&core.Store{DB: d})
				d.Close()
			}
			raw, err := f.w.ReadSource(source)
			if err != nil || raw != tc.incoming {
				t.Fatalf("source changed: %q, %v", raw, err)
			}
			p, _, err = f.w.LoadPlan()
			if err != nil || !p.Notes[0].Appended {
				t.Fatalf("replay changed receipt: %+v, %v", p, err)
			}
		})
	}
}
