package importer

import (
	"fmt"
	"testing"
)

func TestImporterAliasContinuity(t *testing.T) {
	t.Run("find", func(t *testing.T) {
		f := setup(t)
		id, _, err := f.s.CreatePage(ctx, "cli", "Orion", "synthetic text")
		if err != nil {
			t.Fatal(err)
		}
		before, err := Find(ctx, f.s, "Orion")
		if err != nil || len(before) != 1 {
			t.Fatalf("setup find=%+v err=%v", before, err)
		}
		if got, err := f.s.Rename(ctx, "cli", id, "Cedar"); err != nil || got != id {
			t.Fatalf("rename=%d err=%v", got, err)
		}
		if got, err := f.s.PageID(ctx, "Orion"); err != nil || got != id {
			t.Fatalf("alias lookup=%d err=%v", got, err)
		}
		matches, err := Find(ctx, f.s, "Orion")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 || matches[0].Href != fmt.Sprintf("/pages/%d", id) || matches[0].Title != "Cedar" {
			t.Fatalf("old-alias discovery=%+v; want one canonical Cedar result for id %d", matches, id)
		}
	})
	t.Run("reading-reapply", func(t *testing.T) {
		f := setupReadingKeyFixture(t)
		file := "Medical/Renameproof.md"
		writeSource(t, f, file, "2031-07-01 timed: 48 ng/mL\n")
		mustLedger(t, f)
		registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "Synthetic ferritin"})
		applyFacts(t, f, file, []any{readingFact("ferritin", "2031-07-01", "48 ng/mL", "2031-07-01 timed: 48 ng/mL", map[string]string{"taken_at": "2031-07-01T08:00:00.000Z"})})
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatalf("unchanged replay control: %v", err)
		}
		metric, err := f.s.PageID(ctx, "ferritin")
		if err != nil || metric == 0 {
			t.Fatalf("metric=%d err=%v", metric, err)
		}
		type root struct {
			id, metric  int64
			source, key string
			value       float64
		}
		load := func() root {
			t.Helper()
			var r root
			if err := f.s.DB.R.QueryRow(`SELECT id,metric_id,source,import_key,value FROM measurements WHERE metric_id=?`, metric).Scan(&r.id, &r.metric, &r.source, &r.key, &r.value); err != nil {
				t.Fatal(err)
			}
			return r
		}
		before := load()
		if got, err := f.s.Rename(ctx, "cli", metric, "Iron stores"); err != nil || got != metric {
			t.Fatalf("metric rename=%d err=%v", got, err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Errorf("unchanged source reapply after metric rename: %v", err)
		}
		if got := load(); got != before {
			t.Errorf("reading changed: got %+v want %+v", got, before)
		}
		if got := importerMeasurementRows(t, f); got != 1 {
			t.Errorf("reading count=%d want 1", got)
		}
	})
	t.Run("vault-reapply", func(t *testing.T) {
		f := setup(t)
		f.approveRules(t, rulesBody)
		if _, err := f.w.PlanVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", "Recipes", "2031-04-10"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
			t.Fatalf("unchanged vault replay control: %v", err)
		}
		id, err := f.s.PageID(ctx, "Recipes")
		if err != nil || id == 0 {
			t.Fatalf("imported note=%d err=%v", id, err)
		}
		before, err := f.s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, title := range []string{"Cookery", "COOKERY", "Recipes", "Cookery"} {
			if got, err := f.s.Rename(ctx, "cli", id, title); err != nil || got != id {
				t.Fatalf("note rename=%d err=%v", got, err)
			}
			if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
				t.Fatalf("unchanged plan after rename: %v", err)
			}
		}
		for _, title := range []string{"recipes", "Cookery", "COOKERY"} {
			if _, err := f.w.FixPlan(ctx, f.s, "Recipes.md", title, "2031-04-10"); err == nil {
				t.Fatalf("accepted applied plan edit to owned name %q", title)
			}
		}
		after, err := f.s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if after.Title != "Cookery" || after.Body != before.Body || after.Day != before.Day {
			t.Errorf("reapply altered renamed imported note: %+v", after)
		}
	})
}
