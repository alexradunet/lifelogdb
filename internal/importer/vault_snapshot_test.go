package importer

import (
	"path/filepath"
	"testing"
)

func TestVaultReplayUsesVerifiedPlanSnapshot(t *testing.T) {
	f := draftReceiptFixture(t)
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	verified, err := f.w.vaultReplayPlan(ctx, f.s, true)
	if err != nil {
		t.Fatal(err)
	}
	edited, ok, err := f.w.LoadPlan()
	if err != nil || !ok {
		t.Fatalf("plan: %v", err)
	}
	for i := range edited.Notes {
		if edited.Notes[i].Path == "Recipes.md" {
			edited.Notes[i].Title = "Unverified replacement"
		}
	}
	if err := writeJSON(f.w.planPath(), edited); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.db")
	if _, err := f.w.Rehearse(ctx, f.s, target); err == nil {
		t.Fatal("public rehearsal accepted edited current plan")
	}
	result, err := f.w.rehearsePlan(ctx, f.s, target, verified)
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("verified snapshot was replaced during rehearsal: %+v, %v", result, err)
	}
	if exists(target) {
		t.Fatal("snapshot rehearsal created real target")
	}
	noRehearsalLeft(t, f.w)
}
