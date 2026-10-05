package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestVaultRewriteCode(t *testing.T) {
	p := &Plan{Notes: []Note{{Path: "B.md", Title: "Renamed"}}}
	const link = "[[B.md]]"
	const rewritten = "[[Renamed|B.md]]"
	for _, tc := range []struct{ name, code string }{
		{"four ticks", "````\n[[B.md]]\n```\n[[B.md]]\n````\n"},
		{"five tildes", "~~~~~\n[[B.md]]\n~~~~\n[[B.md]]\n~~~~~\n"},
		{"mismatched fences", "```\n~~~\n[[B.md]]\n```\n"},
		{"illegal closer", "```\n``` trailing [[B.md]]\n[[B.md]]\n```\n"},
		{"multiline span", "`first\n[[B.md]]\nlast`\n"},
		{"unequal runs", "``first ` [[B.md]] ``` [[B.md]] end``\n"},
		{"longer inner run", "`first `` [[B.md]] end`\n"},
		{"list indented", "- item\n\n      [[B.md]]\n"},
		{"indented", "    [[B.md]]\n    [[B.md]]\n"},
		{"tab indented", "\t[[B.md]]\n"},
		{"CRLF", "````\r\n[[B.md]]\r\n```\r\n[[B.md]]\r\n````\r\n"},
		{"fence info", "```example [[B.md]]\n[[B.md]]\n```\n"},
		{"quoted fence", "> ````\n> [[B.md]]\n> ```\n> [[B.md]]\n> ````\n"},
		{"unterminated", "````\n[[B.md]]\n```\n[[B.md]]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := link + "\n\n" + tc.code
			want := rewritten + "\n\n" + tc.code
			if tc.name != "unterminated" {
				src += "\n" + link
				want += "\n" + rewritten
			}
			for i := 0; i < 2; i++ {
				if got := rewriteLinks(src, &p.Notes[0], linkIndex(p)); got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
	t.Run("unmatched ticks are prose", func(t *testing.T) {
		src := "` [[B.md]]\n"
		if got := rewriteLinks(src, &p.Notes[0], linkIndex(p)); got != "` "+rewritten+"\n" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestVaultRewriteCodeReplay(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	const source = "Rewrite.md"
	const original = "---\naliases: [Example]\n---\nBefore [[Notes/Target.md|alias]] ![[photo.png]]\r\n\r\n````\r\n[[Notes/Target.md|alias]]\r\n```\r\n[[Notes/Target.md]]\r\n````\r\n\r\n`first\n[[Notes/Target.md]]\nlast`\n\nAfter [[Notes/Target.md]]\n"
	const want = "---\naliases: [Example]\n---\nBefore [[Renamed|alias]] `![[photo.png]]`\r\n\r\n````\r\n[[Notes/Target.md|alias]]\r\n```\r\n[[Notes/Target.md]]\r\n````\r\n\r\n`first\n[[Notes/Target.md]]\nlast`\n\nAfter [[Renamed|Notes/Target.md]]\n"
	for name, body := range map[string]string{source: original, "Notes/Target.md": "Synthetic target."} {
		p, err := f.w.SourcePath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := &Plan{Notes: []Note{{Path: source, Title: "Rewrite", Action: "create"}, {Path: "Notes/Target.md", Title: "Renamed", Action: "create"}}}
	if err := writeJSON(f.w.planPath(), p); err != nil {
		t.Fatal(err)
	}
	check := func(s *core.Store) {
		t.Helper()
		id, err := s.PageID(ctx, "Rewrite")
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
		var titles []string
		for _, e := range page.Out {
			if e.Kind == "wikilink" {
				titles = append(titles, e.Title)
			}
		}
		if strings.Join(titles, ",") != "Renamed" {
			t.Fatalf("links = %v", titles)
		}
		got, err := f.w.ReadSource(source)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != original {
			t.Fatalf("source mutated: %q", got)
		}
	}
	for i := 0; i < 2; i++ {
		res, err := f.w.ApplyVault(ctx, f.s)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && (res.Created != 0 || res.Saved != 0 || res.Appended != 0) {
			t.Fatalf("second apply: %+v", res)
		}
		check(f.s)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	for i := 0; i < 2; i++ {
		res, err := f.w.Replay(ctx, f.s, target)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Failures) != 0 || !res.Integrity.OK {
			t.Fatalf("replay: %+v", res)
		}
		if i == 1 && (res.Vault.Created != 0 || res.Vault.Saved != 0 || res.Vault.Appended != 0) {
			t.Fatalf("second replay: %+v", res)
		}
		d, err := db.Open(target)
		if err != nil {
			t.Fatal(err)
		}
		check(&core.Store{DB: d})
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
