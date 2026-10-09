package importer

import (
	"fmt"
	"testing"

	"lifelog/internal/core"
)

func TestImporterDiscoveryIncludesOwnedNames(t *testing.T) {
	f := setup(t)
	person, err := f.s.CreatePerson(ctx, "cli", "Old person", "Synthetic Full Name", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Rename(ctx, "cli", person, "Current person"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"Old person", "Old", "Synthetic Full Name", "Current person"} {
		matches, err := Find(ctx, f.s, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 || matches[0].Title != "Current person" || matches[0].Href != fmt.Sprintf("/pages/%d", person) {
			t.Fatalf("discovery %q: %+v", query, matches)
		}
	}
	var like []string
	if err := f.s.DryRun(ctx, "import:notebook", func(tx *core.Tx) (err error) { like, err = lookAlikes(tx, "Old"); return err }); err != nil {
		t.Fatal(err)
	}
	if len(like) != 1 || like[0] != `the person "Current person" (more words)` {
		t.Fatalf("the look-alikes of a name ignore a retained alias: %v", like)
	}
	// an approved decision in entities.md writes the name: the owner saw what it looks like
	rules := &Rules{Names: names{{"person", nameKey("Old")}: {Status: "approved", Kind: "person", Name: "Old"}}}
	if err := decide(rules, "person", "Old", "", like); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterMetric(ctx, "cli", "body_weight", "kg", "Synthetic weight"); err != nil {
		t.Fatal(err)
	}
	metric, err := f.s.PageID(ctx, "body_weight")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Rename(ctx, "cli", metric, "Weight record"); err != nil {
		t.Fatal(err)
	}
	matches, err := Find(ctx, f.s, "body weight")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Kind != "metric" || matches[0].Title != "Weight record" || matches[0].Href != fmt.Sprintf("/pages/%d", metric) {
		t.Fatalf("metric aliases duplicated/lost: %+v", matches)
	}
	if err := f.s.Tombstone(ctx, "cli", person); err != nil {
		t.Fatal(err)
	}
	matches, err = Find(ctx, f.s, "Old person")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatal("discovery exposed tombstoned identity")
	}
	if resolved, err := f.s.PageID(ctx, "Old person"); err != nil || resolved != person {
		t.Fatal("tombstone released reserved alias")
	}
}
