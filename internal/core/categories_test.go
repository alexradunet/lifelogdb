package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestMetricNoteLinks(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Diet Plan", "owner note"); err != nil {
		t.Fatal(err)
	}
	note := "Discuss with [[Bob Sample]], [[Diet Plan]], [[diet plan|again]], #health #Health and [[Health/Diet]]."
	want := "Bob Sample,Diet Plan,health"
	if added, err := s.RegisterMetric(ctx, "cli", "Fresh Link Metric", "count", note); err != nil || !added {
		t.Fatalf("fresh metric with a note: %v %v", added, err)
	}
	fresh := pageByTitle(t, s, "Fresh Link Metric")
	if fresh.Body != note || titles(fresh.Out, "wikilink") != want {
		t.Fatalf("fresh metric links: body %q links %v", fresh.Body, fresh.Out)
	}
	for _, target := range []string{"Bob Sample", "Diet Plan", "health"} {
		assertBacklinks(t, s, target, "Fresh Link Metric")
	}
	if id, err := s.PageID(ctx, "Health/Diet"); err != nil || id != 0 {
		t.Fatalf("invalid target became page %d: %v", id, err)
	}

	ghost, _, err := s.CreatePage(ctx, "cli", "Promoted Link Metric", "")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.RegisterMetric(ctx, "cli", "promoted link metric", "count", note); err != nil || !added {
		t.Fatalf("promoted metric with a note: %v %v", added, err)
	}
	promoted, err := s.PageByID(ctx, ghost)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Type != "metric" || promoted.Body != note || titles(promoted.Out, "wikilink") != want {
		t.Fatalf("promoted metric links: %+v", promoted)
	}
	for _, target := range []string{"Bob Sample", "Diet Plan", "health"} {
		assertBacklinks(t, s, target, "Fresh Link Metric,Promoted Link Metric")
	}

	if added, err := s.RegisterMetric(ctx, "cli", "Fresh Link Metric", "count", "Replacement [[Changed Target]] #other"); err != nil || added {
		t.Fatalf("re-registering a metric with a body: %v %v", added, err)
	}
	fresh = pageByTitle(t, s, "Fresh Link Metric")
	if fresh.Body != note || titles(fresh.Out, "wikilink") != want {
		t.Errorf("re-registration changed the body or links: body %q links %v", fresh.Body, fresh.Out)
	}
	if id, err := s.PageID(ctx, "Changed Target"); err != nil || id != 0 {
		t.Errorf("re-registration linked the replacement note to page %d: %v", id, err)
	}
}

func pageByTitle(t *testing.T, s *Store, title string) *Page {
	t.Helper()
	id, err := s.PageID(ctx, title)
	if err != nil || id == 0 {
		t.Fatalf("page %q: id %d, %v", title, id, err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertBacklinks(t *testing.T, s *Store, title, want string) {
	t.Helper()
	p := pageByTitle(t, s, title)
	if got := titles(p.In, "wikilink"); got != want {
		t.Fatalf("%s backlinks = %q, want %q (all backlinks: %s)", title, got, want, strings.Join(edgeTitles(p.In), ","))
	}
}

func edgeTitles(es []Edge) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Title)
	}
	return out
}

func TestMetricsArePagesFiledInCategories(t *testing.T) {
	s := fresh(t)
	file := func(metric, path string) (added bool, err error) {
		err = s.Do(ctx, "cli", func(tx *Tx) error {
			id, err := tx.metricID(metric)
			if err != nil {
				return err
			}
			added, err = tx.File(id, path)
			return err
		})
		return
	}
	if added, err := s.RegisterMetric(ctx, "cli", "LDL cholesterol", "mg/dL", "low-density lipoprotein"); err != nil || !added {
		t.Fatalf("a metric with a title for a name: %v %v", added, err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "ldl CHOLESTEROL", "mmol/L", ""); status(err) != 409 {
		t.Errorf("the same name in another case is the same metric, and its unit never changes: %v", err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "a/b", "", ""); status(err) != 422 {
		t.Errorf("a name that is not a valid title: %v", err)
	}
	ghost, _, err := s.CreatePage(ctx, "cli", "Ferritin", "")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.RegisterMetric(ctx, "cli", "ferritin", "ng/mL", "iron stores"); err != nil || !added {
		t.Fatalf("an empty page of that title becomes the metric: %v %v", added, err)
	}
	if p, _ := s.PageByID(ctx, ghost); p.Type != "metric" || p.Body != "iron stores" {
		t.Errorf("the promoted page: %+v", p)
	}
	written, _, err := s.CreatePage(ctx, "cli", "aPTT", "Clotting time; mine runs long.")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.RegisterMetric(ctx, "cli", "aptt", "seconds", "aPTT"); err != nil || !added {
		t.Fatalf("a page the owner wrote about it becomes the metric: %v %v", added, err)
	}
	if p, _ := s.PageByID(ctx, written); p.Type != "metric" || p.Title != "aPTT" || p.Body != "Clotting time; mine runs long." {
		t.Errorf("the owner's text is kept, the note is not written over it: %+v", p)
	}
	if added, err := s.RegisterMetric(ctx, "cli", "aptt", "seconds", "aPTT"); err != nil || added {
		t.Errorf("registering it again is a no-op: %v %v", added, err)
	}
	if added, err := file("LDL cholesterol", "Biomarkers/Lipids"); err != nil || !added {
		t.Fatalf("filing makes the category pages and their part-of links: %v %v", added, err)
	}
	if added, err := file("ldl cholesterol", "Biomarkers/Lipids"); err != nil || added {
		t.Errorf("filing it again adds nothing: %v %v", added, err)
	}
	if _, err := file("Ferritin", "Biomarkers/Iron"); err != nil {
		t.Fatal(err)
	}
	if _, err := file("Ferritin", "Supplements"); err != nil {
		t.Errorf("a metric in a second category: %v", err)
	}
	if _, err := file("Ferritin", "2026-10-03"); status(err) != 409 {
		t.Errorf("a day page is not a category: %v", err)
	}
	ms, err := s.Metrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Name] = fmt.Sprint(m.Categories)
	}
	if got["LDL cholesterol"] != "[Biomarkers/Lipids]" || got["Ferritin"] != "[Biomarkers/Iron Supplements]" || got["Mood"] != "[]" {
		t.Errorf("metrics carry the paths of their categories: %v", got)
	}
	cs, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range cs {
		paths = append(paths, c.Path)
	}
	if want := "[Biomarkers Biomarkers/Iron Biomarkers/Lipids Supplements]"; fmt.Sprint(paths) != want {
		t.Errorf("categories by path: %v", paths)
	}
}
