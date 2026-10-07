package importer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppliedVaultPlanIdentity(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	_, err := f.w.PlanVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	// Draft identity remains editable.
	if _, err = f.w.FixPlan(ctx, f.s, "Recipes.md", "Recipes", "2031-04-10"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	p, _, err := f.w.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.w.planPath())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		rows, err := f.s.DB.R.Query(`SELECT e.id, n.title, coalesce(e.day, '<null>'), e.body, e.source, e.import_key FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key ORDER BY e.id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var b strings.Builder
		for rows.Next() {
			var id int64
			var title, day, body, source string
			var key *string
			if err := rows.Scan(&id, &title, &day, &body, &source, &key); err != nil {
				t.Fatal(err)
			}
			b.WriteString(title + day + body + source)
			if key != nil {
				b.WriteString(*key)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		links, err := f.s.DB.R.Query(`SELECT from_id, to_id, kind FROM links ORDER BY from_id,to_id,kind`)
		if err != nil {
			t.Fatal(err)
		}
		defer links.Close()
		for links.Next() {
			var from, to int64
			var kind string
			if err := links.Scan(&from, &to, &kind); err != nil {
				t.Fatal(err)
			}
			b.WriteString(string(rune(from)) + string(rune(to)) + kind)
		}
		return b.String()
	}
	original := snapshot()
	for _, change := range [][2]string{{"New recipes", ""}, {"recipes", ""}, {"", "2031-04-09"}} {
		if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", change[0], change[1]); err == nil {
			t.Errorf("accepted identity edit %v", change)
		}
		got, _ := os.ReadFile(f.w.planPath())
		if !bytes.Equal(got, before) || snapshot() != original {
			t.Fatal("refusal mutated plan or trial")
		}
	}
	if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Recipes", "2031-04-10"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	if _, err := f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	for i := range p.Notes {
		if p.Notes[i].Path == "Recipes.md" {
			p.Notes[i].Title = "Manual rename"
		}
	}
	if err := writeJSON(f.w.planPath(), p); err != nil {
		t.Fatal(err)
	}
	edited, _ := os.ReadFile(f.w.planPath())
	if _, err := f.w.ApplyVault(ctx, f.s); err == nil {
		t.Fatal("accepted manual edit")
	}
	fresh := filepath.Join(t.TempDir(), "fresh.db")
	sum := fileSum(t, target)
	for _, dst := range []string{target, fresh} {
		if _, err := f.w.Rehearse(ctx, f.s, dst); err == nil {
			t.Fatal("rehearsed inconsistent trial")
		}
		if _, err := f.w.Replay(ctx, f.s, dst); err == nil {
			t.Fatal("replayed inconsistent trial")
		}
	}
	if exists(fresh) || fileSum(t, target) != sum || snapshot() != original {
		t.Fatal("conflict mutated database")
	}
	got, _ := os.ReadFile(f.w.planPath())
	if !bytes.Equal(got, edited) {
		t.Fatal("conflict rewrote plan")
	}
	noRehearsalLeft(t, f.w)
}

func TestAppliedVaultPlanIdentityAppended(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, _, err := f.s.Capture(ctx, "cli", "2031-04-12", "Existing", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.w.planPath())
	if _, err := f.w.FixPlan(ctx, f.s, "Journal/2031-04-12.md", "2031-04-13", "2031-04-13"); err == nil {
		t.Fatal("changed appended note")
	}
	after, _ := os.ReadFile(f.w.planPath())
	if !bytes.Equal(before, after) {
		t.Fatal("changed plan")
	}
}
