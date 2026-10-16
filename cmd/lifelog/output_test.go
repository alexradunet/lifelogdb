package main

import (
	"bytes"
	"testing"

	"lifelog/internal/api"
)

// TestHumanWritesAReadableSummary: --human writes the title and the classes, the result of a write, the properties in
// the order of their names (a text of more lines indented below its name, an empty text left out, any other value as
// JSON), then the embedded links, the links and the actions. The expected texts are written by hand.
func TestHumanWritesAReadableSummary(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    *api.Entity
		want string
	}{
		{"a title only", &api.Entity{Title: "Ana"}, "# Ana  ()\n"},
		{"properties that are not an object", &api.Entity{Title: "Ana", Class: []string{"page"}, Properties: []int{1}},
			"# Ana  (page)\n"},
		{"every part", &api.Entity{
			Class:  []string{"page", "file"},
			Title:  "Lakeside",
			Result: map[string]any{"linked": []string{"Sam"}},
			Properties: map[string]any{
				"title": "Lakeside", "body": "Walked with [[Sam]].\nCold water.", "kind": "", "id": 12, "tags": []string{"walk"},
			},
			Entities: []api.Link{{Rel: []string{"backlink"}, Title: "2031-05-14", Href: "/pages/3"}},
			Links:    []api.Link{{Rel: []string{"self"}, Href: "/pages/12"}, {Rel: []string{"collection", "up"}, Href: "/pages"}},
			Actions:  []api.Action{{Name: "save-body", Method: "POST", Href: "/pages/12/body"}},
		}, `# Lakeside  (page file)
result: {"linked":["Sam"]}
body:
    Walked with [[Sam]].
    Cold water.
id: 12
tags: ["walk"]
title: Lakeside
  → backlink     2031-05-14                   /pages/3
  ↗ self         /pages/12
  ↗ collection up /pages
  ! save-body      POST /pages/12/body
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			human(&b, tc.e)
			if b.String() != tc.want {
				t.Errorf("human:\n%s\nwant:\n%s", b.String(), tc.want)
			}
		})
	}
}
