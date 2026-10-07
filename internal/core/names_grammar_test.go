package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
	"lifelog/internal/text"
)

func TestReferenceNameContractProductionPipeline(t *testing.T) {
	data, err := os.ReadFile("../../docs/contract/titles-and-wikilinks.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "<!-- reference-name-vectors -->")
	if !ok {
		t.Fatal("missing name vectors")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		t.Fatal("missing name JSON block")
	}
	encoded, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("unterminated name vectors")
	}
	var vectors []struct {
		Name, Key string
		Accepted  bool
	}
	if err := json.Unmarshal([]byte(encoded), &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 30 {
		t.Fatalf("loaded %d name vectors; want 30", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			if got := text.ValidTitle(v.Name); got != v.Accepted {
				t.Fatalf("predicate %v want %v", got, v.Accepted)
			}
			s := fresh(t)
			id, _, err := s.CreatePage(ctx, "cli", v.Name, "")
			if !v.Accepted {
				if status(err) != 422 || id != 0 {
					t.Fatalf("rejected create=%d %v", id, err)
				}
				if got, err := s.PageID(ctx, v.Name); err != nil || got != 0 {
					t.Fatalf("rejected name persisted=%d %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if text.TitleKey(v.Name) != v.Key {
				t.Fatalf("key=%q want %q", text.TitleKey(v.Name), v.Key)
			}
			source, _, err := s.CreatePage(ctx, "cli", "Vector source", "")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{v.Name, norm.NFC.String(v.Name)} {
				for _, body := range []string{"See [[" + name + "]].", "See ![[" + name + "]].", "See [[" + name + "|display]]."} {
					p, err := s.PageByID(ctx, source)
					if err != nil {
						t.Fatal(err)
					}
					r, err := s.SaveBody(ctx, "cli", source, body, p.Version)
					if err != nil || len(r.Linked) != 1 {
						t.Fatalf("save %q: %+v %v", body, r, err)
					}
					p, err = s.PageByID(ctx, source)
					if err != nil || p.Body != body || len(p.Out) != 1 || p.Out[0].ID != id {
						t.Fatalf("stored %q: %+v %v", body, p, err)
					}
					token := p.Version
					if _, err := s.SaveBody(ctx, "cli", source, body, token); err != nil {
						t.Fatal(err)
					}
					again, err := s.PageByID(ctx, source)
					if err != nil || again.Version != token {
						t.Fatalf("re-save token: %+v %v", again, err)
					}
					if got, err := s.PageID(ctx, name); err != nil || got != id {
						t.Fatalf("resolve %q: %d %v", name, got, err)
					}
				}
			}
			if _, err := s.Rename(ctx, "cli", id, "New vector name"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Rename(ctx, "cli", id, v.Name); err != nil {
				t.Fatal(err)
			}
			if err := s.Promote(ctx, "cli", id, "place", ""); err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveBody(ctx, "cli", source, p.Body, p.Version); err != nil {
				t.Fatal(err)
			}
			p, err = s.PageByID(ctx, source)
			if err != nil || len(p.Out) != 1 || p.Out[0].ID != id || p.Out[0].Type != "place" {
				t.Fatalf("promoted alias: %+v %v", p, err)
			}
		})
	}
	// Explicit contextual limitations, not stronger arbitrary compositional safety.
	for _, body := range []string{"See [[a`b]] and [[c`d]].", "[[Ref]]\n\n[Ref]: https://example.invalid", "[[Health *Diet*]]"} {
		_, got, _ := text.Targets(body, "")
		if len(got) != 0 {
			t.Fatalf("context limit %q: %q", body, got)
		}
	}
}
