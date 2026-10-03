package core

import (
	"fmt"
	"testing"
)

func TestCategories(t *testing.T) {
	s := fresh(t)
	if added, err := s.RegisterCategory(ctx, "cli", "biomarkers/lipids", "cholesterol and fats"); err != nil || !added {
		t.Fatalf("a path under a seeded category: %v %v", added, err)
	}
	if added, err := s.RegisterCategory(ctx, "cli", "biomarkers/lipids", ""); err != nil || added {
		t.Errorf("a re-sent path registers nothing: %v %v", added, err)
	}
	if _, err := s.RegisterCategory(ctx, "cli", "substances/lipids", ""); status(err) != 409 {
		t.Errorf("a category under another parent: %v", err)
	}
	if _, err := s.RegisterCategory(ctx, "cli", "Biomarkers/x y", ""); status(err) != 422 {
		t.Errorf("a malformed path: %v", err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "ldl_cholesterol", "mg/dL", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FileMetric(ctx, "cli", "ldl_cholesterol", "lipids"); status(err) != 404 {
		t.Errorf("a path that skips the top: %v", err)
	}
	if changed, err := s.FileMetric(ctx, "cli", "ldl_cholesterol", "biomarkers/lipids"); err != nil || !changed {
		t.Fatalf("filing a metric: %v %v", changed, err)
	}
	if changed, err := s.FileMetric(ctx, "cli", "ldl_cholesterol", "biomarkers/lipids"); err != nil || changed {
		t.Errorf("filing it again changes nothing: %v %v", changed, err)
	}
	if _, err := s.FileMetric(ctx, "cli", "nope", "biomarkers"); status(err) != 404 {
		t.Errorf("an unregistered metric: %v", err)
	}
	ms, err := s.Metrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Name] = m.Category
	}
	if got["ldl_cholesterol"] != "biomarkers/lipids" || got["mood"] != "self_report" {
		t.Errorf("metrics carry the path of their category: %v", got)
	}
	if changed, err := s.FileMetric(ctx, "cli", "ldl_cholesterol", ""); err != nil || !changed {
		t.Errorf("un-filing a metric: %v %v", changed, err)
	}
	cs, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range cs {
		paths = append(paths, c.Path)
	}
	if want := "[biomarkers biomarkers/lipids body self_report substances]"; fmt.Sprint(paths) != want {
		t.Errorf("categories by path: %v", paths)
	}
}
