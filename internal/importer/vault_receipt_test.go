package importer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func draftReceiptFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	f.approveRules(t, rulesBody)
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Recipes", "2031-04-10"); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestVaultReceiptDraftReplay(t *testing.T) {
	f := draftReceiptFixture(t)
	planBefore, err := os.ReadFile(f.w.planPath())
	if err != nil {
		t.Fatal(err)
	}
	countsBefore, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "fresh.db")
	r, err := f.w.Rehearse(ctx, f.s, fresh)
	if err != nil || len(r.Failures) != 0 || !r.Integrity.OK {
		t.Fatalf("draft rehearsal: %+v, %v", r, err)
	}
	if exists(fresh) || exists(f.w.vaultReceiptPath()) {
		t.Fatal("draft rehearsal created a target or receipt")
	}
	after, err := os.ReadFile(f.w.planPath())
	if err != nil {
		t.Fatal(err)
	}
	countsAfter, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(planBefore, after) || !reflect.DeepEqual(countsBefore, countsAfter) {
		t.Fatal("draft rehearsal changed plan or trial")
	}
	existing := filepath.Join(t.TempDir(), "existing.db")
	if err := db.Init(existing); err != nil {
		t.Fatal(err)
	}
	before := fileSum(t, existing)
	for _, target := range []string{fresh, existing} {
		if _, err := f.w.Replay(ctx, f.s, target); err == nil || !strings.Contains(err.Error(), "apply the vault plan to the trial first") {
			t.Fatalf("draft replay: %v", err)
		}
	}
	if exists(fresh) || fileSum(t, existing) != before || exists(f.w.vaultReceiptPath()) {
		t.Fatal("draft replay mutated target/evidence")
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{fresh, existing} {
		if _, err := f.w.Replay(ctx, f.s, target); err != nil {
			t.Fatal(err)
		}
	}
	noRehearsalLeft(t, f.w)
}

func TestVaultReceiptFailures(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-source", "wrong-title", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			f := draftReceiptFixture(t)
			if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
				t.Fatal(err)
			}
			r, err := f.w.loadVaultReceipt()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				if err := os.Remove(f.w.vaultReceiptPath()); err != nil {
					t.Fatal(err)
				}
			case "wrong-source":
				r.Source = "import:other"
				if err := writeJSON(f.w.vaultReceiptPath(), r); err != nil {
					t.Fatal(err)
				}
			case "wrong-title":
				n := r.Notes["Recipes.md"]
				n.Title = "recipes"
				r.Notes["Recipes.md"] = n
				if err := writeJSON(f.w.vaultReceiptPath(), r); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(f.w.vaultReceiptPath(), []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := os.ReadFile(f.w.planPath())
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "fresh.db")
			if _, err := f.w.ApplyVault(ctx, f.s); err == nil {
				t.Fatal("accepted bad evidence on apply")
			}
			if _, err := f.w.Rehearse(ctx, f.s, target); err == nil {
				t.Fatal("accepted bad evidence on rehearsal")
			}
			if _, err := f.w.Replay(ctx, f.s, target); err == nil {
				t.Fatal("accepted bad evidence on replay")
			}
			after, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(f.w.planPath())
			if err != nil {
				t.Fatal(err)
			}
			if exists(target) || !reflect.DeepEqual(before, after) || !bytes.Equal(plan, got) {
				t.Fatal("bad evidence refusal had side effects")
			}
			noRehearsalLeft(t, f.w)
		})
	}
}

func TestVaultReceiptPreparationRetry(t *testing.T) {
	t.Run("file failure precedes entities", func(t *testing.T) {
		f := draftReceiptFixture(t)
		before, err := f.s.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(f.w.vaultReceiptPath(), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err == nil {
			t.Fatal("ignored receipt file failure")
		}
		after, err := f.s.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("receipt failure wrote entities")
		}
		if err := os.Remove(f.w.vaultReceiptPath()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("prepared create is not a commit", func(t *testing.T) {
		f := draftReceiptFixture(t)
		p, ok, err := f.w.LoadPlan()
		if err != nil || !ok {
			t.Fatalf("plan: %v", err)
		}
		source, err := f.w.Name()
		if err != nil {
			t.Fatal(err)
		}
		// Deliberate interruption boundary: durable preparation, rolled-back DB.
		if err := f.s.DryRun(ctx, source, func(tx *core.Tx) error { return f.w.prepareVaultReceipt(tx, p) }); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Recipe draft", "2031-04-10"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		r, err := f.w.loadVaultReceipt()
		if err != nil {
			t.Fatal(err)
		}
		if r.Notes["Recipes.md"].Title != "Recipe draft" {
			t.Fatal("retry retained uncommitted draft identity")
		}
	})
	t.Run("partial application retains bindings", func(t *testing.T) {
		f := draftReceiptFixture(t)
		// The pages are created, text included, in the first transaction; the second pass syncs the links of each note and
		// appends a daily note to its existing day page. The append is where this pass is made to fail.
		if _, _, err := f.s.Capture(ctx, "cli", "2031-04-12", "Written on the day.", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.DB.W.Exec(`CREATE TRIGGER injected_body_failure BEFORE UPDATE OF body ON entities WHEN OLD.preferred_name_key='2031-04-12' AND NEW.body<>OLD.body BEGIN SELECT RAISE(ABORT,'synthetic body failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err == nil {
			t.Fatal("body fault did not fail application")
		}
		evidence, err := os.ReadFile(f.w.vaultReceiptPath())
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "partial-target.db")
		if _, err := f.w.Replay(ctx, f.s, target); err == nil {
			t.Fatal("partial body pass authorized replay")
		}
		if exists(target) {
			t.Fatal("partial body pass created target")
		}
		id, err := f.s.PageID(ctx, "Recipes")
		if err != nil || id == 0 {
			t.Fatalf("first transaction did not bind note: %d, %v", id, err)
		}
		if _, err := f.s.Rename(ctx, "cli", id, "Cookery"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Cookery", "2031-04-10"); err == nil {
			t.Fatal("partial application lost original plan identity")
		}
		if _, err := f.s.DB.W.Exec("DROP TRIGGER injected_body_failure"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(f.w.vaultReceiptPath())
		if err != nil {
			t.Fatal(err)
		}
		var prepared, completed vaultIdentityReceipt
		if err := json.Unmarshal(evidence, &prepared); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(after, &completed); err != nil {
			t.Fatal(err)
		}
		for path, original := range prepared.Notes {
			got := completed.Notes[path]
			if original.Applied || !got.Applied || original.Title != got.Title || original.Day != got.Day {
				t.Fatal("retry replaced original binding or failed to complete it")
			}
		}
		page, err := f.s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if page.Title != "Cookery" || page.Body == "" {
			t.Fatal("retry lost owner rename or failed to save source body")
		}
	})
	t.Run("mixed applied and draft", func(t *testing.T) {
		f := draftReceiptFixture(t)
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		writeSource(t, f, "New note.md", "Synthetic extra note\n")
		p, ok, err := f.w.LoadPlan()
		if err != nil || !ok {
			t.Fatalf("plan: %v", err)
		}
		p.Notes = append(p.Notes, Note{Path: "New note.md", Title: "New note", Action: "create"})
		if err := writeJSON(f.w.planPath(), p); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "fresh.db")
		if _, err := f.w.Replay(ctx, f.s, target); err == nil {
			t.Fatal("mixed plan replayed unbound note")
		}
		if exists(target) {
			t.Fatal("mixed plan created target")
		}
	})
}
