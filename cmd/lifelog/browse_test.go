package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestDoBrowse: the CLI's do browse is the catalog action (docs/plans/117-one-browse-view.md), with its fields and its
// refusals.
func TestDoBrowse(t *testing.T) {
	c := commandClient(t) // the plain page Ana
	ctx := t.Context()
	for _, title := range []string{"Bea", "Cal"} {
		if _, err := do(ctx, c, "create-page", map[string]string{"title": title, "category": "Notes"}); err != nil {
			t.Fatal(err)
		}
	}
	rows := func(vals map[string]string) string {
		t.Helper()
		e, err := do(ctx, c, "browse", vals)
		if err != nil {
			t.Fatalf("do browse %v: %v", vals, err)
		}
		var out []string
		for _, r := range e.Properties.(map[string]any)["rows"].([]any) {
			out = append(out, fmt.Sprint(r.(map[string]any)["title"]))
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		vals map[string]string
		want string
	}{
		{map[string]string{}, "Ana,Bea,Cal,Notes"},
		{map[string]string{"in": "Notes"}, "Bea,Cal"},
		{map[string]string{"unfiled": "1"}, "Ana"},
		{map[string]string{"limit": "1", "offset": "1"}, "Bea"},
	} {
		if got := rows(tc.vals); got != tc.want {
			t.Errorf("do browse %v: %s, want %s", tc.vals, got, tc.want)
		}
	}
	if _, err := do(ctx, c, "browse", map[string]string{"kind": "all"}); err == nil || !strings.Contains(err.Error(), "422") {
		t.Errorf("do browse kind=all: %v, want a 422", err)
	}
}
