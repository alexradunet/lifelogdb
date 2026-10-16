package mcp

import (
	"strings"
	"testing"

	"lifelog/internal/api"
)

func notOwner(a api.Action) bool { return !a.Owner }

func TestToolsRefuseNormalizedDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []api.Action
		want    string
	}{
		{"catalog collision", []api.Action{{Name: "same-name"}, {Name: "same_name"}}, `"same_name"`},
		{"built-in get", []api.Action{{Name: "get"}}, `"get"`},
		{"built-in get day", []api.Action{{Name: "get-day"}}, `"get_day"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Tools(tc.actions, notOwner)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Tools() = %v, want a collision containing %s", err, tc.want)
			}
		})
	}
	tools, err := Tools([]api.Action{{Name: "get", Owner: true}, {Name: "find"}, {Name: "import-find"}}, notOwner)
	if err != nil {
		t.Fatalf("owner-only duplicate and distinct public tools were rejected: %v", err)
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	if got, want := strings.Join(names, " "), "find import_find get_day get"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

func TestToolsRefuseAnInvalidNumericOption(t *testing.T) {
	for _, option := range []string{"oops", "null", "\"1\""} {
		if _, err := inputSchema([]api.Field{{Name: "done", Type: "number", Options: []string{option}}}); err == nil {
			t.Fatalf("accepted %q", option)
		}
	}
}

func TestArgumentsFlattenToFormValues(t *testing.T) {
	got, err := Arguments([]byte(`{"day":"2031-05-14","mood":3,"value":72.50,"note":null}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"day": "2031-05-14", "mood": "3", "value": "72.50"}
	if len(got) != len(want) {
		t.Fatalf("Arguments = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("Arguments[%s] = %q, want %q", k, got[k], v)
		}
	}
	if _, err := Arguments([]byte(`{"list":[1]}`)); err == nil {
		t.Fatal("a list argument was accepted")
	}
	if _, err := Arguments([]byte(`{"day":`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if got, err := Arguments(nil); err != nil || len(got) != 0 {
		t.Fatalf("Arguments(nil) = %v, %v", got, err)
	}
}

// TestToolsCarryNoFileField is the one allowed difference of the MCP surface: a file (a recording, a picture) is
// sent by the CLI or a browser; the tool keeps the action's other fields.
func TestToolsCarryNoFileField(t *testing.T) {
	tools, err := Tools([]api.Action{{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []api.Field{
		{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea"},
		{Name: "audio", Type: "file", Accept: "audio/*"}}}}, notOwner)
	if err != nil {
		t.Fatal(err)
	}
	props, _ := tools[0].Schema["properties"].(map[string]any)
	if _, ok := props["audio"]; ok || props["text"] == nil || props["day"] == nil {
		t.Errorf("the tool's properties: %v", props)
	}
}
