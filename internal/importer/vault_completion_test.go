package importer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/db"
)

func TestVaultCompletionPublicationFailures(t *testing.T) {
	for _, failure := range []string{"replace", "interruption", "plan persistence"} {
		t.Run(failure, func(t *testing.T) {
			f := draftReceiptFixture(t)
			if failure != "plan persistence" {
				if _, _, err := f.s.Capture(ctx, "cli", "2031-04-12", "Existing journal", nil); err != nil {
					t.Fatal(err)
				}
			}
			p, ok, err := f.w.LoadPlan()
			if err != nil || !ok {
				t.Fatalf("plan: %v", err)
			}
			if err := f.w.validatePlan(ctx, f.s, p); err != nil {
				t.Fatal(err)
			}
			var prepared []byte
			injected := errors.New("synthetic receipt replace failure")
			backup := f.w.planPath() + ".synthetic-backup"
			publish := func(path string, r *vaultIdentityReceipt) error {
				completed := false
				for _, n := range r.Notes {
					completed = completed || n.Applied
				}
				if completed {
					if failure == "interruption" {
						return context.Canceled
					}
					return injected
				}
				if err := publishVaultReceipt(path, r); err != nil {
					return err
				}
				var err error
				prepared, err = os.ReadFile(path)
				if err != nil {
					return err
				}
				if failure == "plan persistence" {
					if err := os.Rename(f.w.planPath(), backup); err != nil {
						return err
					}
					return os.Mkdir(f.w.planPath(), 0700)
				}
				return nil
			}
			// Same coordinator and actual SQLite writes; only completion storage is faulted.
			_, err = f.w.applyPlanPublishing(ctx, f.s, p, true, publish)
			if err == nil {
				t.Fatal("publication fault did not fail apply")
			}
			if failure == "replace" && !errors.Is(err, injected) {
				t.Fatalf("lost publication cause: %v", err)
			}
			if failure == "interruption" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			receipt, err := os.ReadFile(f.w.vaultReceiptPath())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(prepared, receipt) {
				t.Fatal("failed publication replaced previous receipt")
			}
			if failure == "plan persistence" {
				if err := os.Remove(f.w.planPath()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(backup, f.w.planPath()); err != nil {
					t.Fatal(err)
				}
			}
			// Identity and bodies have committed; their presence is not replay authorization.
			id, err := f.s.PageID(ctx, "Recipes")
			if err != nil {
				t.Fatal(err)
			}
			note, err := f.s.PageByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if note.Body == "" {
				t.Fatal("fault was not after body work")
			}
			fresh := filepath.Join(t.TempDir(), "fresh.db")
			existing := filepath.Join(t.TempDir(), "existing.db")
			if err := db.Init(existing); err != nil {
				t.Fatal(err)
			}
			sum := fileSum(t, existing)
			for _, target := range []string{fresh, existing} {
				if _, err := f.w.Replay(ctx, f.s, target); err == nil {
					t.Fatal("incomplete receipt authorized replay")
				}
			}
			if exists(fresh) || fileSum(t, existing) != sum {
				t.Fatal("incomplete replay changed target")
			}
			day, err := f.s.Day(ctx, "2031-04-12")
			if err != nil {
				t.Fatal(err)
			}
			journal, err := f.s.PageByID(ctx, day.PageID)
			if err != nil {
				t.Fatal(err)
			}
			body := journal.Body
			if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
				t.Fatal(err)
			}
			after, err := f.s.Day(ctx, "2031-04-12")
			if err != nil {
				t.Fatal(err)
			}
			journalAfter, err := f.s.PageByID(ctx, after.PageID)
			if err != nil {
				t.Fatal(err)
			}
			if journalAfter.Body != body {
				t.Fatal("metadata retry duplicated journal append")
			}
			r, err := f.w.loadVaultReceipt()
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range r.Notes {
				if !n.Applied {
					t.Fatal("retry did not finish completion metadata")
				}
			}
			if _, err := f.w.Replay(ctx, f.s, fresh); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVaultCompletionSurvivesOwnerProseAndRename(t *testing.T) {
	f := draftReceiptFixture(t)
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	id, err := f.s.PageID(ctx, "Recipes")
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SaveBody(ctx, "cli", id, "Owner's subsequent synthetic prose", page.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Rename(ctx, "cli", id, "Cookery"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "replay.db")
	if _, err := f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if owner.Body != "Owner's subsequent synthetic prose" || owner.Title != "Cookery" {
		t.Fatal("replay rewrote trial owner's later edits")
	}
}
